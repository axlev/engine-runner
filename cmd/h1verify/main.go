// h1verify runs H1.1's skeptic pass: a different-vendor model is shown one
// finding at a time and asked whether the defect is real.
//
//	h1verify -runs build/results -cohort <cohort dir> \
//	         -adapter-image engine-runner/adapter-codex:0.153.2 \
//	         -scans <contamscan out> -out <evaluator-only dir>
//
// ONE RUN PER QUALIFYING FINDING, not per case. A case is RISKY if ANY
// finding sits at or above the severity threshold, so overturning the
// verdict means rejecting every one of them: verifying only the
// highest-ranked finding would leave the case RISKY whenever a second
// qualifier survives, which is the majority of RISKY cases in this cohort.
//
// NO RETRY ON INVALID OUTPUT. A response that fails the schema is sealed as
// INCONCLUSIVE with the raw text kept. Retrying would bias the measurement
// in the exact dimension H1.1 measures: if a REJECTED response fails
// validation and the retry returns CONFIRMED, the retry has moved the
// quantity being measured. The count of unparseable first responses is
// reported beside every figure.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/axlev/engine-runner/internal/adapters"
	"github.com/axlev/engine-runner/internal/adapters/codex"
	"github.com/axlev/engine-runner/internal/contextbuilder"
	"github.com/axlev/engine-runner/internal/evaluation"
	"github.com/axlev/engine-runner/internal/h1score"
	"github.com/axlev/engine-runner/internal/orchestrator"
	"github.com/axlev/engine-runner/internal/runner"
)

const (
	// The CLI default as of codex 0.153.2, recorded so a sealed report
	// names the model rather than an empty string.
	defaultCodexModel = "gpt-6-astra"
	schemaVersion     = "h1-verify/v1"
	promptPath        = "prompts/h1.1-v1/verifier.md"
	schemaFile        = "h1-verify.schema.json"
)

// Unit is one (case, arm, finding) to verify.
type Unit struct {
	CaseID    string `json:"case_id"`
	Arm       string `json:"arm"`
	RunID     string `json:"run_id"`
	FindingID string `json:"finding_id"`
	Severity  string `json:"severity"`
	Mechanism string `json:"mechanism"`
	File      string `json:"file"`
	Rank      int    `json:"rank"`
}

// Report is h1-verify-report/v1: one sealed verification.
type Report struct {
	SchemaVersion   string `json:"schema_version"`
	Unit            Unit   `json:"unit"`
	Model           string `json:"model"`
	ReasoningEffort string `json:"reasoning_effort"`
	AuthMode        string `json:"auth_mode"`
	Disposition     string `json:"disposition"`
	Reason          string `json:"reason"`
	Mechanism       string `json:"mechanism_restated,omitempty"`
	Evidence        []any  `json:"evidence,omitempty"`
	Unparseable     bool   `json:"unparseable_first_response,omitempty"`
	RawResponse     string `json:"raw_response,omitempty"`
	InputTokens     int    `json:"input_tokens,omitempty"`
	OutputTokens    int    `json:"output_tokens,omitempty"`
	DurationMS      int64  `json:"duration_ms,omitempty"`
	Error           string `json:"error,omitempty"`
}

