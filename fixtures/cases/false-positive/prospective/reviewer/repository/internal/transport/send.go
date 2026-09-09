//go:build ignore

// Package transport writes framed payloads to a connection.
package transport

import (
	"fmt"
	"io"

	"example.com/widget/internal/frame"
)

// Send writes one framed payload.
func Send(w io.Writer, payload []byte) error {
	if len(payload) > frame.MaxPayload {
		return fmt.Errorf("transport: payload of %d bytes exceeds the %d-byte frame limit",
			len(payload), frame.MaxPayload)
	}
	_, err := w.Write(frame.Encode(payload))
	return err
}
