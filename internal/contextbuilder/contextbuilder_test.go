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

func TestReasoner2HandoffDisabledIgnoresAvailableReviewA(t *testing.T) {
	work := t.TempDir()
	b := New()
	// ReviewAPath is supplied, but the policy does not authorize it -
	// policy must win over mere availability.
	_, err := b.Prepare(adapters.StageReasoner2, bundleRoot, promptPath, work, HandoffPolicy{EnableAToB: false}, StageInputs{ReviewAPath: reviewAOut})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if _, err := os.Stat(filepath.Join(work, "input", "handoff", "review-a.json")); !os.IsNotExist(err) {
		t.Fatalf("review-a.json must not be copied when EnableAToB is false, got err=%v", err)
	}
	assertNoControlLeakage(t, work)
}

func TestReasoner2HandoffEnabledCopiesReviewA(t *testing.T) {
	work := t.TempDir()
	b := New()
	ctx, err := b.Prepare(adapters.StageReasoner2, bundleRoot, promptPath, work, HandoffPolicy{EnableAToB: true}, StageInputs{ReviewAPath: reviewAOut})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	dst := filepath.Join(work, "input", "handoff", "review-a.json")
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("reading copied handoff file: %v", err)
	}
	want, err := os.ReadFile(reviewAOut)
	if err != nil {
		t.Fatalf("reading source review-a.json: %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("copied review-a.json does not match source byte-for-byte")
	}
	found := false
	for _, f := range ctx.InputFiles {
		if f == filepath.Join("input", "handoff", "review-a.json") {
			found = true
		}
	}
	if !found {
		t.Errorf("InputFiles = %v, expected it to list the review-a handoff", ctx.InputFiles)
	}
}

func TestReasoner2HandoffEnabledButMissingReviewAIsError(t *testing.T) {
	work := t.TempDir()
	b := New()
	_, err := b.Prepare(adapters.StageReasoner2, bundleRoot, promptPath, work, HandoffPolicy{EnableAToB: true}, StageInputs{})
	if err == nil {
		t.Fatalf("expected an error when the A-to-B handoff is enabled but no review-a output was supplied")
	}
}

func TestReasoner3AlwaysGetsReviewBNeverReviewAWithoutPolicy(t *testing.T) {
	work := t.TempDir()
	b := New()
	_, err := b.Prepare(adapters.StageReasoner3, bundleRoot, promptPath, work, HandoffPolicy{EnableAToB: false}, StageInputs{ReviewBPath: reviewBOut})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if _, err := os.Stat(filepath.Join(work, "input", "handoff", "review-b.json")); err != nil {
		t.Errorf("expected review-b.json to be present: %v", err)
	}
	if _, err := os.Stat(filepath.Join(work, "input", "handoff", "review-a.json")); !os.IsNotExist(err) {
		t.Errorf("review-a.json must not appear when EnableAToB is false, got err=%v", err)
	}
	assertNoControlLeakage(t, work)
}

func TestReasoner3WithAToBEnabledGetsBothHandoffs(t *testing.T) {
	work := t.TempDir()
	b := New()
	_, err := b.Prepare(adapters.StageReasoner3, bundleRoot, promptPath, work, HandoffPolicy{EnableAToB: true}, StageInputs{
		ReviewAPath: reviewAOut,
		ReviewBPath: reviewBOut,
	})
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	for _, name := range []string{"review-a.json", "review-b.json"} {
		if _, err := os.Stat(filepath.Join(work, "input", "handoff", name)); err != nil {
			t.Errorf("expected %s to be present: %v", name, err)
		}
	}
}

func TestReasoner3MissingReviewBIsError(t *testing.T) {
	work := t.TempDir()
	b := New()
	_, err := b.Prepare(adapters.StageReasoner3, bundleRoot, promptPath, work, HandoffPolicy{}, StageInputs{})
	if err == nil {
		t.Fatalf("expected an error: reasoner-3 always requires review-b, regardless of policy")
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
