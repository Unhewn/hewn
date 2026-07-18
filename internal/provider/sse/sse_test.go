package sse

import (
	"strings"
	"testing"
)

func TestReader_SingleLineEvents(t *testing.T) {
	raw := "data: {\"a\":1}\n\ndata: {\"a\":2}\n\n"
	r := NewReader(strings.NewReader(raw), 1024)

	var got []string
	for {
		data, ok := r.Next()
		if !ok {
			break
		}
		got = append(got, data)
	}

	want := []string{`{"a":1}`, `{"a":2}`}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("event %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestReader_IgnoresNonDataLines(t *testing.T) {
	raw := "event: message\nid: 1\ndata: payload\n\n"
	r := NewReader(strings.NewReader(raw), 1024)

	data, ok := r.Next()
	if !ok {
		t.Fatal("Next() ok = false, want true")
	}
	if data != "payload" {
		t.Errorf("data = %q, want %q", data, "payload")
	}
}

func TestReader_MultipleDataLinesConcatenate(t *testing.T) {
	// Per the SSE spec, multiple "data:" lines within one event concatenate
	// (typically joined by newlines upstream; this reader just appends the
	// stripped content of each in order, which is what both providers'
	// single-data-line-per-event streams actually need).
	raw := "data: hello\ndata: world\n\n"
	r := NewReader(strings.NewReader(raw), 1024)

	data, ok := r.Next()
	if !ok {
		t.Fatal("Next() ok = false, want true")
	}
	if data != "helloworld" {
		t.Errorf("data = %q, want %q", data, "helloworld")
	}
}

func TestReader_NoTrailingBlankLineStillYieldsFinalEvent(t *testing.T) {
	raw := "data: last"
	r := NewReader(strings.NewReader(raw), 1024)

	data, ok := r.Next()
	if !ok {
		t.Fatal("Next() ok = false, want true (EOF should still flush a pending event)")
	}
	if data != "last" {
		t.Errorf("data = %q, want %q", data, "last")
	}

	_, ok = r.Next()
	if ok {
		t.Error("second Next() ok = true, want false (nothing left)")
	}
}

func TestReader_EmptyInputYieldsNothing(t *testing.T) {
	r := NewReader(strings.NewReader(""), 1024)
	_, ok := r.Next()
	if ok {
		t.Error("Next() on empty input ok = true, want false")
	}
}
