// Command judge runs the mechanism-agreement evaluation.
//
// EVALUATOR-ONLY. It mounts fix patches - the answer key at line granularity
// - into a container. internal/oracle's isolation tests keep the review path
// away from this data and from this binary's inputs.
//
// Two calls per case. Call 1 describes what each fix repaired with the
// findings absent from its context entirely; call 2 rules on findings against
// those descriptions, passed in frozen. The split is structural: "do not look
// at the findings yet" cannot be honoured while the findings are in the
// context window.
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
	"github.com/axlev/engine-runner/internal/adapters/claude"
	"github.com/axlev/engine-runner/internal/judge"
	"github.com/axlev/engine-runner/internal/oracle"
)

type fixInput struct {
	FixID string `json:"fix_id"`
	Patch string `json:"patch"`
}

func main() {
	cohort := flag.String("cohort", "/home/alex/repos/miner/output/frr-pilot-cohort", "oracle bundles")
	retro := flag.String("retro", "/home/alex/repos/miner/output/frr-pilot-retrospective-evaluator-only", "materialised fixes")
	results := flag.String("results", "build/results", "sealed run directories")
	out := flag.String("out", "", "evaluator-only output directory (required)")
	image := flag.String("image", "engine-runner/adapter-claude:2.1.263", "adapter image")
	model := flag.String("model", "claude-opus-5", "judge model")
	only := flag.String("only", "", "comma-separated case ids to run (default: all with strong-tier fixes)")
	single := flag.Bool("single-call", false, "A/B control: one call producing mechanisms and verdicts together")
	maxCost := flag.Float64("max-cost", 3.0, "per-call cost ceiling")
	flag.Parse()

	if *out == "" {
		fatal(fmt.Errorf("-out is required"))
	}
	if err := os.MkdirAll(*out, 0o700); err != nil {
		fatal(err)
	}

	adapter, err := claude.New(*image, nil)
	if err != nil {
		fatal(err)
	}

	cases, err := discover(*cohort, *retro, *results, splitCSV(*only))
	if err != nil {
		fatal(err)
	}
	if len(cases) == 0 {
		fatal(fmt.Errorf("no cases with strong-tier fixes found"))
	}

	ctx := context.Background()
	var scored []judge.CaseVerdicts
	index := map[string]judge.PooledFinding{}
	totalCost := 0.0

	for _, c := range cases {
		fmt.Fprintf(os.Stderr, "judge: %s - %d fixes, %d pooled findings\n", c.caseID, len(c.fixes), len(c.pool.Findings))
		for _, f := range c.pool.Findings {
			index[f.OpaqueID] = f
		}

		// Resume: an expensive run must not re-pay for work already
		// sealed. The saved output is the same artifact the call would
		// produce, so reusing it changes nothing except the bill.
		label := "verdicts"
		if *single {
			label = "single"
		}
		if prior, err := os.ReadFile(filepath.Join(*out, c.caseID+"-"+label+".json")); err == nil {
			v, _, perr := parseVerdicts(c, prior, 0)
			if perr == nil {
				fmt.Fprintf(os.Stderr, "judge: %s reusing sealed output\n", c.caseID)
				scored = append(scored, v)
				continue
			}
		}

		var verdicts judge.CaseVerdicts
		var cost float64
		if *single {
			verdicts, cost, err = runSingle(ctx, adapter, *out, *model, *maxCost, c)
		} else {
			verdicts, cost, err = runTwoCall(ctx, adapter, *out, *model, *maxCost, c)
		}
		totalCost += cost
		if err != nil {
			fmt.Fprintf(os.Stderr, "judge: %s FAILED after $%.2f: %v\n", c.caseID, cost, err)
			continue
		}
		scored = append(scored, verdicts)
		fmt.Fprintf(os.Stderr, "judge: %s done, $%.2f (running $%.2f)\n", c.caseID, cost, totalCost)
	}

	rep, unattributed := judge.Score(scored, index, excludedCases(*cohort, *results))
	emit(*out, rep, unattributed, totalCost, *single)
	emitSpecificity(*results)
}

type caseWork struct {
	caseID string
	fixes  []fixInput
	pool   judge.Pool
}

