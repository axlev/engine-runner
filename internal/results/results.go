// Package results turns a completed orchestrator.RunOutcome into the sealed,
// on-disk result tree described in docs/system-design.md section 11. It
// does no orchestration of its own: everything it writes was already
// decided by the orchestrator: which attempts happened, which succeeded,
// and what the run's fingerprints are.
package results

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/axlev/engine-runner/internal/adapters"
	"github.com/axlev/engine-runner/internal/evaluation"
	"github.com/axlev/engine-runner/internal/orchestrator"
)

// Writer writes sealed run directories under root (docs/system-design.md's
// data/results/ in a real deployment).
type Writer struct {
	root string
}

func NewWriter(root string) (*Writer, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("results: creating root %s: %w", root, err)
	}
	return &Writer{root: root}, nil
}

// WriteRun writes stages/<stage>/{request,telemetry,review-*}.json for every
// stage that was attempted, evaluation/{scores,findings}.json when the run
// completed, run.json, events.jsonl, and finally checksums.sha256 over the
// whole tree - in that order, since the checksum manifest must be written
// last to cover everything else.
func (w *Writer) WriteRun(outcome orchestrator.RunOutcome) (string, error) {
	runDir := filepath.Join(w.root, outcome.RunID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		return "", fmt.Errorf("results: creating run dir %s: %w", runDir, err)
	}

	// Written first, and written even when it is the only thing that
	// happened: a case rejected by boundary validation never ran a stage,
	// so this report is the entire record of why (section 11).
	if outcome.BoundaryValidation != nil {
		if err := writeJSONFile(filepath.Join(runDir, "boundary-validation.json"), outcome.BoundaryValidation); err != nil {
			return "", err
		}
	}

	stagesDir := filepath.Join(runDir, "stages")
	stageDocs, err := w.writeStages(stagesDir, runDir, outcome.Attempts)
	if err != nil {
		return "", err
	}

	if outcome.Status == "completed" {
		if err := writeEvaluation(runDir, stagesDir, outcome.RunID, outcome.CaseID, outcome.StageOrder); err != nil {
			return "", err
		}
	}

	if err := writeRunManifest(runDir, outcome, stageDocs); err != nil {
		return "", err
	}
	if err := writeEvents(runDir, outcome); err != nil {
		return "", err
	}
	if err := writeChecksums(runDir); err != nil {
		return "", err
	}

	return runDir, nil
}

type telemetryAttempt struct {
	Attempt int                 `json:"attempt"`
	Request adapters.RunRequest `json:"request"`
	Result  adapters.RunResult  `json:"result"`
	Error   string              `json:"error,omitempty"`
}

type telemetryDoc struct {
	Stage    string             `json:"stage"`
	Attempts []telemetryAttempt `json:"attempts"`
}

type stageOutcomeDoc struct {
	Stage          string `json:"stage"`
	Adapter        string `json:"adapter"`
	AdapterVersion string `json:"adapter_version,omitempty"`
	AuthMode       string `json:"auth_mode,omitempty"`
	Attempts       int    `json:"attempts"`
	FinalExitCode  int    `json:"final_exit_code"`
	StartedAt      string `json:"started_at,omitempty"`
	FinishedAt     string `json:"finished_at,omitempty"`

	// DurationMS is derived here rather than left for a reader to subtract.
	// Two timestamps in a sealed artifact are not a measurement until
	// something computes the difference, and nothing did: cohort timing had
	// to be worked out by hand from run.json.
	//
	// The vendor figures sit beside it so the gap between them is visible.
	// DurationMS - VendorDurationMS is engine overhead: container start,
	// mounting, and the per-attempt copy of the source snapshot. With a
	// 7,600-file tree that overhead is the number worth watching.
	DurationMS          int `json:"duration_ms"`
	VendorDurationMS    int `json:"vendor_duration_ms,omitempty"`
	VendorAPIDurationMS int `json:"vendor_api_duration_ms,omitempty"`

	// Usage is summed across every attempt, matching DurationMS above. It
	// previously carried the winning attempt's usage alone while the
	// durations beside it were already summed - so a retried stage
	// reported all of its time and only some of its money. The comment on
	// DurationMS was already the argument against that: a burned attempt
	// really did cost what it cost. Two narrow-arm cases were understated,
	// opus-c21d519d by $5.37 and opus-de6d5d30 by $5.31.
	Usage adapters.Usage `json:"usage"`

	// FinalAttemptUsage is the winning attempt's usage on its own, and it
	// is NOT redundant with Usage: per-attempt is the correct denominator
	// for a budget check, because max_cost_usd bounds one invocation
	// rather than a stage. Utilisation computed from the summed figure
	// would report a stage that retried as having blown a cap it never
	// approached.
	//
	// It also disambiguates the change above. A sealed run carrying both
	// fields has summed Usage; one carrying only `usage` predates this and
	// holds winning-attempt semantics.
	FinalAttemptUsage adapters.Usage `json:"final_attempt_usage"`

	OutputPath string `json:"output_path,omitempty"`
}

