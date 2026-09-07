package fixture

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/axlev/engine-runner/internal/adapters"
)

// behaviorKind is a closed set of things a fixture stage can simulate. It is
// deliberately small: scenario differences (valid vs. malformed JSON,
// schema-conforming vs. not) live in the response fixture files, not in
// additional behavior kinds here. The adapter never inspects or validates
// response content - that is the engine's job once the runner exists.
type behaviorKind string

const (
	behaviorSuccess behaviorKind = "success"
	behaviorExit    behaviorKind = "exit"
	behaviorHang    behaviorKind = "hang"
)

// behaviorSpec describes what FixtureAdapter.Run does for one stage and one
// attempt.
type behaviorSpec struct {
	Kind behaviorKind `json:"kind"`

	// ResponsePath is required for "success": a path relative to the
	// fixtures root whose raw bytes become the stage's output file
	// verbatim. Content is copied byte-for-byte and is never parsed or
	// validated here.
	ResponsePath string `json:"response_path,omitempty"`

	// ExitCode is used by "exit" to simulate a vendor process or API
	// call failing outright, before producing any structured output.
	ExitCode int `json:"exit_code,omitempty"`

	// DelayMS and Then are used by "hang" to simulate a stage that is
	// still running after DelayMS milliseconds. If the caller's context
	// is cancelled or its deadline expires before DelayMS elapses, Run
	// returns ctx.Err() immediately. Otherwise it falls through to Then,
	// which must be set.
	DelayMS int           `json:"delay_ms,omitempty"`
	Then    *behaviorSpec `json:"then,omitempty"`

	// Usage overrides the stage-level default usage for this specific
	// attempt, if set.
	Usage *usageSpec `json:"usage,omitempty"`
}

type usageSpec struct {
	InputTokens  int     `json:"input_tokens"`
	OutputTokens int     `json:"output_tokens"`
	ToolCalls    int     `json:"tool_calls"`
	CostUSD      float64 `json:"cost_usd"`
}

func (u *usageSpec) toAdapterUsage() adapters.Usage {
	if u == nil {
		return adapters.Usage{}
	}
	return adapters.Usage{
		InputTokens:  u.InputTokens,
		OutputTokens: u.OutputTokens,
		ToolCalls:    u.ToolCalls,
		CostUSD:      u.CostUSD,
	}
}

// stageSpec is one stage's behavior within a scenario: a default applied to
// any attempt without an explicit override, plus optional per-attempt
// overrides keyed by a 1-based attempt number as a string ("1", "2", ...).
// Per-attempt overrides are what let a scenario model "fails once, then
// succeeds" for exercising the runner's retry policy.
type stageSpec struct {
	Usage    *usageSpec              `json:"usage,omitempty"`
	Default  behaviorSpec            `json:"default"`
	Attempts map[string]behaviorSpec `json:"attempts,omitempty"`
}

// behaviorForAttempt resolves which behaviorSpec and usage apply for a given
// attempt string, falling back to the stage default. Resolution is a pure
// function of (stageSpec, attempt): the same inputs always produce the same
// behavior, which is what makes replaying a scenario deterministic.
func (s stageSpec) behaviorForAttempt(attempt string) (behaviorSpec, adapters.Usage) {
	usage := s.Usage.toAdapterUsage()
	behavior := s.Default
	if override, ok := s.Attempts[attempt]; ok {
		behavior = override
		if override.Usage != nil {
			usage = override.Usage.toAdapterUsage()
		}
	}
	return behavior, usage
}

// scenarioFile is the on-disk shape of one file under fixtures/scenarios/.
// The scenario's Name is also the RunRequest.CaseID that selects it:
// fixture test cases are identified by the scenario they exercise.
type scenarioFile struct {
	Name        string               `json:"name"`
	Description string               `json:"description,omitempty"`
	Stages      map[string]stageSpec `json:"stages"`
}

// loadScenarios reads every fixtures/scenarios/*.json file under dir and
// indexes them by name. It fails closed: a malformed or duplicate scenario
// file is a construction-time error, not a runtime surprise during a test.
func loadScenarios(dir string) (map[string]*scenarioFile, error) {
	pattern := filepath.Join(dir, "scenarios", "*.json")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, fmt.Errorf("fixture: globbing %s: %w", pattern, err)
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("fixture: no scenario files found under %s", pattern)
	}

	out := make(map[string]*scenarioFile, len(matches))
	for _, path := range matches {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("fixture: reading %s: %w", path, err)
		}
		var sf scenarioFile
		if err := json.Unmarshal(data, &sf); err != nil {
			return nil, fmt.Errorf("fixture: parsing %s: %w", path, err)
		}
		if sf.Name == "" {
			return nil, fmt.Errorf("fixture: scenario file %s is missing \"name\"", path)
		}
		if _, dup := out[sf.Name]; dup {
			return nil, fmt.Errorf("fixture: duplicate scenario name %q (from %s)", sf.Name, path)
		}
		out[sf.Name] = &sf
	}
	return out, nil
}
