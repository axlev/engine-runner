package fixture

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/axlev/engine-runner/internal/adapters"
)

// fixturesDir points at the repository's top-level fixtures/ directory
// (docs/system-design.md section 8.1), not a Go testdata directory: these
// fixtures are also meant to back full-engine tests once the orchestrator
// exists, not just this package's unit tests.
const fixturesDir = "../../../fixtures"

func newAdapter(t *testing.T) *FixtureAdapter {
	t.Helper()
	a, err := New(fixturesDir)
	if err != nil {
		t.Fatalf("New(%q): %v", fixturesDir, err)
	}
	return a
}

func request(caseID string, stage adapters.Stage, workspace string, env map[string]string) adapters.RunRequest {
	return adapters.RunRequest{
		RunID:         "test-run",
		CaseID:        caseID,
		Stage:         stage,
		WorkspacePath: workspace,
		PromptPath:    "prompts/pilot-v1/" + string(stage) + ".md",
		OutputSchema:  string(stage) + ".schema.json",
		Model:         "fixture-model",
		Budget:        adapters.Budget{MaxWallClockSeconds: 60},
		Environment:   env,
	}
}

func TestHappyPathAllStagesSucceed(t *testing.T) {
	a := newAdapter(t)
	stages := []adapters.Stage{adapters.StageReasoner1, adapters.StageReasoner2, adapters.StageReasoner3}
	wantUsage := []adapters.Usage{
		{InputTokens: 8200, OutputTokens: 950, ToolCalls: 4, CostUSD: 0.041},
		{InputTokens: 9100, OutputTokens: 1100, ToolCalls: 6, CostUSD: 0.052},
		{InputTokens: 7600, OutputTokens: 700, ToolCalls: 3, CostUSD: 0.037},
	}

	for i, stage := range stages {
		ws := t.TempDir()
		res, err := a.Run(context.Background(), request("happy-path", stage, ws, nil))
		if err != nil {
			t.Fatalf("stage %s: unexpected error: %v", stage, err)
		}
		if res.ExitCode != 0 {
			t.Errorf("stage %s: ExitCode = %d, want 0", stage, res.ExitCode)
		}
		if res.Usage != wantUsage[i] {
			t.Errorf("stage %s: Usage = %+v, want %+v", stage, res.Usage, wantUsage[i])
		}
		if res.Adapter != "fixture" || res.Version != "fixture/v1" {
			t.Errorf("stage %s: Adapter/Version = %q/%q, want fixture/fixture-v1", stage, res.Adapter, res.Version)
		}

		raw, err := os.ReadFile(res.OutputPath)
		if err != nil {
			t.Fatalf("stage %s: reading OutputPath %q: %v", stage, res.OutputPath, err)
		}
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatalf("stage %s: output is not valid JSON: %v", stage, err)
		}
		// Responses are content-only, as a real reasoner's are: the engine
		// stamps schema_version/run_id/case_id/stage/generated_at after the
		// adapter returns, so the adapter's own output carries none of them.
		contentKey := map[adapters.Stage]string{
			adapters.StageReasoner1: "findings",
			adapters.StageReasoner2: "assessments",
			adapters.StageReasoner3: "verdicts",
		}[stage]
		if _, ok := doc[contentKey]; !ok {
			t.Errorf("stage %s: output is missing its content key %q; got keys %v", stage, contentKey, keysOf(doc))
		}
		if _, stamped := doc["run_id"]; stamped {
			t.Errorf("stage %s: adapter output must not carry envelope fields; the engine stamps those", stage)
		}
	}
}

