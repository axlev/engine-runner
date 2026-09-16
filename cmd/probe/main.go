// probe runs the contamination probes of pre-registration amendment A11
// over a cohort, before any arm runs. Every positive is asked directly,
// from memory, whether it already knows how this change was fixed; a hit
// voids the case for all arms.
//
//	probe -cohort <dir of case-<id>/reviewer/diff.patch> \
//	      -inputs <dir of <case_id>.json contamination-probe-input/v1> \
//	      -keys <dir of contamination-keys/v1> \
//	      -adapter-image <image> -out <evaluator-only dir>
//
// Paid: two model calls per case. Nothing is mounted and no tools are
// granted, so the model answers from memory or not at all.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/axlev/engine-runner/internal/adapters"
	"github.com/axlev/engine-runner/internal/adapters/claude"
	"github.com/axlev/engine-runner/internal/contamscan"
	"github.com/axlev/engine-runner/internal/probe"
)

func main() {
	cohort := flag.String("cohort", "", "cohort directory holding <case_id>/reviewer/diff.patch (required)")
	inputs := flag.String("inputs", "", "directory of <case_id>.json contamination-probe-input/v1 files (required)")
	keysDir := flag.String("keys", "", "directory of <case_id>.json contamination-keys/v1 files (required)")
	out := flag.String("out", "", "evaluator-only output directory (required)")
	image := flag.String("adapter-image", "engine-runner/adapter-claude:2.1.263", "container image for the vendor CLI")
	model := flag.String("model", "claude-opus-5", "probe model")
	maxCost := flag.Float64("max-cost", 2.0, "per-call cost ceiling (estimate under a subscription token)")
	maxSeconds := flag.Int("max-seconds", 300, "per-call wall clock ceiling")
	only := flag.String("only", "", "comma-separated case ids (default: every input file)")
	dryRun := flag.Bool("dry-run", false, "build and seal the prompts without calling the model")
	flag.Parse()
	for name, v := range map[string]string{"-cohort": *cohort, "-inputs": *inputs, "-keys": *keysDir, "-out": *out} {
		if v == "" {
			fmt.Fprintf(os.Stderr, "probe: %s is required\n", name)
			os.Exit(2)
		}
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		fail(err)
	}

	wanted := map[string]bool{}
	for _, id := range splitCSV(*only) {
		wanted[id] = true
	}
	files, _ := filepath.Glob(filepath.Join(*inputs, "*.json"))
	sort.Strings(files)
	if len(files) == 0 {
		fail(fmt.Errorf("no probe inputs under %s", *inputs))
	}

	var adapter *claude.Adapter
	if !*dryRun {
		a, err := claude.New(*image, nil)
		if err != nil {
			fail(err)
		}
		adapter = a
	}

	var totalCost float64
	var voided, unresolved, done int
	for _, f := range files {
		in, err := probe.LoadInput(f)
		if err != nil {
			fail(err)
		}
		if len(wanted) > 0 && !wanted[in.CaseID] {
			continue
		}
		diffPath := filepath.Join(*cohort, in.CaseID, "reviewer", "diff.patch")
		diff, err := os.ReadFile(diffPath)
		if err != nil {
			fail(fmt.Errorf("probe: case %s: %w", in.CaseID, err))
		}
		keys, keysSHA, err := contamscan.LoadKeys(filepath.Join(*keysDir, in.CaseID+".json"))
		if err != nil {
			fail(fmt.Errorf("probe: case %s: %w", in.CaseID, err))
		}

		report := probe.Report{
			SchemaVersion: probe.ReportSchemaVersion,
			CaseID:        in.CaseID,
			Model:         *model,
			PromptHashes:  probe.PromptHashes(),
			NoTools:       true,
			Responses:     []probe.Response{},
		}

		for _, which := range []string{probe.ProbeA, probe.ProbeB} {
			text, err := probe.PromptFor(which, in, string(diff))
			if err != nil {
				fail(err)
			}
			resp := probe.Response{Probe: which, Model: *model}
			if *dryRun {
				resp.Answer = ""
				resp.Error = "dry run: prompt built, model not called"
			} else {
				answer, cost, err := ask(context.Background(), adapter, in.CaseID, which, text, *model, *maxCost, *maxSeconds)
				resp.Answer, resp.CostUSD = answer, cost
				totalCost += cost
				if err != nil {
					resp.Error = err.Error()
					fmt.Fprintf(os.Stderr, "probe: %s %s: %v\n", in.CaseID, which, err)
				}
			}
			report.Responses = append(report.Responses, resp)
			// The prompt actually sent is sealed beside the report: the
			// hash pins the fixed half, this pins the case-specific half.
			_ = os.WriteFile(filepath.Join(*out, in.CaseID+"-probe-"+which+".prompt.txt"), []byte(text), 0o600)
		}

		report.Scan(keys, keysSHA)
		d := probe.Decide(report, probe.Verdict{}, false)
		if report.Void {
			voided++
		} else if d.Unresolved {
			unresolved++
		}
		if err := writeJSON(filepath.Join(*out, in.CaseID+".contamination-probe.json"), report); err != nil {
			fail(err)
		}
		done++
		fmt.Printf("%s: void=%v hits=%d cost_estimate=$%.2f\n", in.CaseID, report.Void, len(report.MechanicalHits), report.Responses[0].CostUSD+report.Responses[len(report.Responses)-1].CostUSD)
	}

	fmt.Printf("\nprobed %d case(s): %d voided mechanically, %d awaiting an evaluator verdict\n", done, voided, unresolved)
	fmt.Printf("cost estimate total: $%.2f (CLI estimate under a subscription token, not a bill)\n", totalCost)
	if unresolved > 0 {
		fmt.Printf("NOT CLEAN YET: %d case(s) need %s files before scoring - a clean mechanical scan is not an acquittal\n",
			unresolved, probe.VerdictSchemaVersion)
	}
}

