//go:build ignore

package frame

import "errors"

// MaxPayload is the largest payload a single frame can carry.
const MaxPayload = 200

// ErrShortFrame is returned when a buffer is too small to contain a header.
var ErrShortFrame = errors.New("frame: buffer shorter than header")