// discover assembles, per case, the strong-tier fixes and the pooled findings
// from every arm. Strong tier only: a weak signal says a later commit touched
// the same file, which is not evidence about a defect.
func discover(cohort, retro, results string, only map[string]bool) ([]caseWork, error) {
	var work []caseWork
	dirs, _ := filepath.Glob(filepath.Join(cohort, "case-*-evaluator-only"))
	sort.Strings(dirs)

	for _, d := range dirs {
		caseID := strings.TrimSuffix(filepath.Base(d), "-evaluator-only")
		if len(only) > 0 && !only[caseID] {
			continue
		}
		ev, err := oracle.Load(filepath.Join(d, "correlated-report.json"), caseID)
		if err != nil {
			return nil, err
		}
		if ev.MaxStrength < oracle.StrengthStrong {
			continue
		}
		refs, err := strongRefs(filepath.Join(d, "correlated-report.json"))
		if err != nil {
			return nil, err
		}
		var fixes []fixInput
		for _, r := range refs {
			p := filepath.Join(retro, caseID, "patches", r+".patch")
			b, err := os.ReadFile(p)
			if err != nil {
				continue
			}
			fixes = append(fixes, fixInput{FixID: r[:12], Patch: string(b)})
		}
		if len(fixes) == 0 {
			continue
		}
		pool := judge.NewPool(caseID, findingsFor(results, caseID))
		if len(pool.Findings) == 0 {
			continue
		}
		if err := pool.CheckOpaque(); err != nil {
			return nil, err
		}
		work = append(work, caseWork{caseID: caseID, fixes: fixes, pool: pool})
	}
	return work, nil
}

