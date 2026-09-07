package orchestrator

import (
	"fmt"
	"os"

	"github.com/axlev/engine-runner/internal/adapters"
	"gopkg.in/yaml.v3"
)

// Protocol is the frozen, versioned configuration for one cohort: which
// adapter to use, which handoffs are enabled, and each stage's prompt,
// model, budget, and output schema. Design principle 6: "A cohort runs
// under a frozen protocol. Prompts are not tuned per case." Nothing in this
// package mutates a loaded Protocol.
type Protocol struct {
	Version     string                   `yaml:"protocol_version"`
	Adapter     string                   `yaml:"adapter"`
	Handoffs    HandoffConfig            `yaml:"handoffs"`
	Stages      map[string]StageProtocol `yaml:"stages"`
	RetryPolicy RetryPolicy              `yaml:"retry_policy"`
}

type HandoffConfig struct {
	// EnableAToB is section 6.4's "A-to-B handoff": whether reasoner-2 (and,
	// transitively, reasoner-3) may see review-a.json. Pilot v1 sets this
	// true.
	EnableAToB bool `yaml:"enable_a_to_b"`
}

type StageProtocol struct {
	Prompt         string       `yaml:"prompt"`
	Model          string       `yaml:"model"`
	ReasoningLevel string       `yaml:"reasoning_level"`
	OutputSchema   string       `yaml:"output_schema"`
	Budget         BudgetConfig `yaml:"budget"`
}

type BudgetConfig struct {
	MaxInputTokens      int     `yaml:"max_input_tokens"`
	MaxOutputTokens     int     `yaml:"max_output_tokens"`
	MaxWallClockSeconds int     `yaml:"max_wall_clock_seconds"`
	MaxToolCalls        int     `yaml:"max_tool_calls"`
	MaxCostUSD          float64 `yaml:"max_cost_usd"`
}

func (b BudgetConfig) toAdapterBudget() adapters.Budget {
	return adapters.Budget{
		MaxInputTokens:      b.MaxInputTokens,
		MaxOutputTokens:     b.MaxOutputTokens,
		MaxWallClockSeconds: b.MaxWallClockSeconds,
		MaxToolCalls:        b.MaxToolCalls,
		MaxCostUSD:          b.MaxCostUSD,
	}
}

// RetryPolicy governs how many total attempts a stage gets. Section 12:
// "Treat invalid structured output as an explicit stage failure unless the
// frozen protocol defines one repair attempt." MaxAttempts: 1 means no
// repair attempt; 2 means exactly one.
type RetryPolicy struct {
	MaxAttempts int `yaml:"max_attempts"`
}

var requiredStages = []adapters.Stage{adapters.StageReasoner1, adapters.StageReasoner2, adapters.StageReasoner3}

// LoadProtocol reads and validates a protocol YAML file. It fails closed: a
// protocol missing a required stage, a version, or a positive retry budget
// is a construction-time error, never a runtime surprise mid-cohort.
func LoadProtocol(path string) (*Protocol, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("orchestrator: reading protocol %s: %w", path, err)
	}
	var p Protocol
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("orchestrator: parsing protocol %s: %w", path, err)
	}
	if err := p.validate(); err != nil {
		return nil, fmt.Errorf("orchestrator: protocol %s is invalid: %w", path, err)
	}
	return &p, nil
}

func (p Protocol) validate() error {
	if p.Version == "" {
		return fmt.Errorf("missing protocol_version")
	}
	if p.Adapter == "" {
		return fmt.Errorf("missing adapter")
	}
	if p.RetryPolicy.MaxAttempts < 1 {
		return fmt.Errorf("retry_policy.max_attempts must be >= 1, got %d", p.RetryPolicy.MaxAttempts)
	}
	for _, stage := range requiredStages {
		sp, ok := p.Stages[string(stage)]
		if !ok {
			return fmt.Errorf("missing stage %q", stage)
		}
		if sp.Prompt == "" {
			return fmt.Errorf("stage %q missing prompt", stage)
		}
		if sp.Model == "" {
			return fmt.Errorf("stage %q missing model", stage)
		}
		if sp.OutputSchema == "" {
			return fmt.Errorf("stage %q missing output_schema", stage)
		}
	}
	return nil
}
