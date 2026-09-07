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
)

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
	if p.Adapter != "fixture" {
		t.Errorf("Adapter = %q, want fixture", p.Adapter)
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

	o, err := New(repoRoot, protocol, adapter, work)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

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
}

func TestOrchestratorRetryRecoversMidRun(t *testing.T) {
	protocol := loadPilotV1(t)
	adapter := newFixtureAdapter(t)
	work := t.TempDir()

	o, err := New(repoRoot, protocol, adapter, work)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

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

	o, err := New(repoRoot, protocol, adapter, work)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

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

	o, err := New(repoRoot, &disabled, adapter, work)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

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
