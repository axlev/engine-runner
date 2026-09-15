package results

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/axlev/engine-runner/internal/adapters"
)

// A T run that lost its stage B must not seal as a G verdict.
func TestSealRefusesToScoreATRunWithoutStageB(t *testing.T) {
	dir := t.TempDir()
	stages := filepath.Join(dir, "stages")
	if err := os.MkdirAll(filepath.Join(stages, "reasoner-1"), 0o755); err != nil {
		t.Fatal(err)
	}
	a := `{"schema_version":"h1-review-a/v1","run_id":"r","case_id":"c","stage":"reasoner-1","generated_at":"2026-09-15T00:00:00Z","findings":[],"empty_reason":"x"}`
	if err := os.WriteFile(filepath.Join(stages, "reasoner-1", "review-a.json"), []byte(a), 0o644); err != nil {
		t.Fatal(err)
	}
	err := writeEvaluation(dir, stages, "r", "c", []adapters.Stage{adapters.StageReasoner1, adapters.StageReasoner2})
	if err == nil {
		t.Fatal("expected the seal to fail closed")
	}
	if _, statErr := os.Stat(filepath.Join(dir, "evaluation", "verdict.json")); statErr == nil {
		t.Error("a verdict was written for a T run with no stage B")
	}

	// The same outputs under a one-stage declaration are a valid G run.
	if err := writeEvaluation(dir, stages, "r", "c", []adapters.Stage{adapters.StageReasoner1}); err != nil {
		t.Fatalf("G: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "evaluation", "verdict.json"))
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		ArmRule string `json:"arm_rule"`
		Verdict string `json:"verdict"`
	}
	_ = json.Unmarshal(raw, &v)
	if v.ArmRule != "G" || v.Verdict != "CLEAN" {
		t.Errorf("got %+v", v)
	}
}

// A one-stage protocol on the PILOT schema (fixtures/protocols/single-stage)
// is not an H1 run: it lands not-evaluated.json as before, never a verdict.
func TestSealDoesNotApplyH1RuleToPilotShapedOutput(t *testing.T) {
	dir := t.TempDir()
	stages := filepath.Join(dir, "stages")
	_ = os.MkdirAll(filepath.Join(stages, "reasoner-1"), 0o755)
	a := `{"schema_version":"review-a/v1","findings":[]}`
	_ = os.WriteFile(filepath.Join(stages, "reasoner-1", "review-a.json"), []byte(a), 0o644)
	if err := writeEvaluation(dir, stages, "r", "c", []adapters.Stage{adapters.StageReasoner1}); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "evaluation", "not-evaluated.json")); err != nil {
		t.Error("expected not-evaluated.json")
	}
	if _, err := os.Stat(filepath.Join(dir, "evaluation", "verdict.json")); err == nil {
		t.Error("a pilot-schema run must not receive an H1 verdict")
	}
}
