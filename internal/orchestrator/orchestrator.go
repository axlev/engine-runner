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

	Protocol  *Protocol
	Adapter   adapters.AgentAdapter
	Builder   *contextbuilder.Builder
	Runner    *runner.Runner
	Validator *SchemaValidator
}

// New wires an Orchestrator. workspaceRoot is where the Runner creates fresh
// per-attempt workspace directories.
func New(repoRoot, protocolPath string, protocol *Protocol, adapter adapters.AgentAdapter, workspaceRoot string) (*Orchestrator, error) {
	r, err := runner.New(workspaceRoot)
	if err != nil {
		return nil, fmt.Errorf("orchestrator: %w", err)
	}
	return &Orchestrator{
		RepoRoot:     repoRoot,
		ProtocolPath: protocolPath,
		Protocol:     protocol,
		Adapter:      adapter,
		Builder:      contextbuilder.New(),
		Runner:       r,
		Validator:    NewSchemaValidator(repoRoot),
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
	validation, err := boundaryvalidator.Validate(caseID, bundleRoot)
	if err != nil {
		return outcome, fmt.Errorf("orchestrator: boundary validation could not run: %w", err)
	}
	outcome.BoundaryValidation = &validation

	// Fingerprints are computed even for a rejected case, so its sealed
	// record still says which protocol, prompts and schemas were in play
	// when it was rejected. Validation's diagnosis stays the reported
	// error either way - it is the more actionable one.
	fp, fpErr := computeStaticFingerprints(o.ProtocolPath, o.RepoRoot, bundleRoot, o.Protocol)
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
			RunID:          runID,
			CaseID:         caseID,
			Stage:          stage,
			WorkspacePath:  stageCtx.WorkspacePath,
			PromptPath:     stageCtx.PromptPath,
			OutputSchema:   stageProto.OutputSchema,
			Model:          stageProto.Model,
			ReasoningLevel: stageProto.ReasoningLevel,
			Budget:         stageProto.Budget.toAdapterBudget(),
			Environment: map[string]string{
				adapters.AttemptEnvKey: strconv.Itoa(attempt),
			},
		}

		result, runErr := o.Adapter.Run(ctx, req)
		result.Attempt = attempt
		if result.Adapter == "" {
			result.Adapter = o.Adapter.Name()
		}

		record := StageAttemptRecord{Stage: stage, Attempt: attempt, Request: req, Result: result}

		if runErr != nil {
			record.Err = runErr.Error()
			records = append(records, record)
			lastErr = fmt.Errorf("stage %s attempt %d: adapter run failed: %w", stage, attempt, runErr)
			continue
		}

		if err := o.Validator.ValidateFile(result.OutputPath, stageProto.OutputSchema); err != nil {
			record.Err = err.Error()
			records = append(records, record)
			lastErr = fmt.Errorf("stage %s attempt %d: %w", stage, attempt, err)
			continue
		}

		records = append(records, record)
		return result.OutputPath, records, nil
	}

	return "", records, fmt.Errorf("stage %s failed after %d attempt(s), last error: %w", stage, o.Protocol.RetryPolicy.MaxAttempts, lastErr)
}
