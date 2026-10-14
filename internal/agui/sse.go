// Copyright 2026 BitWise Media Group Ltd
// SPDX-License-Identifier: MIT

package agui

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
)

// maxEventBytes bounds one SSE event's data so a misbehaving agent cannot
// exhaust memory with an unterminated line.
const maxEventBytes = 32 << 20

// errTrailingEvent marks an event dispatched at EOF without its terminating
// blank line. The event is still delivered (AG-UI producers close the body
// right after the last terminal event, and a lost RUN_FINISHED would read as
// truncation); the caller records a warning.
var errTrailingEvent = errors.New("final event not terminated by a blank line")

// sseReader decodes a text/event-stream body into its events' data payloads,
// per the WHATWG event-stream grammar: lines end in LF, CR, or CRLF; a line
// starting with ':' is a comment; "data:" with or without the one optional
// space; multiple data lines join with LF; event/id/retry are ignored; a
// blank line dispatches. Only data matters to AG-UI — each event's data is
// one JSON object.
type sseReader struct {
	r      *bufio.Reader
	skipLF bool // the previous line ended in CR: drop a following LF
	eof    bool
}

func newSSEReader(r io.Reader) *sseReader {
	return &sseReader{r: bufio.NewReaderSize(r, 64<<10)}
}

// Next returns the next event's data. It returns io.EOF at a clean end of
// stream, errTrailingEvent alongside the data of a final event the stream
// ended without terminating, and any other error from the underlying reader.
func (s *sseReader) Next() ([]byte, error) {
	var data bytes.Buffer
	hasData := false
	for {
		line, err := s.line()
		if err != nil {
			if errors.Is(err, io.EOF) && hasData {
				return data.Bytes(), errTrailingEvent
			}
			return nil, err
		}
		if len(line) == 0 {
			if hasData {
				return data.Bytes(), nil
			}
			continue
		}
		if line[0] == ':' {
			continue
		}
		field, value, found := bytes.Cut(line, []byte{':'})
		if !found {
			value = nil
		} else if len(value) > 0 && value[0] == ' ' {
			value = value[1:]
		}
		if string(field) != "data" {
			continue
		}
		if hasData {
			data.WriteByte('\n')
		}
		hasData = true
		data.Write(value)
		if data.Len() > maxEventBytes {
			return nil, fmt.Errorf("SSE event exceeds %d bytes", maxEventBytes)
		}
	}
}

// line reads one line without its terminator. A final line the stream ends
// without terminating is returned whole; io.EOF follows it.
func (s *sseReader) line() ([]byte, error) {
	if s.eof {
		return nil, io.EOF
	}
	var buf []byte
	for {
		b, err := s.r.ReadByte()
		if err != nil {
			if errors.Is(err, io.EOF) {
				s.eof = true
				if len(buf) > 0 {
					return buf, nil
				}
			}
			return nil, err
		}
		if s.skipLF {
			s.skipLF = false
			if b == '\n' {
				continue
			}
		}
		switch b {
		case '\n':
			return buf, nil
		case '\r':
			s.skipLF = true
			return buf, nil
		}
		buf = append(buf, b)
		if len(buf) > maxEventBytes {
			return nil, fmt.Errorf("SSE line exceeds %d bytes", maxEventBytes)
		}
	}
}