func strongRefs(path string) ([]string, error) {
	var doc struct {
		Retrospective struct {
			StrongSignals []struct {
				SourceRef string `json:"source_ref"`
			} `json:"strong_signals"`
		} `json:"retrospective"`
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	for _, s := range doc.Retrospective.StrongSignals {
		if len(s.SourceRef) == 40 && !seen[s.SourceRef] {
			seen[s.SourceRef] = true
			out = append(out, s.SourceRef)
		}
	}
	sort.Strings(out)
	return out, nil
}

// findingsFor returns the stage-1 findings that also have a B/C pair, which
// is the population every other number in this project is computed over.
func findingsFor(results, caseID string) []judge.PooledFinding {
	var out []judge.PooledFinding
	dirs, _ := filepath.Glob(filepath.Join(results, "*"))
	sort.Strings(dirs)
	for _, dir := range dirs {
		base := filepath.Base(dir)
		arm := armOf(base)
		if arm == "" {
			continue
		}
		var run struct {
			CaseID string `json:"case_id"`
			Status string `json:"status"`
		}
		if !readJSON(filepath.Join(dir, "run.json"), &run) || run.CaseID != caseID || run.Status != "completed" {
			continue
		}
		var a struct {
			Findings []struct {
				ID          string `json:"id"`
				Title       string `json:"title"`
				Description string `json:"description"`
				Evidence    []struct {
					File string `json:"file"`
				} `json:"evidence"`
			} `json:"findings"`
		}
		if !readJSON(filepath.Join(dir, "stages", "reasoner-1", "review-a.json"), &a) {
			continue
		}
		var ev struct {
			Findings []struct {
				ID string  `json:"id"`
				B  *string `json:"b_disposition"`
			} `json:"findings"`
		}
		if !readJSON(filepath.Join(dir, "evaluation", "findings.json"), &ev) {
			continue
		}
		paired := map[string]bool{}
		for _, f := range ev.Findings {
			if f.B != nil {
				paired[f.ID] = true
			}
		}
		for _, f := range a.Findings {
			if !paired[f.ID] {
				continue
			}
			seen := map[string]bool{}
			var files []string
			for _, e := range f.Evidence {
				if e.File != "" && !seen[e.File] {
					seen[e.File] = true
					files = append(files, e.File)
				}
			}
			out = append(out, judge.PooledFinding{
				Arm: arm, RunID: base, FindingID: f.ID,
				Title: f.Title, Body: f.Description, Files: files,
			})
		}
	}
	return out
}

func armOf(base string) string {
	switch {
	case strings.HasPrefix(base, "opuswide-"):
		return "opus-wide"
	case strings.HasPrefix(base, "opus-"):
		return "opus-narrow"
	case strings.HasPrefix(base, "frr-"):
		return "sonnet"
	}
	return ""
}

func excludedCases(cohort, results string) []judge.ExcludedCase {
	var out []judge.ExcludedCase
	dirs, _ := filepath.Glob(filepath.Join(cohort, "case-*-evaluator-only"))
	sort.Strings(dirs)
	for _, d := range dirs {
		caseID := strings.TrimSuffix(filepath.Base(d), "-evaluator-only")
		ev, err := oracle.Load(filepath.Join(d, "correlated-report.json"), caseID)
		if err != nil || ev.MaxStrength >= oracle.StrengthStrong {
			continue
		}
		out = append(out, judge.ExcludedCase{
			CaseID:   caseID,
			Findings: len(findingsFor(results, caseID)),
			Reason:   "no strong-tier corrective signal; no fix to judge against",
		})
	}
	return out
}

func runTwoCall(ctx context.Context, a *claude.Adapter, out, model string, maxCost float64, c caseWork) (judge.CaseVerdicts, float64, error) {
	cost := 0.0

	mechRaw, spent, err := call(ctx, a, out, model, maxCost, c.caseID, "mechanisms",
		"internal/judge/prompt-mechanisms.md", "schemas/judge-mechanisms.schema.json",
		map[string]any{"case_id": c.caseID, "fixes": c.fixes})
	cost += spent
	if err != nil {
		return judge.CaseVerdicts{}, cost, fmt.Errorf("call 1: %w", err)
	}

	// Call 2 receives call 1's output VERBATIM. No re-derivation and no
	// editing between calls, or the isolation is decorative.
	var mech struct {
		FixMechanisms []map[string]any `json:"fix_mechanisms"`
	}
	if err := json.Unmarshal(mechRaw, &mech); err != nil {
		return judge.CaseVerdicts{}, cost, fmt.Errorf("call 1 output: %w", err)
	}

	verdRaw, spent, err := call(ctx, a, out, model, maxCost, c.caseID, "verdicts",
		"internal/judge/prompt-verdicts.md", "schemas/judge-verdict.schema.json",
		map[string]any{"case_id": c.caseID, "fix_mechanisms": mech.FixMechanisms, "findings": c.pool.View()})
	cost += spent
	if err != nil {
		return judge.CaseVerdicts{}, cost, fmt.Errorf("call 2: %w", err)
	}
	return parseVerdicts(c, verdRaw, cost)
}

func runSingle(ctx context.Context, a *claude.Adapter, out, model string, maxCost float64, c caseWork) (judge.CaseVerdicts, float64, error) {
	raw, spent, err := call(ctx, a, out, model, maxCost, c.caseID, "single",
		"internal/judge/prompt-single-call-control.md", "schemas/judge-verdict-single.schema.json",
		map[string]any{"case_id": c.caseID, "fixes": c.fixes, "findings": c.pool.View()})
	if err != nil {
		return judge.CaseVerdicts{}, spent, err
	}
	return parseVerdicts(c, raw, spent)
}

// missing accumulates findings the judge returned no verdict for. A dropped
// finding silently shrinks the precision denominator, which is the same
// "exclusion makes it free" hazard that TOO_VAGUE creates - and a dropped
// finding is more likely to be a hard one than an easy one, so the bias runs
// toward flattering the result. It is reported, never absorbed.
var missing = map[string][]string{}

func parseVerdicts(c caseWork, raw []byte, cost float64) (judge.CaseVerdicts, float64, error) {
	var doc struct {
		Verdicts []judge.Verdict `json:"verdicts"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return judge.CaseVerdicts{}, cost, err
	}
	got := map[string]bool{}
	for _, v := range doc.Verdicts {
		got[v.FindingID] = true
	}
	for _, f := range c.pool.Findings {
		if !got[f.OpaqueID] {
			missing[c.caseID] = append(missing[c.caseID], f.OpaqueID)
		}
	}
	var fixIDs []string
	for _, f := range c.fixes {
		fixIDs = append(fixIDs, f.FixID)
	}
	return judge.CaseVerdicts{CaseID: c.caseID, FixIDs: fixIDs, Verdicts: doc.Verdicts}, cost, nil
}

func call(ctx context.Context, a *claude.Adapter, out, model string, maxCost float64,
	caseID, label, promptPath, schemaPath string, input map[string]any) ([]byte, float64, error) {

	ws, err := os.MkdirTemp("", "judge-"+label+"-")
	if err != nil {
		return nil, 0, err
	}
	defer os.RemoveAll(ws)
	for _, sub := range []string{"input", "output"} {
		if err := os.MkdirAll(filepath.Join(ws, sub), 0o755); err != nil {
			return nil, 0, err
		}
	}
	blob, err := json.MarshalIndent(input, "", "  ")
	if err != nil {
		return nil, 0, err
	}
	if err := os.WriteFile(filepath.Join(ws, "input", "input.json"), blob, 0o644); err != nil {
		return nil, 0, err
	}
	schema, err := os.ReadFile(schemaPath)
	if err != nil {
		return nil, 0, err
	}
	promptBody, err := os.ReadFile(promptPath)
	if err != nil {
		return nil, 0, err
	}
	full := string(promptBody) + "\n\n---\n\nYour input is the JSON document at `/workspace/input/input.json`. Read it first.\n"
	promptFile := filepath.Join(ws, "prompt.md")
	if err := os.WriteFile(promptFile, []byte(full), 0o644); err != nil {
		return nil, 0, err
	}

	req := adapters.RunRequest{
		RunID: "judge-" + caseID, CaseID: caseID,
		Stage:         adapters.Stage(label),
		WorkspacePath: ws, PromptPath: promptFile,
		OutputSchema: filepath.Base(schemaPath), OutputSchemaJSON: schema,
		Model: model, ReasoningLevel: "high",
		Tools:  []string{"Read"},
		Budget: adapters.Budget{MaxCostUSD: maxCost, MaxWallClockSeconds: 900, MaxOutputTokens: 64000},
	}
	cctx, cancel := context.WithTimeout(ctx, 900*time.Second)
	defer cancel()

	res, err := a.Run(cctx, req)
	spent := res.Usage.CostUSD
	if err != nil {
		return nil, spent, err
	}
	data, err := os.ReadFile(res.OutputPath)
	if err != nil {
		return nil, spent, err
	}
	dst := filepath.Join(out, caseID+"-"+label+".json")
	_ = os.WriteFile(dst, data, 0o600)
	return data, spent, nil
}

func emit(out string, rep judge.Report, unattributed int, cost float64, single bool) {
	mode := "two-call"
	if single {
		mode = "single-call A/B control"
	}
	fmt.Printf("\n=== mechanism anticipation (%s) ===\n", mode)
	fmt.Println("JUDGED BY AN IN-FAMILY MODEL; CROSS-VENDOR CHECK PENDING.")
	fmt.Println("ANTICIPATION RATE, NOT PRECISION. A NONE verdict conflates a false positive with a")
	fmt.Println("real defect nobody fixed and one the oracle missed, so true precision >= this figure.")
	fmt.Println()

	loc, ok := rep.Overall.Anticipation.LocalityRatio()
	fmt.Printf("HEALTH CHECK  MECHANISM=%d  LOCALITY_ONLY=%d  ",
		rep.Overall.Anticipation.Mechanism, rep.Overall.Anticipation.Locality)
	if ok {
		fmt.Printf("locality ratio %.1f%%\n", loc)
	} else {
		fmt.Printf("locality ratio undefined (no location-related verdicts)\n")
	}
	fmt.Println()
	fmt.Printf("overall  %s\n", rep.Overall)
	for _, arm := range sortedKeys(rep.ByArm) {
		fmt.Printf("  %-12s %s\n", arm, rep.ByArm[arm])
	}
	fmt.Println()
	fmt.Printf("scoreable cases: %d   spend: $%.2f   unattributed verdicts: %d\n", rep.Cases, cost, unattributed)
	totalMissing := 0
	for _, ids := range missing {
		totalMissing += len(ids)
	}
	if totalMissing > 0 {
		fmt.Printf("  WARNING %d finding(s) received NO verdict - the denominator is short by that much:\n", totalMissing)
		for _, cid := range sortedStrings(missing) {
			fmt.Printf("    %s: %d missing\n", cid, len(missing[cid]))
		}
	}
	for _, e := range rep.Excluded {
		fmt.Printf("  EXCLUDED %s (%d findings): %s\n", e.CaseID, e.Findings, e.Reason)
	}
	b, _ := json.MarshalIndent(rep, "", "  ")
	_ = os.WriteFile(filepath.Join(out, "report-"+strings.ReplaceAll(mode, " ", "-")+".json"), b, 0o600)
}

// classifyAll walks every sealed run, not just the scoreable ones. Specificity
// needs the designed negatives, which the anticipation metric correctly
// excludes - so the two denominators differ and both are printed.
func classifyAll(results string) []judge.ClassifiedFinding {
	var out []judge.ClassifiedFinding
	dirs, _ := filepath.Glob(filepath.Join(results, "*"))
	sort.Strings(dirs)
	for _, dir := range dirs {
		base := filepath.Base(dir)
		arm := armOf(base)
		if arm == "" {
			continue
		}
		var run struct {
			CaseID string `json:"case_id"`
			Status string `json:"status"`
		}
		if !readJSON(filepath.Join(dir, "run.json"), &run) || run.Status != "completed" {
			continue
		}
		var ev struct {
			Findings []struct {
				B *string `json:"b_disposition"`
				C *string `json:"c_disposition"`
			} `json:"findings"`
		}
		if !readJSON(filepath.Join(dir, "evaluation", "findings.json"), &ev) {
			continue
		}
		for _, f := range ev.Findings {
			c := ""
			if f.C != nil {
				c = *f.C
			}
			out = append(out, judge.ClassifiedFinding{
				Arm: arm, CaseID: run.CaseID, Negative: judge.NegativeCases[run.CaseID],
				Disposition: c, NewByStage3: f.B == nil,
			})
		}
	}
	return out
}

func emitSpecificity(results string) {
	all := classifyAll(results)
	universe := map[string]bool{}
	for _, f := range all {
		universe[f.CaseID] = f.Negative
	}
	fmt.Printf("\n=== specificity: survival on DESIGNED NEGATIVES vs positives ===\n")
	fmt.Println("The cohort is 7 positives + 3 designed negatives (backlog 1.2). Negatives are")
	fmt.Println("EXCLUDED from anticipation/recall, which need a fix to compare against, and")
	fmt.Println("INCLUDED here, which does not. The judge never saw them, so the negative class")
	fmt.Println("has NO mechanism-agreement data - these are stage dispositions only.")
	for _, inc := range []bool{false, true} {
		label := "excluding stage-3 discoveries"
		if inc {
			label = "including stage-3 discoveries"
		}
		s := judge.Specificity(all, inc, universe)
		fmt.Printf("\n  [%s]\n  pooled  %s\n  one-sided PR-level permutation p = %.3f\n",
			label, s, s.PermutationOneSided())
		n, p := s.PerCaseSurvival()
		fmt.Printf("  per PR  clean %.2f surviving findings/PR (%d PRs)   buggy %.2f (%d PRs)\n",
			n, s.NegCases, p, s.PosCases)
		byArm := judge.SpecificityByArm(all, inc, universe)
		for _, arm := range judge.ArmNames(byArm) {
			a := byArm[arm]
			an, ap := a.PerCaseSurvival()
			ratioStr := "n/a"
			if an > 0 {
				ratioStr = fmt.Sprintf("%.1fx", ap/an)
			}
			perm, _ := a.PValues()
			flag := ""
			if a.NegSurvived < 5 || a.PosSurvived < 5 {
				flag = "  NOT SIGNIFICANT (cell count < 5; do not quote the ratio)"
			}
			fmt.Printf("    %-12s clean %.2f/PR (%d findings)  buggy %.2f/PR (%d)  ratio %s  PR-level p=%.3f%s\n",
				arm, an, a.NegSurvived, ap, a.PosSurvived, ratioStr, perm, flag)
		}
	}
}

func sortedStrings(m map[string][]string) []string {
	var k []string
	for s := range m {
		k = append(k, s)
	}
	sort.Strings(k)
	return k
}

func sortedKeys(m map[string]judge.ArmReport) []string {
	var k []string
	for s := range m {
		k = append(k, s)
	}
	sort.Strings(k)
	return k
}

func splitCSV(s string) map[string]bool {
	m := map[string]bool{}
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			m[p] = true
		}
	}
	return m
}

func readJSON(path string, v any) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return json.Unmarshal(b, v) == nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "judge:", err)
	os.Exit(1)
}
