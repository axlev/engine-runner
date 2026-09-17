// h1run runs the H1 arms across a published cohort: all T cases, then all
// G, two at a time, resumable, refusing any case the A11 probe voided or
// left unresolved.
//
//	h1run -cohort miner/output/frr-h1-cohort -agents configs/agents/h1-opus-wide.yaml \
//	      -adapter-image engine-runner/adapter-claude:2.1.263 \
//	      -probes <probe output dir> -probe-verdicts <verdict dir> \
//	      (or -probe-gate <gate file> -cohort-records <sha>) \
//	      -results-root build/results -out <run record>
//
// Arms run in sequence deliberately: if the subscription window runs dry
// part-way, one complete arm is worth far more than two half arms, because
// a half arm cannot be compared against anything.
//
// Re-running is always safe. A case is skipped when a sealed run already
// exists for it under the same protocol_version AND protocol_hash, so
// nothing is paid for twice and a stall costs only the waiting.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/axlev/engine-runner/internal/batch"
	"github.com/axlev/engine-runner/internal/probe"
)

func main() {
	cohort := flag.String("cohort", "", "published cohort directory, one case-<id>/ per case (required)")
	agents := flag.String("agents", "configs/agents/h1-opus-wide.yaml", "arm file: the vendor binding for both arms")
	image := flag.String("adapter-image", "engine-runner/adapter-claude:2.1.263", "container image for the vendor CLI")
	resultsRoot := flag.String("results-root", "build/results", "root under which sealed runs are written")
	probesDir := flag.String("probes", "", "cmd/probe output directory; cases it voided or left unresolved are refused")
	verdictsDir := flag.String("probe-verdicts", "", "evaluator verdict directory (contamination-probe-verdict/v1)")
	probeGate := flag.String("probe-gate", "", "evaluator gate file (contamination-probe-gate/v1): one clean|void|unresolved per case. Alternative to -probes/-probe-verdicts that never opens a probe report")
	cohortRecords := flag.String("cohort-records", "", "the cohort manifest's records_sha256; required with -probe-gate, which is refused if it was written for a different cohort")
	armsFlag := flag.String("arms", "T,G", "arms to run, in order")
	concurrency := flag.Int("concurrency", 2, "cases in flight at once")
	only := flag.String("only", "", "comma-separated case ids (default: every case in the cohort)")
	stallWait := flag.Duration("stall-wait", 15*time.Minute, "how long to wait out a rate limit when the vendor names no reset time")
	maxStalls := flag.Int("max-stalls", 3, "how many times one case waits out a rate limit before being left for the next run")
	out := flag.String("out", "", "where to write the batch record (default: <results-root>/h1-batch-<timestamp>.json)")
	dryRun := flag.Bool("dry-run", false, "use the fixture adapter and a canned clean answer: exercises gating, ordering, resume and sealing at no cost")
	flag.Parse()
	if *cohort == "" {
		fmt.Fprintln(os.Stderr, "h1run: -cohort is required")
		os.Exit(2)
	}

	// Paid runs need a credential in the environment. Checked up front
	// rather than per case: discovering it on case 1 of 40 wastes nothing,
	// but discovering it after a stall wait would.
	if !*dryRun && os.Getenv("CLAUDE_CODE_OAUTH_TOKEN") == "" && os.Getenv("ANTHROPIC_API_KEY") == "" {
		fmt.Fprintln(os.Stderr, "h1run: no CLAUDE_CODE_OAUTH_TOKEN or ANTHROPIC_API_KEY in the environment; refusing to start a paid batch")
		os.Exit(1)
	}

	cases, err := discoverCases(*cohort, splitCSV(*only))
	check(err)
	if len(cases) == 0 {
		check(fmt.Errorf("no case-*/ directories under %s", *cohort))
	}

	if *probeGate != "" && (*probesDir != "" || *verdictsDir != "") {
		check(fmt.Errorf("-probe-gate replaces -probes/-probe-verdicts; pass one route or the other, not both"))
	}
	var gate batch.ProbeGate
	if *probeGate != "" {
		var g probe.Gate
		gate, g, err = gateFromFile(*probeGate, *cohortRecords, cases)
		check(err)
		fmt.Printf("h1run: probe gate %s accepted for cohort records_sha256 %s\n", *probeGate, g.RecordsSHA256)
	} else {
		gate, err = buildGate(*probesDir, *verdictsDir, cases)
		check(err)
	}

	arms, err := buildArms(splitCSV(*armsFlag), *dryRun)
	check(err)

	fmt.Printf("h1run: %d case(s), arms %s, concurrency %d, results under %s\n",
		len(cases), strings.Join(splitCSV(*armsFlag), " then "), *concurrency, *resultsRoot)
	if n := len(gate.Voided) + len(gate.Unresolved); n > 0 {
		fmt.Printf("h1run: %d case(s) refused by the probe gate (%d voided, %d unresolved)\n",
			n, len(gate.Voided), len(gate.Unresolved))
	}

	opts := batch.Options{
		Arms:            arms,
		Cases:           cases,
		BundlePath:      func(id string) string { return filepath.Join(*cohort, id) },
		ResultsRoot:     *resultsRoot,
		Gate:            gate,
		Concurrency:     *concurrency,
		MaxStallRetries: *maxStalls,
		StallWait:       *stallWait,
		Progress:        progressPrinter(),
		Run: func(ctx context.Context, arm batch.Arm, caseID, bundlePath, runID string) (string, float64, error) {
			return runBench(ctx, arm, caseID, bundlePath, runID, *agents, *image, *resultsRoot, *dryRun)
		},
	}

	result, err := batch.Run(context.Background(), opts)
	dest := *out
	if dest == "" {
		dest = filepath.Join(*resultsRoot, fmt.Sprintf("h1-batch-%s.json", time.Now().UTC().Format("20060102T150405Z")))
	}
	if writeErr := writeJSON(dest, result); writeErr != nil {
		fmt.Fprintln(os.Stderr, "h1run: writing record:", writeErr)
	}
	check(err)

	fmt.Println()
	for _, s := range result.Summaries {
		fmt.Println(s)
	}
	fmt.Printf("total cost estimate $%.2f (%s)\n", result.CostUSD, result.Note)
	fmt.Printf("record: %s\n", dest)
	if stalled := countStatus(result, batch.StatusStalled); stalled > 0 {
		fmt.Printf("\n%d case(s) STALLED on the subscription window. Nothing was sealed for them, so re-running this exact command picks them up where it left off.\n", stalled)
	}
	if failed := countStatus(result, batch.StatusFailed); failed > 0 {
		fmt.Printf("%d case(s) FAILED for reasons other than rate limiting; see the record.\n", failed)
		os.Exit(1)
	}
}

