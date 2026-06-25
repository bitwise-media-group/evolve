// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package web

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBrokerPublish(t *testing.T) {
	b := newBroker()
	ch := b.subscribe()
	if b.count() != 1 {
		t.Fatalf("count = %d, want 1", b.count())
	}
	b.publish()
	select {
	case got := <-ch:
		if got != "results-changed" {
			t.Errorf("got %q, want results-changed", got)
		}
	case <-time.After(time.Second):
		t.Fatal("no event delivered")
	}
	b.unsubscribe(ch)
	if b.count() != 0 {
		t.Errorf("count after unsubscribe = %d, want 0", b.count())
	}
	if _, open := <-ch; open {
		t.Error("channel should be closed after unsubscribe")
	}
}

func TestBrokerPublishDoesNotBlock(t *testing.T) {
	b := newBroker()
	ch := b.subscribe()
	// Overfill the buffer (cap 8) without draining; publish must not block.
	done := make(chan struct{})
	go func() {
		for range 100 {
			b.publish()
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("publish blocked on a full subscriber buffer")
	}
	b.unsubscribe(ch)
}

func TestHandleEventsStream(t *testing.T) {
	repo, _ := fixtureRepo(t)
	s := NewServer(repo, "v1", nil)
	srv := httptest.NewServer(s.Handler())
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /events: %v", err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}

	// Once the handler has subscribed, publish and expect the named event.
	waitFor(t, time.Second, "client subscription", func() bool { return s.broker.count() == 1 })
	s.broker.publish()

	lines := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			if strings.HasPrefix(sc.Text(), "event: results-changed") {
				lines <- sc.Text()
				return
			}
		}
	}()
	select {
	case <-lines:
	case <-ctx.Done():
		t.Fatal("did not receive results-changed event")
	}
}