func main() {
	runs := flag.String("runs", "build/results", "sealed run directories, all arms")
	cohort := flag.String("cohort", "", "published cohort directory, one case-<id>/ per case (required)")
	scans := flag.String("scans", "", "contamscan output directory; voided runs are not verified. A voided run is dropped from every figure, so verifying one spends a call on a finding nothing can count")
	out := flag.String("out", "", "evaluator-only output directory (required)")
	image := flag.String("adapter-image", "engine-runner/adapter-codex:0.153.2", "container image for the verifier CLI")
	model := flag.String("model", "", "verifier model; empty uses the CLI default (gpt-6-astra as of codex 0.153.2)")
	effort := flag.String("reasoning-effort", "high", "verifier reasoning effort. Defaults to high to match the arms being judged (h1-opus-wide.yaml: reasoning_level high). Codex's own default is NONE, and a skeptic that is not reasoning agrees with plausible claims - which would make a null result a fact about the configuration, not about cross-vendor verification")
	only := flag.String("only", "", "comma-separated case ids (default: every eligible case)")
	subsetPath := flag.String("subset", "", "h1.1-subset/v1 file selecting exact (case_id, arm) pairs. Selection is per PAIR, not per case: an arm can be voided while the other is not, and -only cannot express that")
	maxCost := flag.Float64("max-cost", 3.0, "per-call cost ceiling (no-op under a subscription, which reports no billable figure)")
	maxSeconds := flag.Int("max-seconds", 900, "per-call wall clock ceiling")
	maxStalls := flag.Int("max-stalls", 4, "how many times one verification waits out a rate limit before being left for a later run")
	stallFallback := flag.Duration("stall-wait", 20*time.Minute, "how long to wait when the vendor gives no reset time")
	cap_ := flag.Int("cap", 120, "refuse to start more than this many verifications; a runaway guard, not a budget")
	workspaceRoot := flag.String("workspace-root", filepath.Join(os.Getenv("HOME"), ".cache", "engine-runner", "runs"), "root for per-attempt workspaces")
	keepWorkspace := flag.Bool("keep-workspace", false, "keep each verification's workspace instead of reclaiming it on success")
	dryRun := flag.Bool("dry-run", false, "enumerate and seal the prompts without calling the model")
	flag.Parse()
	for name, v := range map[string]string{"-cohort": *cohort, "-out": *out} {
		if v == "" {
			fmt.Fprintf(os.Stderr, "h1verify: %s is required\n", name)
			os.Exit(2)
		}
	}
	check(os.MkdirAll(*out, 0o755))

	voided, err := h1score.LoadVoided(*scans)
	check(err)
	units, skippedVoid, err := enumerate(*runs, voided, splitCSV(*only))
	check(err)
	if *subsetPath != "" {
		var dropped int
		units, dropped, err = applySubset(units, *subsetPath)
		check(err)
		fmt.Printf("h1verify: subset %s selects %d verification(s); %d outside it dropped\n",
			filepath.Base(*subsetPath), len(units), dropped)
	}
	if len(units) == 0 {
		check(fmt.Errorf("h1verify: no RISKY qualifying findings to verify under %s", *runs))
	}
	fmt.Printf("h1verify: %d verification(s) across %d run(s); %d skipped as scan-voided\n",
		len(units), countRuns(units), skippedVoid)
	if len(units) > *cap_ {
		check(fmt.Errorf("h1verify: %d verifications exceeds -cap %d; raise it deliberately or narrow with -only", len(units), *cap_))
	}

	var adapter *codex.Adapter
	if !*dryRun {
		a, err := codex.New(*image, nil)
		check(err)
		adapter = a
		fmt.Printf("h1verify: auth_mode %s\n", a.Credentials.Kind)
	}
	r, err := runner.New(*workspaceRoot)
	check(err)
	validator := orchestrator.NewSchemaValidator("schemas")

	var done, confirmed, rejected, inconclusive, unparseable, failed, stalls int
	var inTok, outTok int
	for _, u := range units {
		dest := filepath.Join(*out, u.RunID+"."+u.FindingID+".h1-verify.json")
		if _, err := os.Stat(dest); err == nil {
			fmt.Printf("  %s/%s: already verified\n", u.RunID, u.FindingID)
			continue
		}
		// A stall is not a failure: the vendor is rationing, so the unit
		// is still runnable and must be waited out and retried rather
		// than sealed as failed. Sealing it would lose the finding AND
		// keep hammering the limit for every remaining unit.
		var rep Report
		for attempt := 1; ; attempt++ {
			rep = verifyOne(context.Background(), adapter, r, validator, u, *cohort, *model, *effort, *maxCost, *maxSeconds, *dryRun, *keepWorkspace)
			matched, stalled := isStall(rep.Error)
			if !stalled || attempt > *maxStalls {
				if stalled {
					fmt.Printf("  %s/%s: still rate-limited after %d wait(s); leaving for a later run\n", u.RunID, u.FindingID, *maxStalls)
				}
				break
			}
			// Back off across consecutive stalls: a quota that is still
			// exhausted after one wait is usually exhausted for a while,
			// and re-asking at a fixed interval neither helps nor is free.
			backoff := *stallFallback << uint(minInt(attempt-1, 3))
			w := stallWait(rep.Error, time.Now(), backoff)
			stalls++
			fmt.Printf("  %s/%s: rate limited (%q); waiting %s then retrying\n",
				u.RunID, u.FindingID, matched, fmtWait(w))
			time.Sleep(w)
		}
		if rep.Error != "" {
			if _, stalled := isStall(rep.Error); stalled {
				// Not sealed: a rate-limited unit is unverified, not
				// verified-as-inconclusive, and resume must retry it.
				continue
			}
		}
		check(writeJSON(dest, rep))
		done++
		inTok += rep.InputTokens
		outTok += rep.OutputTokens
		switch {
		case rep.Error != "":
			failed++
		case rep.Unparseable:
			unparseable++
			inconclusive++
		case rep.Disposition == "CONFIRMED":
			confirmed++
		case rep.Disposition == "REJECTED":
			rejected++
		default:
			inconclusive++
		}
		fmt.Printf("  %s/%s: %s%s\n", u.RunID, u.FindingID, dispOf(rep), errSuffix(rep))
	}

	fmt.Println()
	fmt.Printf("verified %d: CONFIRMED %d  REJECTED %d  INCONCLUSIVE %d (unparseable %d)  failed %d\n",
		done, confirmed, rejected, inconclusive, unparseable, failed)
	fmt.Printf("tokens: %d in, %d out (no billable figure is reported under a subscription)\n", inTok, outTok)
	if stalls > 0 {
		fmt.Printf("rate-limit waits: %d\n", stalls)
	}
	if failed > 0 {
		fmt.Fprintf(os.Stderr, "h1verify: %d verification(s) FAILED and were sealed with an error; re-run to pick them up\n", failed)
		os.Exit(1)
	}
}

