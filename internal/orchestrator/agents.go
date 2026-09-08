package orchestrator

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/axlev/engine-runner/internal/adapters"
	"gopkg.in/yaml.v3"
)

// AgentConfig is one reasoner's vendor binding: which adapter runs it, on
// which model, with what effort and bounds.
//
// This is deliberately separate from the protocol (docs/system-design.md
// section 4.2's configs/agents/ vs configs/protocols/). The protocol
// describes the experiment - prompts, handoffs, stopping rules - and varies
// when the experiment changes. The vendor binding varies independently: the
// same frozen cohort should be runnable through Claude, through Codex, or
// through the fixture adapter for a smoke test, without minting a new
// protocol version each time.
//
// reasoning_level and budget live here because both are model-relative.
// Effort is vendor-specific vocabulary (--effort for claude, a
// model_reasoning_effort config override for codex), and a cost ceiling only
// means something against a particular model's pricing, as a token ceiling
// does against its context window. They are safety rails on a vendor, not
// experimental variables of the cohort.
type AgentConfig struct {
	Adapter        string       `yaml:"adapter"`
	Model          string       `yaml:"model"`
	ReasoningLevel string       `yaml:"reasoning_level"`
	Budget         BudgetConfig `yaml:"budget"`
}

// AgentSet is the vendor binding for every stage of a run, loaded from one
// directory.
type AgentSet map[adapters.Stage]AgentConfig

// LoadAgentSet reads <dir>/reasoner-{1,2,3}.yaml. File names are resolved
// implicitly from the stage names, matching section 4.2's layout, so a
// protocol never names an agent file and stays free of vendor detail.
//
// Every stage must be present: a partially-specified agent set would run
// some stages and fail others midway, after spending money.
func LoadAgentSet(dir string) (AgentSet, error) {
	set := AgentSet{}
	for _, stage := range stageOrder {
		path := filepath.Join(dir, string(stage)+".yaml")
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("orchestrator: reading agent config %s: %w", path, err)
		}
		var cfg AgentConfig
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("orchestrator: parsing agent config %s: %w", path, err)
		}
		if cfg.Adapter == "" {
			return nil, fmt.Errorf("orchestrator: agent config %s is missing adapter", path)
		}
		if cfg.Model == "" {
			return nil, fmt.Errorf("orchestrator: agent config %s is missing model", path)
		}
		set[stage] = cfg
	}
	return set, nil
}

// Adapters returns the distinct adapter names this set requires. A set may
// legitimately name different adapters per stage - discovery on one vendor,
// verification on another is a coherent experiment - so callers construct
// one adapter per name rather than assuming a single one.
func (s AgentSet) Adapters() []string {
	seen := map[string]bool{}
	var names []string
	for _, stage := range stageOrder {
		name := s[stage].Adapter
		if name != "" && !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	return names
}
