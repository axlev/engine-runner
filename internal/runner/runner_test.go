package runner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/axlev/engine-runner/internal/adapters"
)

func TestFreshWorkspaceIsUniquePerAttempt(t *testing.T) {
	r, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	seen := make(map[string]bool)
	for attempt := 1; attempt <= 3; attempt++ {
		ws, err := r.FreshWorkspace("run-1", adapters.StageReasoner1, attempt)
		if err != nil {
			t.Fatalf("FreshWorkspace(attempt=%d): %v", attempt, err)
		}
		if seen[ws] {
			t.Fatalf("FreshWorkspace returned a path already handed out: %s", ws)
		}
		seen[ws] = true

		info, err := os.Stat(ws)
		if err != nil || !info.IsDir() {
			t.Fatalf("expected %s to be an existing, empty directory: %v", ws, err)
		}
		entries, err := os.ReadDir(ws)
		if err != nil || len(entries) != 0 {
			t.Fatalf("expected %s to be empty, got entries=%v err=%v", ws, entries, err)
		}
	}
}

func TestFreshWorkspaceSeparatesDifferentStages(t *testing.T) {
	r, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ws1, err := r.FreshWorkspace("run-1", adapters.StageReasoner1, 1)
	if err != nil {
		t.Fatalf("FreshWorkspace: %v", err)
	}
	ws2, err := r.FreshWorkspace("run-1", adapters.StageReasoner2, 1)
	if err != nil {
		t.Fatalf("FreshWorkspace: %v", err)
	}
	if ws1 == ws2 {
		t.Fatalf("reasoner-1 and reasoner-2 got the same workspace: %s", ws1)
	}
}

func TestReclaimRemovesOnlyThatRun(t *testing.T) {
	root := t.TempDir()
	r, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	keep, err := r.FreshWorkspace("run-keep", adapters.StageReasoner1, 1)
	if err != nil {
		t.Fatal(err)
	}
	gone, err := r.FreshWorkspace("run-gone", adapters.StageReasoner1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Reclaim("run-gone"); err != nil {
		t.Fatalf("Reclaim: %v", err)
	}
	if _, err := os.Stat(gone); !os.IsNotExist(err) {
		t.Error("the reclaimed run's workspace still exists")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("an unrelated run's workspace was removed: %v", err)
	}
}

// The run id reaches this from a caller, so a traversal must not delete
// outside the root.
func TestReclaimRefusesToEscapeTheRoot(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(root, "..", "sibling")
	if err := os.MkdirAll(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	r, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"", "..", "../sibling", "../../etc"} {
		if err := r.Reclaim(bad); err == nil {
			t.Errorf("Reclaim(%q) was allowed", bad)
		}
	}
	if _, err := os.Stat(outside); err != nil {
		t.Errorf("a directory outside the root was removed: %v", err)
	}
}
