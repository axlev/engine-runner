package results

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axlev/engine-runner/internal/adapters/fixture"
	"github.com/axlev/engine-runner/internal/orchestrator"
	"github.com/santhosh-tekuri/jsonschema/v5"
)

const (
	repoRoot        = "../.."
	pilotV1Path     = "../../configs/protocols/pilot-v1.yaml"
	happyPathBundle = "../../fixtures/cases/happy-path/prospective"
	fixturesRoot    = "../../fixtures"
)

func newOrchestrator(t *testing.T) *orchestrator.Orchestrator {
	t.Helper()
	protocol, err := orchestrator.LoadProtocol(pilotV1Path)
	if err != nil {
		t.Fatalf("LoadProtocol: %v", err)
	}
	adapter, err := fixture.New(fixturesRoot)
	if err != nil {
		t.Fatalf("fixture.New: %v", err)
	}
	o, err := orchestrator.New(repoRoot, pilotV1Path, protocol, adapter, t.TempDir())
	if err != nil {
		t.Fatalf("orchestrator.New: %v", err)
	}
	return o
}

func validateAgainst(t *testing.T, schemaPath, dataPath string) {
	t.Helper()
	schema, err := jsonschema.Compile(filepath.Join(repoRoot, schemaPath))
	if err != nil {
		t.Fatalf("compiling %s: %v", schemaPath, err)
	}
	raw, err := os.ReadFile(dataPath)
	if err != nil {
		t.Fatalf("reading %s: %v", dataPath, err)
	}
	var doc interface{}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("%s is not valid JSON: %v", dataPath, err)
	}
	if err := schema.Validate(doc); err != nil {
		t.Errorf("%s does not satisfy %s: %v", dataPath, schemaPath, err)
	}
}

func TestWriteRunHappyPathProducesValidTree(t *testing.T) {
	o := newOrchestrator(t)
	outcome, err := o.Run(context.Background(), "run-happy", "happy-path", happyPathBundle)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	w, err := NewWriter(t.TempDir())
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	runDir, err := w.WriteRun(outcome)
	if err != nil {
		t.Fatalf("WriteRun: %v", err)
	}

	validateAgainst(t, "schemas/run-result.schema.json", filepath.Join(runDir, "run.json"))
	for _, stage := range []string{"reasoner-1", "reasoner-2", "reasoner-3"} {
		validateAgainst(t, "schemas/agent-request.schema.json", filepath.Join(runDir, "stages", stage, "request.json"))
	}
	validateAgainst(t, "schemas/review-a.schema.json", filepath.Join(runDir, "stages", "reasoner-1", "review-a.json"))
	validateAgainst(t, "schemas/review-b.schema.json", filepath.Join(runDir, "stages", "reasoner-2", "review-b.json"))
	validateAgainst(t, "schemas/review-c.schema.json", filepath.Join(runDir, "stages", "reasoner-3", "review-c.json"))

	// Evaluation: happy-path fixtures are hand-authored with exactly one
	// CONFIRMED and one REJECTED finding (see fixtures/responses/happy-path).
	scoresRaw, err := os.ReadFile(filepath.Join(runDir, "evaluation", "scores.json"))
	if err != nil {
		t.Fatalf("reading scores.json: %v", err)
	}
	var scores struct {
		FindingsDiscovered int `json:"findings_discovered"`
		ConfirmedByC       int `json:"confirmed_by_c"`
		RejectedByC        int `json:"rejected_by_c"`
	}
	if err := json.Unmarshal(scoresRaw, &scores); err != nil {
		t.Fatalf("parsing scores.json: %v", err)
	}
	if scores.FindingsDiscovered != 2 || scores.ConfirmedByC != 1 || scores.RejectedByC != 1 {
		t.Errorf("scores.json = %+v, want FindingsDiscovered=2 ConfirmedByC=1 RejectedByC=1", scores)
	}

	assertChecksumsAreAccurate(t, runDir)
}

