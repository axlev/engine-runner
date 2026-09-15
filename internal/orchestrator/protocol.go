package orchestrator

import (
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/axlev/engine-runner/internal/adapters"
	"github.com/axlev/engine-runner/internal/contextbuilder"
	"gopkg.in/yaml.v3"
)

// knownStages is the engine's stage vocabulary - the names a protocol may
// declare, in the order the original three-stage pipeline ran them. It is
// NOT the order any given protocol runs: that is Protocol.Order(), declared
// per protocol. It exists because stage identity is still baked into things
// downstream of the orchestrator - the agent-request schema's enum, the
// results writer's review-a/b/c file names, the evaluator's three inputs -
// so a protocol may only compose these names, not invent new ones.
var knownStages = []adapters.Stage{adapters.StageReasoner1, adapters.StageReasoner2, adapters.StageReasoner3}

func isKnownStage(name string) bool {
	for _, stage := range knownStages {
		if string(stage) == name {
			return true
		}
	}
	return false
}

// Protocol is the frozen, versioned definition of one cohort's experiment:
// which stages run and in what order, which upstream outputs each receives,
// each stage's prompt and output schema, the stopping rules, and any
// boundary-validation waivers. Design principle 6: "A cohort runs under a
// frozen protocol. Prompts are not tuned per case." Nothing in this package
// mutates a loaded Protocol.
//
// It deliberately carries no vendor detail. Which adapter and model run a
// stage lives in the agent set (agents.go), so the same frozen cohort can be
// smoke-tested through the fixture adapter and then run for real through a
// vendor without minting a new protocol version.
type Protocol struct {
	Version string `yaml:"protocol_version"`

	// StageOrder is the ordered list of stages this protocol runs. It is
	// OPTIONAL for one reason: pilot-v1's file predates it, and that file
	// must never be edited - its sealed cohorts are hashed against its
	// bytes. When absent, the protocol runs the original three stages in
	// their original order with the original handoffs (see resolveLegacy),
	// so an unchanged file means exactly what it always did.
	//
	// When present, every handoff must be declared explicitly under each
	// stage's `receives`, and Handoffs.EnableAToB has no meaning.
	StageOrder []string `yaml:"stage_order"`

	Handoffs           HandoffConfig            `yaml:"handoffs"`
	Stages             map[string]StageProtocol `yaml:"stages"`
	RetryPolicy        RetryPolicy              `yaml:"retry_policy"`
	BoundaryValidation BoundaryValidationConfig `yaml:"boundary_validation"`

	// Resolved at load, never read from YAML. After LoadProtocol these are
	// always fully explicit - a legacy file and a declared file both end
	// up here in the same shape - so nothing downstream ever infers a
	// handoff from a stage's position or identity again.
	order    []adapters.Stage
	handoffs map[adapters.Stage][]contextbuilder.HandoffGrant
}

// Order is the stages this protocol runs, in order.
func (p *Protocol) Order() []adapters.Stage {
	return append([]adapters.Stage(nil), p.order...)
}

// HandoffsFor is exactly which upstream outputs stage may see, as declared.
// The context builder treats this as the sole authority; an output that
// exists on disk but is not granted here is never copied.
func (p *Protocol) HandoffsFor(stage adapters.Stage) []contextbuilder.HandoffGrant {
	return append([]contextbuilder.HandoffGrant(nil), p.handoffs[stage]...)
}

// BoundaryValidationConfig carries the frozen protocol's boundary-validator
// settings. Putting waivers here rather than in the validator's own source
// is the point: a cohort's decision to allow a specific path is part of
// that cohort's protocol, is hashed into the run's protocol fingerprint,
// and is visible to anyone auditing the result.
type BoundaryValidationConfig struct {
	// WaivedOracleShapedPaths lists bundle-relative paths where the
	// oracle-name heuristic is knowingly overridden - for a real
	// repository that legitimately contains, say,
	// reviewer/repository/db/oracle_dialect.go.
	//
	// Only that one lexical rule is waivable. Structural violations
	// (symlinks, git metadata, checksum mismatches) are never waivable at
	// any level of configuration.
	WaivedOracleShapedPaths []string `yaml:"waived_oracle_shaped_paths"`
}

