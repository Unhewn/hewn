package session

import (
	"crypto/rand"
	"time"
)

// crockford is the 32-character alphabet ULIDs are base32-encoded with:
// Crockford's variant, which drops I, L, O, and U to avoid visual
// ambiguity and accidental profanity.
const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// New returns a new ULID: a 26-character, lexically time-sortable ID built
// from a 48-bit millisecond timestamp followed by 80 bits of crypto/rand
// entropy (https://github.com/ulid/spec).
func New() string {
	return newULID(time.Now())
}

func newULID(t time.Time) string {
	var data [16]byte

	ms := uint64(t.UnixMilli()) //nolint:gosec // ULID's 48-bit timestamp field; not meaningful before year 10889
	data[0] = byteAt(ms, 40)
	data[1] = byteAt(ms, 32)
	data[2] = byteAt(ms, 24)
	data[3] = byteAt(ms, 16)
	data[4] = byteAt(ms, 8)
	data[5] = byteAt(ms, 0)

	if _, err := rand.Read(data[6:]); err != nil {
		panic("session: crypto/rand unavailable: " + err.Error())
	}

	return encodeULID(data)
}

// byteAt truncates v to the byte at the given bit offset. The truncation is
// the point -- this is how a wider integer is split into its constituent
// bytes -- not an overflow risk (gosec G115 false positive).
func byteAt(v uint64, shift uint) byte {
	return byte(v >> shift) //nolint:gosec
}

// encodeULID base32-encodes the 16 bytes (128 bits) of a ULID into its
// 26-character text form: 10 characters for the 48-bit timestamp (50 bits,
// 2 leading zero-padding bits), then 16 characters for the 80-bit
// randomness (exactly, no padding).
func encodeULID(data [16]byte) string {
	var out [26]byte

	out[0] = crockford[data[0]>>5]
	out[1] = crockford[data[0]&31]
	out[2] = crockford[(data[1]&248)>>3]
	out[3] = crockford[((data[1]&7)<<2)|((data[2]&192)>>6)]
	out[4] = crockford[(data[2]&62)>>1]
	out[5] = crockford[((data[2]&1)<<4)|((data[3]&240)>>4)]
	out[6] = crockford[((data[3]&15)<<1)|((data[4]&128)>>7)]
	out[7] = crockford[(data[4]&124)>>2]
	out[8] = crockford[((data[4]&3)<<3)|((data[5]&224)>>5)]
	out[9] = crockford[data[5]&31]

	out[10] = crockford[(data[6]&248)>>3]
	out[11] = crockford[((data[6]&7)<<2)|((data[7]&192)>>6)]
	out[12] = crockford[(data[7]&62)>>1]
	out[13] = crockford[((data[7]&1)<<4)|((data[8]&240)>>4)]
	out[14] = crockford[((data[8]&15)<<1)|((data[9]&128)>>7)]
	out[15] = crockford[(data[9]&124)>>2]
	out[16] = crockford[((data[9]&3)<<3)|((data[10]&224)>>5)]
	out[17] = crockford[data[10]&31]
	out[18] = crockford[(data[11]&248)>>3]
	out[19] = crockford[((data[11]&7)<<2)|((data[12]&192)>>6)]
	out[20] = crockford[(data[12]&62)>>1]
	out[21] = crockford[((data[12]&1)<<4)|((data[13]&240)>>4)]
	out[22] = crockford[((data[13]&15)<<1)|((data[14]&128)>>7)]
	out[23] = crockford[(data[14]&124)>>2]
	out[24] = crockford[((data[14]&3)<<3)|((data[15]&224)>>5)]
	out[25] = crockford[data[15]&31]

	return string(out[:])
}
