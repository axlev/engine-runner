// Package adapters defines the vendor-polymorphism contract used by the
// engine-runner orchestrator to invoke a reasoning stage. See
// docs/system-design.md section 8.
//
// An AgentAdapter is a thin, stateless boundary: it turns one RunRequest into
// one RunResult. It must not retry silently, must not carry conversation
// memory between stages or across vendors, and must not read or write
// anything outside the paths named in the request. Cross-stage knowledge
// flows only through the explicit, schema-validated handoff files the
// orchestrator places on disk (e.g. review-a.json) - never through adapter
// state.
package adapters

import (
	"context"
	"time"
)

// Stage identifies which reasoning role a request is for. It is a closed
// set: the protocol defines exactly these three stages for the MVP.
type Stage string

const (
	StageReasoner1 Stage = "reasoner-1" // discovery
	StageReasoner2 Stage = "reasoner-2" // evidence
	StageReasoner3 Stage = "reasoner-3" // adversarial verification
)

// AttemptEnvKey is a reserved RunRequest.Environment key the caller may set
// to a 1-based attempt number ("1", "2", ...) before each attempt of the
// same stage. It is not part of what an adapter must honor - a real vendor
// adapter is free to ignore it - but it lets a deterministic test adapter
// (FixtureAdapter) define different behavior per attempt, e.g. failing once
// and succeeding on retry, without becoming stateful itself.
const AttemptEnvKey = "ENGINE_ATTEMPT"

// Budget bounds a single stage execution. The adapter is responsible for
// enforcing what it can (e.g. token limits passed to the vendor API) and for
// reporting Usage so the runner can enforce the rest (e.g. wall-clock
// timeout) from the outside.
type Budget struct {
	MaxInputTokens      int
	MaxOutputTokens     int
	MaxWallClockSeconds int
	MaxToolCalls        int
	MaxCostUSD          float64
}

// Usage reports what a stage execution actually consumed, normalized across
// vendors so cost and value can be compared stage-to-stage and run-to-run.
type Usage struct {
	InputTokens  int
	OutputTokens int
	ToolCalls    int
	CostUSD      float64
}

// RunRequest is the exact, frozen input to one stage execution. It is
// serializable to agent-request.schema.json and is written verbatim to
// stages/<stage>/request.json before the adapter runs, so every invocation
// is reconstructable after the fact.
type RunRequest struct {
	RunID  string
	CaseID string
	Stage  Stage

	// WorkspacePath is a path inside the isolated execution environment.
	// It must never resolve outside that stage's own read-only and
	// writable mounts.
	WorkspacePath string

	// PromptPath points to the frozen, versioned prompt for this stage
	// and protocol version. Adapters must not alter it.
	PromptPath string

	// OutputSchema identifies the schema (e.g. "review-a.schema.json")
	// the adapter's structured output at RunResult.OutputPath must
	// satisfy. Validation is performed by the caller, not the adapter.
	OutputSchema string

	Model          string
	ReasoningLevel string
	Budget         Budget

	// Environment carries scoped, non-secret values into the isolated
	// execution. Credentials are injected by the runner as scoped
	// secrets and must never appear here or be echoed into RunResult.
	Environment map[string]string
}

// RunResult reports the outcome of one stage execution. Attempt distinguishes
// retries: the caller decides retry policy and records every attempt, so an
// adapter must return one RunResult per attempt rather than retrying
// internally.
type RunResult struct {
	OutputPath string
	ExitCode   int
	StartedAt  time.Time
	FinishedAt time.Time
	Usage      Usage
	Adapter    string
	Version    string
	Attempt    int
}

// AgentAdapter provides vendor polymorphism without creating a generic
// memory system. Implementations (ClaudeAdapter, CodexAdapter,
// FixtureAdapter) must be stateless across calls: nothing observed in one
// Run call may influence another, whether for a different stage, a different
// case, or a retry attempt of the same request.
type AgentAdapter interface {
	// Name returns the adapter's stable identifier, e.g. "claude", "codex",
	// "fixture". It is recorded in results for provenance.
	Name() string

	// Version returns the concrete, resolvable version of the underlying
	// executable or API this adapter is currently bound to. It is
	// recorded in results so a run can be reproduced against the same
	// vendor version.
	Version(ctx context.Context) (string, error)

	// Run executes exactly one stage invocation to completion or failure
	// and returns without retrying. The caller is responsible for
	// constructing an already-isolated environment: Run must not itself
	// create, widen, or reuse isolation boundaries.
	Run(ctx context.Context, req RunRequest) (RunResult, error)
}