// writeStages writes stages/<stage>/{telemetry,request,review-*}.json for
// each stage that has at least one recorded attempt, and returns the
// run.json summary row for each.
func (w *Writer) writeStages(stagesDir, runDir string, attempts []orchestrator.StageAttemptRecord) ([]stageOutcomeDoc, error) {
	var order []adapters.Stage
	byStage := map[adapters.Stage][]orchestrator.StageAttemptRecord{}
	for _, rec := range attempts {
		if _, seen := byStage[rec.Stage]; !seen {
			order = append(order, rec.Stage)
		}
		byStage[rec.Stage] = append(byStage[rec.Stage], rec)
	}

	// Non-nil on purpose: a run invalidated by boundary validation has no
	// stages at all, and run-result.schema.json requires "stages" to be an
	// array - a nil slice would marshal to null and fail validation.
	docs := []stageOutcomeDoc{}
	for _, stage := range order {
		recs := byStage[stage]
		stageDir := filepath.Join(stagesDir, string(stage))
		if err := os.MkdirAll(stageDir, 0o755); err != nil {
			return nil, fmt.Errorf("results: creating %s: %w", stageDir, err)
		}

		telemetry := telemetryDoc{Stage: string(stage)}
		for _, rec := range recs {
			telemetry.Attempts = append(telemetry.Attempts, telemetryAttempt{
				Attempt: rec.Attempt, Request: rec.Request, Result: rec.Result, Error: rec.Err,
			})
		}
		if err := writeJSONFile(filepath.Join(stageDir, "telemetry.json"), telemetry); err != nil {
			return nil, err
		}

		last := recs[len(recs)-1]

		// Preserve the prompt itself, not just its hash in the run's
		// fingerprints. A prompt is experiment input, on the same footing
		// as the case bundle and the diff: when two arms of an experiment
		// differ, the next question is always what exactly changed, and a
		// hash proves two runs differed without saying how. Copying the
		// bytes makes a sealed result readable on its own, without needing
		// the repository at the right commit - which is precisely the
		// situation where the hash alone stops being enough.
		//
		// Written per stage rather than per winning attempt: every attempt
		// of a stage is given the same prompt, and a stage that failed
		// outright still needs to record what it was asked.
		if last.Request.PromptPath != "" {
			promptDst := filepath.Join(stageDir, filepath.Base(last.Request.PromptPath))
			if err := copyFile(last.Request.PromptPath, promptDst); err != nil {
				return nil, err
			}
		}

		doc := stageOutcomeDoc{
			Stage:          string(stage),
			Adapter:        last.Result.Adapter,
			AdapterVersion: last.Result.Version,
			AuthMode:       last.Result.AuthMode,
			Attempts:       len(recs),
			FinalExitCode:  last.Result.ExitCode,
			StartedAt:      formatTime(last.Result.StartedAt),
			FinishedAt:     formatTime(last.Result.FinishedAt),
			// Summed across every attempt, not just the winning one: a
			// stage that burned a repair attempt really did cost that time,
			// and a per-case total that ignored it would understate the
			// cohort.
			DurationMS:          totalDurationMS(recs),
			VendorDurationMS:    sumVendorDurationMS(recs),
			VendorAPIDurationMS: sumVendorAPIDurationMS(recs),
			Usage:               sumUsage(recs),
			FinalAttemptUsage:   last.Result.Usage,
		}

		if winning := lastSuccessful(recs); winning != nil {
			if err := writeJSONFile(filepath.Join(stageDir, "request.json"), winning.Request); err != nil {
				return nil, err
			}
			dst := filepath.Join(stageDir, reviewFileName(stage))
			if err := copyFile(winning.Result.OutputPath, dst); err != nil {
				return nil, err
			}
			rel, err := filepath.Rel(runDir, dst)
			if err != nil {
				return nil, fmt.Errorf("results: %w", err)
			}
			doc.OutputPath = rel
			doc.FinalAttemptUsage = winning.Result.Usage
			doc.FinalExitCode = winning.Result.ExitCode
			doc.StartedAt = formatTime(winning.Result.StartedAt)
			doc.FinishedAt = formatTime(winning.Result.FinishedAt)
		}

		docs = append(docs, doc)
	}
	return docs, nil
}

