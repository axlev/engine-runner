// h1score is the H1 cross-run aggregate (#5b), run by the evaluator over
// sealed runs from arms T and G with the cohort's label file.
//
//	h1score -runs build/results -labels h1-labels.json -cohort-manifest cohort.json \
//	        [-history DIR] [-fixing-paths DIR] [-scans DIR] -out <evaluator-only dir>
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/axlev/engine-runner/internal/h1judge"
	"github.com/axlev/engine-runner/internal/h1score"
	"github.com/axlev/engine-runner/internal/probe"
)

func main() {
	// -render is a second mode: read a scored report and write the results
	// document. Separate from scoring so the document can be regenerated
	// from a sealed result without re-reading any run, and so nothing in
	// it can disagree with the JSON it came from.
	if len(os.Args) > 2 && os.Args[1] == "-render" {
		renderMode(os.Args[2], os.Args[3:])
		return
	}
	runs := flag.String("runs", "build/results", "sealed run directories, all arms")
	labels := flag.String("labels", "", "h1-labels/v1 file (required)")
	manifest := flag.String("cohort-manifest", "", "cohort manifest the labels were written for (required)")
	history := flag.String("history", "", "directory of <case_id>.json history-baseline/v1 files (arm H)")
	fixing := flag.String("fixing-paths", "", "directory of <case_id>.json h1-fixing-paths/v1 files")
	scans := flag.String("scans", "", "contamscan output directory; voided runs are excluded")
	probes := flag.String("probes", "", "cmd/probe output directory (A11); a probe hit voids the case for EVERY arm")
	probeVerdicts := flag.String("probe-verdicts", "", "directory of <case_id>.json contamination-probe-verdict/v1 files written by the evaluator after reading the probe responses")
	judgeDir := flag.String("judge", "", "cmd/h1judge output directory of <case_id>.h1-judge.json files. Without it the section 2 reason-match criterion renders absent, and H1 is not evaluated")
	armT := flag.String("arm-t", "h1-t-v1", "protocol_version of arm T")
	armG := flag.String("arm-g", "h1-g-v1", "protocol_version of arm G")
	threshold := flag.Float64("threshold", 15, "pre-registered points over each baseline")
	out := flag.String("out", "", "evaluator-only output directory (required)")
	flag.Parse()
	if *labels == "" || *manifest == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "h1score: -labels, -cohort-manifest and -out are required")
		os.Exit(2)
	}

	l, labelsSum, err := h1score.LoadLabels(*labels, *manifest)
	check(err)
	voided, err := h1score.LoadVoided(*scans)
	check(err)
	armIDs := map[string]string{h1score.ArmT: *armT, h1score.ArmG: *armG}
	verdicts, voidCases, err := h1score.LoadVerdicts(*runs, armIDs, voided)
	check(err)
	hist, histReasons, err := h1score.LoadHistory(*history, l.Cases)
	check(err)
	fp, err := h1score.LoadFixingPaths(*fixing, l.Cases)
	check(err)
	probeVoided, probeUnresolved := loadProbes(*probes, *probeVerdicts, l.Cases)
	reasonMatch, err := loadReasonMatch(*judgeDir, l.Cases, verdicts)
	check(err)

	inputs := map[string]string{
		"labels_sha256":          labelsSum,
		"cohort_manifest_sha256": l.CohortManifestSHA256,
		"runs_root":              *runs,
		"history_dir":            *history,
		"fixing_paths_dir":       *fixing,
		"scans_dir":              *scans,
		"probes_dir":             *probes,
		"probe_verdicts_dir":     *probeVerdicts,
		"judge_dir":              *judgeDir,
	}
	report, err := h1score.Score(h1score.Options{
		Labels: l, Inputs: inputs, ArmIDs: armIDs, Verdicts: verdicts, Voided: voidCases,
		History: hist, HistoryAbsentReasons: histReasons, FixingPaths: fp, Threshold: *threshold,
		ProbeVoided: probeVoided, ProbeUnresolved: probeUnresolved,
		ReasonMatch: reasonMatch,
	})
	check(err)
	check(os.MkdirAll(*out, 0o755))
	b, _ := json.MarshalIndent(report, "", "  ")
	check(os.WriteFile(filepath.Join(*out, "h1-score.json"), append(b, '\n'), 0o644))
	for _, arm := range []string{"T", "G", "H"} {
		a := report.Arms[arm]
		fmt.Printf("%s: cases %d voided %d  TP %d FP %d FN %d TN %d  precision %s  recall %s\n",
			arm, a.Cases, a.Voided, a.Counts.TP, a.Counts.FP, a.Counts.FN, a.Counts.TN, show(a.Precision), show(a.Recall))
	}
	if len(report.ProbeVoidedCases) > 0 {
		fmt.Printf("probe-voided for every arm: %d case(s)\n", len(report.ProbeVoidedCases))
	}
	if report.Provisional {
		fmt.Printf("PROVISIONAL: %s\n", report.ProvisionalReason)
	}
	for _, p := range report.Pairwise {
		fmt.Printf("%s: common %d discordant %d  dPrecision %s (p1 %s)  dRecall %s (p1 %s)\n",
			p.Comparison, p.CommonCases, p.Discordant, showF(p.DeltaPrecision), showF(p.PPrecision.OneSided), showF(p.DeltaRecall), showF(p.PRecall.OneSided))
	}
}