// enumerate finds every qualifying finding of every RISKY run, by
// re-scoring the sealed reviews with the same rule the scorer applies.
// Nothing is recomputed here: evaluation.ScoreCase owns the threshold and
// the disposition set, so this cannot drift from the verdict being tested.
func enumerate(runsRoot string, voided map[string]bool, only []string) ([]Unit, int, error) {
	want := map[string]bool{}
	for _, id := range only {
		want[id] = true
	}
	var units []Unit
	var skippedVoid int
	manifests, _ := filepath.Glob(filepath.Join(runsRoot, "*", "run.json"))
	sort.Strings(manifests)
	for _, m := range manifests {
		var run struct {
			RunID           string `json:"run_id"`
			CaseID          string `json:"case_id"`
			ProtocolVersion string `json:"protocol_version"`
			Status          string `json:"status"`
		}
		if err := readJSON(m, &run); err != nil {
			return nil, 0, err
		}
		if run.Status != "completed" {
			continue
		}
		if len(want) > 0 && !want[run.CaseID] {
			continue
		}
		if voided[run.RunID] {
			skippedVoid++
			continue
		}
		arm, armRule := armOf(run.ProtocolVersion)
		if arm == "" {
			continue
		}
		dir := filepath.Dir(m)
		reviewA := filepath.Join(dir, "stages", "reasoner-1", "review-a.json")
		reviewB := ""
		if armRule == evaluation.ArmRuleT {
			reviewB = filepath.Join(dir, "stages", "reasoner-2", "review-b.json")
		}
		v, err := evaluation.ScoreCase(run.RunID, run.CaseID, armRule, reviewA, reviewB)
		if err != nil {
			return nil, 0, fmt.Errorf("h1verify: re-scoring %s: %w", run.RunID, err)
		}
		if v.Verdict != evaluation.VerdictRisky {
			continue
		}
		for i, f := range v.QualifyingFindings {
			units = append(units, Unit{
				CaseID: run.CaseID, Arm: arm, RunID: run.RunID,
				FindingID: f.ID, Severity: f.Severity,
				Mechanism: f.Mechanism, File: f.File, Rank: i,
			})
		}
	}
	// Order by CASE, then arm: T and G for the same case are verified
	// together rather than all of one arm and then the other. The sealed
	// runs glob as g-case-* before t-case-*, which put every G first - so
	// a pass interrupted part way (and under a subscription quota, that is
	// the normal case) yielded 13 G verifications and no T at all, and
	// T'-T had nothing to say. Pairing them makes a partial pass a
	// usable partial answer: the arms are compared on the same cases,
	// which also controls for case difficulty rather than hoping the two
	// samples happen to match.
	sort.Slice(units, func(i, j int) bool {
		if units[i].CaseID != units[j].CaseID {
			return units[i].CaseID < units[j].CaseID
		}
		if units[i].Arm != units[j].Arm {
			return units[i].Arm < units[j].Arm
		}
		return units[i].Rank < units[j].Rank
	})
	return units, skippedVoid, nil
}

