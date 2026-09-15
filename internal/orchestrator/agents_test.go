package orchestrator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/axlev/engine-runner/internal/adapters"
)

// writeArm writes a throwaway agent set file and returns its path.
func writeArm(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "arm.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	return path
}

func TestLoadFixtureAgentSet(t *testing.T) {
	set, err := LoadAgentSet(fixtureAgentsDir)
	if err != nil {
		t.Fatalf("LoadAgentSet: %v", err)
	}
	for _, stage := range knownStages {
		cfg := set[stage]
		if cfg.Adapter != "fixture" {
			t.Errorf("%s: Adapter = %q, want fixture", stage, cfg.Adapter)
		}
		if cfg.Model == "" {
			t.Errorf("%s: Model is empty", stage)
		}
		if cfg.Budget.MaxCostUSD <= 0 {
			t.Errorf("%s: Budget.MaxCostUSD = %v, want a positive ceiling", stage, cfg.Budget.MaxCostUSD)
		}
	}
}

// TestLoadRealAgentSet: the committed configs/agents/ set must stay loadable
// even though it is not runnable yet (no adapter image exists). A malformed
// real agent set would otherwise only be discovered on the first live run.
func TestLoadRealAgentSet(t *testing.T) {
	for _, arm := range []string{"opus", "sonnet", "haiku", "fable"} {
		t.Run(arm, func(t *testing.T) {
			set, err := LoadAgentSet("../../configs/agents/" + arm + ".yaml")
			if err != nil {
				t.Fatalf("LoadAgentSet: %v", err)
			}
			if got := set.Adapters(); len(got) != 1 || got[0] != "claude" {
				t.Errorf("Adapters() = %v, want exactly [claude]", got)
			}
			// Every stage must inherit the arm's model and carry a spending
			// ceiling: an arm that silently left one unbounded would only
			// show up as an unexpected bill.
			for _, stage := range knownStages {
				if set[stage].Model == "" {
					t.Errorf("%s: model did not reach the stage", stage)
				}
				if set[stage].Budget.MaxCostUSD <= 0 {
					t.Errorf("%s: no cost ceiling", stage)
				}
			}
		})
	}
}

// TestArmDefaultsReachEveryStage pins the point of the one-file-per-arm
// format: adapter, model and effort are stated once and inherited, so a new
// arm is one changed line rather than three files kept in sync.
func TestArmDefaultsReachEveryStage(t *testing.T) {
	path := writeArm(t, `
adapter: fixture
model: shared-model
reasoning_level: high
budget:
  max_cost_usd: 1.5
stages:
  reasoner-1: {}
  reasoner-2: {}
  reasoner-3: {}
`)
	set, err := LoadAgentSet(path)
	if err != nil {
		t.Fatalf("LoadAgentSet: %v", err)
	}
	for _, stage := range knownStages {
		cfg := set[stage]
		if cfg.Model != "shared-model" || cfg.ReasoningLevel != "high" || cfg.Budget.MaxCostUSD != 1.5 {
			t.Errorf("%s did not inherit the arm defaults: %+v", stage, cfg)
		}
	}
}

// A per-stage override must win over the arm default - running discovery on
// one vendor and verification on another is a coherent experiment.
func TestPerStageOverrideWins(t *testing.T) {
	path := writeArm(t, `
adapter: fixture
model: shared-model
stages:
  reasoner-1: {}
  reasoner-2:
    adapter: claude
    model: other-model
    budget:
      max_cost_usd: 9.0
  reasoner-3: {}
`)
	set, err := LoadAgentSet(path)
	if err != nil {
		t.Fatalf("LoadAgentSet: %v", err)
	}
	if got := set[adapters.StageReasoner2]; got.Adapter != "claude" || got.Model != "other-model" || got.Budget.MaxCostUSD != 9.0 {
		t.Errorf("reasoner-2 override did not win: %+v", got)
	}
	if got := set[adapters.StageReasoner1].Model; got != "shared-model" {
		t.Errorf("reasoner-1 should keep the default, got %q", got)
	}
}

// A typo in a key must fail loudly. Silently ignoring "budgets:" would bind
// a run to defaults nobody chose, and the first symptom would be a bill.
func TestUnknownKeyIsRejected(t *testing.T) {
	path := writeArm(t, "adapter: fixture\nmodel: m\nbudgets:\n  max_cost_usd: 1\n")
	if _, err := LoadAgentSet(path); err == nil {
		t.Fatalf("expected an error for the misspelled key \"budgets\"")
	}
}

// A stage name that is not part of the protocol must fail rather than be
// silently dropped: it means the author believed they configured something.
func TestUnknownStageIsRejected(t *testing.T) {
	path := writeArm(t, "adapter: fixture\nmodel: m\nstages:\n  reasoner-9:\n    budget:\n      max_cost_usd: 1\n")
	if _, err := LoadAgentSet(path); err == nil {
		t.Fatalf("expected an error for unknown stage reasoner-9")
	}
}

// A stage that resolves to no adapter or model must fail up front: a
// partial set would run two stages and fail the third after spending money.
func TestLoadAgentSetRejectsStageWithoutBinding(t *testing.T) {
	path := writeArm(t, `
model: only-a-model
stages:
  reasoner-1:
    adapter: fixture
  reasoner-2:
    adapter: fixture
`)
	if _, err := LoadAgentSet(path); err == nil {
		t.Fatalf("expected an error: reasoner-3 resolves to no adapter")
	}
}

func TestLoadAgentSetRejectsMissingAdapterOrModel(t *testing.T) {
	for name, body := range map[string]string{
		"no adapter": "model: m\n",
		"no model":   "adapter: fixture\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := LoadAgentSet(writeArm(t, body)); err == nil {
				t.Fatalf("expected an error for an agent set with %s", name)
			}
		})
	}
}

// TestAdaptersDeduplicates: an agent set may bind stages to different
// vendors, and each distinct vendor needs exactly one constructed adapter.
func TestAdaptersDeduplicates(t *testing.T) {
	set := AgentSet{
		adapters.StageReasoner1: {Adapter: "claude"},
		adapters.StageReasoner2: {Adapter: "codex"},
		adapters.StageReasoner3: {Adapter: "claude"},
	}
	got := set.Adapters()
	if len(got) != 2 || got[0] != "claude" || got[1] != "codex" {
		t.Errorf("Adapters() = %v, want [claude codex] in stage order, deduplicated", got)
	}
}

// TestNewRejectsMissingAdapter: constructing an orchestrator whose agent set
// names an adapter the caller never supplied must fail up front, not part
// way through a run that has already spent money.
func TestNewRejectsMissingAdapter(t *testing.T) {
	_, err := New(Options{
		RepoRoot:      repoRoot,
		ProtocolPath:  pilotV1Path,
		Protocol:      loadPilotV1(t),
		AgentSetPath:  fixtureAgentsDir,
		AgentSet:      loadFixtureAgents(t),
		Adapters:      map[string]adapters.AgentAdapter{}, // none supplied
		WorkspaceRoot: t.TempDir(),
	})
	if err == nil {
		t.Fatalf("expected an error when no adapter was supplied for the set's stages")
	}
}
