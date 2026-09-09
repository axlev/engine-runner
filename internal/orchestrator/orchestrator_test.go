package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axlev/engine-runner/internal/adapters"
	"github.com/axlev/engine-runner/internal/adapters/fixture"
)

const (
	repoRoot         = "../.."
	pilotV1Path      = "../../configs/protocols/pilot-v1.yaml"
	happyPathBundle  = "../../fixtures/cases/happy-path/prospective"
	fixturesRootPath = "../../fixtures"
	fixtureAgentsDir = "../../fixtures/agents/fixture.yaml"
)

func loadFixtureAgents(t *testing.T) AgentSet {
	t.Helper()
	set, err := LoadAgentSet(fixtureAgentsDir)
	if err != nil {
		t.Fatalf("LoadAgentSet(%q): %v", fixtureAgentsDir, err)
	}
	return set
}

// newOrch wires an orchestrator over the fixture agent set - the smoke-test
// vendor binding - leaving the protocol free to be whatever the test needs.
func newOrch(t *testing.T, protocol *Protocol, adapter adapters.AgentAdapter, workspaceRoot string) *Orchestrator {
	t.Helper()
	o, err := New(Options{
		RepoRoot:      repoRoot,
		ProtocolPath:  pilotV1Path,
		Protocol:      protocol,
		AgentSetPath:  fixtureAgentsDir,
		AgentSet:      loadFixtureAgents(t),
		Adapters:      map[string]adapters.AgentAdapter{"fixture": adapter},
		WorkspaceRoot: workspaceRoot,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return o
}

func loadPilotV1(t *testing.T) *Protocol {
	t.Helper()
	p, err := LoadProtocol(pilotV1Path)
	if err != nil {
		t.Fatalf("LoadProtocol(%q): %v", pilotV1Path, err)
	}
	return p
}

func newFixtureAdapter(t *testing.T) adapters.AgentAdapter {
	t.Helper()
	a, err := fixture.New(fixturesRootPath)
	if err != nil {
		t.Fatalf("fixture.New: %v", err)
	}
	return a
}

func TestLoadPilotV1Protocol(t *testing.T) {
	p := loadPilotV1(t)
	if p.Version != "pilot-v1" {
		t.Errorf("Version = %q, want pilot-v1", p.Version)
	}
	if !p.Handoffs.EnableAToB {
		t.Errorf("Handoffs.EnableAToB = false, want true (section 6.4: pilot v1 enables this handoff)")
	}
	if p.RetryPolicy.MaxAttempts != 2 {
		t.Errorf("RetryPolicy.MaxAttempts = %d, want 2", p.RetryPolicy.MaxAttempts)
	}
	for _, stage := range []string{"reasoner-1", "reasoner-2", "reasoner-3"} {
		if _, ok := p.Stages[stage]; !ok {
			t.Errorf("missing stage %q", stage)
		}
	}
}

func TestLoadProtocolRejectsMissingStage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.yaml")
	content := `
protocol_version: broken-v1
adapter: fixture
retry_policy:
  max_attempts: 1
stages:
  reasoner-1:
    prompt: p.md
    model: m
    output_schema: s.json
  reasoner-2:
    prompt: p.md
    model: m
    output_schema: s.json
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if _, err := LoadProtocol(path); err == nil {
		t.Fatalf("expected an error for a protocol missing reasoner-3")
	}
}

func TestOrchestratorHappyPathEndToEnd(t *testing.T) {
	protocol := loadPilotV1(t)
	adapter := newFixtureAdapter(t)
	work := t.TempDir()

	o := newOrch(t, protocol, adapter, work)

	outcome, err := o.Run(context.Background(), "run-happy", "happy-path", happyPathBundle)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if outcome.Status != "completed" {
		t.Fatalf("Status = %q, want completed (reason: %s)", outcome.Status, outcome.FailureReason)
	}
	if len(outcome.Attempts) != 3 {
		t.Fatalf("len(Attempts) = %d, want 3 (one per stage, no retries needed)", len(outcome.Attempts))
	}
	for _, rec := range outcome.Attempts {
		if rec.Err != "" {
			t.Errorf("stage %s attempt %d recorded an error in the happy path: %s", rec.Stage, rec.Attempt, rec.Err)
		}
		if rec.Attempt != 1 {
			t.Errorf("stage %s: Attempt = %d, want 1", rec.Stage, rec.Attempt)
		}
	}
	for _, p := range []string{outcome.ReviewAPath, outcome.ReviewBPath, outcome.ReviewCPath} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected output file to exist at %s: %v", p, err)
		}
	}

	fp := outcome.Fingerprints
	if fp.ProtocolHash == "" {
		t.Errorf("Fingerprints.ProtocolHash is empty")
	}
	for _, stage := range []string{"reasoner-1", "reasoner-2", "reasoner-3"} {
		if fp.PromptHashes[stage] == "" {
			t.Errorf("Fingerprints.PromptHashes[%q] is empty", stage)
		}
		if fp.AdapterVersions[stage] != "fixture/fixture/v1" {
			t.Errorf("Fingerprints.AdapterVersions[%q] = %q, want \"fixture/fixture/v1\"", stage, fp.AdapterVersions[stage])
		}
	}
	if len(fp.InputChecksums) != 6 {
		t.Errorf("len(Fingerprints.InputChecksums) = %d, want 6 (matching the happy-path bundle's checksums.sha256)", len(fp.InputChecksums))
	}
}

func TestOrchestratorRetryRecoversMidRun(t *testing.T) {
	protocol := loadPilotV1(t)
	adapter := newFixtureAdapter(t)
	work := t.TempDir()

	o := newOrch(t, protocol, adapter, work)

	outcome, err := o.Run(context.Background(), "run-retry", "orchestrator-retry-recovery", happyPathBundle)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if outcome.Status != "completed" {
		t.Fatalf("Status = %q, want completed (reason: %s)", outcome.Status, outcome.FailureReason)
	}

	var reasoner1Attempts []StageAttemptRecord
	for _, rec := range outcome.Attempts {
		if rec.Stage == adapters.StageReasoner1 {
			reasoner1Attempts = append(reasoner1Attempts, rec)
		}
	}
	if len(reasoner1Attempts) != 2 {
		t.Fatalf("reasoner-1 made %d attempt(s), want 2 (fail then recover)", len(reasoner1Attempts))
	}
	if reasoner1Attempts[0].Err == "" {
		t.Errorf("reasoner-1 attempt 1 should have failed, but recorded no error")
	}
	if reasoner1Attempts[1].Err != "" {
		t.Errorf("reasoner-1 attempt 2 should have succeeded, got error: %s", reasoner1Attempts[1].Err)
	}
	if len(outcome.Attempts) != 4 {
		t.Fatalf("len(Attempts) = %d, want 4 (2 for reasoner-1, 1 each for reasoner-2/3)", len(outcome.Attempts))
	}
}

func TestOrchestratorFailsFastAndStopsAfterExhaustingRetries(t *testing.T) {
	protocol := loadPilotV1(t)
	adapter := newFixtureAdapter(t)
	work := t.TempDir()

	o := newOrch(t, protocol, adapter, work)

	// "schema-violation" only defines behavior for reasoner-1, on purpose:
	// if the orchestrator incorrectly proceeded to reasoner-2 after
	// reasoner-1 permanently fails, it would blow up for the wrong reason
	// (no fixture behavior for that stage) instead of the failure this test
	// actually checks for.
	outcome, err := o.Run(context.Background(), "run-fail", "schema-violation", happyPathBundle)
	if err == nil {
		t.Fatalf("expected Run to return an error")
	}
	if outcome.Status != "failed" {
		t.Fatalf("Status = %q, want failed", outcome.Status)
	}
	if !strings.Contains(outcome.FailureReason, "does not satisfy") {
		t.Errorf("FailureReason = %q, want it to mention schema validation", outcome.FailureReason)
	}

	if len(outcome.Attempts) != 2 {
		t.Fatalf("len(Attempts) = %d, want exactly 2 (both reasoner-1 attempts, protocol max_attempts=2)", len(outcome.Attempts))
	}
	for _, rec := range outcome.Attempts {
		if rec.Stage != adapters.StageReasoner1 {
			t.Errorf("recorded an attempt for %s; orchestrator must stop after reasoner-1 fails permanently", rec.Stage)
		}
		if rec.Err == "" {
			t.Errorf("attempt %d should be recorded as failed", rec.Attempt)
		}
	}
}

func TestOrchestratorReasoner2NeverSeesReviewAWhenHandoffDisabled(t *testing.T) {
	protocol := loadPilotV1(t)
	disabled := *protocol
	disabled.Handoffs = HandoffConfig{EnableAToB: false}

	adapter := newFixtureAdapter(t)
	work := t.TempDir()

	o := newOrch(t, &disabled, adapter, work)

	outcome, err := o.Run(context.Background(), "run-no-handoff", "happy-path", happyPathBundle)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if outcome.Status != "completed" {
		t.Fatalf("Status = %q, want completed", outcome.Status)
	}

	for _, rec := range outcome.Attempts {
		if rec.Stage != adapters.StageReasoner2 && rec.Stage != adapters.StageReasoner3 {
			continue
		}
		if _, err := os.Stat(filepath.Join(rec.Request.WorkspacePath, "input", "handoff", "review-a.json")); !os.IsNotExist(err) {
			t.Errorf("%s workspace must not contain review-a.json when EnableAToB is false, got err=%v", rec.Stage, err)
		}
	}
}

// contaminatedBundle copies the clean fixture bundle and plants an
// oracle-shaped file in it, the way a real mistake would.
func contaminatedBundle(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.WalkDir(happyPathBundle, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(happyPathBundle, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatalf("copying bundle: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dst, "reviewer", "repository", "oracle.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	return dst
}

// TestBoundaryValidationRejectsBeforeAnyAdapterCall is the concrete proof of
// section 12's "a boundary-validation failure invalidates the case before
// LLM cost is incurred": the run is invalidated with zero recorded attempts,
// and the FixtureAdapter - which records every call it receives - was never
// invoked at all.
func TestBoundaryValidationRejectsBeforeAnyAdapterCall(t *testing.T) {
	protocol := loadPilotV1(t)
	adapter, err := fixture.New(fixturesRootPath)
	if err != nil {
		t.Fatalf("fixture.New: %v", err)
	}

	o := newOrch(t, protocol, adapter, t.TempDir())

	outcome, runErr := o.Run(context.Background(), "run-contaminated", "happy-path", contaminatedBundle(t))
	if runErr == nil {
		t.Fatalf("expected an error for a contaminated bundle")
	}
	if outcome.Status != "invalidated" {
		t.Errorf("Status = %q, want \"invalidated\" (distinct from a plain execution failure)", outcome.Status)
	}
	if len(outcome.Attempts) != 0 {
		t.Errorf("len(Attempts) = %d, want 0: no stage may run after validation fails", len(outcome.Attempts))
	}
	if len(adapter.Calls()) != 0 {
		t.Errorf("adapter received %d call(s); it must never be invoked for an invalidated case", len(adapter.Calls()))
	}
	if outcome.BoundaryValidation == nil || outcome.BoundaryValidation.Passed() {
		t.Errorf("expected a failing boundary-validation report on the outcome")
	}
	if outcome.FailureReason == "" {
		t.Errorf("expected FailureReason to carry the violation summary")
	}
}

func TestBoundaryValidationReportIsAttachedOnSuccessToo(t *testing.T) {
	protocol := loadPilotV1(t)
	o := newOrch(t, protocol, newFixtureAdapter(t), t.TempDir())
	outcome, err := o.Run(context.Background(), "run-clean", "happy-path", happyPathBundle)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if outcome.BoundaryValidation == nil || !outcome.BoundaryValidation.Passed() {
		t.Errorf("expected a passing boundary-validation report to be attached to a completed run")
	}
}