func keysOf(doc map[string]any) []string {
	out := make([]string, 0, len(doc))
	for k := range doc {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestInvalidJSONIsPassedThroughVerbatim(t *testing.T) {
	a := newAdapter(t)
	ws := t.TempDir()
	res, err := a.Run(context.Background(), request("invalid-json", adapters.StageReasoner1, ws, nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0 (the process succeeded; only its content is bad)", res.ExitCode)
	}
	raw, err := os.ReadFile(res.OutputPath)
	if err != nil {
		t.Fatalf("reading OutputPath: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err == nil {
		t.Fatalf("expected output to be invalid JSON, but it parsed cleanly: %s", raw)
	}
}

func TestSchemaViolationIsPassedThroughVerbatim(t *testing.T) {
	a := newAdapter(t)
	ws := t.TempDir()
	res, err := a.Run(context.Background(), request("schema-violation", adapters.StageReasoner1, ws, nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	raw, err := os.ReadFile(res.OutputPath)
	if err != nil {
		t.Fatalf("reading OutputPath: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("expected valid JSON that merely violates the schema, got parse error: %v", err)
	}
	// The violation is in the CONTENT, not the envelope: the engine stamps
	// the envelope after this point, so a fixture that only omitted
	// envelope fields would be silently repaired and stop testing anything.
	// Here the single finding is missing every required field except title.
	findings, ok := doc["findings"].([]any)
	if !ok || len(findings) != 1 {
		t.Fatalf("expected exactly one finding, got %v", doc["findings"])
	}
	finding, ok := findings[0].(map[string]any)
	if !ok {
		t.Fatalf("finding is not an object: %v", findings[0])
	}
	for _, required := range []string{"id", "description", "severity", "confidence", "evidence"} {
		if _, present := finding[required]; present {
			t.Errorf("fixture must omit %q to violate review-a.schema.json, but it was present", required)
		}
	}
}

func TestPartialOutputIsTruncated(t *testing.T) {
	a := newAdapter(t)
	ws := t.TempDir()
	res, err := a.Run(context.Background(), request("partial-output", adapters.StageReasoner2, ws, nil))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	raw, err := os.ReadFile(res.OutputPath)
	if err != nil {
		t.Fatalf("reading OutputPath: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err == nil {
		t.Fatalf("expected truncated JSON to fail to parse, but it parsed cleanly: %s", raw)
	}
}

func TestNonZeroExitReturnsError(t *testing.T) {
	a := newAdapter(t)
	ws := t.TempDir()
	res, err := a.Run(context.Background(), request("non-zero-exit", adapters.StageReasoner3, ws, nil))
	if err == nil {
		t.Fatalf("expected an error for a simulated non-zero exit, got nil")
	}
	if res.ExitCode != 1 {
		t.Errorf("ExitCode = %d, want 1", res.ExitCode)
	}
	if res.OutputPath != "" {
		t.Errorf("OutputPath = %q, want empty on a failed exit", res.OutputPath)
	}
}

func TestTimeoutReturnsPromptlyOnContextDeadline(t *testing.T) {
	a := newAdapter(t)
	ws := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	started := time.Now()
	_, err := a.Run(ctx, request("timeout", adapters.StageReasoner1, ws, nil))
	elapsed := time.Since(started)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	if elapsed > time.Second {
		t.Fatalf("Run took %s to return after its deadline; it must not block for the scenario's full 30s delay", elapsed)
	}
}

func TestCancellationReturnsPromptlyOnExplicitCancel(t *testing.T) {
	a := newAdapter(t)
	ws := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	started := time.Now()
	_, err := a.Run(ctx, request("cancellation", adapters.StageReasoner2, ws, nil))
	elapsed := time.Since(started)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if elapsed > time.Second {
		t.Fatalf("Run took %s to return after cancellation; it must not block for the scenario's full 30s delay", elapsed)
	}
}

func TestRetryThenSuccessAndIdempotency(t *testing.T) {
	a := newAdapter(t)

	ws1 := t.TempDir()
	_, err := a.Run(context.Background(), request("retry-then-success", adapters.StageReasoner1, ws1, map[string]string{
		adapters.AttemptEnvKey: "1",
	}))
	if err == nil {
		t.Fatalf("attempt 1: expected simulated failure, got nil error")
	}

	// Two separate "attempt 2" calls, with fresh workspaces, must produce
	// byte-identical output: FixtureAdapter's behavior is a pure function
	// of (scenario, stage, attempt), not of how many times it has been
	// called before.
	var outputs [][]byte
	for i := 0; i < 2; i++ {
		ws := t.TempDir()
		res, err := a.Run(context.Background(), request("retry-then-success", adapters.StageReasoner1, ws, map[string]string{
			adapters.AttemptEnvKey: "2",
		}))
		if err != nil {
			t.Fatalf("attempt 2 (call %d): unexpected error: %v", i, err)
		}
		raw, err := os.ReadFile(res.OutputPath)
		if err != nil {
			t.Fatalf("attempt 2 (call %d): reading output: %v", i, err)
		}
		outputs = append(outputs, raw)
	}
	if string(outputs[0]) != string(outputs[1]) {
		t.Errorf("repeated attempt-2 calls produced different output; FixtureAdapter must be deterministic")
	}
}

func TestCallsRecordsEveryRequestForIsolationAssertions(t *testing.T) {
	a := newAdapter(t)
	ws := t.TempDir()
	req := request("happy-path", adapters.StageReasoner1, ws, nil)

	if _, err := a.Run(context.Background(), req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	calls := a.Calls()
	if len(calls) != 1 {
		t.Fatalf("len(Calls()) = %d, want 1", len(calls))
	}
	if calls[0].WorkspacePath != ws {
		t.Errorf("recorded WorkspacePath = %q, want %q", calls[0].WorkspacePath, ws)
	}
	if calls[0].Stage != adapters.StageReasoner1 {
		t.Errorf("recorded Stage = %q, want %q", calls[0].Stage, adapters.StageReasoner1)
	}
}

func TestUnknownCaseIsAnError(t *testing.T) {
	a := newAdapter(t)
	ws := t.TempDir()
	_, err := a.Run(context.Background(), request("no-such-scenario", adapters.StageReasoner1, ws, nil))
	if err == nil {
		t.Fatalf("expected an error for an unregistered scenario, got nil")
	}
}

func TestUnknownStageForScenarioIsAnError(t *testing.T) {
	a := newAdapter(t)
	ws := t.TempDir()
	// "non-zero-exit" only defines behavior for reasoner-3.
	_, err := a.Run(context.Background(), request("non-zero-exit", adapters.StageReasoner1, ws, nil))
	if err == nil {
		t.Fatalf("expected an error for a stage the scenario does not define, got nil")
	}
}
