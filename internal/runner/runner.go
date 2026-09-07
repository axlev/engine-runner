// Package runner creates the fresh, isolated workspace each stage attempt
// executes in. See docs/system-design.md section 9.
//
// For this phase of the MVP, "isolated" means a workspace directory unique
// to this exact (run, stage, attempt) tuple, guaranteed never to have
// existed before and never to be reused. It does not yet mean an OCI
// container, network isolation, or resource limits - those are added on top
// of this same directory contract once containerization lands (Milestone 1,
// later phase). Every attempt getting its own fresh directory, not just
// every stage, matches the design's "adapters must be stateless across
// calls": a retried attempt must not be able to observe anything the failed
// attempt before it wrote.
package runner

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/axlev/engine-runner/internal/adapters"
)

// Runner creates fresh workspace directories under a root. It is safe for
// concurrent use: workspace uniqueness comes from os.MkdirTemp, not from any
// shared counter.
type Runner struct {
	root string
}

// New returns a Runner that creates workspaces under root. root is created
// if it does not already exist.
func New(root string) (*Runner, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("runner: creating root %s: %w", root, err)
	}
	return &Runner{root: root}, nil
}

// FreshWorkspace creates and returns a brand-new, empty directory for one
// stage attempt. The returned path has never been handed out before by this
// or any other Runner rooted elsewhere.
func (r *Runner) FreshWorkspace(runID string, stage adapters.Stage, attempt int) (string, error) {
	runDir := filepath.Join(r.root, runID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return "", fmt.Errorf("runner: creating run dir %s: %w", runDir, err)
	}
	pattern := fmt.Sprintf("%s-attempt%d-*", stage, attempt)
	ws, err := os.MkdirTemp(runDir, pattern)
	if err != nil {
		return "", fmt.Errorf("runner: creating fresh workspace under %s: %w", runDir, err)
	}
	return ws, nil
}
