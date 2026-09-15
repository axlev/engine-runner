// Command tiersense measures how far the correspondence rate moves when the
// oracle's signal tier threshold changes.
//
// It is an EVALUATOR tool, not part of the review path: it reads the
// answer-key side of a case. internal/oracle's isolation tests keep
// cmd/bench and the reasoner packages away from this data; this binary is
// deliberately separate so that the evaluator can read what the engine must
// not.
//
// WHY THE OUTPUT IS SPLIT IN TWO. Stdout carries aggregate rates only.
// Per-case and per-finding detail goes to -detail, a file the operator does
// not open. The paths a signal names are where the defect was, so per-case
// output would de-blind whoever runs this on every case in the cohort - and
// the cohort's disposition analysis has been performed blind throughout. The
// detail file exists because a non-blind agent needs it later for actual
// scoring.
//
// It chooses no tier. It reports three and leaves the choice to a human with
// the spread in front of them, for the same reason the A→C confidence
// threshold was reported as a range: a metric with a free parameter that
// swings the answer can be tuned to whatever conclusion is wanted.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/axlev/engine-runner/internal/oracle"
)

type threshold struct {
	name string
	min  oracle.Strength
}

// The three thresholds. StrengthNone is deliberately not one of them: paths
// that appear only in a reversal overlap are not attested by any signal, and
// counting them would mean scoring against evidence nobody claimed.
var thresholds = []threshold{
	{"strong-only", oracle.StrengthStrong},
	{"strong+medium", oracle.StrengthMedium},
	{"all-tiers", oracle.StrengthWeak},
}

type finding struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Files []string
}

type detailRow struct {
	Arm       string              `json:"arm"`
	RunID     string              `json:"run_id"`
	CaseID    string              `json:"case_id"`
	FindingID string              `json:"finding_id"`
	Files     []string            `json:"files"`
	Matched   map[string]bool     `json:"matched_by_threshold"`
	MatchedOn map[string][]string `json:"matched_on_paths"`
}

func main() {
	results := flag.String("results", "build/results", "root holding sealed run directories")
	cohort := flag.String("cohort", "/home/alex/repos/miner/output/frr-pilot-cohort", "root holding <case>-evaluator-only/")
	detail := flag.String("detail", "", "path for per-case/per-finding detail (REQUIRED; do not open it if you are doing blind analysis)")
	flag.Parse()

	if *detail == "" {
		fmt.Fprintln(os.Stderr, "tiersense: -detail is required; refusing to run without somewhere to put the per-case output")
		os.Exit(2)
	}

	arms := []struct{ name, glob string }{
		{"sonnet", "frr-*"},
		{"opus-narrow", "opus-*"},
		{"opus-wide", "opuswide-*"},
	}

	oracleCache := map[string]oracle.Evidence{}
	var rows []detailRow

	for _, arm := range arms {
		dirs, err := filepath.Glob(filepath.Join(*results, arm.glob))
		if err != nil {
			fatal(err)
		}
		for _, dir := range dirs {
			base := filepath.Base(dir)
			// "opus-*" also matches "opuswide-*"; keep the arms disjoint.
			if arm.name == "opus-narrow" && strings.HasPrefix(base, "opuswide-") {
				continue
			}
			caseID, ok := readCaseID(dir)
			if !ok {
				continue
			}
			findings, ok := readFindings(dir)
			if !ok {
				continue
			}
			ev, ok := oracleCache[caseID]
			if !ok {
				p := filepath.Join(*cohort, caseID+"-evaluator-only", "correlated-report.json")
				loaded, err := oracle.Load(p, caseID)
				if err != nil {
					fatal(fmt.Errorf("case %s: %w", caseID, err))
				}
				oracleCache[caseID] = loaded
				ev = loaded
			}

			for _, f := range findings {
				row := detailRow{
					Arm: arm.name, RunID: base, CaseID: caseID, FindingID: f.ID,
					Files: f.Files, Matched: map[string]bool{}, MatchedOn: map[string][]string{},
				}
				for _, th := range thresholds {
					hits := matchPaths(ev, f.Files, th.min)
					row.Matched[th.name] = len(hits) > 0
					row.MatchedOn[th.name] = hits
				}
				rows = append(rows, row)
			}
		}
	}

	writeDetail(*detail, rows, oracleCache)
	report(rows, oracleCache)
}

