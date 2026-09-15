// contamscan is the pre-registration section 8 post-hoc contamination scan,
// run by the evaluator over sealed results after an arm completes.
//
//	contamscan -results build/results -keys <dir of contamination-keys/v1> \
//	           -bundles <cohort dir with <case_id>/prospective> -out <evaluator-only dir>
//
// It writes one contamination-scan/v1 per run and a summary.json. Runs
// whose case has no keys file are listed as skipped, never scored clean.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/axlev/engine-runner/internal/contamscan"
)

func main() {
	results := flag.String("results", "build/results", "sealed run directories")
	keysDir := flag.String("keys", "", "directory of <case_id>.json contamination-keys/v1 files (required)")
	bundles := flag.String("bundles", "", "cohort directory holding <case_id>/prospective; omit to scan without the exclusion set")
	out := flag.String("out", "", "evaluator-only output directory (required)")
	flag.Parse()
	if *keysDir == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "contamscan: -keys and -out are required")
		os.Exit(2)
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		fail(err)
	}

	runDirs, _ := filepath.Glob(filepath.Join(*results, "*", "run.json"))
	sort.Strings(runDirs)
	var scans []contamscan.Scan
	var skipped []string
	for _, manifest := range runDirs {
		runDir := filepath.Dir(manifest)
		var m struct {
			RunID  string `json:"run_id"`
			CaseID string `json:"case_id"`
		}
		raw, err := os.ReadFile(manifest)
		if err != nil {
			fail(err)
		}
		if err := json.Unmarshal(raw, &m); err != nil {
			fail(fmt.Errorf("%s: %w", manifest, err))
		}
		keysPath := filepath.Join(*keysDir, m.CaseID+".json")
		keys, sum, err := contamscan.LoadKeys(keysPath)
		if os.IsNotExist(err) {
			skipped = append(skipped, m.RunID)
			fmt.Fprintf(os.Stderr, "contamscan: %s: no keys for case %s; skipped, NOT clean\n", m.RunID, m.CaseID)
			continue
		}
		if err != nil {
			fail(err)
		}
		bundle := ""
		if *bundles != "" {
			bundle = filepath.Join(*bundles, m.CaseID, "prospective")
			if _, err := os.Stat(filepath.Join(bundle, "reviewer")); err != nil {
				fmt.Fprintf(os.Stderr, "contamscan: %s: no prospective bundle at %s; scanning without exclusion\n", m.RunID, bundle)
				bundle = ""
			}
		}
		scan, err := contamscan.ScanRun(runDir, keys, sum, bundle)
		if err != nil {
			fail(err)
		}
		scans = append(scans, scan)
		if err := writeJSON(filepath.Join(*out, m.RunID+".contamination-scan.json"), scan); err != nil {
			fail(err)
		}
	}
	summary := contamscan.Summarise(scans, skipped, *keysDir)
	if err := writeJSON(filepath.Join(*out, "summary.json"), summary); err != nil {
		fail(err)
	}
	for arm, c := range summary.Arms {
		fmt.Printf("%s: scanned %d, voided %d\n", arm, c.Scanned, c.Voided)
	}
	fmt.Printf("skipped (no keys): %d\n", len(summary.Skipped))
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "contamscan:", err)
	os.Exit(1)
}
