package orchestrator

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/axlev/engine-runner/internal/adapters"
)

func writeAgentFile(t *testing.T, dir, stage, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, stage+".yaml"), []byte(body), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
}

func TestLoadFixtureAgentSet(t *testing.T) {
	set, err := LoadAgentSet(fixtureAgentsDir)
	if err != nil {
		t.Fatalf("LoadAgentSet: %v", err)
	}
	for _, stage := range stageOrder {
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
	set, err := LoadAgentSet("../../configs/agents")
	if err != nil {
		t.Fatalf("LoadAgentSet: %v", err)
	}
	if got := set.Adapters(); len(got) != 1 || got[0] != "claude" {
		t.Errorf("Adapters() = %v, want exactly [claude]", got)
	}
}

func TestLoadAgentSetRejectsMissingStage(t *testing.T) {
	dir := t.TempDir()
	writeAgentFile(t, dir, "reasoner-1", "adapter: fixture\nmodel: m\n")
	writeAgentFile(t, dir, "reasoner-2", "adapter: fixture\nmodel: m\n")
	// reasoner-3 deliberately absent: a partial set would run two stages and
	// fail the third after spending money.
	if _, err := LoadAgentSet(dir); err == nil {
		t.Fatalf("expected an error for an agent set missing reasoner-3")
	}
}

func TestLoadAgentSetRejectsMissingAdapterOrModel(t *testing.T) {
	for name, body := range map[string]string{
		"no adapter": "model: m\n",
		"no model":   "adapter: fixture\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			for _, stage := range stageOrder {
				writeAgentFile(t, dir, string(stage), body)
			}
			if _, err := LoadAgentSet(dir); err == nil {
				t.Fatalf("expected an error for an agent config with %s", name)
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
		AgentsDir:     fixtureAgentsDir,
		AgentSet:      loadFixtureAgents(t),
		Adapters:      map[string]adapters.AgentAdapter{}, // none supplied
		WorkspaceRoot: t.TempDir(),
	})
	if err == nil {
		t.Fatalf("expected an error when no adapter was supplied for the set's stages")
	}
}