// HandoffConfig is the LEGACY handoff switch, meaningful only for a protocol
// that declares no stage_order. Section 6.4's "A-to-B handoff": whether
// reasoner-2 (and, transitively, reasoner-3) may see review-a.json. Pilot v1
// sets this true. A protocol with an explicit stage_order declares its
// handoffs per stage instead, and setting this alongside is an error rather
// than a silently ignored knob.
type HandoffConfig struct {
	EnableAToB bool `yaml:"enable_a_to_b"`
}

// StageProtocol is one stage's experimental definition. It carries no
// vendor detail: which adapter and model run this stage, at what effort and
// under what budget, is the agent set's business (see agents.go).
type StageProtocol struct {
	Prompt       string `yaml:"prompt"`
	OutputSchema string `yaml:"output_schema"`

	// Receives declares which earlier stages' outputs this stage may see,
	// and under what file name they appear in its handoff directory. Only
	// valid alongside stage_order. Position in the order never implies a
	// handoff: a stage with no `receives` sees the prospective bundle only,
	// wherever it runs.
	Receives []HandoffSpec `yaml:"receives"`
}

// HandoffSpec is one declared handoff: the output of stage From, placed in
// the receiving stage's input/handoff/ directory as As.
type HandoffSpec struct {
	From string `yaml:"from"`
	As   string `yaml:"as"`
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

// LoadProtocol reads, validates and resolves a protocol YAML file. It fails
// closed: a declared stage with no prompt or schema, a handoff from an
// undeclared or later stage, an empty stage list, or a stage declared but
// not ordered is a construction-time error naming the stage, never a runtime
// surprise mid-cohort after money has been spent.
func LoadProtocol(path string) (*Protocol, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("orchestrator: reading protocol %s: %w", path, err)
	}
	var p Protocol
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("orchestrator: parsing protocol %s: %w", path, err)
	}
	if err := p.resolve(); err != nil {
		return nil, fmt.Errorf("orchestrator: protocol %s is invalid: %w", path, err)
	}
	return &p, nil
}

// resolve validates the manifest and fills the explicit order and handoff
// graph. Two modes, decided by whether stage_order is present.
func (p *Protocol) resolve() error {
	if p.Version == "" {
		return fmt.Errorf("missing protocol_version")
	}
	if p.RetryPolicy.MaxAttempts < 1 {
		return fmt.Errorf("retry_policy.max_attempts must be >= 1, got %d", p.RetryPolicy.MaxAttempts)
	}
	for name := range p.Stages {
		if !isKnownStage(name) {
			return fmt.Errorf("stage %q is not a stage this engine knows (one of %v)", name, knownStages)
		}
	}
	if p.StageOrder == nil {
		return p.resolveLegacy()
	}
	return p.resolveDeclared()
}

// resolveLegacy is what an unchanged pre-stage_order file means: the three
// original stages in their original order, with the original handoffs
// derived from enable_a_to_b. This is the one place that inference lives,
// and it produces the same explicit graph a declared file would.
func (p *Protocol) resolveLegacy() error {
	for name, sp := range p.Stages {
		if len(sp.Receives) > 0 {
			return fmt.Errorf("stage %q declares receives but the protocol has no stage_order; explicit handoffs require an explicit stage_order", name)
		}
	}
	for _, stage := range knownStages {
		sp, ok := p.Stages[string(stage)]
		if !ok {
			return fmt.Errorf("missing stage %q", stage)
		}
		if err := validateStageFiles(string(stage), sp); err != nil {
			return err
		}
	}
	p.order = append([]adapters.Stage(nil), knownStages...)
	p.handoffs = map[adapters.Stage][]contextbuilder.HandoffGrant{}
	reviewA := contextbuilder.HandoffGrant{From: adapters.StageReasoner1, As: "review-a.json"}
	reviewB := contextbuilder.HandoffGrant{From: adapters.StageReasoner2, As: "review-b.json"}
	if p.Handoffs.EnableAToB {
		p.handoffs[adapters.StageReasoner2] = []contextbuilder.HandoffGrant{reviewA}
		p.handoffs[adapters.StageReasoner3] = []contextbuilder.HandoffGrant{reviewA, reviewB}
	} else {
		// Reasoner 3 always sees what reasoner 2 saw, plus review-b - never
		// review-a on its own authority.
		p.handoffs[adapters.StageReasoner3] = []contextbuilder.HandoffGrant{reviewB}
	}
	return nil
}

