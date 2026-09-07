package runner

import (
	"os"
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
