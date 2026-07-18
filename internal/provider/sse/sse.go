// Package sse is a minimal Server-Sent Events line reader: it frames
// "data:" lines into one payload per event. It knows nothing about what
// shape that payload holds (JSON or otherwise) -- decoding it is each
// provider's own job.
package sse

import (
	"bufio"
	"io"
	"strings"
)

// Reader frames one SSE event's concatenated "data:" lines at a time from
// an underlying stream.
type Reader struct {
	scan *bufio.Scanner
}

// NewReader wraps body (typically an HTTP response body) as a Reader.
// maxLineBytes bounds the largest single buffered line -- a large
// tool-call argument or text delta can exceed bufio.Scanner's default
// 64KiB limit.
func NewReader(body io.Reader, maxLineBytes int) *Reader {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	return &Reader{scan: scanner}
}

// Next returns one event's concatenated "data:" payload, or ok=false once
// the underlying reader is exhausted with no further data.
func (r *Reader) Next() (data string, ok bool) {
	var b strings.Builder
	found := false
	for r.scan.Scan() {
		line := r.scan.Text()
		if line == "" {
			if found {
				return b.String(), true
			}
			continue
		}
		if rest, ok := strings.CutPrefix(line, "data:"); ok {
			found = true
			b.WriteString(strings.TrimPrefix(rest, " "))
		}
	}
	if found {
		return b.String(), true
	}
	return "", false
}