// resolveDeclared validates an explicit stage_order and per-stage receives.
func (p *Protocol) resolveDeclared() error {
	if len(p.StageOrder) == 0 {
		return fmt.Errorf("stage_order is empty; a protocol must run at least one stage")
	}
	if p.Handoffs.EnableAToB {
		return fmt.Errorf("handoffs.enable_a_to_b has no meaning when stage_order is declared; declare each stage's receives instead")
	}

	position := map[string]int{}
	for i, name := range p.StageOrder {
		if !isKnownStage(name) {
			return fmt.Errorf("stage_order names %q, which is not a stage this engine knows (one of %v)", name, knownStages)
		}
		if _, dup := position[name]; dup {
			return fmt.Errorf("stage_order lists %q twice", name)
		}
		position[name] = i
		sp, ok := p.Stages[name]
		if !ok {
			return fmt.Errorf("stage_order names %q but stages has no entry for it", name)
		}
		if err := validateStageFiles(name, sp); err != nil {
			return err
		}
	}
	for name := range p.Stages {
		if _, ok := position[name]; !ok {
			// A stage that is declared but never runs would also never be
			// hashed, so its prompt could drift unrecorded. Refuse rather
			// than run a protocol that is not the one written down.
			return fmt.Errorf("stage %q is declared under stages but absent from stage_order", name)
		}
	}

	p.order = make([]adapters.Stage, 0, len(p.StageOrder))
	for _, name := range p.StageOrder {
		p.order = append(p.order, adapters.Stage(name))
	}
	p.handoffs = map[adapters.Stage][]contextbuilder.HandoffGrant{}
	for _, name := range p.StageOrder {
		sp := p.Stages[name]
		seenAs := map[string]bool{}
		var grants []contextbuilder.HandoffGrant
		for _, h := range sp.Receives {
			from, ok := position[h.From]
			if !ok {
				return fmt.Errorf("stage %q receives from %q, which is not declared in stage_order", name, h.From)
			}
			if from >= position[name] {
				return fmt.Errorf("stage %q receives from %q, which does not run before it", name, h.From)
			}
			if err := validateHandoffName(h.As); err != nil {
				return fmt.Errorf("stage %q receives from %q: %w", name, h.From, err)
			}
			if seenAs[h.As] {
				return fmt.Errorf("stage %q receives two handoffs as %q", name, h.As)
			}
			seenAs[h.As] = true
			grants = append(grants, contextbuilder.HandoffGrant{From: adapters.Stage(h.From), As: h.As})
		}
		if len(grants) > 0 {
			p.handoffs[adapters.Stage(name)] = grants
		}
	}
	return nil
}

func validateStageFiles(name string, sp StageProtocol) error {
	if sp.Prompt == "" {
		return fmt.Errorf("stage %q missing prompt", name)
	}
	if sp.OutputSchema == "" {
		return fmt.Errorf("stage %q missing output_schema", name)
	}
	return nil
}

// validateHandoffName keeps a handoff inside the handoff directory: a bare
// .json file name, no path components. The context builder re-checks this,
// but a bad name should fail at load, not at the first attempt.
func validateHandoffName(as string) error {
	if as == "" {
		return fmt.Errorf("handoff has no 'as' file name")
	}
	if path.Base(as) != as || strings.ContainsAny(as, `/\`) || as == "." || as == ".." {
		return fmt.Errorf("'as' must be a bare file name, got %q", as)
	}
	if !strings.HasSuffix(as, ".json") {
		return fmt.Errorf("'as' must end in .json, got %q", as)
	}
	return nil
}