func verifyOne(ctx context.Context, a *codex.Adapter, r *runner.Runner, v *orchestrator.SchemaValidator,
	u Unit, cohort, model, effort string, maxCost float64, maxSeconds int, dryRun, keepWorkspace bool) Report {

	// Record what actually ran, not what was requested: an empty -model
	// means the CLI default, and a sealed report that says "" leaves a
	// reader guessing which model produced the verdict.
	recorded := model
	if recorded == "" {
		recorded = defaultCodexModel
	}
	rep := Report{SchemaVersion: schemaVersion, Unit: u, Model: recorded, ReasoningEffort: effort}
	workRunID := "verify-" + u.RunID + "-" + u.FindingID
	ws, err := r.FreshWorkspace(workRunID, adapters.StageReasoner3, 1)
	if err != nil {
		rep.Error = err.Error()
		return rep
	}

	prompt, err := buildPrompt(u)
	if err != nil {
		rep.Error = err.Error()
		return rep
	}
	promptFile := filepath.Join(filepath.Dir(ws), "verifier-"+u.FindingID+".md")
	if err := os.WriteFile(promptFile, []byte(prompt), 0o644); err != nil {
		rep.Error = err.Error()
		return rep
	}

	sc, err := contextbuilder.New().Prepare(adapters.StageReasoner3,
		filepath.Join(cohort, u.CaseID), promptFile, ws,
		contextbuilder.HandoffPolicy{}, contextbuilder.StageInputs{})
	if err != nil {
		rep.Error = err.Error()
		return rep
	}
	if dryRun {
		rep.Disposition = "INCONCLUSIVE"
		rep.Reason = "dry run: prompt sealed, model not called"
		return rep
	}

	cctx, cancel := context.WithTimeout(ctx, time.Duration(maxSeconds)*time.Second)
	defer cancel()
	started := time.Now()
	res, err := a.Run(cctx, adapters.RunRequest{
		RunID: workRunID, CaseID: u.CaseID, Stage: adapters.StageReasoner3,
		WorkspacePath: sc.WorkspacePath, PromptPath: sc.PromptPath,
		OutputSchema: schemaFile, Model: model, ReasoningLevel: effort,
		Tools:  []string{"Read", "Grep", "Glob"},
		Budget: adapters.Budget{MaxCostUSD: maxCost, MaxWallClockSeconds: maxSeconds, MaxOutputTokens: 16000},
	})
	rep.DurationMS = time.Since(started).Milliseconds()
	rep.InputTokens, rep.OutputTokens = res.Usage.InputTokens, res.Usage.OutputTokens
	rep.AuthMode = res.AuthMode
	if err != nil {
		rep.Error = err.Error()
		return rep
	}

	raw, readErr := os.ReadFile(res.OutputPath)
	if readErr != nil {
		rep.Error = readErr.Error()
		return rep
	}
	// No retry: an unparseable response is INCONCLUSIVE with the text
	// kept. Retrying would move the measured quantity.
	if err := v.ValidateFile(res.OutputPath, schemaFile); err != nil {
		rep.Unparseable = true
		rep.Disposition = "INCONCLUSIVE"
		rep.Reason = "response did not satisfy " + schemaVersion + ": " + err.Error()
		rep.RawResponse = string(raw)
		return rep
	}
	var doc struct {
		FindingID   string `json:"finding_id"`
		Disposition string `json:"disposition"`
		Mechanism   string `json:"mechanism_restated"`
		Evidence    []any  `json:"evidence"`
		Reason      string `json:"reason"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		rep.Unparseable = true
		rep.Disposition = "INCONCLUSIVE"
		rep.Reason = "response did not parse: " + err.Error()
		rep.RawResponse = string(raw)
		return rep
	}
	// A verdict attributed to the wrong finding is not a datum about this
	// one, and silently counting it would corrupt the denominator.
	if doc.FindingID != u.FindingID {
		rep.Unparseable = true
		rep.Disposition = "INCONCLUSIVE"
		rep.Reason = fmt.Sprintf("response is for finding %q but %q was sent", doc.FindingID, u.FindingID)
		rep.RawResponse = string(raw)
		return rep
	}
	rep.Disposition, rep.Reason, rep.Mechanism, rep.Evidence = doc.Disposition, doc.Reason, doc.Mechanism, doc.Evidence
	if !keepWorkspace {
		_ = r.Reclaim(workRunID)
	}
	return rep
}

func buildPrompt(u Unit) (string, error) {
	base, err := os.ReadFile(promptPath)
	if err != nil {
		return "", fmt.Errorf("h1verify: reading %s: %w", promptPath, err)
	}
	var b strings.Builder
	b.Write(base)
	b.WriteString("\n\n## Finding under review\n\n")
	fmt.Fprintf(&b, "- `finding_id`: `%s`\n", u.FindingID)
	if u.File != "" {
		fmt.Fprintf(&b, "- reported location: `%s`\n", u.File)
	}
	if u.Severity != "" {
		fmt.Fprintf(&b, "- reported severity: %s\n", u.Severity)
	}
	b.WriteString("\n### Claimed defect\n\n")
	b.WriteString(u.Mechanism)
	b.WriteString("\n")
	return b.String(), nil
}

// armOf maps a protocol version to the arm label and the scoring rule.
// Anything else is not an H1 arm and is not verified.
func armOf(protocolVersion string) (string, string) {
	switch protocolVersion {
	case "h1-t-v1":
		return "T", evaluation.ArmRuleT
	case "h1-g-v1":
		return "G", evaluation.ArmRuleG
	}
	return "", ""
}

func countRuns(units []Unit) int {
	seen := map[string]bool{}
	for _, u := range units {
		seen[u.RunID] = true
	}
	return len(seen)
}

func dispOf(r Report) string {
	if r.Error != "" {
		return "FAILED"
	}
	if r.Unparseable {
		return "INCONCLUSIVE (unparseable)"
	}
	return r.Disposition
}

func errSuffix(r Report) string {
	if r.Error == "" {
		return ""
	}
	return ": " + r.Error
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func readJSON(path string, into any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, into)
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "h1verify:", err)
		os.Exit(1)
	}
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// subsetDoc is h1.1-subset/v1: the exact (case, arm) pairs to verify. The
// evaluator picks them, because a balanced pool needs labels and this side
// does not have them; the file itself carries none - case ids and arms only.
type subsetDoc struct {
	SchemaVersion string `json:"schema_version"`
	Pairs         []struct {
		CaseID string `json:"case_id"`
		Arm    string `json:"arm"`
	} `json:"pairs"`
}

// applySubset narrows the units to the selected pairs.
//
// Per PAIR, not per case, and that distinction is the point: the section 8
// scan voids one arm's RUN, so a case can be voided for G and live for T.
// Selecting by case id would pull the voided arm back in - which is how two
// of the first eleven verifications landed on a scan-voided pair and had to
// be discarded.
func applySubset(units []Unit, path string) ([]Unit, int, error) {
	var doc subsetDoc
	if err := readJSON(path, &doc); err != nil {
		return nil, 0, fmt.Errorf("h1verify: reading subset %s: %w", path, err)
	}
	if doc.SchemaVersion != "h1.1-subset/v1" {
		return nil, 0, fmt.Errorf("h1verify: %s is %q, want h1.1-subset/v1", path, doc.SchemaVersion)
	}
	if len(doc.Pairs) == 0 {
		return nil, 0, fmt.Errorf("h1verify: subset %s selects no pairs", path)
	}
	want := make(map[string]bool, len(doc.Pairs))
	for _, p := range doc.Pairs {
		if p.CaseID == "" || p.Arm == "" {
			return nil, 0, fmt.Errorf("h1verify: subset %s has a pair missing case_id or arm", path)
		}
		want[p.Arm+"/"+p.CaseID] = true
	}
	var kept []Unit
	var dropped int
	seen := map[string]bool{}
	for _, u := range units {
		k := u.Arm + "/" + u.CaseID
		if want[k] {
			kept = append(kept, u)
			seen[k] = true
			continue
		}
		dropped++
	}
	// A selected pair with no unit means the pool and the sealed runs
	// disagree about what is verifiable; running a partial subset silently
	// would hand back a pool the scorer cannot interpret.
	var missing []string
	for k := range want {
		if !seen[k] {
			missing = append(missing, k)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return nil, 0, fmt.Errorf("h1verify: subset selects %d pair(s) with no qualifying finding in the sealed runs: %s",
			len(missing), strings.Join(missing, ", "))
	}
	return kept, dropped, nil
}