func lastSuccessful(recs []orchestrator.StageAttemptRecord) *orchestrator.StageAttemptRecord {
	for i := len(recs) - 1; i >= 0; i-- {
		if recs[i].Err == "" {
			return &recs[i]
		}
	}
	return nil
}

func reviewFileName(stage adapters.Stage) string {
	switch stage {
	case adapters.StageReasoner1:
		return "review-a.json"
	case adapters.StageReasoner2:
		return "review-b.json"
	case adapters.StageReasoner3:
		return "review-c.json"
	default:
		return string(stage) + ".json"
	}
}

// notEvaluatedDoc is written in place of scores when the run did not
// produce all three review outputs the deterministic evaluator needs. It is
// a statement, not a score: a one-stage protocol has nothing for a
// disposition-delta evaluator to compare, and writing zeros would let an
// unscored run be read as a scored one.
type notEvaluatedDoc struct {
	SchemaVersion string   `json:"schema_version"`
	RunID         string   `json:"run_id"`
	CaseID        string   `json:"case_id"`
	Reason        string   `json:"reason"`
	StagesPresent []string `json:"stages_present"`
	StagesAbsent  []string `json:"stages_absent"`
}

func writeEvaluation(runDir, stagesDir, runID, caseID string, stageOrder []adapters.Stage) error {
	evalDir := filepath.Join(runDir, "evaluation")
	if err := os.MkdirAll(evalDir, 0o755); err != nil {
		return fmt.Errorf("results: creating %s: %w", evalDir, err)
	}

	reviews := []struct {
		stage adapters.Stage
		path  string
	}{
		{adapters.StageReasoner1, filepath.Join(stagesDir, "reasoner-1", "review-a.json")},
		{adapters.StageReasoner2, filepath.Join(stagesDir, "reasoner-2", "review-b.json")},
		{adapters.StageReasoner3, filepath.Join(stagesDir, "reasoner-3", "review-c.json")},
	}
	var present, absent []string
	for _, r := range reviews {
		if _, err := os.Stat(r.path); err == nil {
			present = append(present, string(r.stage))
		} else {
			absent = append(absent, string(r.stage))
		}
	}
	// The H1 protocols declare one stage (arm G) or two (arm T) on their
	// own schemas, and the case-level scorer (pre-registration section 5)
	// applies. The arm rule comes from the order the protocol DECLARED,
	// recorded on the outcome - never from which files exist, or a T run
	// whose stage B was skipped would be scored as G. Any disagreement
	// between the declaration and what is present fails the seal.
	// A one- or two-stage protocol on the pilot schemas (the fixture
	// protocols from migration #2) is not an H1 run either; it falls
	// through to not-evaluated.json as before.
	if armRule := h1ArmRuleFor(stageOrder); armRule != "" && reviewSchemaVersion(reviews[0].path) == "h1-review-a/v1" {
		reviewB := ""
		if armRule == evaluation.ArmRuleT {
			reviewB = reviews[1].path
			if _, err := os.Stat(reviewB); err != nil {
				return fmt.Errorf("results: protocol declares stage reasoner-2 but the run has no review-b.json; refusing to score a T run as G")
			}
		}
		verdict, err := evaluation.ScoreCase(runID, caseID, armRule, reviews[0].path, reviewB)
		if err != nil {
			return fmt.Errorf("results: scoring case: %w", err)
		}
		return writeJSONFile(filepath.Join(evalDir, "verdict.json"), verdict)
	}
	if len(absent) > 0 {
		return writeJSONFile(filepath.Join(evalDir, "not-evaluated.json"), notEvaluatedDoc{
			SchemaVersion: "not-evaluated/v1",
			RunID:         runID,
			CaseID:        caseID,
			Reason:        "the deterministic evaluator scores stage-2 and stage-3 dispositions against stage-1 findings and needs all three review outputs; this protocol did not run every stage. Case-level scoring for shorter protocols is a separate evaluator.",
			StagesPresent: present,
			StagesAbsent:  absent,
		})
	}

	scores, findings, err := evaluation.Evaluate(runID, caseID, reviews[0].path, reviews[1].path, reviews[2].path)
	if err != nil {
		return fmt.Errorf("results: evaluating: %w", err)
	}
	if err := writeJSONFile(filepath.Join(evalDir, "scores.json"), scores); err != nil {
		return err
	}
	return writeJSONFile(filepath.Join(evalDir, "findings.json"), findings)
}

