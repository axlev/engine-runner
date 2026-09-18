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
	"strings"

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

// Reclaim removes every workspace this run created.
//
// Per-attempt workspaces are scratch, but nothing was reclaiming them: an
// H1 cohort left 175 directories and 29 GB behind, each holding a copy of
// an ~86 MB repository snapshot, and every later batch started with less
// headroom than the last.
//
// It is deliberately NOT automatic on failure. A failed run's workspace is
// the only place the inputs as the container saw them still exist, and that
// is exactly when someone needs to look - the planted-CLAUDE.md steering
// control was confirmed by finding the file in a sealed run's workspace.
// Callers reclaim after a run they are satisfied with, and keep the rest.
func (r *Runner) Reclaim(runID string) error {
	if runID == "" {
		return fmt.Errorf("runner: refusing to reclaim an empty run id")
	}
	dir := filepath.Join(r.root, runID)
	// Containment check: the run id comes from a caller and a traversal
	// would delete outside the root. Cheap to check, expensive to miss.
	rel, err := filepath.Rel(r.root, dir)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("runner: run id %q does not resolve inside the workspace root", runID)
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("runner: reclaiming %s: %w", dir, err)
	}
	return nil
}
