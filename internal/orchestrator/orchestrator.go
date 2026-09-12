// Package orchestrator sequences the three reasoning stages under a frozen
// protocol: it loads the protocol, drives the context builder and runner for
// each stage, invokes the adapter, validates structured output, and performs
// explicit handoffs between stages. See docs/system-design.md sections 6.3
// and 10.
//
// This phase deliberately does not include boundary validation
// (internal/boundaryvalidator) or containerized isolation
// (docs/system-design.md section 9): those depend on a real miner-produced
// bundle and an OCI runtime respectively, neither of which exists yet for
// the fixture-only vertical slice. The Runner still gives every attempt its
// own fresh directory, so the isolation *contract* this package assumes is
// already real - only the container boundary around it is still pending.
package orchestrator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/axlev/engine-runner/internal/adapters"
	"github.com/axlev/engine-runner/internal/boundaryvalidator"
	"github.com/axlev/engine-runner/internal/contextbuilder"
	"github.com/axlev/engine-runner/internal/runner"
)

// StageAttemptRecord is one attempt of one stage, successful or not. Section
// 12: "Never silently retry. Record the failed attempt and the retry policy
// decision." Every attempt produces exactly one record, in order.
type StageAttemptRecord struct {
	Stage   adapters.Stage
	Attempt int
	Request adapters.RunRequest
	Result  adapters.RunResult

	// Err is empty only when this attempt both executed successfully and
	// produced output satisfying its schema. A non-empty Err on the last
	// attempt of a stage means the whole run failed.
	Err string
}

// RunOutcome is the in-memory result of one full run. Turning this into the
// sealed, on-disk result tree (run.json, stages/<stage>/*.json) is a later
// phase's job (results writer); this package only produces the data.
type RunOutcome struct {
	RunID           string
	CaseID          string
	ProtocolVersion string
	CreatedAt       time.Time
	Status          string // "completed", "failed", or "invalidated"
	FailureReason   string
	Attempts        []StageAttemptRecord
	ReviewAPath     string
	ReviewBPath     string
	ReviewCPath     string
	Fingerprints    Fingerprints

	// BoundaryValidation is the report from the pre-flight check on the
	// prospective bundle. It is always present: a run that never got past
	// validation is exactly the case where this report is the only
	// evidence of what happened.
	BoundaryValidation *boundaryvalidator.Report
}

var stageOrder = []adapters.Stage{adapters.StageReasoner1, adapters.StageReasoner2, adapters.StageReasoner3}

// Orchestrator sequences stageOrder under one loaded Protocol.
type Orchestrator struct {
	// RepoRoot resolves the protocol's repo-relative paths: stage prompts
	// (e.g. "prompts/pilot-v1/reasoner-1.md") and output schemas (e.g.
	// "schemas/review-a.schema.json").
	RepoRoot string

	// ProtocolPath is the on-disk YAML file Protocol was loaded from. It
	// is hashed into every run's Fingerprints so a result is traceable
	// back to the exact protocol bytes used, not just its declared
	// version string.
	ProtocolPath string

	Protocol *Protocol

	// AgentSetPath is the file AgentSet was loaded from. It is
	// hashed into every run's fingerprints, so a result records the vendor
	// binding that produced it as precisely as it records the protocol.
	AgentSetPath string
	AgentSet     AgentSet

	// Adapters is keyed by adapter name, not by stage: an agent set may
	// bind different stages to different vendors, and each distinct vendor
	// needs exactly one constructed adapter.
	Adapters map[string]adapters.AgentAdapter

	Builder   *contextbuilder.Builder
	Runner    *runner.Runner
	Validator *SchemaValidator

	// Warn receives non-fatal diagnostics, such as a budget bound that
	// could not be checked because the adapter reported no usage. Nil
	// discards them.
	Warn func(string)
}

func (o *Orchestrator) warnf(format string, args ...interface{}) {
	if o.Warn == nil {
		return
	}
	o.Warn(fmt.Sprintf(format, args...))
}

