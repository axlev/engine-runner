package results

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axlev/engine-runner/internal/adapters"
	"github.com/axlev/engine-runner/internal/adapters/fixture"
	"github.com/axlev/engine-runner/internal/orchestrator"
	"github.com/santhosh-tekuri/jsonschema/v5"
)

const (
	repoRoot         = "../.."
	pilotV1Path      = "../../configs/protocols/pilot-v1.yaml"
	happyPathBundle  = "../../fixtures/cases/happy-path/prospective"
	fixturesRoot     = "../../fixtures"
	fixtureAgentsDir = "../../fixtures/agents/fixture.yaml"
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
	agentSet, err := orchestrator.LoadAgentSet(fixtureAgentsDir)
	if err != nil {
		t.Fatalf("LoadAgentSet: %v", err)
	}
	o, err := orchestrator.New(orchestrator.Options{
		RepoRoot:      repoRoot,
		ProtocolPath:  pilotV1Path,
		Protocol:      protocol,
		AgentSetPath:  fixtureAgentsDir,
		AgentSet:      agentSet,
		Adapters:      map[string]adapters.AgentAdapter{"fixture": adapter},
		WorkspaceRoot: t.TempDir(),
	})
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

// TestWriteRunInvalidatedByBoundaryValidation covers the case where the
// only thing that happened was rejection: no stage ran, so the boundary
// report is the entire record of the run, and run.json must still be a
// schema-valid artifact carrying status "invalidated" - distinct from a
// plain execution "failed".
func TestWriteRunInvalidatedByBoundaryValidation(t *testing.T) {
	// Contaminate a copy of the clean bundle with an oracle-shaped file.
	bundle := t.TempDir()
	if err := filepath.WalkDir(happyPathBundle, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(happyPathBundle, path)
		if err != nil {
			return err
		}
		target := filepath.Join(bundle, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	}); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "reviewer", "ground_truth.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	o := newOrchestrator(t)
	outcome, runErr := o.Run(context.Background(), "run-invalidated", "happy-path", bundle)
	if runErr == nil {
		t.Fatalf("expected the contaminated bundle to be rejected")
	}
	if outcome.Status != "invalidated" {
		t.Fatalf("Status = %q, want invalidated", outcome.Status)
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
	validateAgainst(t, "schemas/boundary-validation.schema.json", filepath.Join(runDir, "boundary-validation.json"))

	raw, err := os.ReadFile(filepath.Join(runDir, "run.json"))
	if err != nil {
		t.Fatalf("reading run.json: %v", err)
	}
	var doc struct {
		Status             string `json:"status"`
		InvalidationReason string `json:"invalidation_reason"`
		Stages             []any  `json:"stages"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parsing run.json: %v", err)
	}
	if doc.Status != "invalidated" {
		t.Errorf("run.json status = %q, want \"invalidated\"", doc.Status)
	}
	if doc.InvalidationReason == "" {
		t.Errorf("run.json is missing invalidation_reason")
	}
	if len(doc.Stages) != 0 {
		t.Errorf("run.json lists %d stage(s); an invalidated case ran none", len(doc.Stages))
	}

	if _, err := os.Stat(filepath.Join(runDir, "stages")); !os.IsNotExist(err) {
		t.Errorf("expected no stages/ directory for an invalidated case, got err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(runDir, "evaluation")); !os.IsNotExist(err) {
		t.Errorf("expected no evaluation/ directory for an invalidated case, got err=%v", err)
	}

	assertChecksumsAreAccurate(t, runDir)
}

// TestPromptIsPreservedVerbatimInResult pins the property that makes prompt
// experiments interpretable: the sealed result carries the prompt's actual
// bytes, not just its hash. Comparing two arms is only possible if each run
// says what it asked, without needing the repository at the right commit.
func TestPromptIsPreservedVerbatimInResult(t *testing.T) {
	o := newOrchestrator(t)
	outcome, err := o.Run(context.Background(), "run-prompt", "happy-path", happyPathBundle)
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

	// Each stage has its own prompt under the protocol, so compare per stage
	// rather than against one shared file.
	for _, stage := range []string{"reasoner-1", "reasoner-2", "reasoner-3"} {
		want, err := os.ReadFile(filepath.Join(repoRoot, "prompts/pilot-v1", stage+".md"))
		if err != nil {
			t.Fatalf("%s: reading the protocol's prompt file: %v", stage, err)
		}
		got, err := os.ReadFile(filepath.Join(runDir, "stages", stage, "prompt.md"))
		if err != nil {
			t.Fatalf("%s: prompt not preserved in result: %v", stage, err)
		}
		if string(got) != string(want) {
			t.Errorf("%s: preserved prompt differs from the protocol's prompt file", stage)
		}
	}

	raw, err := os.ReadFile(filepath.Join(runDir, "checksums.sha256"))
	if err != nil {
		t.Fatalf("reading checksums: %v", err)
	}
	if !strings.Contains(string(raw), "stages/reasoner-1/prompt.md") {
		t.Errorf("preserved prompt is not covered by checksums.sha256")
	}
}

// TestPromptIsPreservedForAFailedStage: a stage that never produced usable
// output still has to record what it was asked, or the failure cannot be
// interpreted later.
func TestPromptIsPreservedForAFailedStage(t *testing.T) {
	o := newOrchestrator(t)
	outcome, runErr := o.Run(context.Background(), "run-prompt-fail", "schema-violation", happyPathBundle)
	if runErr == nil {
		t.Fatalf("expected the schema-violation scenario to fail")
	}

	w, err := NewWriter(t.TempDir())
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	runDir, err := w.WriteRun(outcome)
	if err != nil {
		t.Fatalf("WriteRun: %v", err)
	}

	if _, err := os.Stat(filepath.Join(runDir, "stages", "reasoner-1", "prompt.md")); err != nil {
		t.Errorf("a failed stage must still preserve its prompt: %v", err)
	}
}

// TestAuthModeValidatesForEveryVendorValue guards a trap in adding an
// enum-constrained optional field to a strict (additionalProperties:false)
// object: the field is populated by two adapters that use DIFFERENT value
// sets. claude reports api_key/oauth_token, codex api_key/access_token. An
// enum covering only claude's pair would have made every codex run fail
// validation at seal time - after the money was spent.
func TestAuthModeValidatesForEveryVendorValue(t *testing.T) {
	schema, err := jsonschema.Compile(filepath.Join(repoRoot, "schemas/run-result.schema.json"))
	if err != nil {
		t.Fatalf("compiling schema: %v", err)
	}

	stageRow := func(authMode interface{}) map[string]interface{} {
		row := map[string]interface{}{
			"stage": "reasoner-1", "adapter": "claude",
			"attempts": 1, "final_exit_code": 0,
			"usage": map[string]interface{}{},
		}
		if authMode != nil {
			row["auth_mode"] = authMode
		}
		return row
	}
	doc := func(authMode interface{}) interface{} {
		return map[string]interface{}{
			"schema_version": "run-result/v1",
			"run_id":         "r", "case_id": "c",
			"protocol_version": "pilot-v1",
			"created_at":       "2026-01-01T00:00:00Z",
			"sealed_at":        "2026-01-01T00:00:00Z",
			"status":           "completed",
			"fingerprints": map[string]interface{}{
				"protocol_hash":    "sha256:0",
				"schema_versions":  map[string]interface{}{},
				"prompt_hashes":    map[string]interface{}{},
				"adapter_versions": map[string]interface{}{},
				"input_checksums":  map[string]interface{}{},
			},
			"stages": []interface{}{stageRow(authMode)},
		}
	}

	// Every value an adapter can actually emit, plus absence.
	for _, valid := range []interface{}{"api_key", "oauth_token", "access_token", nil} {
		if err := schema.Validate(doc(valid)); err != nil {
			t.Errorf("auth_mode=%v must validate as run-result/v1: %v", valid, err)
		}
	}

	// A typo must not slip through as free-text provenance.
	if err := schema.Validate(doc("subscription")); err == nil {
		t.Errorf("auth_mode=%q should be rejected; the enum is what makes the field trustworthy", "subscription")
	}
}