// ask makes one probe call: nothing mounted, no tools, prompt only.
func ask(ctx context.Context, a *claude.Adapter, caseID, which, promptText, model string, maxCost float64, maxSeconds int) (string, float64, error) {
	ws, err := os.MkdirTemp("", "probe-"+caseID+"-"+which+"-")
	if err != nil {
		return "", 0, err
	}
	defer os.RemoveAll(ws)
	promptFile := filepath.Join(ws, "prompt.md")
	if err := os.WriteFile(promptFile, []byte(promptText), 0o644); err != nil {
		return "", 0, err
	}

	req := adapters.RunRequest{
		RunID:            "probe-" + caseID,
		CaseID:           caseID,
		Stage:            adapters.Stage("probe-" + which),
		WorkspacePath:    ws,
		PromptPath:       promptFile,
		OutputSchema:     "probe-response.schema.json",
		OutputSchemaJSON: probe.ResponseSchemaJSON(),
		Model:            model,
		ReasoningLevel:   "high",
		// A11: the model answers from memory or not at all.
		NoTools: true,
		Budget: adapters.Budget{
			MaxCostUSD:          maxCost,
			MaxWallClockSeconds: maxSeconds,
			MaxOutputTokens:     16000,
		},
	}
	cctx, cancel := context.WithTimeout(ctx, time.Duration(maxSeconds)*time.Second)
	defer cancel()

	res, err := a.Run(cctx, req)
	cost := res.Usage.CostUSD
	if err != nil {
		return "", cost, err
	}
	raw, err := os.ReadFile(res.OutputPath)
	if err != nil {
		return "", cost, err
	}
	var doc struct {
		Answer string `json:"answer"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		// Keep whatever came back rather than discarding a paid answer.
		return string(raw), cost, nil
	}
	return doc.Answer, cost, nil
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range filepath.SplitList("") {
		_ = p
	}
	cur := ""
	for _, r := range s {
		if r == ',' {
			if cur != "" {
				out = append(out, cur)
			}
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o600)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "probe:", err)
	os.Exit(1)
}