// Options are the inputs to New. It is a struct rather than a parameter
// list because a run is defined by two independent configurations - the
// protocol (the experiment) and the agent set (the vendor binding) - and
// positional arguments made that easy to transpose.
type Options struct {
	RepoRoot      string
	ProtocolPath  string
	Protocol      *Protocol
	AgentSetPath  string
	AgentSet      AgentSet
	Adapters      map[string]adapters.AgentAdapter
	WorkspaceRoot string
	Warn          func(string)
}

// New wires an Orchestrator. workspaceRoot is where the Runner creates fresh
// per-attempt workspace directories.
func New(opts Options) (*Orchestrator, error) {
	r, err := runner.New(opts.WorkspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("orchestrator: %w", err)
	}
	// Fail here rather than mid-run: a stage whose adapter was never
	// constructed would otherwise blow up after earlier stages had already
	// spent money.
	for _, stage := range stageOrder {
		name := opts.AgentSet[stage].Adapter
		if _, ok := opts.Adapters[name]; !ok {
			return nil, fmt.Errorf("orchestrator: stage %s needs adapter %q, which was not supplied", stage, name)
		}
	}
	return &Orchestrator{
		RepoRoot:     opts.RepoRoot,
		ProtocolPath: opts.ProtocolPath,
		Protocol:     opts.Protocol,
		AgentSetPath: opts.AgentSetPath,
		AgentSet:     opts.AgentSet,
		Adapters:     opts.Adapters,
		Builder:      contextbuilder.New(),
		Runner:       r,
		Validator:    NewSchemaValidator(opts.RepoRoot),
		Warn:         opts.Warn,
	}, nil
}

// Run drives one case's three stages to completion or to the first
// unrecoverable stage failure. bundleRoot is the case's prospective/
// directory.
func (o *Orchestrator) Run(ctx context.Context, runID, caseID, bundleRoot string) (RunOutcome, error) {
	outcome := RunOutcome{
		RunID:           runID,
		CaseID:          caseID,
		ProtocolVersion: o.Protocol.Version,
		CreatedAt:       time.Now().UTC(),
	}

	// Boundary validation runs before anything else touches the bundle
	// (section 10 step 3). Section 12: a boundary-validation failure
	// invalidates the case *before* LLM cost is incurred - so this returns
	// without preparing a single stage context or invoking any adapter.
	validation, err := boundaryvalidator.ValidateWithConfig(caseID, bundleRoot, boundaryvalidator.Config{
		WaivedOracleShapedPaths: o.Protocol.BoundaryValidation.WaivedOracleShapedPaths,
	})
	if err != nil {
		return outcome, fmt.Errorf("orchestrator: boundary validation could not run: %w", err)
	}
	outcome.BoundaryValidation = &validation

	// Fingerprints are computed even for a rejected case, so its sealed
	// record still says which protocol, prompts and schemas were in play
	// when it was rejected. Validation's diagnosis stays the reported
	// error either way - it is the more actionable one.
	fp, fpErr := computeStaticFingerprints(o.ProtocolPath, o.AgentSetPath, o.RepoRoot, bundleRoot, o.Protocol, o.AgentSet)
	if fpErr == nil {
		outcome.Fingerprints = fp
	}

	if !validation.Passed() {
		outcome.Status = "invalidated"
		outcome.FailureReason = validation.Summary()
		return outcome, fmt.Errorf("orchestrator: boundary validation rejected case %q: %s", caseID, validation.Summary())
	}
	if fpErr != nil {
		return outcome, fmt.Errorf("orchestrator: computing fingerprints: %w", fpErr)
	}

	var reviewAPath, reviewBPath string
	policy := contextbuilder.HandoffPolicy{EnableAToB: o.Protocol.Handoffs.EnableAToB}

	for _, stage := range stageOrder {
		stageProto := o.Protocol.Stages[string(stage)]
		promptPath := filepath.Join(o.RepoRoot, stageProto.Prompt)

		var inputs contextbuilder.StageInputs
		switch stage {
		case adapters.StageReasoner2:
			if policy.EnableAToB {
				inputs.ReviewAPath = reviewAPath
			}
		case adapters.StageReasoner3:
			if policy.EnableAToB {
				inputs.ReviewAPath = reviewAPath
			}
			inputs.ReviewBPath = reviewBPath
		}

		outputPath, records, err := o.runStageWithRetries(ctx, runID, caseID, stage, stageProto, promptPath, bundleRoot, policy, inputs)
		outcome.Attempts = append(outcome.Attempts, records...)
		if len(records) > 0 {
			last := records[len(records)-1]
			outcome.Fingerprints.AdapterVersions[string(stage)] = last.Result.Adapter + "/" + last.Result.Version
			if d := last.Result.ImageDigest; d != "" {
				if outcome.Fingerprints.ContainerDigests == nil {
					outcome.Fingerprints.ContainerDigests = map[string]string{}
				}
				outcome.Fingerprints.ContainerDigests[string(stage)] = d
			}
		}
		if err != nil {
			outcome.Status = "failed"
			outcome.FailureReason = err.Error()
			return outcome, fmt.Errorf("orchestrator: %w", err)
		}

		switch stage {
		case adapters.StageReasoner1:
			reviewAPath = outputPath
			outcome.ReviewAPath = outputPath
		case adapters.StageReasoner2:
			reviewBPath = outputPath
			outcome.ReviewBPath = outputPath
		case adapters.StageReasoner3:
			outcome.ReviewCPath = outputPath
		}
	}

	outcome.Status = "completed"
	return outcome, nil
}