func progressPrinter() func(batch.Summary) {
	var last time.Time
	return func(s batch.Summary) {
		// Every ~10 completions or at least every 2 minutes, plus always
		// when something stalls or fails.
		now := time.Now()
		if s.Stalled > 0 || s.Failed > 0 || (s.Done+s.Skipped)%10 == 0 || now.Sub(last) > 2*time.Minute {
			fmt.Println("h1run:", s)
			last = now
		}
	}
}

// runBench shells out to cmd/bench, which is the single definition of how
// a case is run and sealed. Calling the orchestrator directly here would
// create a second path that could drift from the one every other result
// was produced by.
func runBench(ctx context.Context, arm batch.Arm, caseID, bundlePath, runID, agents, image, resultsRoot string, dryRun bool) (string, float64, error) {
	args := []string{"run", "./cmd/bench",
		"-protocol", arm.ProtocolPath,
		"-bundle", bundlePath,
		"-case-id", caseID,
		"-run-id", runID,
		"-results-root", resultsRoot,
	}
	if dryRun {
		// The canned scenario must match the arm's STAGE COUNT: h1-clean
		// defines reasoner-1 only, so using it for two-stage T fails every
		// case at reasoner-2. Caught by the first dry run over the real
		// cohort, which is what that mode is for.
		args = append(args, "-agents", "fixtures/agents/fixture.yaml", "-fixture-scenario", dryRunScenario(arm))
	} else {
		args = append(args, "-agents", agents, "-adapter-image", image)
	}

	cmd := exec.CommandContext(ctx, "go", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// The vendor's message is in the output, and the batch layer needs
		// it to tell a rate limit from a real failure.
		return "", costOf(resultsRoot, runID), fmt.Errorf("%v: %s", err, strings.TrimSpace(string(output)))
	}
	return runID, costOf(resultsRoot, runID), nil
}

