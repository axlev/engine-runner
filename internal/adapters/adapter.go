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
	MaxInputTokens      int     `json:"max_input_tokens,omitempty"`
	MaxOutputTokens     int     `json:"max_output_tokens,omitempty"`
	MaxWallClockSeconds int     `json:"max_wall_clock_seconds,omitempty"`
	MaxToolCalls        int     `json:"max_tool_calls,omitempty"`
	MaxCostUSD          float64 `json:"max_cost_usd,omitempty"`
}

// Usage reports what a stage execution actually consumed, normalized across
// vendors so cost and value can be compared stage-to-stage and run-to-run.
type Usage struct {
	InputTokens  int     `json:"input_tokens,omitempty"`
	OutputTokens int     `json:"output_tokens,omitempty"`
	ToolCalls    int     `json:"tool_calls,omitempty"`
	CostUSD      float64 `json:"cost_usd,omitempty"`
}

// RunRequest is the exact, frozen input to one stage execution. It is
// serializable to agent-request.schema.json and is written verbatim to
// stages/<stage>/request.json before the adapter runs, so every invocation
// is reconstructable after the fact.
type RunRequest struct {
	RunID  string `json:"run_id"`
	CaseID string `json:"case_id"`
	Stage  Stage  `json:"stage"`

	// WorkspacePath is a path inside the isolated execution environment.
	// It must never resolve outside that stage's own read-only and
	// writable mounts.
	WorkspacePath string `json:"workspace_path"`

	// PromptPath points to the frozen, versioned prompt for this stage
	// and protocol version. Adapters must not alter it.
	PromptPath string `json:"prompt_path"`

	// OutputSchema identifies the schema (e.g. "review-a.schema.json")
	// the adapter's structured output at RunResult.OutputPath must
	// satisfy. Validation is performed by the caller, not the adapter.
	OutputSchema string `json:"output_schema"`

	// OutputSchemaJSON is that schema's actual bytes, supplied so an
	// adapter can hand the contract to a vendor that supports structured
	// output (claude's --json-schema). Adapters whose vendor has no such
	// facility ignore it.
	//
	// Content rather than a path because the adapter has no repo root to
	// resolve against, and json:"-" because request.json is a frozen
	// artifact: embedding a copy of a schema that is already fingerprinted
	// by name would bloat every result and create a second place for the
	// same bytes to drift. Handing the schema to the vendor never replaces
	// the caller's own validation - it only reduces the odds of a stage
	// failing on formatting rather than on substance.
	OutputSchemaJSON []byte `json:"-"`

	Model          string `json:"model"`
	ReasoningLevel string `json:"reasoning_level,omitempty"`
	Budget         Budget `json:"budget"`

	// Environment carries scoped, non-secret values into the isolated
	// execution. Credentials are injected by the runner as scoped
	// secrets and must never appear here or be echoed into RunResult.
	Environment map[string]string `json:"environment,omitempty"`
}

// RunResult reports the outcome of one stage execution. Attempt distinguishes
// retries: the caller decides retry policy and records every attempt, so an
// adapter must return one RunResult per attempt rather than retrying
// internally.
type RunResult struct {
	OutputPath string    `json:"output_path,omitempty"`
	ExitCode   int       `json:"exit_code"`
	StartedAt  time.Time `json:"started_at"`
	FinishedAt time.Time `json:"finished_at"`

	// VendorDurationMS and VendorAPIDurationMS are the vendor's own timing,
	// when it reports any. StartedAt/FinishedAt bound everything the engine
	// did - container start, the CLI's own work, the model call - so on
	// their own they cannot say whether a slow stage was slow at the model
	// or slow in our container. These separate it. Zero when unreported;
	// codex reports no timing.
	VendorDurationMS    int    `json:"vendor_duration_ms,omitempty"`
	VendorAPIDurationMS int    `json:"vendor_api_duration_ms,omitempty"`
	Usage               Usage  `json:"usage"`
	Adapter             string `json:"adapter"`
	Version             string `json:"version"`

	// AuthMode records WHICH KIND of credential produced this result, never
	// the credential itself. It matters because the kind changes the
	// invocation, not just the billing: an api_key run gets claude's --bare
	// (no hooks, no LSP, no CLAUDE.md discovery), while an oauth_token run
	// cannot use --bare and runs with the CLI's normal defaults active. A
	// result produced under oauth_token is therefore not comparable with a
	// bare one, and that has to be visible in the artifact rather than
	// remembered. Empty for adapters with no credential concept.
	AuthMode string `json:"auth_mode,omitempty"`
	Attempt  int    `json:"attempt"`
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
