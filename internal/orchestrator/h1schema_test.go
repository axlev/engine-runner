package orchestrator

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axlev/engine-runner/internal/adapters"
)

// The envelope stamp must follow the declared output schema, not the stage
// name: H1 protocols reuse reasoner-1 and reasoner-2 with different schemas.
func TestSchemaVersionComesFromTheDeclaredSchema(t *testing.T) {
	cases := map[string]string{
		"schemas/review-a.schema.json":    "review-a/v1",
		"schemas/review-b.schema.json":    "review-b/v1",
		"schemas/review-c.schema.json":    "review-c/v1",
		"schemas/h1-review-a.schema.json": "h1-review-a/v1",
		"schemas/h1-review-b.schema.json": "h1-review-b/v1",
	}
	for path, want := range cases {
		got, err := schemaVersionOf(filepath.Join(repoRoot, path))
		if err != nil {
			t.Errorf("%s: %v", path, err)
			continue
		}
		if got != want {
			t.Errorf("%s: schema_version = %q, want %q", path, got, want)
		}
	}
}

// A stage output schema that does not pin its version is refused before any
// stage runs - a stamp with an empty version would validate against nothing.
func TestSchemaWithoutVersionConstFailsClosed(t *testing.T) {
	p := filepath.Join(t.TempDir(), "unpinned.schema.json")
	if err := os.WriteFile(p, []byte(`{"type":"object","properties":{"schema_version":{"type":"string"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := schemaVersionOf(p); err == nil || !strings.Contains(err.Error(), "does not pin") {
		t.Errorf("expected a fail-closed error naming the missing const, got %v", err)
	}
}

func TestH1TwoStageFixtureRunsAndStampsH1Versions(t *testing.T) {
	adapter := newFixtureAdapter(t)
	o := newOrchFor(t, filepath.Join(repoRoot, "fixtures", "protocols", "h1-two-stage.yaml"), adapter)

	outcome, err := o.Run(context.Background(), "run-h1-two", "h1-two-stage", happyPathBundle)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if outcome.Status != "completed" {
		t.Fatalf("Status = %q (%s)", outcome.Status, outcome.FailureReason)
	}
	for stage, path := range map[adapters.Stage]string{adapters.StageReasoner1: outcome.ReviewAPath, adapters.StageReasoner2: outcome.ReviewBPath} {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s output: %v", stage, err)
		}
		var doc struct {
			SchemaVersion string `json:"schema_version"`
			Stage         string `json:"stage"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		want := map[adapters.Stage]string{adapters.StageReasoner1: "h1-review-a/v1", adapters.StageReasoner2: "h1-review-b/v1"}[stage]
		if doc.SchemaVersion != want || doc.Stage != string(stage) {
			t.Errorf("%s stamped %q/%q, want %q/%q", stage, doc.SchemaVersion, doc.Stage, want, stage)
		}
	}
	if outcome.ReviewCPath != "" {
		t.Errorf("no reasoner-3 in this protocol, got ReviewCPath %q", outcome.ReviewCPath)
	}
}

func TestH1SingleStageAndCleanFixturesRun(t *testing.T) {
	for _, caseID := range []string{"h1-single-stage", "h1-clean"} {
		adapter := newFixtureAdapter(t)
		o := newOrchFor(t, filepath.Join(repoRoot, "fixtures", "protocols", "h1-single-stage.yaml"), adapter)
		outcome, err := o.Run(context.Background(), "run-"+caseID, caseID, happyPathBundle)
		if err != nil {
			t.Fatalf("%s: Run: %v", caseID, err)
		}
		if outcome.Status != "completed" {
			t.Fatalf("%s: Status = %q (%s)", caseID, outcome.Status, outcome.FailureReason)
		}
	}
}

// The schema constraints that carry the pre-registration's scoring rules
// must actually bite. Each doc below is the clean fixture with one field
// broken; each must fail validation, or the rule it encodes is decorative.
func TestH1SchemaRulesBite(t *testing.T) {
	v := NewSchemaValidator(filepath.Join(repoRoot, "schemas"))
	base, err := os.ReadFile(filepath.Join(repoRoot, "fixtures", "responses", "h1", "reasoner-1.json"))
	if err != nil {
		t.Fatal(err)
	}
	stamp := func(doc map[string]any) map[string]any {
		doc["schema_version"] = "h1-review-a/v1"
		doc["run_id"] = "r"
		doc["case_id"] = "c"
		doc["stage"] = "reasoner-1"
		doc["generated_at"] = "2026-09-15T00:00:00Z"
		return doc
	}
	load := func() map[string]any {
		var d map[string]any
		if err := json.Unmarshal(base, &d); err != nil {
			t.Fatal(err)
		}
		return stamp(d)
	}
	write := func(doc map[string]any) string {
		p := filepath.Join(t.TempDir(), "doc.json")
		b, _ := json.Marshal(doc)
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	if err := v.ValidateFile(write(load()), "h1-review-a.schema.json"); err != nil {
		t.Fatalf("the fixture itself must validate: %v", err)
	}

	mutate := map[string]func(map[string]any){
		"empty findings without empty_reason": func(d map[string]any) { d["findings"] = []any{}; delete(d, "empty_reason") },
		"class outside the s7 enum":           func(d map[string]any) { d["findings"].([]any)[0].(map[string]any)["class"] = "memory" },
		"missing mechanism":                   func(d map[string]any) { delete(d["findings"].([]any)[0].(map[string]any), "mechanism") },
		"missing recommended_validation":      func(d map[string]any) { delete(d["findings"].([]any)[0].(map[string]any), "recommended_validation") },
		"validation with no paths": func(d map[string]any) {
			d["findings"].([]any)[0].(map[string]any)["recommended_validation"].(map[string]any)["paths"] = []any{}
		},
		"verdict field smuggled in": func(d map[string]any) { d["verdict"] = "RISKY" },
	}
	for name, fn := range mutate {
		d := load()
		fn(d)
		if err := v.ValidateFile(write(d), "h1-review-a.schema.json"); err == nil {
			t.Errorf("%s: expected validation to fail", name)
		}
	}
	// A clean review IS valid when it says why.
	clean := stamp(map[string]any{"findings": []any{}, "empty_reason": "confined to a rename"})
	if err := v.ValidateFile(write(clean), "h1-review-a.schema.json"); err != nil {
		t.Errorf("an empty finding list with a reason must validate: %v", err)
	}
}

func TestH1ReviewBRequiresValidationForSurvivors(t *testing.T) {
	v := NewSchemaValidator(filepath.Join(repoRoot, "schemas"))
	mk := func(disposition string, withValidation bool) string {
		a := map[string]any{"finding_id": "f1", "discovery_mechanism": "M1", "disposition": disposition,
			"causal_chain": []any{map[string]any{"file": "x.c"}}}
		if disposition == "CONFIRMED" || disposition == "NARROWED" {
			a["mechanism"] = "M1"
		}
		if disposition == "REJECTED" {
			a["rejection_reason"] = "guarded"
		}
		if withValidation {
			a["recommended_validation"] = map[string]any{"kind": "new_test", "paths": []any{"t/x_test.c"}, "description": "s"}
		}
		doc := map[string]any{"schema_version": "h1-review-b/v1", "run_id": "r", "case_id": "c", "stage": "reasoner-2",
			"generated_at": "2026-09-15T00:00:00Z", "assessments": []any{a}}
		p := filepath.Join(t.TempDir(), "b.json")
		b, _ := json.Marshal(doc)
		_ = os.WriteFile(p, b, 0o644)
		return p
	}
	for _, d := range []string{"CONFIRMED", "NARROWED"} {
		if err := v.ValidateFile(mk(d, false), "h1-review-b.schema.json"); err == nil {
			t.Errorf("%s without recommended_validation must fail", d)
		}
		if err := v.ValidateFile(mk(d, true), "h1-review-b.schema.json"); err != nil {
			t.Errorf("%s with recommended_validation must pass: %v", d, err)
		}
	}
	for _, d := range []string{"REJECTED", "INCONCLUSIVE"} {
		if err := v.ValidateFile(mk(d, false), "h1-review-b.schema.json"); err != nil {
			t.Errorf("%s needs no recommended_validation: %v", d, err)
		}
	}
}