// costOf reads the sealed run's own cost figure. Under a subscription
// token this is the CLI's estimate, not an amount billed.
func costOf(resultsRoot, runID string) float64 {
	raw, err := os.ReadFile(filepath.Join(resultsRoot, runID, "run.json"))
	if err != nil {
		return 0
	}
	var run struct {
		Stages []struct {
			Usage struct {
				CostUSD float64 `json:"cost_usd"`
			} `json:"usage"`
		} `json:"stages"`
	}
	if json.Unmarshal(raw, &run) != nil {
		return 0
	}
	var total float64
	for _, s := range run.Stages {
		total += s.Usage.CostUSD
	}
	return total
}

// dryRunScenario picks a canned scenario with the same shape as the arm.
//
// Both are CLEAN scenarios - no findings, and therefore no citations. That
// is required, not incidental: the other h1 scenarios cite the synthetic
// paging fixture, which exists in no mined bundle, so the evidence check
// rejects them on every real case. A dry run must exercise validation,
// handoffs and sealing without depending on the snapshot's contents.
func dryRunScenario(arm batch.Arm) string {
	if strings.EqualFold(arm.Label, "T") {
		return "h1-two-stage-clean"
	}
	return "h1-clean"
}

func discoverCases(cohort string, only []string) ([]string, error) {
	entries, err := os.ReadDir(cohort)
	if err != nil {
		return nil, err
	}
	want := map[string]bool{}
	for _, id := range only {
		want[id] = true
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "case-") {
			continue
		}
		if len(want) > 0 && !want[e.Name()] {
			continue
		}
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out, nil
}

// buildGate reads the probe reports and the evaluator's verdicts. With no
// -probes directory the gate is empty and every case may run: that is for
// the fixture dry run and for cohorts with no positives to probe, and the
// operator sees the count printed either way.
func buildGate(probesDir, verdictsDir string, cases []string) (batch.ProbeGate, error) {
	gate := batch.ProbeGate{Voided: map[string]bool{}, Unresolved: map[string]bool{}}
	if probesDir == "" {
		return gate, nil
	}
	verdicts, err := probe.LoadVerdicts(verdictsDir, cases)
	if err != nil {
		return gate, err
	}
	for _, id := range cases {
		raw, err := os.ReadFile(filepath.Join(probesDir, id+".contamination-probe.json"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return gate, err
		}
		var rep probe.Report
		if err := json.Unmarshal(raw, &rep); err != nil {
			return gate, fmt.Errorf("probe report for %s: %w", id, err)
		}
		v, have := verdicts[id]
		switch d := probe.Decide(rep, v, have); {
		case d.Void:
			gate.Voided[id] = true
		case d.Unresolved:
			gate.Unresolved[id] = true
		}
	}
	return gate, nil
}

// gateFromFile builds the gate from the evaluator's published decision
// instead of from probe reports. The runner learns clean/void/unresolved and
// nothing else - no symptom prose, no model answers, no file under an
// -evaluator-inputs directory opened by anything on this side.
func gateFromFile(path, cohortRecords string, cases []string) (batch.ProbeGate, probe.Gate, error) {
	gate := batch.ProbeGate{Voided: map[string]bool{}, Unresolved: map[string]bool{}}
	g, err := probe.LoadGate(path, cohortRecords, cases)
	if err != nil {
		return gate, probe.Gate{}, err
	}
	for _, id := range cases {
		switch g.Cases[id] {
		case probe.GateVoid:
			gate.Voided[id] = true
		case probe.GateUnresolved:
			gate.Unresolved[id] = true
		}
	}
	return gate, g, nil
}

func buildArms(labels []string, dryRun bool) ([]batch.Arm, error) {
	known := map[string]batch.Arm{
		"T": {Label: "T", ProtocolPath: "configs/protocols/h1-t-v1.yaml", Version: "h1-t-v1"},
		"G": {Label: "G", ProtocolPath: "configs/protocols/h1-g-v1.yaml", Version: "h1-g-v1"},
	}
	var out []batch.Arm
	for _, l := range labels {
		a, ok := known[strings.ToUpper(l)]
		if !ok {
			return nil, fmt.Errorf("unknown arm %q (known: T, G)", l)
		}
		out = append(out, a)
	}
	return out, nil
}

func countStatus(r batch.Result, status string) int {
	n := 0
	for _, c := range r.Cases {
		if c.Status == status {
			n++
		}
	}
	return n
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

func writeJSON(path string, v any) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "h1run:", err)
		os.Exit(1)
	}
}