// loadReasonMatch turns the judge's per-case reports into the section 6
// figure. It computes nothing itself: h1judge.Summarise already owns that
// arithmetic and cmd/h1judge prints the same numbers from it, so a second
// implementation here could drift from the one whose output is published.
//
// It fails closed on two disagreements, because both mean the figure would
// describe something other than what was scored:
//
//   - a judged finding that is not the scored primary. The judge is supposed
//     to read the primary carried in verdict.json precisely so it cannot
//     pick a different finding than the scorer did; if they differ, the
//     verdict answers a question about a finding this report does not count.
//   - reports disagreeing on judge model or in-family. Section 6 requires
//     in-family stated on every figure derived from it, and one figure
//     cannot carry two answers.
//
// Absent -judge it returns nil, which leaves the criterion rendering absent
// rather than inventing a zero.
func loadReasonMatch(dir string, cases []h1score.Label, verdicts map[string]map[string]h1score.CaseVerdict) (any, error) {
	if dir == "" {
		return nil, nil
	}
	ids := make([]string, 0, len(cases))
	for _, c := range cases {
		ids = append(ids, c.CaseID)
	}
	reports, err := h1judge.LoadReports(dir, ids)
	if err != nil {
		return nil, err
	}
	if len(reports) == 0 {
		return nil, fmt.Errorf("h1score: -judge %s holds no h1-judge/v1 report for any labelled case", dir)
	}

	model, inFamily := reports[0].JudgeModel, reports[0].InFamily
	for _, r := range reports {
		if r.JudgeModel != model || r.InFamily != inFamily {
			return nil, fmt.Errorf("h1score: judge reports disagree: %s is %q (in-family %v) but %s is %q (in-family %v)",
				reports[0].CaseID, model, inFamily, r.CaseID, r.JudgeModel, r.InFamily)
		}
		for arm, v := range r.Verdicts {
			cv, ok := verdicts[arm][r.CaseID]
			if !ok {
				return nil, fmt.Errorf("h1score: judge report for %s judges arm %s, which has no scored verdict for that case", r.CaseID, arm)
			}
			if cv.PrimaryFindingID != v.FindingID {
				return nil, fmt.Errorf("h1score: judge read finding %q for arm %s on %s but the scored primary is %q; the verdict describes a finding this report does not count",
					v.FindingID, arm, r.CaseID, cv.PrimaryFindingID)
			}
		}
	}
	return h1judge.Summarise(reports, model, inFamily), nil
}

