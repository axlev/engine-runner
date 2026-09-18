package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/axlev/engine-runner/internal/h1score"
)

// verifyReport is the part of an h1-verify report the scorer reads.
type verifyReport struct {
	Unit struct {
		CaseID    string `json:"case_id"`
		Arm       string `json:"arm"`
		RunID     string `json:"run_id"`
		FindingID string `json:"finding_id"`
	} `json:"unit"`
	Disposition string `json:"disposition"`
	Unparseable bool   `json:"unparseable_first_response"`
	Error       string `json:"error"`
}

// VerifySummary is what the skeptic pass did, reported beside every figure
// derived from it.
type VerifySummary struct {
	Verifications  int            `json:"verifications"`
	ByDisposition  map[string]int `json:"by_disposition"`
	Unparseable    int            `json:"unparseable_first_responses"`
	CasesCovered   int            `json:"case_arm_pairs_covered"`
	RiskyWithdrawn int            `json:"risky_withdrawn"`
}

// loadVerification reads the skeptic pass and derives, per arm and case,
// whether the RISKY verdict survives.
//
// RISKY' = RISKY AND at least one qualifying finding CONFIRMED. "At least
// one" is not a softening: a case is RISKY if ANY qualifying finding stands,
// so one surviving confirmation keeps it RISKY, and withdrawing the verdict
// requires the verifier to have rejected every one of them.
//
// INCONCLUSIVE and an unparseable response both count as NOT confirmed, per
// the pre-registered rule, and are reported rather than folded into the
// rejections - "the skeptic could not tell" and "the skeptic said no" are
// different claims and only one of them supports H1.1.
//
// A pair with any errored verification is left UNCOVERED, so the original
// verdict stands. A partially verified case would otherwise have its
// verdict withdrawn on the strength of the findings that happened to run.
func loadVerification(dir string) (map[string]map[string]bool, VerifySummary, error) {
	sum := VerifySummary{ByDisposition: map[string]int{}}
	if dir == "" {
		return nil, sum, nil
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.h1-verify.json"))
	if len(files) == 0 {
		return nil, sum, fmt.Errorf("h1score: -verify %s holds no h1-verify report", dir)
	}
	type pair struct{ arm, caseID string }
	confirmed := map[pair]bool{}
	broken := map[pair]bool{}
	seen := map[pair]bool{}

	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			return nil, sum, err
		}
		var r verifyReport
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, sum, fmt.Errorf("h1score: parsing %s: %w", f, err)
		}
		if r.Unit.Arm == "" || r.Unit.CaseID == "" {
			return nil, sum, fmt.Errorf("h1score: %s names no arm or case", f)
		}
		k := pair{r.Unit.Arm, r.Unit.CaseID}
		seen[k] = true
		sum.Verifications++
		switch {
		case r.Error != "":
			broken[k] = true
			sum.ByDisposition["ERROR"]++
		case r.Unparseable:
			sum.Unparseable++
			sum.ByDisposition["INCONCLUSIVE"]++
		default:
			sum.ByDisposition[strings.ToUpper(r.Disposition)]++
			if strings.EqualFold(r.Disposition, "CONFIRMED") {
				confirmed[k] = true
			}
		}
	}

	out := map[string]map[string]bool{}
	for k := range seen {
		if broken[k] {
			continue // not covered; the original verdict stands
		}
		if out[k.arm] == nil {
			out[k.arm] = map[string]bool{}
		}
		out[k.arm][k.caseID] = confirmed[k]
		sum.CasesCovered++
		if !confirmed[k] {
			sum.RiskyWithdrawn++
		}
	}
	return out, sum, nil
}

// armsOf is a tiny helper so main can print the summary without importing
// the internal package's arm constants twice.
func armsOf() []string { return []string{h1score.ArmTPrime, h1score.ArmGPrime} }
