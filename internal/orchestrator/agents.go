package orchestrator

import (
	"bytes"
	"fmt"
	"os"

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

// AgentSet is the vendor binding for every stage of a run.
type AgentSet map[adapters.Stage]AgentConfig

// agentSetFile is one arm, in one file.
//
// The earlier layout was one file per stage in a directory, which meant a
// new model arm was three files differing by a single word - adapter, model
// and reasoning_level were identical across all three, and only the budgets
// varied. Nine files to keep in sync across three arms is a drift
// generator, and the fields genuinely vary along two axes: budgets by
// STAGE (reasoner-2 receives a handoff, so it needs more input), model and
// cost ceiling by MODEL (a dollar limit is meaningless without a price).
//
// One file per arm expresses both: shared binding at the top, per-stage
// budgets below. A new arm is one file with one changed line, and it stays
// a single hashed artifact - which a -model command-line override would
// destroy, since a result must be able to say what produced it from
// fingerprinted files alone.
type agentSetFile struct {
	Adapter        string       `yaml:"adapter"`
	Model          string       `yaml:"model"`
	ReasoningLevel string       `yaml:"reasoning_level"`
	Budget         BudgetConfig `yaml:"budget"`

	Stages map[string]agentStageFile `yaml:"stages"`
}

// agentStageFile overrides the arm's defaults for one stage. Pointers so an
// absent key is distinguishable from a zero value: "reasoning_level: """
// and "no reasoning_level here" must not mean the same thing.
//
// Overriding adapter or model per stage is deliberately allowed - running
// discovery on one vendor and verification on another is a coherent
// experiment - but the common case is only a budget.
type agentStageFile struct {
	Adapter        *string       `yaml:"adapter"`
	Model          *string       `yaml:"model"`
	ReasoningLevel *string       `yaml:"reasoning_level"`
	Budget         *BudgetConfig `yaml:"budget"`
}

// LoadAgentSet reads one arm file and resolves it into a per-stage binding.
//
// Every stage must end up with an adapter and a model: a partially
// specified agent set would run some stages and fail others midway, after
// spending money.
func LoadAgentSet(path string) (AgentSet, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("orchestrator: reading agent set %s: %w", path, err)
	}

	var file agentSetFile
	// KnownFields so a typo ("budgets:", "resoning_level:") is an error
	// rather than a silently ignored key that leaves a run bound to
	// defaults nobody chose.
	dec := yaml.NewDecoder(newBytesReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&file); err != nil {
		return nil, fmt.Errorf("orchestrator: parsing agent set %s: %w", path, err)
	}

	set := AgentSet{}
	for _, stage := range stageOrder {
		cfg := AgentConfig{
			Adapter:        file.Adapter,
			Model:          file.Model,
			ReasoningLevel: file.ReasoningLevel,
			Budget:         file.Budget,
		}

		if override, ok := file.Stages[string(stage)]; ok {
			if override.Adapter != nil {
				cfg.Adapter = *override.Adapter
			}
			if override.Model != nil {
				cfg.Model = *override.Model
			}
			if override.ReasoningLevel != nil {
				cfg.ReasoningLevel = *override.ReasoningLevel
			}
			// Wholesale replacement rather than a field-wise merge: a
			// half-inherited budget ("which of these five numbers came from
			// where?") is harder to reason about than repeating five short
			// lines, and these are spending limits.
			if override.Budget != nil {
				cfg.Budget = *override.Budget
			}
		}

		if cfg.Adapter == "" {
			return nil, fmt.Errorf("orchestrator: agent set %s: stage %s has no adapter", path, stage)
		}
		if cfg.Model == "" {
			return nil, fmt.Errorf("orchestrator: agent set %s: stage %s has no model", path, stage)
		}
		set[stage] = cfg
	}

	for name := range file.Stages {
		if !isKnownStage(name) {
			return nil, fmt.Errorf("orchestrator: agent set %s: unknown stage %q", path, name)
		}
	}

	return set, nil
}

func isKnownStage(name string) bool {
	for _, stage := range stageOrder {
		if string(stage) == name {
			return true
		}
	}
	return false
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

// newBytesReader exists only so the YAML decoder (which needs an io.Reader
// for KnownFields) can read an already-read []byte.
func newBytesReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }
