// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package agui

import (
	"errors"
	"io"
	"strings"
	"testing"
)

// readAll drains an SSE body, returning each event's data and whether the
// last one arrived unterminated.
func readAll(t *testing.T, body string) (events []string, trailing bool) {
	t.Helper()
	r := newSSEReader(strings.NewReader(body))
	for {
		data, err := r.Next()
		if errors.Is(err, io.EOF) {
			return events, trailing
		}
		if errors.Is(err, errTrailingEvent) {
			trailing = true
		} else if err != nil {
			t.Fatalf("Next: %v", err)
		}
		events = append(events, string(data))
	}
}

func TestSSEReader(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		want     []string
		trailing bool
	}{
		{"space after colon", "data: {\"a\":1}\n\n", []string{`{"a":1}`}, false},
		{"no space after colon", "data:{\"a\":1}\n\n", []string{`{"a":1}`}, false},
		{"only one space stripped", "data:  x\n\n", []string{" x"}, false},
		{"multi-line data joins with LF", "data: a\ndata: b\n\n", []string{"a\nb"}, false},
		{"comments ignored", ": keep-alive\ndata: x\n: more\n\n", []string{"x"}, false},
		{"event id retry ignored", "event: message\nid: 7\nretry: 10\ndata: x\n\n", []string{"x"}, false},
		{"CRLF line endings", "data: a\r\n\r\ndata: b\r\n\r\n", []string{"a", "b"}, false},
		{"CR line endings", "data: a\r\rdata: b\r\r", []string{"a", "b"}, false},
		{"blank lines without data dispatch nothing", "\n\n\ndata: x\n\n\n\n", []string{"x"}, false},
		{"field without colon", "data\n\n", []string{""}, false},
		{"empty data line still dispatches", "data:\n\n", []string{""}, false},
		{"final event without blank line", "data: a\n\ndata: b\n", []string{"a", "b"}, true},
		{"final event without any newline", "data: a", []string{"a"}, true},
		{"only comments", ": hi\n\n", nil, false},
		{"empty body", "", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, trailing := readAll(t, tt.body)
			if strings.Join(got, "|") != strings.Join(tt.want, "|") || len(got) != len(tt.want) {
				t.Errorf("events = %q, want %q", got, tt.want)
			}
			if trailing != tt.trailing {
				t.Errorf("trailing = %v, want %v", trailing, tt.trailing)
			}
		})
	}
}

// FuzzSSEReader feeds arbitrary bytes through the reader: it must never
// panic or loop, and every event it yields is bounded by the input.
func FuzzSSEReader(f *testing.F) {
	for _, seed := range []string{
		"data: {\"type\":\"RUN_STARTED\"}\n\n",
		"data:a\r\ndata:b\r\n\r\n",
		": c\rdata: x\r\r",
		"data: tail",
		"\n\n\n",
		"data\n\nevent: x\n",
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, body []byte) {
		r := newSSEReader(strings.NewReader(string(body)))
		for range len(body) + 2 {
			data, err := r.Next()
			if len(data) > len(body) {
				t.Fatalf("event of %d bytes from %d bytes of input", len(data), len(body))
			}
			if err != nil && !errors.Is(err, errTrailingEvent) {
				return
			}
		}
		t.Fatalf("reader yielded more events than input bytes")
	})
}
