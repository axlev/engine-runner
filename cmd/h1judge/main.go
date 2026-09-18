// h1judge runs the section 6 reason match over sealed H1 runs.
//
//	h1judge -runs build/results -labels h1-labels.json \
//	        -fixes miner/output/frr-h1-cohort-evaluator-inputs/fixes \
//	        -out <evaluator-only dir> [-dry-run]
//
// Two calls per case, as the pilot: first the fix mechanisms are described
// by a reader who is shown only the patch and never a finding, then the
// verdicts are judged against those frozen descriptions. Findings are
// pooled under opaque ids so the judge cannot tell which arm wrote which.
//
// Paid unless -dry-run. Evaluator-side: it reads the fixing diff.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	"github.com/axlev/engine-runner/internal/h1judge"
	"github.com/axlev/engine-runner/internal/h1score"
	"github.com/axlev/engine-runner/internal/judge"
)

func main() {
	runs := flag.String("runs", "build/results", "sealed run directories, all arms")
	labelsPath := flag.String("labels", "", "h1-labels/v1 file (required)")
	manifest := flag.String("cohort-manifest", "", "cohort manifest the labels were written for (required)")
	fixes := flag.String("fixes", "", "directory of <case_id>.patch fixing diffs (required)")
	out := flag.String("out", "", "evaluator-only output directory (required)")
	image := flag.String("adapter-image", "engine-runner/adapter-claude:2.1.263", "container image for the vendor CLI")
	model := flag.String("model", "claude-opus-5", "judge model")
	armT := flag.String("arm-t", "h1-t-v1", "protocol_version of arm T")
	armG := flag.String("arm-g", "h1-g-v1", "protocol_version of arm G")
	maxCost := flag.Float64("max-cost", 3.0, "per-call cost ceiling (estimate under a subscription token)")
	dryRun := flag.Bool("dry-run", false, "build and seal the pools and prompts without calling the model")
	only := flag.String("only", "", "comma-separated case ids to judge (default: every eligible case)")
	resume := flag.Bool("resume", true, "skip a case whose sealed report already carries fix mechanisms. A report with none was never really judged - that is the shape the parse defect produced - so those are re-judged rather than trusted")
	flag.Parse()
	for name, v := range map[string]string{"-labels": *labelsPath, "-cohort-manifest": *manifest, "-fixes": *fixes, "-out": *out} {
		if v == "" {
			fmt.Fprintf(os.Stderr, "h1judge: %s is required\n", name)
			os.Exit(2)
		}
	}
	if !*dryRun && os.Getenv("CLAUDE_CODE_OAUTH_TOKEN") == "" && os.Getenv("ANTHROPIC_API_KEY") == "" {
		fmt.Fprintln(os.Stderr, "h1judge: no credential in the environment; refusing to start a paid judge pass")
		os.Exit(1)
	}
	check(h1judge.RefuseKeyedFixes(*fixes))
	check(os.MkdirAll(*out, 0o755))

	labels, _, err := h1score.LoadLabels(*labelsPath, *manifest)
	check(err)
	positives := map[string]bool{}
	var caseIDs []string
	for _, c := range labels.Cases {
		caseIDs = append(caseIDs, c.CaseID)
		if c.Label == "positive" {
			positives[c.CaseID] = true
		}
	}
	sort.Strings(caseIDs)

	armIDs := map[string]string{h1score.ArmT: *armT, h1score.ArmG: *armG}
	verdicts, _, err := h1score.LoadVerdicts(*runs, armIDs, nil)
	check(err)

	candidates := h1judge.EligibleCandidates(caseIDs, []string{h1score.ArmT, h1score.ArmG},
		func(arm, caseID string) (h1judge.Candidate, bool, bool) {
			v, ok := verdicts[arm][caseID]
			if !ok {
				return h1judge.Candidate{}, false, false
			}
			return h1judge.Candidate{
				CaseID: caseID, Arm: arm, RunID: v.RunID,
				FindingID: v.PrimaryFindingID, Mechanism: v.PrimaryMechanism,
				File: v.PrimaryFile, Line: v.PrimaryLine,
			}, v.Risky, true
		},
		func(caseID string) bool { return positives[caseID] })

	byCase := map[string][]h1judge.Candidate{}
	for _, c := range candidates {
		byCase[c.CaseID] = append(byCase[c.CaseID], c)
	}
	fmt.Printf("h1judge: %d RISKY true positive(s) to judge across %d case(s)\n", len(candidates), len(byCase))

	var adapter *claude.Adapter
	if !*dryRun {
		a, err := claude.New(*image, nil)
		check(err)
		adapter = a
	}

	var reports []h1judge.Report
	var totalCost float64
	wanted := map[string]bool{}
	for _, id := range splitCSV(*only) {
		wanted[id] = true
	}
	var skipped, failed int
	for _, caseID := range sortedKeys(byCase) {
		if len(wanted) > 0 && !wanted[caseID] {
			continue
		}
		// Resume deliberately keys on "has mechanisms", not "file exists".
		// The parse defect sealed reports that look complete and carry an
		// empty fix list; keying on existence would skip exactly the cases
		// that need re-judging.
		if *resume && !*dryRun && sealedWithMechanisms(*out, caseID) {
			skipped++
			continue
		}
		pool := h1judge.PoolFor(caseID, byCase[caseID])
		if err := pool.CheckOpaque(); err != nil {
			check(fmt.Errorf("case %s: %w", caseID, err))
		}
		patch, err := os.ReadFile(filepath.Join(*fixes, caseID+".patch"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "h1judge: %s: no fixing diff (%v); NOT judged, not scored as a miss\n", caseID, err)
			continue
		}
		sum := sha256.Sum256(patch)

		r := h1judge.Report{
			SchemaVersion: h1judge.SchemaVersion,
			CaseID:        caseID,
			JudgeModel:    *model,
			InFamily:      strings.Contains(*model, "opus"),
			PromptHashes:  promptHashes(),
			FixSHA256:     hex.EncodeToString(sum[:]),
			FixSHAs:       h1judge.FixSHAsIn(string(patch)),
			Verdicts:      map[string]h1judge.ArmVerdict{},
		}
		if *dryRun {
			// Seal what WOULD be sent, so the pool and prompts can be
			// reviewed before any money is spent.
			writePrompt(*out, caseID, "mechanisms", string(patch))
			view, _ := json.MarshalIndent(pool.View(), "", "  ")
			writePrompt(*out, caseID, "verdicts", string(view))
			fmt.Printf("  %s: %d finding(s) pooled, prompts sealed (dry run)\n", caseID, len(pool.View()))
		} else {
			cost, err := runJudge(context.Background(), adapter, &r, pool, string(patch), *model, *maxCost)
			totalCost += cost
			if err != nil {
				failed++
				fmt.Fprintf(os.Stderr, "h1judge: %s: %v\n", caseID, err)
			}
		}
		reports = append(reports, r)
		check(writeJSON(filepath.Join(*out, caseID+".h1-judge.json"), r))
	}

	if skipped > 0 {
		fmt.Printf("h1judge: %d case(s) skipped, already judged with mechanisms\n", skipped)
	}
	summary := h1judge.Summarise(reports, *model, strings.Contains(*model, "opus"))
	check(writeJSON(filepath.Join(*out, "h1-judge-summary.json"), summary))
	fmt.Println()
	for _, arm := range sortedKeys(summary.Arms) {
		a := summary.Arms[arm]
		rate := "absent"
		if a.ReasonMatch != nil {
			rate = fmt.Sprintf("%.3f (%d/%d)", *a.ReasonMatch, a.Mechanism, a.Judged)
		}
		fmt.Printf("arm %s: reason match %s  MECHANISM %d  LOCALITY_ONLY %d  NONE %d  TOO_VAGUE %d\n",
			arm, rate, a.Mechanism, a.LocalityOnly, a.None, a.TooVague)
	}
	if !*dryRun {
		fmt.Printf("cost estimate $%.2f (CLI estimate under a subscription token, not a bill)\n", totalCost)
	}
	fmt.Printf("judge model %s, in-family: %v - state this on every figure derived from it\n", *model, summary.InFamily)
	// A judge pass with failed cases must not look like a clean one. The
	// defect this guard was added for produced verdicts that read as
	// evidence while nothing had errored; a non-zero exit is the cheapest
	// way for an operator or supervisor to notice.
	if failed > 0 {
		fmt.Fprintf(os.Stderr, "h1judge: %d case(s) FAILED and were not judged; re-run to pick them up (resume skips only cases with mechanisms)\n", failed)
		os.Exit(1)
	}
}

