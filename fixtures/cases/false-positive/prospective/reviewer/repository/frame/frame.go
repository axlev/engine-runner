// Package frame encodes and decodes length-prefixed wire frames.
package frame

// HeaderSize is the number of bytes prefixed to every payload.
const HeaderSize = 1

// Encode frames payload for the wire.
func Encode(payload []byte) []byte {
	out := make([]byte, HeaderSize+len(payload))
	out[0] = byte(len(payload))
	copy(out[HeaderSize:], payload)
	return out
}

// Decode returns the payload carried by b.
func Decode(b []byte) ([]byte, error) {
	if len(b) < HeaderSize {
		return nil, ErrShortFrame
	}
	n := int(b[0])
	return b[HeaderSize : HeaderSize+n], nil
}
