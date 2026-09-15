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

	"github.com/axlev/engine-runner/internal/h1score"
)

func main() {
	runs := flag.String("runs", "build/results", "sealed run directories, all arms")
	labels := flag.String("labels", "", "h1-labels/v1 file (required)")
	manifest := flag.String("cohort-manifest", "", "cohort manifest the labels were written for (required)")
	history := flag.String("history", "", "directory of <case_id>.json history-baseline/v1 files (arm H)")
	fixing := flag.String("fixing-paths", "", "directory of <case_id>.json h1-fixing-paths/v1 files")
	scans := flag.String("scans", "", "contamscan output directory; voided runs are excluded")
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
	verdicts, voidCount, err := h1score.LoadVerdicts(*runs, armIDs, voided)
	check(err)
	hist, err := h1score.LoadHistory(*history, l.Cases)
	check(err)
	fp, err := h1score.LoadFixingPaths(*fixing, l.Cases)
	check(err)

	inputs := map[string]string{
		"labels_sha256":          labelsSum,
		"cohort_manifest_sha256": l.CohortManifestSHA256,
		"runs_root":              *runs,
		"history_dir":            *history,
		"fixing_paths_dir":       *fixing,
		"scans_dir":              *scans,
	}
	report, err := h1score.Score(h1score.Options{
		Labels: l, Inputs: inputs, ArmIDs: armIDs, Verdicts: verdicts, Voided: voidCount,
		History: hist, FixingPaths: fp, Threshold: *threshold,
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
	for _, p := range report.Pairwise {
		fmt.Printf("%s: common %d discordant %d  dPrecision %s (p1 %s)  dRecall %s (p1 %s)\n",
			p.Comparison, p.CommonCases, p.Discordant, showF(p.DeltaPrecision), showF(p.PPrecision.OneSided), showF(p.DeltaRecall), showF(p.PRecall.OneSided))
	}
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
