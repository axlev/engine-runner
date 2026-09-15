package contextbuilder

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/axlev/engine-runner/internal/adapters"
)

const (
	bundleRoot = "../../fixtures/cases/happy-path/prospective"
	promptPath = "../../fixtures/prompts/placeholder.md"
	reviewAOut = "../../fixtures/responses/happy-path/reasoner-1.json"
	reviewBOut = "../../fixtures/responses/happy-path/reasoner-2.json"
)

// walkFiles returns every regular file under root, as paths relative to root.
func walkFiles(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out = append(out, rel)
		return nil
	})
	if err != nil {
		t.Fatalf("walking %s: %v", root, err)
	}
	return out
}

func assertNoControlLeakage(t *testing.T, workDir string) {
	t.Helper()
	for _, f := range walkFiles(t, workDir) {
		base := filepath.Base(f)
		if base == "manifest.json" || base == "checksums.sha256" {
			t.Errorf("control file %q leaked into workspace %s", f, workDir)
		}
	}
}

func TestReasoner1SeesOnlyProspectiveNoHandoff(t *testing.T) {
	work := t.TempDir()
	b := New()
	ctx, err := b.Prepare(adapters.StageReasoner1, bundleRoot, promptPath, work, HandoffPolicy{}, StageInputs{})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	assertNoControlLeakage(t, work)

	if _, err := os.Stat(filepath.Join(work, "input", "reviewer", "diff.patch")); err != nil {
		t.Errorf("expected diff.patch to be present: %v", err)
	}
	if _, err := os.Stat(filepath.Join(work, "input", "reviewer", "metadata.json")); err != nil {
		t.Errorf("expected metadata.json to be present: %v", err)
	}
	if _, err := os.Stat(filepath.Join(work, "input", "reviewer", "repository", "internal", "paging", "paginate.go")); err != nil {
		t.Errorf("expected repository snapshot to be present: %v", err)
	}
	if _, err := os.Stat(filepath.Join(work, "input", "handoff")); !os.IsNotExist(err) {
		t.Errorf("reasoner-1 must have no handoff directory, got err=%v", err)
	}
	if _, err := os.Stat(ctx.PromptPath); err != nil {
		t.Errorf("expected prompt file at %s: %v", ctx.PromptPath, err)
	}
}

// A prior output that exists on disk is NOT copied unless a grant names it.
// The policy is the sole authority; the inputs map only says where bytes
// live.
func TestNoGrantIgnoresAvailableReviewA(t *testing.T) {
	b := New()
	work := t.TempDir()
	_, err := b.Prepare(adapters.StageReasoner2, bundleRoot, promptPath, work, HandoffPolicy{},
		StageInputs{Outputs: map[adapters.Stage]string{adapters.StageReasoner1: reviewAOut}})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if _, err := os.Stat(filepath.Join(work, "input", "handoff", "review-a.json")); !os.IsNotExist(err) {
		t.Fatalf("review-a.json must not be copied without a grant, got err=%v", err)
	}
}

func TestGrantCopiesReviewAUnderTheDeclaredName(t *testing.T) {
	b := New()
	work := t.TempDir()
	ctx, err := b.Prepare(adapters.StageReasoner2, bundleRoot, promptPath, work,
		HandoffPolicy{Grants: []HandoffGrant{{From: adapters.StageReasoner1, As: "review-a.json"}}},
		StageInputs{Outputs: map[adapters.Stage]string{adapters.StageReasoner1: reviewAOut}})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	dst := filepath.Join(work, "input", "handoff", "review-a.json")
	if _, err := os.Stat(dst); err != nil {
		t.Fatalf("expected review-a.json to be copied: %v", err)
	}
	found := false
	for _, f := range ctx.InputFiles {
		if f == filepath.Join("input", "handoff", "review-a.json") {
			found = true
		}
	}
	if !found {
		t.Errorf("InputFiles = %v, want it to list the handoff", ctx.InputFiles)
	}
	assertNoControlLeakage(t, work)
}

// The name a handoff appears under is the grant's business, not the
// source stage's. A two-stage protocol may call reasoner-1's output
// whatever its reasoner-2 prompt expects.
func TestGrantHonoursTheDeclaredFileName(t *testing.T) {
	b := New()
	work := t.TempDir()
	_, err := b.Prepare(adapters.StageReasoner2, bundleRoot, promptPath, work,
		HandoffPolicy{Grants: []HandoffGrant{{From: adapters.StageReasoner1, As: "prior-review.json"}}},
		StageInputs{Outputs: map[adapters.Stage]string{adapters.StageReasoner1: reviewAOut}})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if _, err := os.Stat(filepath.Join(work, "input", "handoff", "prior-review.json")); err != nil {
		t.Fatalf("expected the handoff under its declared name: %v", err)
	}
}

