package oracle

import (
	"bytes"
	"testing"

	"example.com/widget/frame"
	"example.com/widget/transport"
)

// The REAL defect: Decode trusts the length byte.
func TestDecodePanicsOnTruncatedFrame(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected a panic: this case's ground truth is that Decode is unsafe")
		} else {
			t.Logf("confirmed real defect: %v", r)
		}
	}()
	// Length byte says 200 bytes follow; only 3 do.
	_, _ = frame.Decode([]byte{200, 1, 2, 3})
}

// The TRAP: Encode's one-byte length looks like truncation above 255, but
// no payload that large can reach it.
func TestOversizedPayloadNeverReachesEncode(t *testing.T) {
	var buf bytes.Buffer
	err := transport.Send(&buf, make([]byte, 300))
	if err == nil {
		t.Fatalf("Send accepted a 300-byte payload; the trap would be a REAL bug")
	}
	if buf.Len() != 0 {
		t.Fatalf("Send wrote %d bytes; Encode must never have been reached", buf.Len())
	}
	t.Logf("confirmed NOT a defect: Send rejected it (%v) and wrote nothing", err)
}

// And the framing is exact at the largest reachable payload.
func TestEncodeIsExactAtMaxPayload(t *testing.T) {
	var buf bytes.Buffer
	if err := transport.Send(&buf, make([]byte, frame.MaxPayload)); err != nil {
		t.Fatalf("Send rejected a legal payload: %v", err)
	}
	if got := int(buf.Bytes()[0]); got != frame.MaxPayload {
		t.Fatalf("length header = %d, want %d: truncation WOULD be real", got, frame.MaxPayload)
	}
}
