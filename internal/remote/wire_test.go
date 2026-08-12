// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package remote

import "testing"

func TestTruncate(t *testing.T) {
	tests := []struct {
		name string
		s    string
		max  int
		want string
	}{
		{"under", "short", 10, "short"},
		{"exact", "exact", 5, "exact"},
		{"ascii clip", "abcdefghij", 5, "ab…"},
		{"multibyte on boundary", "ééé", 5, "é…"},
		{"multibyte backs off mid-rune", "ééé", 4, "…"},
		{"tighter than the ellipsis", "abcdef", 2, "…"},
		{"zero", "abcdef", 0, "…"},
		{"empty", "", 5, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Truncate(tt.s, tt.max)
			if got != tt.want {
				t.Errorf("Truncate(%q, %d) = %q, want %q", tt.s, tt.max, got, tt.want)
			}
		})
	}
}

func TestDecode(t *testing.T) {
	valid := `EVOLVE-EVENT: {"v":1,"type":"warn","msg":"hi"}`
	tests := []struct {
		name string
		line string
		ok   bool
	}{
		{"plain", valid, true},
		{"padded", "  " + valid + "  ", true},
		{"kubernetes timestamp prefix", "2026-08-12T10:00:00.000000000Z stdout F " + valid, true},
		{"no prefix", `{"v":1,"type":"warn"}`, false},
		{"garbage payload", "EVOLVE-EVENT: not-json", false},
		{"wrong version", `EVOLVE-EVENT: {"v":2,"type":"warn"}`, false},
		{"missing type", `EVOLVE-EVENT: {"v":1}`, false},
		{"plain log noise", "starting unit workflow-commit", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev, ok := Decode([]byte(tt.line))
			if ok != tt.ok {
				t.Fatalf("Decode(%q) ok = %v, want %v", tt.line, ok, tt.ok)
			}
			if ok && (ev.Type != TypeWarn || ev.Msg != "hi") {
				t.Errorf("Decode(%q) = %+v, want warn/hi", tt.line, ev)
			}
		})
	}
}

func TestEncodeStampsVersion(t *testing.T) {
	line, err := Event{Type: TypeWarn, Msg: "hi"}.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	ev, ok := Decode([]byte(line))
	if !ok {
		t.Fatalf("Encode produced an undecodable line: %q", line)
	}
	if ev.V != EventVersion || ev.Msg != "hi" {
		t.Errorf("round trip = %+v, want v=%d msg=hi", ev, EventVersion)
	}
}