// A grant whose source produced nothing is an error, never a silent gap:
// the receiving prompt would otherwise run believing it had seen a review.
func TestGrantWithNoOutputIsError(t *testing.T) {
	b := New()
	work := t.TempDir()
	_, err := b.Prepare(adapters.StageReasoner2, bundleRoot, promptPath, work,
		HandoffPolicy{Grants: []HandoffGrant{{From: adapters.StageReasoner1, As: "review-a.json"}}},
		StageInputs{})
	if err == nil {
		t.Fatalf("expected an error when a granted handoff has no output")
	}
}

// Pilot-v1's reasoner-3 shape, expressed as grants: review-b always,
// review-a only when granted. Nothing about stage identity decides this any
// more - the grants do.
func TestTwoGrantsCopyBoth(t *testing.T) {
	b := New()
	work := t.TempDir()
	_, err := b.Prepare(adapters.StageReasoner3, bundleRoot, promptPath, work,
		HandoffPolicy{Grants: []HandoffGrant{
			{From: adapters.StageReasoner1, As: "review-a.json"},
			{From: adapters.StageReasoner2, As: "review-b.json"},
		}},
		StageInputs{Outputs: map[adapters.Stage]string{
			adapters.StageReasoner1: reviewAOut,
			adapters.StageReasoner2: reviewBOut,
		}})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	for _, name := range []string{"review-a.json", "review-b.json"} {
		if _, err := os.Stat(filepath.Join(work, "input", "handoff", name)); err != nil {
			t.Errorf("expected %s: %v", name, err)
		}
	}
}

// The handoff directory is the only place a handoff may land. A grant name
// with a path component is refused here even though the loader already
// refuses it - this package must not trust its caller on that.
func TestGrantNameMustBeBare(t *testing.T) {
	b := New()
	for _, bad := range []string{"", "../escape.json", "sub/review.json", "/abs.json"} {
		_, err := b.Prepare(adapters.StageReasoner2, bundleRoot, promptPath, t.TempDir(),
			HandoffPolicy{Grants: []HandoffGrant{{From: adapters.StageReasoner1, As: bad}}},
			StageInputs{Outputs: map[adapters.Stage]string{adapters.StageReasoner1: reviewAOut}})
		if err == nil {
			t.Errorf("grant name %q must be refused", bad)
		}
	}
}

func TestUnknownStageIsError(t *testing.T) {
	work := t.TempDir()
	b := New()
	_, err := b.Prepare(adapters.Stage("reasoner-99"), bundleRoot, promptPath, work, HandoffPolicy{}, StageInputs{})
	if err == nil {
		t.Fatalf("expected an error for an unknown stage")
	}
}

func TestSymlinkInBundleIsRejected(t *testing.T) {
	// Build a throwaway bundle with a symlink under repository/, to prove
	// the copier refuses it rather than silently following it out of the
	// sanctioned tree.
	tmp := t.TempDir()
	reviewerDir := filepath.Join(tmp, "reviewer")
	repoDir := filepath.Join(reviewerDir, "repository")
	if err := os.MkdirAll(repoDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	outside := filepath.Join(tmp, "outside.txt")
	if err := os.WriteFile(outside, []byte("should never be reachable"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(repoDir, "escape.txt")); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(reviewerDir, "diff.patch"), []byte("diff"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	work := t.TempDir()
	b := New()
	_, err := b.Prepare(adapters.StageReasoner1, tmp, promptPath, work, HandoffPolicy{}, StageInputs{})
	if err == nil {
		t.Fatalf("expected Prepare to refuse a bundle containing a symlink")
	}
}

// TestPrepareCreatesAContainerWritableOutputDir pins a fix that cost a paid
// model call. Nothing in this package writes to output/, so it looked like
// the adapter's business - but if it does not exist when the container
// starts, docker creates the bind-mount source as root:root 0755 and the
// stage fails with "permission denied" only AFTER the vendor call has
// completed and been billed.
func TestPrepareCreatesAContainerWritableOutputDir(t *testing.T) {
	b := New()
	ws := t.TempDir()

	if _, err := b.Prepare(adapters.StageReasoner1, bundleRoot, promptPath, ws, HandoffPolicy{}, StageInputs{}); err != nil {
		t.Fatalf("Prepare: %v", err)
	}

	info, err := os.Stat(filepath.Join(ws, "output"))
	if err != nil {
		t.Fatalf("output dir must exist before the container starts, or docker creates it as root: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("output is not a directory")
	}
	// The container's uid belongs to the image, not the host, so "writable
	// by the host user" is not enough - it must be writable by any uid.
	if perm := info.Mode().Perm(); perm&0o002 == 0 {
		t.Errorf("output dir mode = %04o, want world-writable: the stage runs as uid 10001, unrelated to the host uid that created this", perm)
	}
}