// parseMechanisms reads call 1's response.
//
// The response and the sealed report are different documents.
// judge-mechanisms.schema.json defines {"fix_mechanisms":[{"fix_id",
// "mechanism"}]}; h1judge.Mechanism is the REPORT's shape, {"fix_id",
// "description"}. Unmarshalling the response straight into the report type
// matched neither key, so the list was always empty, call 2 was handed no
// fixes, and the judge returned NONE for everything with both calls
// reporting success - 11 cases and ~23 paid calls sealed as evidence.
//
// Do not align the tags to "simplify" this: that changes the sealed
// h1-judge/v1 report, and the schema is hashed into the sealed prompts.
func parseMechanisms(raw []byte) ([]h1judge.Mechanism, error) {
	var doc struct {
		FixMechanisms []struct {
			FixID     string `json:"fix_id"`
			Mechanism string `json:"mechanism"`
		} `json:"fix_mechanisms"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parsing mechanisms: %w", err)
	}
	out := make([]h1judge.Mechanism, 0, len(doc.FixMechanisms))
	for _, m := range doc.FixMechanisms {
		out = append(out, h1judge.Mechanism{FixID: m.FixID, Description: m.Mechanism})
	}
	return out, nil
}

// runJudge makes the two calls. Kept small and separate so the shape is
// readable: describe the fixes blind, then judge against those frozen
// descriptions.
func runJudge(ctx context.Context, a *claude.Adapter, r *h1judge.Report, pool judge.Pool, patch, model string, maxCost float64) (float64, error) {
	var spent float64

	mechRaw, cost, err := ask(ctx, a, r.CaseID, "mechanisms", mechanismsPrompt(patch), model, maxCost)
	spent += cost
	if err != nil {
		return spent, fmt.Errorf("mechanisms call: %w", err)
	}
	// The call-1 RESPONSE and the sealed report are different documents and
	// must be parsed as such. judge-mechanisms.schema.json defines
	// {"fix_mechanisms":[{"fix_id","mechanism"}]}; h1judge.Mechanism is the
	// report's shape, {"fix_id","description"}. Unmarshalling the response
	// straight into the report type matched NEITHER key, so Mechanisms was
	// always empty, call 2 was handed an empty fix list, and the judge
	// correctly returned NONE for every finding - with both calls
	// reporting success. 11 cases and ~23 paid calls were sealed that way.
	// Do not "simplify" this by aligning the tags: that would silently
	// change the sealed h1-judge/v1 report, and the schema is hashed into
	// the sealed prompts.
	mechs, err := parseMechanisms(mechRaw)
	if err != nil {
		return spent, err
	}
	r.Mechanisms = mechs

	// (2) Fail closed. A fix diff with no mechanisms is not a judgeable
	// case: call 2 would be asked "does this finding match these fixes?"
	// with no fixes, and NONE is the only answer it could give. That is
	// indistinguishable from a reviewer who genuinely missed, so it must
	// stop here rather than produce a verdict that reads like evidence.
	if len(r.Mechanisms) == 0 && len(strings.TrimSpace(patch)) > 0 {
		return spent, fmt.Errorf("call 1 returned no fix mechanisms for a %d-byte fix diff; refusing to judge against an empty fix list", len(patch))
	}

	verdictRaw, cost, err := ask(ctx, a, r.CaseID, "verdicts", verdictsPrompt(r.Mechanisms, pool), model, maxCost)
	spent += cost
	if err != nil {
		return spent, fmt.Errorf("verdicts call: %w", err)
	}
	var vDoc struct {
		Verdicts []struct {
			FindingID string `json:"finding_id"`
			Agreement string `json:"agreement"`
			FixID     string `json:"fix_id"`
			Rationale string `json:"rationale"`
		} `json:"verdicts"`
	}
	if err := json.Unmarshal(verdictRaw, &vDoc); err != nil {
		return spent, fmt.Errorf("parsing verdicts: %w", err)
	}

	index := pool.Index()
	seen := map[string]bool{}
	for _, v := range vDoc.Verdicts {
		f, ok := index[v.FindingID]
		if !ok {
			return spent, fmt.Errorf("verdict for unknown opaque id %q", v.FindingID)
		}
		seen[v.FindingID] = true
		r.Verdicts[f.Arm] = h1judge.ArmVerdict{
			Arm: f.Arm, RunID: f.RunID, FindingID: f.FindingID, OpaqueID: f.OpaqueID,
			Agreement: v.Agreement, FixID: v.FixID, Rationale: v.Rationale,
		}
	}
	// A dropped verdict is a hole in the denominator, not a NONE. The pilot
	// judge dropped one and it took a dedicated check to notice.
	for id := range index {
		if !seen[id] {
			return spent, fmt.Errorf("judge returned no verdict for pooled finding %s; refusing to score it as NONE", id)
		}
	}
	return spent, nil
}

func ask(ctx context.Context, a *claude.Adapter, caseID, label, promptText, model string, maxCost float64) ([]byte, float64, error) {
	ws, err := os.MkdirTemp("", "h1judge-"+caseID+"-"+label+"-")
	if err != nil {
		return nil, 0, err
	}
	defer os.RemoveAll(ws)
	promptFile := filepath.Join(ws, "prompt.md")
	if err := os.WriteFile(promptFile, []byte(promptText), 0o644); err != nil {
		return nil, 0, err
	}
	schemaName := "judge-mechanisms.schema.json"
	if label == "verdicts" {
		schemaName = "judge-verdict.schema.json"
	}
	schema, err := os.ReadFile(filepath.Join("schemas", schemaName))
	if err != nil {
		return nil, 0, err
	}
	req := adapters.RunRequest{
		RunID: "h1judge-" + caseID, CaseID: caseID,
		Stage:         adapters.Stage("judge-" + label),
		WorkspacePath: ws, PromptPath: promptFile,
		OutputSchema: schemaName, OutputSchemaJSON: schema,
		Model: model, ReasoningLevel: "high",
		// The judge reasons over text it is given; it needs no tools, and
		// withholding them keeps it from wandering into a repository.
		NoTools: true,
		Budget:  adapters.Budget{MaxCostUSD: maxCost, MaxWallClockSeconds: 900, MaxOutputTokens: 32000},
	}
	cctx, cancel := context.WithTimeout(ctx, 900*time.Second)
	defer cancel()
	res, err := a.Run(cctx, req)
	cost := res.Usage.CostUSD
	if err != nil {
		return nil, cost, err
	}
	raw, err := os.ReadFile(res.OutputPath)
	return raw, cost, err
}

func mechanismsPrompt(patch string) string {
	return readPrompt("prompt-mechanisms.md") +
		"\n\n---\n\nThe fix patch:\n\n```diff\n" + patch + "\n```\n"
}

func verdictsPrompt(mechanisms []h1judge.Mechanism, pool judge.Pool) string {
	var b strings.Builder
	b.WriteString(readPrompt("prompt-verdicts.md"))
	b.WriteString("\n\n---\n\n## Fix mechanisms\n\n")
	for _, m := range mechanisms {
		fmt.Fprintf(&b, "- **%s**: %s\n", m.FixID, m.Description)
	}
	b.WriteString("\n## Reviewer findings\n\n")
	for _, v := range pool.View() {
		fmt.Fprintf(&b, "- **%s** (%s): %s\n", v.ID, strings.Join(v.Files, ", "), v.Body)
	}
	return b.String()
}

func readPrompt(name string) string {
	raw, err := os.ReadFile(filepath.Join("internal", "judge", name))
	check(err)
	return string(raw)
}

func promptHashes() map[string]string {
	out := map[string]string{}
	for _, n := range []string{"prompt-mechanisms.md", "prompt-verdicts.md"} {
		sum := sha256.Sum256([]byte(readPrompt(n)))
		out[n] = hex.EncodeToString(sum[:])
	}
	return out
}

func writePrompt(out, caseID, label, body string) {
	_ = os.WriteFile(filepath.Join(out, caseID+"-"+label+".prompt.txt"), []byte(body), 0o600)
}

func sortedKeys[V any](m map[string]V) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o600)
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "h1judge:", err)
		os.Exit(1)
	}
}

// sealedWithMechanisms reports whether this case already has a report that
// was actually judged. An unreadable or mechanism-less report counts as not
// judged, so it is re-run rather than silently kept.
func sealedWithMechanisms(outDir, caseID string) bool {
	raw, err := os.ReadFile(filepath.Join(outDir, caseID+".h1-judge.json"))
	if err != nil {
		return false
	}
	var r h1judge.Report
	if err := json.Unmarshal(raw, &r); err != nil {
		return false
	}
	return r.SchemaVersion == h1judge.SchemaVersion && len(r.Mechanisms) > 0
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
