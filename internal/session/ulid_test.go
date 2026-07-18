package session

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// decodeULID reverses encodeULID, independently re-derived from the same
// bit layout, so a round-trip test actually exercises the arithmetic rather
// than just calling encode's own inverse of itself.
func decodeULID(t *testing.T, s string) [16]byte {
	t.Helper()
	if len(s) != 26 {
		t.Fatalf("decodeULID(%q): length = %d, want 26", s, len(s))
	}

	var v [26]byte
	for i, r := range s {
		idx := strings.IndexRune(crockford, r)
		if idx < 0 {
			t.Fatalf("decodeULID(%q): invalid character %q", s, r)
		}
		v[i] = byte(idx)
	}

	var data [16]byte
	data[0] = (v[0] << 5) | v[1]
	data[1] = (v[2] << 3) | (v[3] >> 2)
	data[2] = ((v[3] & 3) << 6) | (v[4] << 1) | (v[5] >> 4)
	data[3] = ((v[5] & 15) << 4) | (v[6] >> 1)
	data[4] = ((v[6] & 1) << 7) | (v[7] << 2) | (v[8] >> 3)
	data[5] = ((v[8] & 7) << 5) | v[9]

	data[6] = (v[10] << 3) | (v[11] >> 2)
	data[7] = ((v[11] & 3) << 6) | (v[12] << 1) | (v[13] >> 4)
	data[8] = ((v[13] & 15) << 4) | (v[14] >> 1)
	data[9] = ((v[14] & 1) << 7) | (v[15] << 2) | (v[16] >> 3)
	data[10] = ((v[16] & 7) << 5) | v[17]

	data[11] = (v[18] << 3) | (v[19] >> 2)
	data[12] = ((v[19] & 3) << 6) | (v[20] << 1) | (v[21] >> 4)
	data[13] = ((v[21] & 15) << 4) | (v[22] >> 1)
	data[14] = ((v[22] & 1) << 7) | (v[23] << 2) | (v[24] >> 3)
	data[15] = ((v[24] & 7) << 5) | v[25]

	return data
}

func TestULID_RoundTrip(t *testing.T) {
	cases := [][16]byte{
		{},
		{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
		{0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF, 0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF},
		{0xAA, 0x55, 0xAA, 0x55, 0xAA, 0x55, 0xAA, 0x55, 0xAA, 0x55, 0xAA, 0x55, 0xAA, 0x55, 0xAA, 0x55},
	}

	for _, data := range cases {
		encoded := encodeULID(data)
		got := decodeULID(t, encoded)
		if got != data {
			t.Errorf("round trip: encodeULID(%v) = %q, decodeULID(...) = %v, want %v", data, encoded, got, data)
		}
	}
}

func TestULID_Format(t *testing.T) {
	id := New()
	if len(id) != 26 {
		t.Fatalf("len(New()) = %d, want 26", len(id))
	}
	for _, r := range id {
		if !strings.ContainsRune(crockford, r) {
			t.Errorf("New() = %q contains character %q outside the Crockford alphabet", id, r)
		}
	}
}

func TestULID_Unique(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		id := New()
		if seen[id] {
			t.Fatalf("duplicate ULID generated: %s", id)
		}
		seen[id] = true
	}
}

func TestULID_LexicallySortableByTime(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	var prev string
	for i := 0; i < 5; i++ {
		id := newULID(base.Add(time.Duration(i) * time.Second))
		if i > 0 && id <= prev {
			t.Fatalf("ULID at t+%ds (%s) does not sort after previous (%s)", i, id, prev)
		}
		prev = id
	}
}

func TestULID_TimestampRoundTrip(t *testing.T) {
	tm := time.Date(2026, 7, 18, 12, 30, 45, 123_000_000, time.UTC)
	id := newULID(tm)

	data := decodeULID(t, id)
	gotMS := uint64(data[0])<<40 | uint64(data[1])<<32 | uint64(data[2])<<24 |
		uint64(data[3])<<16 | uint64(data[4])<<8 | uint64(data[5])

	wantMS := uint64(tm.UnixMilli())
	if gotMS != wantMS {
		t.Errorf("decoded timestamp = %d, want %d", gotMS, wantMS)
	}
}

func ExampleNew() {
	id := New()
	_, _ = fmt.Println(len(id))
	// Output: 26
}
