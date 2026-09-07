package evaluation

import "testing"

const (
	reviewA = "../../fixtures/responses/happy-path/reasoner-1.json"
	reviewB = "../../fixtures/responses/happy-path/reasoner-2.json"
	reviewC = "../../fixtures/responses/happy-path/reasoner-3.json"
)

// The happy-path fixtures are hand-authored (fixtures/responses/happy-path):
// f1 is CONFIRMED by both B and C, f2 is REJECTED by both, no new findings.
// This test pins that known content to known scores, so a change to either
// the fixture or the evaluator's logic breaks a visible, specific test.
func TestEvaluateHappyPath(t *testing.T) {
	scores, findings, err := Evaluate("run-1", "happy-path", reviewA, reviewB, reviewC)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	want := Scores{
		SchemaVersion:      "evaluation-scores/v1",
		RunID:              "run-1",
		CaseID:             "happy-path",
		FindingsDiscovered: 2,
		ConfirmedByB:       1,
		RejectedByB:        1,
		ConfirmedByC:       1,
		RejectedByC:        1,
	}
	if scores != want {
		t.Errorf("Evaluate() scores = %+v, want %+v", scores, want)
	}

	if len(findings.Findings) != 2 {
		t.Fatalf("len(findings.Findings) = %d, want 2", len(findings.Findings))
	}
	byID := map[string]Finding{}
	for _, f := range findings.Findings {
		byID[f.ID] = f
	}
	if f1 := byID["f1"]; f1.FinalStatus != StatusConfirmed {
		t.Errorf("f1.FinalStatus = %q, want %q", f1.FinalStatus, StatusConfirmed)
	}
	if f2 := byID["f2"]; f2.FinalStatus != StatusRejected {
		t.Errorf("f2.FinalStatus = %q, want %q", f2.FinalStatus, StatusRejected)
	}
}

func TestEvaluateMissingFileIsError(t *testing.T) {
	if _, _, err := Evaluate("run-1", "case-1", "does-not-exist.json", reviewB, reviewC); err == nil {
		t.Fatalf("expected an error for a missing review-a file")
	}
}
