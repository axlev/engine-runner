package orchestrator

import (
	"strings"
	"testing"
)

// The engine's own revision must be recorded by the run, not asserted
// beside it. A test binary may carry no VCS stamp, so the contract is
// "absent or well-formed", never "invented".
func TestEngineCommitIsWellFormedOrAbsent(t *testing.T) {
	got := engineCommit()
	if got == "" {
		t.Skip("test binary carries no VCS stamp")
	}
	rev := strings.TrimSuffix(got, "-dirty")
	if len(rev) != 40 {
		t.Fatalf("engine commit %q is not a 40-char revision", rev)
	}
	for _, c := range rev {
		if !strings.ContainsRune("0123456789abcdef", c) {
			t.Fatalf("engine commit %q is not hex", rev)
		}
	}
}