func show(r h1score.Rate) string {
	if r.Value == nil {
		return "absent (" + r.Absent + ")"
	}
	return fmt.Sprintf("%.3f (%d/%d)", *r.Value, r.Numerator, r.Denominator)
}

func showF(p *float64) string {
	if p == nil {
		return "absent"
	}
	return fmt.Sprintf("%.3f", *p)
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "h1score:", err)
		os.Exit(1)
	}
}

// loadProbes reads the A11 probe reports and the evaluator's verdicts, and
// splits the cohort into cases the probe voided and cases it could not
// clear. A case with no probe report at all is neither: probing is the
// evaluator's step, and h1score does not invent a result for a case that
// was never probed.
func loadProbes(probesDir, verdictsDir string, cases []h1score.Label) (voided, unresolved []string) {
	if probesDir == "" {
		return nil, nil
	}
	ids := make([]string, 0, len(cases))
	for _, c := range cases {
		ids = append(ids, c.CaseID)
	}
	verdicts, err := probe.LoadVerdicts(verdictsDir, ids)
	check(err)
	for _, c := range cases {
		p := filepath.Join(probesDir, c.CaseID+".contamination-probe.json")
		raw, err := os.ReadFile(p)
		if os.IsNotExist(err) {
			continue
		}
		check(err)
		var rep probe.Report
		check(json.Unmarshal(raw, &rep))
		if rep.SchemaVersion != probe.ReportSchemaVersion || rep.CaseID != c.CaseID {
			check(fmt.Errorf("%s is not a %s file for %s", p, probe.ReportSchemaVersion, c.CaseID))
		}
		v, have := verdicts[c.CaseID]
		switch d := probe.Decide(rep, v, have); {
		case d.Void:
			voided = append(voided, c.CaseID)
		case d.Unresolved:
			unresolved = append(unresolved, c.CaseID)
		}
	}
	return voided, unresolved
}

// renderMode turns a scored h1-score.json into docs/h1-results.md.
//
//	h1score -render <scored.json> [-labels <h1-labels.json>] [-draft] > docs/h1-results.md
//
// The labels file supplies the covariates the strata need; without it the
// document reports each stratum as not recorded rather than inventing one.
func renderMode(scoredPath string, rest []string) {
	fs := flag.NewFlagSet("render", flag.ExitOnError)
	labelsPath := fs.String("labels", "", "h1-labels/v1 file, for the covariate strata")
	draft := fs.Bool("draft", false, "mark the document a draft scaffold: numbers are fixture or partial")
	engineCommit := fs.String("engine-commit", "", "engine commit the arms ran at")
	cohortRecords := fs.String("cohort-records", "", "cohort manifest records_sha256")
	var notes stringList
	fs.Var(&notes, "note", "a provenance line to record verbatim; repeatable. The document is generated, so notes belong here rather than edited into it")
	_ = fs.Parse(rest)

	raw, err := os.ReadFile(scoredPath)
	check(err)
	var report h1score.Report
	check(json.Unmarshal(raw, &report))
	if report.SchemaVersion != h1score.SchemaVersion {
		check(fmt.Errorf("%s is %q, want %s", scoredPath, report.SchemaVersion, h1score.SchemaVersion))
	}

	var labels []h1score.Label
	if *labelsPath != "" {
		lraw, err := os.ReadFile(*labelsPath)
		check(err)
		var l h1score.Labels
		check(json.Unmarshal(lraw, &l))
		labels = l.Cases
	}

	meta := h1score.RenderMeta{
		SourceFile:      filepath.Base(scoredPath),
		EngineCommit:    *engineCommit,
		CohortRecords:   *cohortRecords,
		Draft:           *draft,
		ProvenanceNotes: notes,
		ProtocolHashes:  map[string]string{},
		PromptHashes:    map[string]string{},
	}
	fmt.Print(h1score.Render(report, labels, meta))
}

// stringList collects a repeatable flag in the order given.
type stringList []string

func (s *stringList) String() string { return strings.Join(*s, "; ") }

func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}
