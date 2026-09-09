package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const fpCase = "../../fixtures/cases/false-positive"

// TestFalsePositiveCitationsAreExact catches a class of quiet wrongness in
// hand-authored fixtures. The first version of this case had every evidence
// line number wrong by one or two lines, and every test still passed,
// because nothing compares a citation against the file it cites.
//
// It matters beyond tidiness: evidence_ref requires only "file", so
// start_line is unvalidated for REAL model output too. A model could cite a
// plausible-looking location it never read. Fixtures that are themselves
// wrong would make that impossible to notice.
func TestFalsePositiveCitationsAreExact(t *testing.T) {
	snapshot := filepath.Join(fpCase, "prospective/reviewer/repository")

	for _, stage := range []string{"reasoner-1", "reasoner-2"} {
		raw, err := os.ReadFile(filepath.Join("../../fixtures/responses/false-positive", stage+".json"))
		if err != nil {
			t.Fatalf("reading %s response: %v", stage, err)
		}
		var doc struct {
			Findings []struct {
				Evidence []evidenceRef `json:"evidence"`
			} `json:"findings"`
			Assessments []struct {
				CausalChain []evidenceRef `json:"causal_chain"`
			} `json:"assessments"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("parsing %s: %v", stage, err)
		}

		var refs []evidenceRef
		for _, f := range doc.Findings {
			refs = append(refs, f.Evidence...)
		}
		for _, a := range doc.Assessments {
			refs = append(refs, a.CausalChain...)
		}
		if len(refs) == 0 {
			t.Fatalf("%s: no citations found; the test would pass vacuously", stage)
		}

		for _, ref := range refs {
			ref.check(t, stage, snapshot)
		}
	}
}

type evidenceRef struct {
	File      string `json:"file"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Excerpt   string `json:"excerpt"`
}

func (r evidenceRef) check(t *testing.T, stage, snapshot string) {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(snapshot, r.File))
	if err != nil {
		t.Errorf("%s cites %s, which is not in the reviewer snapshot: %v", stage, r.File, err)
		return
	}
	lines := strings.Split(string(body), "\n")
	if r.StartLine < 1 || r.EndLine > len(lines) || r.EndLine < r.StartLine {
		t.Errorf("%s cites %s:%d-%d, outside a %d-line file", stage, r.File, r.StartLine, r.EndLine, len(lines))
		return
	}
	if r.Excerpt == "" {
		return // excerpt is optional; the range check above still applied
	}
	got := strings.TrimSpace(strings.Join(lines[r.StartLine-1:r.EndLine], "\n"))
	if got != strings.TrimSpace(r.Excerpt) {
		t.Errorf("%s cites %s:%d-%d\n  excerpt: %q\n  actual : %q",
			stage, r.File, r.StartLine, r.EndLine, r.Excerpt, got)
	}
}

// TestFalsePositiveGroundTruthHolds runs the oracle module, which executes
// the reviewer's own snapshot. It turns "one of these findings is real and
// the other is not" from my assertion into a fact the suite re-checks: the
// Decode panic actually panics, and an oversized payload actually cannot
// reach Encode.
//
// The oracle is a separate module (its own go.mod), so it is invisible to
// this repository's ./... and has to be run explicitly.
func TestFalsePositiveGroundTruthHolds(t *testing.T) {
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}
	cmd := exec.Command("go", "test", "./...")
	cmd.Dir = filepath.Join(fpCase, "oracle")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the case's ground truth no longer holds:\n%s", out)
	}
}

// TestTLVBoundsGroundTruthHolds runs the C case's oracle, which compiles the
// reviewer's own snapshot under AddressSanitizer and proves BOTH directions:
// the area-address TLV really does read past the end of the stream buffer,
// and the handler-table index really cannot go out of range. Ground truth is
// a sanitizer trace rather than an assertion in a comment.
//
// Skips rather than fails without a C toolchain: the engine's own tests must
// not require one.
func TestTLVBoundsGroundTruthHolds(t *testing.T) {
	if _, err := exec.LookPath("gcc"); err != nil {
		t.Skip("no C toolchain; the oracle needs gcc with -fsanitize=address")
	}
	cmd := exec.Command("./run.sh")
	cmd.Dir = "../../fixtures/cases/tlv-bounds/oracle"
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the case's ground truth no longer holds:\n%s", out)
	}
	if !strings.Contains(string(out), "heap-buffer-overflow") {
		t.Errorf("expected an ASan heap-buffer-overflow to prove the real defect:\n%s", out)
	}
	if !strings.Contains(string(out), "all 256 type octets dispatched") {
		t.Errorf("expected the dispatch table to be shown total:\n%s", out)
	}
}