// matchPaths returns the oracle paths at or above min that a finding cites.
//
// This is same-file matching and nothing more - the coarsest of the four
// candidate rules, and the only one this record can support unaided. It is
// not a rule adoption: it is the floor, reported so the tier question can be
// answered independently of the rule question.
func matchPaths(ev oracle.Evidence, files []string, min oracle.Strength) []string {
	var hits []string
	for _, f := range files {
		if f == "" {
			continue
		}
		if pe, ok := ev.ByPath(f); ok && pe.Strength >= min {
			hits = append(hits, f)
		}
	}
	sort.Strings(hits)
	return hits
}

// scoreable reports whether a case has any path at or above min. A case with
// no signals at a tier cannot produce correspondence there, and folding it in
// with the cases that can would conflate "no evidence" with "no match".
func scoreable(ev oracle.Evidence, min oracle.Strength) bool {
	for _, p := range ev.Paths {
		if p.Strength >= min {
			return true
		}
	}
	return false
}

func report(rows []detailRow, cache map[string]oracle.Evidence) {
	arms := []string{"sonnet", "opus-narrow", "opus-wide"}

	fmt.Println("Correspondence rate by oracle signal tier (same-file matching).")
	fmt.Println("Aggregate only, by design: per-case output names the paths a signal")
	fmt.Println("touched, which is where the defect was.")
	fmt.Println()

	for _, th := range thresholds {
		fmt.Printf("=== %s ===\n", th.name)

		// "all findings" counts every finding in the arm. "scoreable base"
		// counts only findings in cases that have at least one signal at
		// this tier - the distinction matters most at strong-only, where
		// several cases have nothing to match against.
		var allN, allHit, scN, scHit int
		perArm := map[string][4]int{}

		for _, r := range rows {
			ev := cache[r.CaseID]
			hit := r.Matched[th.name]
			sc := scoreable(ev, th.min)

			a := perArm[r.Arm]
			a[0]++
			if hit {
				a[1]++
			}
			if sc {
				a[2]++
				if hit {
					a[3]++
				}
			}
			perArm[r.Arm] = a

			allN++
			if hit {
				allHit++
			}
			if sc {
				scN++
				if hit {
					scHit++
				}
			}
		}

		fmt.Printf("  overall      all findings %3d/%3d = %5.1f%%   scoreable base %3d/%3d = %5.1f%%\n",
			allHit, allN, pct(allHit, allN), scHit, scN, pct(scHit, scN))
		for _, a := range arms {
			v := perArm[a]
			fmt.Printf("  %-12s all findings %3d/%3d = %5.1f%%   scoreable base %3d/%3d = %5.1f%%\n",
				a, v[1], v[0], pct(v[1], v[0]), v[3], v[2], pct(v[3], v[2]))
		}

		var nCases int
		for _, ev := range cache {
			if scoreable(ev, th.min) {
				nCases++
			}
		}
		fmt.Printf("  cases with any path at this tier: %d of %d\n\n", nCases, len(cache))
	}
}

func pct(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b) * 100
}

func writeDetail(path string, rows []detailRow, cache map[string]oracle.Evidence) {
	out := map[string]any{
		"note":  "Per-finding detail. Contains oracle path names: do not read this while performing blind analysis.",
		"rows":  rows,
		"cases": cache,
	}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		fatal(err)
	}
	fmt.Fprintf(os.Stderr, "tiersense: per-finding detail written to %s (%d rows) - not read by this process\n", path, len(rows))
}

func readCaseID(dir string) (string, bool) {
	var doc struct {
		CaseID string `json:"case_id"`
	}
	if !readJSON(filepath.Join(dir, "run.json"), &doc) || doc.CaseID == "" {
		return "", false
	}
	return doc.CaseID, true
}

// readFindings returns stage-1 findings that also have a B/C pair, which is
// the same population every other number in this project is computed over.
func readFindings(dir string) ([]finding, bool) {
	var a struct {
		Findings []struct {
			ID       string `json:"id"`
			Title    string `json:"title"`
			Evidence []struct {
				File string `json:"file"`
			} `json:"evidence"`
		} `json:"findings"`
	}
	if !readJSON(filepath.Join(dir, "stages", "reasoner-1", "review-a.json"), &a) {
		return nil, false
	}
	var ev struct {
		Findings []struct {
			ID string  `json:"id"`
			B  *string `json:"b_disposition"`
		} `json:"findings"`
	}
	if !readJSON(filepath.Join(dir, "evaluation", "findings.json"), &ev) {
		return nil, false
	}
	paired := map[string]bool{}
	for _, f := range ev.Findings {
		if f.B != nil {
			paired[f.ID] = true
		}
	}

	var out []finding
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
		out = append(out, finding{ID: f.ID, Title: f.Title, Files: files})
	}
	return out, true
}

func readJSON(path string, v any) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	return json.Unmarshal(data, v) == nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "tiersense:", err)
	os.Exit(1)
}
