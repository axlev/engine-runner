package orchestrator

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/axlev/engine-runner/internal/adapters"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "out.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	return path
}

func readDoc(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	return doc
}

func TestStampEnvelopeFillsIdentityFields(t *testing.T) {
	path := writeTemp(t, `{"findings":[]}`)
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	if err := stampEnvelope(path, "run-7", "case-9", adapters.StageReasoner1, now); err != nil {
		t.Fatalf("stampEnvelope: %v", err)
	}

	doc := readDoc(t, path)
	for field, want := range map[string]string{
		"schema_version": "review-a/v1",
		"run_id":         "run-7",
		"case_id":        "case-9",
		"stage":          "reasoner-1",
		"generated_at":   "2026-01-02T03:04:05Z",
	} {
		if doc[field] != want {
			t.Errorf("%s = %v, want %q", field, doc[field], want)
		}
	}
	if _, ok := doc["findings"]; !ok {
		t.Errorf("content key was lost")
	}
}

// TestStampEnvelopeOverwritesRatherThanFills is the point of the design: a
// reasoner must not be able to misstate its own identity even by emitting
// those fields deliberately.
func TestStampEnvelopeOverwritesRatherThanFills(t *testing.T) {
	path := writeTemp(t, `{"run_id":"attacker","case_id":"wrong","stage":"reasoner-3","schema_version":"bogus/v9","generated_at":"1999-01-01T00:00:00Z","findings":[]}`)

	if err := stampEnvelope(path, "real-run", "real-case", adapters.StageReasoner1, time.Now()); err != nil {
		t.Fatalf("stampEnvelope: %v", err)
	}

	doc := readDoc(t, path)
	if doc["run_id"] != "real-run" || doc["case_id"] != "real-case" {
		t.Errorf("model-supplied identity survived: run_id=%v case_id=%v", doc["run_id"], doc["case_id"])
	}
	if doc["stage"] != "reasoner-1" || doc["schema_version"] != "review-a/v1" {
		t.Errorf("model-supplied stage/schema survived: %v / %v", doc["stage"], doc["schema_version"])
	}
	if doc["generated_at"] == "1999-01-01T00:00:00Z" {
		t.Errorf("model-supplied timestamp survived")
	}
}

// TestStampEnvelopePreservesIntegersExactly guards the json.Number handling:
// without it a line number round-trips through float64 and re-marshals as
// 1e+06, which then fails the schemas' "integer" constraint.
func TestStampEnvelopePreservesIntegersExactly(t *testing.T) {
	path := writeTemp(t, `{"findings":[{"evidence":[{"file":"a.go","start_line":1000000,"end_line":1000004}],"confidence":0.6}]}`)

	if err := stampEnvelope(path, "r", "c", adapters.StageReasoner1, time.Now()); err != nil {
		t.Fatalf("stampEnvelope: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	for _, want := range []string{"1000000", "1000004", "0.6"} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("numeric literal %s was not preserved verbatim; got:\n%s", want, raw)
		}
	}
}

func TestStampEnvelopeRejectsUnparseableOutput(t *testing.T) {
	// A model that wrapped its JSON in a markdown fence, or was truncated,
	// must fail loudly here rather than be papered over.
	path := writeTemp(t, "```json\n{\"findings\":[]}\n```")
	if err := stampEnvelope(path, "r", "c", adapters.StageReasoner1, time.Now()); err == nil {
		t.Fatalf("expected an error for fenced/unparseable output")
	}
}

func TestStampEnvelopeRejectsUnknownStage(t *testing.T) {
	path := writeTemp(t, `{"findings":[]}`)
	if err := stampEnvelope(path, "r", "c", adapters.Stage("reasoner-99"), time.Now()); err == nil {
		t.Fatalf("expected an error for an unknown stage")
	}
}