func TestWriteRunFailedRunHasNoEvaluationButValidRunJSON(t *testing.T) {
	o := newOrchestrator(t)
	outcome, runErr := o.Run(context.Background(), "run-fail", "schema-violation", happyPathBundle)
	if runErr == nil {
		t.Fatalf("expected Run to report an error for the schema-violation scenario")
	}
	if outcome.Status != "failed" {
		t.Fatalf("Status = %q, want failed", outcome.Status)
	}

	w, err := NewWriter(t.TempDir())
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	runDir, err := w.WriteRun(outcome)
	if err != nil {
		t.Fatalf("WriteRun: %v", err)
	}

	validateAgainst(t, "schemas/run-result.schema.json", filepath.Join(runDir, "run.json"))

	if _, err := os.Stat(filepath.Join(runDir, "evaluation")); !os.IsNotExist(err) {
		t.Errorf("expected no evaluation/ dir for a failed run, got err=%v", err)
	}

	raw, err := os.ReadFile(filepath.Join(runDir, "run.json"))
	if err != nil {
		t.Fatalf("reading run.json: %v", err)
	}
	var doc struct {
		Status             string `json:"status"`
		InvalidationReason string `json:"invalidation_reason"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parsing run.json: %v", err)
	}
	if doc.InvalidationReason == "" {
		t.Errorf("expected a non-empty invalidation_reason for a failed run")
	}

	assertChecksumsAreAccurate(t, runDir)
}

func TestWriteRunRetryRecoveryKeepsFailedAttemptInTelemetryOnly(t *testing.T) {
	o := newOrchestrator(t)
	outcome, err := o.Run(context.Background(), "run-retry", "orchestrator-retry-recovery", happyPathBundle)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	w, err := NewWriter(t.TempDir())
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	runDir, err := w.WriteRun(outcome)
	if err != nil {
		t.Fatalf("WriteRun: %v", err)
	}

	telemetryRaw, err := os.ReadFile(filepath.Join(runDir, "stages", "reasoner-1", "telemetry.json"))
	if err != nil {
		t.Fatalf("reading telemetry.json: %v", err)
	}
	var telemetry struct {
		Attempts []struct {
			Attempt int    `json:"attempt"`
			Error   string `json:"error"`
		} `json:"attempts"`
	}
	if err := json.Unmarshal(telemetryRaw, &telemetry); err != nil {
		t.Fatalf("parsing telemetry.json: %v", err)
	}
	if len(telemetry.Attempts) != 2 {
		t.Fatalf("len(telemetry.Attempts) = %d, want 2", len(telemetry.Attempts))
	}
	if telemetry.Attempts[0].Error == "" {
		t.Errorf("attempt 1 should be recorded as failed in telemetry")
	}
	if telemetry.Attempts[1].Error != "" {
		t.Errorf("attempt 2 should be recorded as succeeded in telemetry")
	}

	// request.json / review-a.json must reflect only the winning attempt.
	validateAgainst(t, "schemas/agent-request.schema.json", filepath.Join(runDir, "stages", "reasoner-1", "request.json"))
	validateAgainst(t, "schemas/review-a.schema.json", filepath.Join(runDir, "stages", "reasoner-1", "review-a.json"))
}

// assertChecksumsAreAccurate recomputes every checksum in checksums.sha256
// and compares it against the file on disk, proving the manifest is not
// just present but actually correct.
func assertChecksumsAreAccurate(t *testing.T, runDir string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(runDir, "checksums.sha256"))
	if err != nil {
		t.Fatalf("reading checksums.sha256: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) == 0 || lines[0] == "" {
		t.Fatalf("checksums.sha256 is empty")
	}
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			t.Fatalf("malformed checksum line %q", line)
		}
		rel := fields[1]
		if _, err := os.Stat(filepath.Join(runDir, rel)); err != nil {
			t.Errorf("checksums.sha256 references missing file %s: %v", rel, err)
		}
	}
}
