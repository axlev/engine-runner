package adapters

import (
	"os"
	"path/filepath"
	"testing"
)

// The bug this guards: docker creates a missing bind-mount source as
// root:root 0755, and the host-side write of an already-paid-for result is
// then refused. The regression is cheap to test and expensive to hit.
func TestPrepareWorkspaceMakesBothMountSourcesWritable(t *testing.T) {
	ws := t.TempDir()
	if err := PrepareWorkspace(ws); err != nil {
		t.Fatalf("PrepareWorkspace: %v", err)
	}
	for _, sub := range []string{"input", "output"} {
		info, err := os.Stat(filepath.Join(ws, sub))
		if err != nil {
			t.Fatalf("%s not created: %v", sub, err)
		}
		if !info.IsDir() {
			t.Fatalf("%s is not a directory", sub)
		}
	}
	// output/ must be writable by the container's uid, which is a property
	// of the image and cannot be chowned to without root - so the mode has
	// to carry it, and umask must not have masked it back to 0755.
	info, _ := os.Stat(filepath.Join(ws, "output"))
	if perm := info.Mode().Perm(); perm != 0o777 {
		t.Errorf("output dir is %04o, want 0777: umask masked the mode and the container could not write", perm)
	}
	// The host-side write that actually failed in cmd/probe.
	if err := os.WriteFile(filepath.Join(ws, "output", "probe-A.json"), []byte(`{}`), 0o644); err != nil {
		t.Errorf("host-side result write refused: %v", err)
	}
}

func TestPrepareWorkspaceIsIdempotentAndChecksItsInput(t *testing.T) {
	ws := t.TempDir()
	if err := PrepareWorkspace(ws); err != nil {
		t.Fatalf("first call: %v", err)
	}
	// The pipeline path runs over a workspace contextbuilder already made.
	if err := PrepareWorkspace(ws); err != nil {
		t.Errorf("second call over an existing workspace: %v", err)
	}
	if err := PrepareWorkspace(""); err == nil {
		t.Error("expected refusal for an empty workspace path")
	}
}
