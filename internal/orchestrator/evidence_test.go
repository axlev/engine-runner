package orchestrator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fpBundle = "../../fixtures/cases/false-positive/prospective"

func writeOutput(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "review.json")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	return path
}

// A citation naming a file the reviewer was never given is a claim about
// code that does not exist. That is the hallucination this rule exists to
// catch, and it must fail the stage so the retry gets another attempt.
func TestCitationToAbsentFileFails(t *testing.T) {
	out := writeOutput(t, `{"findings":[{"evidence":[{"file":"frame/nonexistent.go"}]}]}`)
	err, _ := validateEvidence(out, fpBundle)
	if err == nil {
		t.Fatalf("expected a failure for a citation to a file not in the snapshot")
	}
	if !strings.Contains(err.Error(), "nonexistent.go") {
		t.Errorf("error = %q, want it to name the offending file", err)
	}
}

// Line numbers past the end of a real file are the exact defect found in
// the committed fixtures: they cited lines 42-47 of a 20-line file and
// every test passed.
func TestCitationBeyondEndOfFileFails(t *testing.T) {
	out := writeOutput(t, `{"findings":[{"evidence":[{"file":"frame/frame.go","start_line":900,"end_line":905}]}]}`)
	err, _ := validateEvidence(out, fpBundle)
	if err == nil {
		t.Fatalf("expected a failure for a line range outside the file")
	}
	if !strings.Contains(err.Error(), "outside") {
		t.Errorf("error = %q, want it to say the range is outside the file", err)
	}
}

// Path traversal is rejected rather than resolved: "../" in a citation is
// not a typo worth accommodating, and the snapshot boundary is the point.
func TestCitationEscapingTheSnapshotFails(t *testing.T) {
	for _, path := range []string{"../../control/manifest.json", "/etc/passwd"} {
		out := writeOutput(t, `{"findings":[{"evidence":[{"file":"`+path+`"}]}]}`)
		if err, _ := validateEvidence(out, fpBundle); err == nil {
			t.Errorf("%s: expected a failure for a citation outside the snapshot", path)
		}
	}
}

// An excerpt that does not match warns rather than failing. A model quoting
// with different whitespace has still pointed at the right place, and
// failing a paid stage over formatting repeats the mistake --json-schema
// was added to fix.
func TestExcerptMismatchWarnsButPasses(t *testing.T) {
	out := writeOutput(t, `{"findings":[{"evidence":[
		{"file":"frame/frame.go","start_line":10,"end_line":10,"excerpt":"something the file does not say"}]}]}`)
	err, warnings := validateEvidence(out, fpBundle)
	if err != nil {
		t.Fatalf("an excerpt mismatch must not fail the stage: %v", err)
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "frame/frame.go:10") {
		t.Errorf("warnings = %v, want one naming the citation", warnings)
	}
}

// The committed fixture responses must satisfy the rule they are used to
// test, or the suite is validating its own mistakes.
func TestCommittedFixtureCitationsPass(t *testing.T) {
	for _, tc := range []struct{ response, bundle string }{
		{"../../fixtures/responses/false-positive/reasoner-1.json", fpBundle},
		{"../../fixtures/responses/false-positive/reasoner-2.json", fpBundle},
		{"../../fixtures/responses/happy-path/reasoner-1.json", "../../fixtures/cases/happy-path/prospective"},
		{"../../fixtures/responses/happy-path/reasoner-2.json", "../../fixtures/cases/happy-path/prospective"},
	} {
		err, warnings := validateEvidence(tc.response, tc.bundle)
		if err != nil {
			t.Errorf("%s: %v", tc.response, err)
		}
		if len(warnings) > 0 {
			t.Errorf("%s: excerpts should be exact in committed fixtures: %v", tc.response, warnings)
		}
	}
}

// Citations are found wherever they appear. review-b nests them under
// assessments[].causal_chain[] rather than findings[].evidence[], and a
// traversal that only knew about review-a would silently pass review-b -
// silently skipping being the failure this rule exists to prevent.
func TestCitationsAreFoundInEveryDocumentShape(t *testing.T) {
	for name, body := range map[string]string{
		"review-a evidence":    `{"findings":[{"evidence":[{"file":"nope.go"}]}]}`,
		"review-b causal":      `{"assessments":[{"causal_chain":[{"file":"nope.go"}]}]}`,
		"review-c new finding": `{"new_findings":[{"evidence":[{"file":"nope.go"}]}]}`,
		"deeply nested":        `{"a":{"b":[{"c":{"evidence":[{"file":"nope.go"}]}}]}}`,
	} {
		if err, _ := validateEvidence(writeOutput(t, body), fpBundle); err == nil {
			t.Errorf("%s: citation was not found and checked", name)
		}
	}
}
