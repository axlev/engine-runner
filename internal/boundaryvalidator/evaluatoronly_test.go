package boundaryvalidator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A reviewer must never be pointed at evaluator material. The isolation
// test in internal/oracle stops the review path NAMING these paths; this
// stops an operator passing one on the command line. It lives here, not in
// cmd/bench, because naming the markers is exactly what that test forbids
// a review-path package from doing.
func TestRefuseEvaluatorOnlyBundle(t *testing.T) {
	root := t.TempDir()
	refused := []string{
		filepath.Join(root, "frr-h1-cohort-evaluator-inputs", "case-1"),
		filepath.Join(root, "frr-pilot-retrospective-evaluator-only", "case-1", "prospective"),
		filepath.Join(root, "x-evaluator-inputs"),
	}
	for _, p := range refused {
		if err := RefuseEvaluatorOnlyBundle(p); err == nil {
			t.Errorf("must refuse %s", p)
		} else if !strings.Contains(err.Error(), "evaluator-only material") {
			t.Errorf("%s: unhelpful error %v", p, err)
		}
	}
	for _, p := range []string{
		filepath.Join(root, "frr-h1-cohort", "case-1"),
		filepath.Join(root, "fixtures", "cases", "happy-path", "prospective"),
	} {
		if err := RefuseEvaluatorOnlyBundle(p); err != nil {
			t.Errorf("must allow %s: %v", p, err)
		}
	}
}

// A symlink must not launder the path: the check resolves before matching.
func TestRefuseEvaluatorOnlyBundleFollowsSymlinks(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "cohort-evaluator-inputs", "case-1")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "innocent-looking")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := RefuseEvaluatorOnlyBundle(link); err == nil {
		t.Error("a symlink into evaluator material must be refused")
	}
}