// h1ArmRuleFor maps a declared stage order onto the section 5 arm rule:
// exactly [reasoner-1] is G, exactly [reasoner-1, reasoner-2] is T, and
// anything else (pilot-v1's three stages included) is not an H1 shape and
// returns "".
func h1ArmRuleFor(order []adapters.Stage) string {
	switch {
	case len(order) == 1 && order[0] == adapters.StageReasoner1:
		return evaluation.ArmRuleG
	case len(order) == 2 && order[0] == adapters.StageReasoner1 && order[1] == adapters.StageReasoner2:
		return evaluation.ArmRuleT
	}
	return ""
}

// reviewSchemaVersion reads only the schema_version of a stage output;
// an unreadable or unversioned document reads as "".
func reviewSchemaVersion(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var doc struct {
		SchemaVersion string `json:"schema_version"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return ""
	}
	return doc.SchemaVersion
}

type runResultDoc struct {
	SchemaVersion      string                    `json:"schema_version"`
	RunID              string                    `json:"run_id"`
	CaseID             string                    `json:"case_id"`
	ProtocolVersion    string                    `json:"protocol_version"`
	CreatedAt          string                    `json:"created_at"`
	SealedAt           string                    `json:"sealed_at"`
	Status             string                    `json:"status"`
	InvalidationReason string                    `json:"invalidation_reason,omitempty"`
	Fingerprints       orchestrator.Fingerprints `json:"fingerprints"`
	StageOrder         []string                  `json:"stage_order"`
	Stages             []stageOutcomeDoc         `json:"stages"`
}

func writeRunManifest(runDir string, outcome orchestrator.RunOutcome, stageDocs []stageOutcomeDoc) error {
	status := outcome.Status
	if status == "" {
		status = "failed"
	}
	doc := runResultDoc{
		SchemaVersion:   "run-result/v1",
		RunID:           outcome.RunID,
		CaseID:          outcome.CaseID,
		ProtocolVersion: outcome.ProtocolVersion,
		CreatedAt:       formatTime(outcome.CreatedAt),
		SealedAt:        formatTime(time.Now().UTC()),
		Status:          status,
		Fingerprints:    outcome.Fingerprints,
		StageOrder:      stageNames(outcome.StageOrder),
		Stages:          stageDocs,
	}
	if status != "completed" {
		doc.InvalidationReason = outcome.FailureReason
		if doc.InvalidationReason == "" {
			doc.InvalidationReason = "unknown failure"
		}
	}
	return writeJSONFile(filepath.Join(runDir, "run.json"), doc)
}

type event struct {
	Time    string `json:"time"`
	Type    string `json:"type"`
	Stage   string `json:"stage,omitempty"`
	Attempt int    `json:"attempt,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

// writeEvents reconstructs a chronological event log from the outcome's
// attempt records. This is a simplification for the fixture-only MVP: a
// live event stream emitted during orchestrator.Run itself is a reasonable
// future enhancement, not required for Milestone 1's exit criterion.
func writeEvents(runDir string, outcome orchestrator.RunOutcome) error {
	path := filepath.Join(runDir, "events.jsonl")
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("results: creating %s: %w", path, err)
	}
	defer f.Close()

	enc := json.NewEncoder(f)
	for _, rec := range outcome.Attempts {
		ev := event{Time: formatTime(rec.Result.FinishedAt), Stage: string(rec.Stage), Attempt: rec.Attempt}
		if rec.Err == "" {
			ev.Type = "stage_attempt_succeeded"
		} else {
			ev.Type = "stage_attempt_failed"
			ev.Detail = rec.Err
		}
		if err := enc.Encode(ev); err != nil {
			return fmt.Errorf("results: writing event: %w", err)
		}
	}

	final := event{Time: formatTime(time.Now().UTC())}
	if outcome.Status == "completed" {
		final.Type = "run_completed"
	} else {
		final.Type = "run_failed"
		final.Detail = outcome.FailureReason
	}
	if err := enc.Encode(final); err != nil {
		return fmt.Errorf("results: writing final event: %w", err)
	}
	return nil
}

// writeChecksums hashes every file already written under runDir and writes
// checksums.sha256 last, so the manifest covers the complete, final tree -
// including run.json and events.jsonl, which are themselves written before
// this call.
func writeChecksums(runDir string) error {
	var lines []string
	err := filepath.WalkDir(runDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(runDir, path)
		if err != nil {
			return err
		}
		if rel == "checksums.sha256" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		lines = append(lines, fmt.Sprintf("%s  %s", hex.EncodeToString(sum[:]), filepath.ToSlash(rel)))
		return nil
	})
	if err != nil {
		return fmt.Errorf("results: computing checksums under %s: %w", runDir, err)
	}
	sort.Strings(lines)
	content := strings.Join(lines, "\n")
	if content != "" {
		content += "\n"
	}
	if err := os.WriteFile(filepath.Join(runDir, "checksums.sha256"), []byte(content), 0o644); err != nil {
		return fmt.Errorf("results: writing checksums.sha256: %w", err)
	}
	return nil
}