// runStageWithRetries runs one stage up to Protocol.RetryPolicy.MaxAttempts
// times, giving every attempt its own fresh workspace, and returns the
// winning attempt's output path plus a record of every attempt made.
func (o *Orchestrator) runStageWithRetries(
	ctx context.Context,
	runID, caseID string,
	stage adapters.Stage,
	stageProto StageProtocol,
	promptPath, bundleRoot string,
	policy contextbuilder.HandoffPolicy,
	inputs contextbuilder.StageInputs,
) (string, []StageAttemptRecord, error) {
	// The vendor binding for this stage. New() has already verified the
	// named adapter was supplied, so this lookup cannot miss.
	agentCfg := o.AgentSet[stage]
	adapter := o.Adapters[agentCfg.Adapter]

	// Read once per stage rather than per attempt: the file cannot change
	// mid-stage, and a read failure should not be discovered on a retry.
	// Non-fatal - an adapter that cannot use it ignores it, and the
	// caller's own validation is unaffected either way.
	schemaJSON, schemaErr := os.ReadFile(filepath.Join(o.RepoRoot, stageProto.OutputSchema))
	if schemaErr != nil {
		o.warnf("stage %s: could not read %s to hand to the vendor (%v); the stage still runs and its output is still validated",
			stage, stageProto.OutputSchema, schemaErr)
		schemaJSON = nil
	}

	var records []StageAttemptRecord
	var lastErr error

	for attempt := 1; attempt <= o.Protocol.RetryPolicy.MaxAttempts; attempt++ {
		ws, err := o.Runner.FreshWorkspace(runID, stage, attempt)
		if err != nil {
			return "", records, fmt.Errorf("stage %s attempt %d: %w", stage, attempt, err)
		}

		stageCtx, err := o.Builder.Prepare(stage, bundleRoot, promptPath, ws, policy, inputs)
		if err != nil {
			return "", records, fmt.Errorf("stage %s attempt %d: preparing context: %w", stage, attempt, err)
		}

		req := adapters.RunRequest{
			RunID:            runID,
			CaseID:           caseID,
			Stage:            stage,
			WorkspacePath:    stageCtx.WorkspacePath,
			PromptPath:       stageCtx.PromptPath,
			OutputSchema:     stageProto.OutputSchema,
			OutputSchemaJSON: schemaJSON,
			Model:            agentCfg.Model,
			ReasoningLevel:   agentCfg.ReasoningLevel,
			Budget:           agentCfg.Budget.toAdapterBudget(),
			Tools:            agentCfg.Tools,
			Environment: map[string]string{
				adapters.AttemptEnvKey: strconv.Itoa(attempt),
			},
		}

		// max_wall_clock_seconds is enforced here, not by the vendor:
		// neither CLI has a timeout flag. The deadline is per ATTEMPT, not
		// per stage - a retry gets its own full allowance, since the bound
		// describes one invocation.
		attemptCtx := ctx
		cancel := context.CancelFunc(func() {})
		if secs := agentCfg.Budget.MaxWallClockSeconds; secs > 0 {
			attemptCtx, cancel = context.WithTimeout(ctx, time.Duration(secs)*time.Second)
		}

		result, runErr := adapter.Run(attemptCtx, req)
		// Released immediately rather than deferred: the deadline covers
		// exactly the adapter call, and a deferred cancel inside this loop
		// would hold one timer per attempt until the whole stage returns.
		cancel()
		result.Attempt = attempt
		if result.Adapter == "" {
			result.Adapter = adapter.Name()
		}

		record := StageAttemptRecord{Stage: stage, Attempt: attempt, Request: req, Result: result}

		if runErr != nil {
			record.Err = runErr.Error()
			records = append(records, record)
			lastErr = fmt.Errorf("stage %s attempt %d: adapter run failed: %w", stage, attempt, runErr)

			// A cancelled PARENT context means the operator interrupted the
			// run, so retrying would open a new vendor call - and new spend -
			// under a context that is already dead. Checked on ctx rather
			// than attemptCtx precisely to keep it distinct from an attempt
			// exhausting its own wall-clock budget, which SHOULD retry: the
			// retry policy deliberately gives each attempt a fresh allowance.
			if ctx.Err() != nil {
				return "", records, lastErr
			}
			continue
		}

		// Detection, not prevention: max_input_tokens, max_output_tokens and
		// max_tool_calls have no flag on either vendor CLI, so the declared
		// bound can only be checked against what the stage reports having
		// used. The spend already happened; failing here keeps the protocol's
		// limits meaningful rather than reporting an over-budget run clean.
		if budgetErr, unavailable := checkUsageAgainstBudget(result.Adapter, agentCfg.Budget, result.Usage); budgetErr != nil {
			record.Err = budgetErr.Error()
			records = append(records, record)
			lastErr = fmt.Errorf("stage %s attempt %d: %w", stage, attempt, budgetErr)
			continue
		} else if unavailable != nil {
			// Not a failure - but it must not pass silently either, or a
			// bound enforced on one vendor looks enforced on all of them.
			o.warnf("stage %s attempt %d: adapter %q did not report %v, so %s unenforced this attempt",
				stage, attempt, unavailable.Adapter, unavailable.Bounds,
				plural(len(unavailable.Bounds), "that bound was", "those bounds were"))
		}

		// The reasoner authors content; the engine supplies identity. This
		// runs before validation because the schemas require the envelope
		// fields the reasoner cannot know.
		if err := stampEnvelope(result.OutputPath, runID, caseID, stage, time.Now()); err != nil {
			record.Err = err.Error()
			records = append(records, record)
			lastErr = fmt.Errorf("stage %s attempt %d: %w", stage, attempt, err)
			continue
		}

		if err := o.Validator.ValidateFile(result.OutputPath, stageProto.OutputSchema); err != nil {
			record.Err = err.Error()
			records = append(records, record)
			lastErr = fmt.Errorf("stage %s attempt %d: %w", stage, attempt, err)
			continue
		}

		// Evidence citations are checked after the schema, not before: an
		// output that is not schema-valid has nothing worth citation-checking,
		// and the schema error is the more useful one to report.
		evidenceErr, evidenceWarnings := validateEvidence(result.OutputPath, bundleRoot)
		for _, w := range evidenceWarnings {
			o.warnf("stage %s attempt %d: %s", stage, attempt, w)
		}
		if evidenceErr != nil {
			record.Err = evidenceErr.Error()
			records = append(records, record)
			lastErr = fmt.Errorf("stage %s attempt %d: %w", stage, attempt, evidenceErr)
			continue
		}

		records = append(records, record)
		return result.OutputPath, records, nil
	}

	return "", records, fmt.Errorf("stage %s failed after %d attempt(s), last error: %w", stage, o.Protocol.RetryPolicy.MaxAttempts, lastErr)
}

// plural picks between two phrasings. Worth the three lines: this string is
// an operator-facing warning about an unenforced budget bound, and "1 bounds
// were unenforced" reads like a bug in the warning rather than a real gap.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
