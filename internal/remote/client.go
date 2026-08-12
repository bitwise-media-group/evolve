// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package remote

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client talks to a patchy remote-evaluation service, attaching the stored
// bearer credential (refreshed as needed) to every request.
type Client struct {
	// BaseURL of the service.
	BaseURL string
	// Store holds the login credential; Info the service's auth discovery.
	Store *Store
	Info  *AuthInfo
	// HTTPClient defaults to http.DefaultClient.
	HTTPClient *http.Client
}

// NewClient discovers the remote's auth mode and binds the credential store.
func NewClient(ctx context.Context, store *Store, baseURL string) (*Client, error) {
	info, err := FetchAuthInfo(ctx, baseURL)
	if err != nil {
		return nil, err
	}
	return &Client{BaseURL: NormalizeRemote(baseURL), Store: store, Info: info}, nil
}

func (c *Client) http() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

// do sends the request with the bearer attached.
func (c *Client) do(req *http.Request) (*http.Response, error) {
	bearer, err := Bearer(req.Context(), c.Store, c.BaseURL, c.Info)
	if err != nil {
		return nil, err
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	return c.http().Do(req)
}

// apiError renders a non-2xx response, surfacing the server's message.
func apiError(op string, resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	var serr SubmissionError
	if json.Unmarshal(raw, &serr) == nil && serr.Error != "" {
		return fmt.Errorf("remote: %s: %s (status %d)", op, serr.Error, resp.StatusCode)
	}
	return fmt.Errorf("remote: %s: unexpected status %d", op, resp.StatusCode)
}

// HasWorkspace reports whether the digest is already cached server-side.
func (c *Client) HasWorkspace(ctx context.Context, digest string) (bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead,
		c.BaseURL+"/api/v1/workspaces/"+digest, nil)
	if err != nil {
		return false, err
	}
	resp, err := c.do(req)
	if err != nil {
		return false, fmt.Errorf("remote: workspace probe: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusNoContent, http.StatusOK:
		return true, nil
	case http.StatusNotFound:
		return false, nil
	default:
		return false, apiError("workspace probe", resp)
	}
}

// UploadWorkspace uploads a bundle under its digest.
func (c *Client) UploadWorkspace(ctx context.Context, digest string, bundle []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut,
		c.BaseURL+"/api/v1/workspaces/"+digest, bytes.NewReader(bundle))
	if err != nil {
		return err
	}
	req.ContentLength = int64(len(bundle))
	resp, err := c.do(req)
	if err != nil {
		return fmt.Errorf("remote: workspace upload: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return apiError("workspace upload", resp)
	}
	return nil
}

// Submit posts the submission and returns the evaluation to monitor.
func (c *Client) Submit(ctx context.Context, sub *Submission) (*SubmissionResponse, error) {
	raw, err := json.Marshal(sub)
	if err != nil {
		return nil, fmt.Errorf("remote: marshal submission: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.BaseURL+"/api/v1/evaluations", bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.do(req)
	if err != nil {
		return nil, fmt.Errorf("remote: submit: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated {
		return nil, apiError("submit", resp)
	}
	var out SubmissionResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("remote: submit response: %w", err)
	}
	return &out, nil
}

// Status fetches the evaluation's snapshot.
func (c *Client) Status(ctx context.Context, name string) (*EvaluationStatusWire, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.BaseURL+"/api/v1/evaluations/"+name, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.do(req)
	if err != nil {
		return nil, fmt.Errorf("remote: status: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, apiError("status", resp)
	}
	var out EvaluationStatusWire
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("remote: status: %w", err)
	}
	return &out, nil
}

// Cancel deletes the evaluation server-side.
func (c *Client) Cancel(ctx context.Context, name string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		c.BaseURL+"/api/v1/evaluations/"+name, nil)
	if err != nil {
		return err
	}
	resp, err := c.do(req)
	if err != nil {
		return fmt.Errorf("remote: cancel: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusNotFound {
		return apiError("cancel", resp)
	}
	return nil
}

// Events follows the evaluation's SSE monitor until the end event: onUnit
// for every unit state, onEvent for relayed in-pod progress (best-effort —
// the server may not send any). A dropped stream reconnects with backoff,
// re-fetching the snapshot to reconcile; the final snapshot is returned.
func (c *Client) Events(ctx context.Context, name string,
	onUnit func(UnitStatusWire), onEvent func(EventWire)) (*EvaluationStatusWire, error) {
	backoff := time.Second
	for {
		final, err := c.streamOnce(ctx, name, onUnit, onEvent)
		if err == nil {
			return final, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// Reconcile before retrying: the evaluation may have settled (or
		// vanished) while disconnected.
		snap, serr := c.Status(ctx, name)
		if serr != nil {
			return nil, errors.Join(err, serr)
		}
		for _, u := range snap.Units {
			onUnit(u)
		}
		if snap.Phase == "Complete" || snap.Phase == "Failed" {
			return snap, nil
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 30*time.Second)
	}
}

// streamOnce consumes one SSE connection until end (nil error) or a drop.
func (c *Client) streamOnce(ctx context.Context, name string,
	onUnit func(UnitStatusWire), onEvent func(EventWire)) (*EvaluationStatusWire, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.BaseURL+"/api/v1/evaluations/"+name+"/events", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "text/event-stream")
	resp, err := c.do(req)
	if err != nil {
		return nil, fmt.Errorf("remote: events: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, apiError("events", resp)
	}

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64<<10), 8<<20)
	var event string
	var data bytes.Buffer
	dispatch := func() (*EvaluationStatusWire, error) {
		defer func() { event = ""; data.Reset() }()
		switch event {
		case SSEEventUnit:
			var u UnitStatusWire
			if json.Unmarshal(data.Bytes(), &u) == nil {
				onUnit(u)
			}
		case SSEEventEvent:
			var ev EventWire
			if json.Unmarshal(data.Bytes(), &ev) == nil && onEvent != nil {
				onEvent(ev)
			}
		case SSEEventEnd:
			var final EvaluationStatusWire
			if err := json.Unmarshal(data.Bytes(), &final); err != nil {
				return nil, fmt.Errorf("remote: end event: %w", err)
			}
			return &final, nil
		}
		return nil, nil
	}
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "":
			if final, err := dispatch(); final != nil || err != nil {
				return final, err
			}
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			data.WriteString(strings.TrimPrefix(line, "data: "))
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("remote: events stream: %w", err)
	}
	return nil, errors.New("remote: events stream ended without an end event")
}