func stageNames(order []adapters.Stage) []string {
	names := make([]string, 0, len(order))
	for _, s := range order {
		names = append(names, string(s))
	}
	return names
}

func writeJSONFile(path string, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("results: marshaling %s: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("results: creating %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("results: writing %s: %w", path, err)
	}
	return nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("results: reading %s: %w", src, err)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("results: creating %s: %w", filepath.Dir(dst), err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return fmt.Errorf("results: writing %s: %w", dst, err)
	}
	return nil
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// sumUsage totals what a stage was billed across every attempt, failed ones
// included. A retry is not free, and an attempt killed at its budget cap is
// the most expensive kind: it is billed in full and returns no review.
func sumUsage(recs []orchestrator.StageAttemptRecord) adapters.Usage {
	var total adapters.Usage
	for _, rec := range recs {
		total.InputTokens += rec.Result.Usage.InputTokens
		total.OutputTokens += rec.Result.Usage.OutputTokens
		total.CostUSD += rec.Result.Usage.CostUSD
		total.ToolCalls += rec.Result.Usage.ToolCalls
	}
	return total
}

// totalDurationMS sums wall-clock time across every attempt of a stage.
func totalDurationMS(recs []orchestrator.StageAttemptRecord) int {
	total := 0
	for _, rec := range recs {
		if rec.Result.FinishedAt.IsZero() || rec.Result.StartedAt.IsZero() {
			continue
		}
		if d := rec.Result.FinishedAt.Sub(rec.Result.StartedAt); d > 0 {
			total += int(d.Milliseconds())
		}
	}
	return total
}

func sumVendorDurationMS(recs []orchestrator.StageAttemptRecord) int {
	total := 0
	for _, rec := range recs {
		total += rec.Result.VendorDurationMS
	}
	return total
}

func sumVendorAPIDurationMS(recs []orchestrator.StageAttemptRecord) int {
	total := 0
	for _, rec := range recs {
		total += rec.Result.VendorAPIDurationMS
	}
	return total
}
