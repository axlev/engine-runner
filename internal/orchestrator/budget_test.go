package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/axlev/engine-runner/internal/adapters"
)

// agentSetWithBudget writes a throwaway fixture-adapter agent set carrying a
// specific budget, so a test can vary one bound and see it enforced.
func agentSetWithBudget(t *testing.T, budgetYAML string) string {
	t.Helper()
	dir := t.TempDir()
	for _, stage := range stageOrder {
		body := "adapter: fixture\nmodel: fixture-model\nreasoning_level: standard\n" + budgetYAML
		if err := os.WriteFile(filepath.Join(dir, string(stage)+".yaml"), []byte(body), 0o644); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}
	return dir
}

func orchWithAgents(t *testing.T, agentsDir string, warn func(string)) *Orchestrator {
	t.Helper()
	set, err := LoadAgentSet(agentsDir)
	if err != nil {
		t.Fatalf("LoadAgentSet: %v", err)
	}
	o, err := New(Options{
		RepoRoot:      repoRoot,
		ProtocolPath:  pilotV1Path,
		Protocol:      loadPilotV1(t),
		AgentsDir:     agentsDir,
		AgentSet:      set,
		Adapters:      map[string]adapters.AgentAdapter{"fixture": newFixtureAdapter(t)},
		WorkspaceRoot: t.TempDir(),
		Warn:          warn,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return o
}

// TestWallClockBudgetActuallyStopsAHangingStage is the headline of issue 3.
// Neither vendor CLI has a timeout flag, so if the orchestrator does not
// enforce this the bound is decorative and a hung stage runs until something
// else gives up. The "timeout" fixture scenario sleeps 30s; a 1s budget must
// end it in about a second.
func TestWallClockBudgetActuallyStopsAHangingStage(t *testing.T) {
	agentsDir := agentSetWithBudget(t, "budget:\n  max_wall_clock_seconds: 1\n")
	o := orchWithAgents(t, agentsDir, nil)

	started := time.Now()
	outcome, err := o.Run(context.Background(), "run-timeout", "timeout", happyPathBundle)
	elapsed := time.Since(started)

	if err == nil {
		t.Fatalf("expected the run to fail on its wall-clock budget")
	}
	if elapsed > 10*time.Second {
		t.Fatalf("took %s; the 1s budget did not stop the scenario's 30s hang", elapsed)
	}
	if outcome.Status != "failed" {
		t.Errorf("Status = %q, want failed", outcome.Status)
	}
	if !strings.Contains(outcome.FailureReason, "deadline") && !strings.Contains(outcome.FailureReason, "context") {
		t.Errorf("FailureReason = %q, want it to name the deadline", outcome.FailureReason)
	}
}

// TestNoWallClockBudgetMeansNoDeadline: a zero bound must not silently
// become an immediate timeout.
func TestNoWallClockBudgetMeansNoDeadline(t *testing.T) {
	agentsDir := agentSetWithBudget(t, "budget:\n  max_cost_usd: 1.0\n")
	o := orchWithAgents(t, agentsDir, nil)

	outcome, err := o.Run(context.Background(), "run-nodeadline", "happy-path", happyPathBundle)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if outcome.Status != "completed" {
		t.Errorf("Status = %q, want completed", outcome.Status)
	}
}

// TestUsageOverrunFailsTheStage: the token and tool-call bounds have no flag
// on either CLI, so they can only be checked after the fact. The happy-path
// fixture reports 950 output tokens for reasoner-1; a 100 bound must fail it.
func TestUsageOverrunFailsTheStage(t *testing.T) {
	agentsDir := agentSetWithBudget(t, "budget:\n  max_output_tokens: 100\n")
	o := orchWithAgents(t, agentsDir, nil)

	outcome, err := o.Run(context.Background(), "run-overrun", "happy-path", happyPathBundle)
	if err == nil {
		t.Fatalf("expected the run to fail on its output-token budget")
	}
	if !strings.Contains(outcome.FailureReason, "max_output_tokens") {
		t.Errorf("FailureReason = %q, want it to name the exceeded bound", outcome.FailureReason)
	}
}

// TestUsageWithinBudgetPasses guards the check from being trivially strict.
func TestUsageWithinBudgetPasses(t *testing.T) {
	agentsDir := agentSetWithBudget(t, "budget:\n  max_output_tokens: 5000\n  max_tool_calls: 50\n  max_cost_usd: 10.0\n")
	o := orchWithAgents(t, agentsDir, nil)

	outcome, err := o.Run(context.Background(), "run-within", "happy-path", happyPathBundle)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if outcome.Status != "completed" {
		t.Errorf("Status = %q, want completed", outcome.Status)
	}
}

func TestCheckUsageAgainstBudget(t *testing.T) {
	budget := BudgetConfig{MaxInputTokens: 1000, MaxOutputTokens: 500, MaxToolCalls: 10, MaxCostUSD: 1.0}

	t.Run("within bounds", func(t *testing.T) {
		err, unavail := checkUsageAgainstBudget("claude", budget,
			adapters.Usage{InputTokens: 900, OutputTokens: 400, ToolCalls: 9, CostUSD: 0.9})
		if err != nil || unavail != nil {
			t.Errorf("err=%v unavailable=%v, want both nil", err, unavail)
		}
	})

	for name, usage := range map[string]adapters.Usage{
		"max_input_tokens":  {InputTokens: 1001, OutputTokens: 1},
		"max_output_tokens": {InputTokens: 1, OutputTokens: 501},
		"max_tool_calls":    {InputTokens: 1, ToolCalls: 11},
		"max_cost_usd":      {InputTokens: 1, CostUSD: 1.5},
	} {
		t.Run("exceeds "+name, func(t *testing.T) {
			err, _ := checkUsageAgainstBudget("claude", budget, usage)
			if err == nil {
				t.Fatalf("expected an error for exceeding %s", name)
			}
			if !strings.Contains(err.Error(), name) {
				t.Errorf("error = %q, want it to name %s", err, name)
			}
		})
	}

	// A non-instrumented adapter (codex reports nothing today) must surface
	// as explicitly uncheckable, never as a silent pass.
	t.Run("no usage reported", func(t *testing.T) {
		err, unavail := checkUsageAgainstBudget("codex", budget, adapters.Usage{})
		if err != nil {
			t.Errorf("err = %v, want nil - absent usage is not an overrun", err)
		}
		if unavail == nil {
			t.Fatalf("expected the bounds to be reported as uncheckable")
		}
		if unavail.Adapter != "codex" || len(unavail.Bounds) != 4 {
			t.Errorf("unavailable = %+v, want codex with all four declared bounds", unavail)
		}
	})

	t.Run("no bounds declared", func(t *testing.T) {
		err, unavail := checkUsageAgainstBudget("codex", BudgetConfig{}, adapters.Usage{})
		if err != nil || unavail != nil {
			t.Errorf("err=%v unavailable=%v, want both nil when nothing was declared", err, unavail)
		}
	})
}

// TestUnavailableUsageWarnsRatherThanPassingSilently: a bound enforced on one
// vendor and skipped on another must be visible, or the protocol's limits
// look stronger than they are.
func TestUnavailableUsageWarnsRatherThanPassingSilently(t *testing.T) {
	var warnings []string
	agentsDir := agentSetWithBudget(t, "budget:\n  max_output_tokens: 5000\n")
	o := orchWithAgents(t, agentsDir, func(msg string) { warnings = append(warnings, msg) })

	// "no-usage" reports zero usage across the board, as codex does today.
	if _, err := o.Run(context.Background(), "run-nousage", "no-usage", happyPathBundle); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(warnings) == 0 {
		t.Fatalf("expected a warning that the bound could not be checked")
	}
	if !strings.Contains(warnings[0], "max_output_tokens") {
		t.Errorf("warning = %q, want it to name the unchecked bound", warnings[0])
	}
}

// TestCancelledRunDoesNotRetry pins the money-losing half of the interrupt
// bug. The retry loop treated every adapter error alike, so once Ctrl+C
// cancelled the run the stage opened ANOTHER vendor call under a context
// that was already dead. An attempt that exhausts its own wall-clock budget
// must still retry - a different condition, covered by
// TestWallClockBudgetActuallyStopsAHangingStage above.
func TestCancelledRunDoesNotRetry(t *testing.T) {
	// No wall-clock bound, so cancellation is the only thing that can end
	// this run: a second attempt would prove the bug rather than a timeout.
	// The "timeout" scenario sleeps 30s, leaving room to cancel mid-flight.
	agentsDir := agentSetWithBudget(t, "budget:\n  max_cost_usd: 1.0\n")
	o := orchWithAgents(t, agentsDir, nil)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(300 * time.Millisecond)
		cancel()
	}()

	started := time.Now()
	outcome, err := o.Run(ctx, "run-cancelled", "timeout", happyPathBundle)
	elapsed := time.Since(started)

	if err == nil {
		t.Fatalf("expected the interrupted run to fail")
	}
	// Two attempts of a 30s scenario would take ~60s; one takes ~0.3s.
	if elapsed > 20*time.Second {
		t.Errorf("took %s, which means a second attempt ran after cancellation", elapsed)
	}

	attempts := 0
	for _, rec := range outcome.Attempts {
		if rec.Stage == adapters.StageReasoner1 {
			attempts++
		}
	}
	if attempts != 1 {
		t.Errorf("reasoner-1 recorded %d attempts, want exactly 1: a cancelled run must not open another vendor call", attempts)
	}
}
