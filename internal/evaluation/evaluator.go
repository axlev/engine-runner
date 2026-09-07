// Package evaluation implements the MVP's deterministic scorer, per
// docs/system-design.md section 6.5.
//
// This evaluator computes only reviewer-side metrics - what reasoner-2 and
// reasoner-3 did with reasoner-1's findings - because no oracle bundle
// exists yet for any fixture case (oracle bundles arrive with real mined
// cases in Milestone 3). Precision/recall against retrospective ground
// truth is deliberately not computed here: faking it against absent oracle
// data would be worse than not reporting it. A later LLM evaluator adding
// oracle-based metrics must still run as a separate evaluator-only agent
// per section 6.5 - this package does not need that isolation today only
// because it is deterministic Go code with no oracle access to protect.
package evaluation

import (
	"encoding/json"
	"fmt"
	"os"
)

type Scores struct {
	SchemaVersion string `json:"schema_version"`
	RunID         string `json:"run_id"`
	CaseID        string `json:"case_id"`

	FindingsDiscovered int `json:"findings_discovered"`

	ConfirmedByB    int `json:"confirmed_by_b"`
	NarrowedByB     int `json:"narrowed_by_b"`
	RejectedByB     int `json:"rejected_by_b"`
	InconclusiveByB int `json:"inconclusive_by_b"`

	ConfirmedByC    int `json:"confirmed_by_c"`
	NarrowedByC     int `json:"narrowed_by_c"`
	RejectedByC     int `json:"rejected_by_c"`
	InconclusiveByC int `json:"inconclusive_by_c"`

	NewFindingsByC int `json:"new_findings_by_c"`
}

// FinalStatus values. A finding without a C verdict at all (which should
// not happen when reasoner-3 runs to completion) falls back to its B
// disposition, then to "unassessed".
const (
	StatusConfirmed     = "confirmed"
	StatusNarrowed      = "narrowed"
	StatusRejected      = "rejected"
	StatusInconclusive  = "inconclusive"
	StatusUnassessed    = "unassessed"
	StatusNewUnverified = "new_unverified"
)

type Finding struct {
	ID           string  `json:"id"`
	Title        string  `json:"title"`
	Severity     string  `json:"severity,omitempty"`
	Confidence   float64 `json:"confidence,omitempty"`
	BDisposition string  `json:"b_disposition,omitempty"`
	CDisposition string  `json:"c_disposition,omitempty"`
	FinalStatus  string  `json:"final_status"`
}

type Findings struct {
	SchemaVersion string    `json:"schema_version"`
	RunID         string    `json:"run_id"`
	CaseID        string    `json:"case_id"`
	Findings      []Finding `json:"findings"`
}

type reviewADoc struct {
	Findings []struct {
		ID         string  `json:"id"`
		Title      string  `json:"title"`
		Severity   string  `json:"severity"`
		Confidence float64 `json:"confidence"`
	} `json:"findings"`
}

type reviewBDoc struct {
	Assessments []struct {
		FindingID   string `json:"finding_id"`
		Disposition string `json:"disposition"`
	} `json:"assessments"`
}

type reviewCDoc struct {
	Verdicts []struct {
		FindingID   string `json:"finding_id"`
		Disposition string `json:"disposition"`
	} `json:"verdicts"`
	NewFindings []struct {
		ID         string  `json:"id"`
		Title      string  `json:"title"`
		Severity   string  `json:"severity"`
		Confidence float64 `json:"confidence"`
	} `json:"new_findings"`
}

func readJSON(path string, v interface{}) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("evaluation: reading %s: %w", path, err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("evaluation: parsing %s: %w", path, err)
	}
	return nil
}

// Evaluate computes deterministic finding-disposition metrics from a
// completed run's three review outputs. All three paths must point at
// schema-conforming files - this is only ever called after the orchestrator
// reports a "completed" run, so that condition already holds.
func Evaluate(runID, caseID, reviewAPath, reviewBPath, reviewCPath string) (Scores, Findings, error) {
	var a reviewADoc
	if err := readJSON(reviewAPath, &a); err != nil {
		return Scores{}, Findings{}, err
	}
	var b reviewBDoc
	if err := readJSON(reviewBPath, &b); err != nil {
		return Scores{}, Findings{}, err
	}
	var c reviewCDoc
	if err := readJSON(reviewCPath, &c); err != nil {
		return Scores{}, Findings{}, err
	}

	byID := make(map[string]*Finding, len(a.Findings))
	var ordered []*Finding
	for _, af := range a.Findings {
		f := &Finding{ID: af.ID, Title: af.Title, Severity: af.Severity, Confidence: af.Confidence}
		byID[af.ID] = f
		ordered = append(ordered, f)
	}

	scores := Scores{SchemaVersion: "evaluation-scores/v1", RunID: runID, CaseID: caseID}
	scores.FindingsDiscovered = len(ordered)

	for _, bf := range b.Assessments {
		if f, ok := byID[bf.FindingID]; ok {
			f.BDisposition = bf.Disposition
		}
		switch bf.Disposition {
		case "CONFIRMED":
			scores.ConfirmedByB++
		case "NARROWED":
			scores.NarrowedByB++
		case "REJECTED":
			scores.RejectedByB++
		case "INCONCLUSIVE":
			scores.InconclusiveByB++
		}
	}

	for _, cv := range c.Verdicts {
		if f, ok := byID[cv.FindingID]; ok {
			f.CDisposition = cv.Disposition
		}
		switch cv.Disposition {
		case "CONFIRMED":
			scores.ConfirmedByC++
		case "NARROWED":
			scores.NarrowedByC++
		case "REJECTED":
			scores.RejectedByC++
		case "INCONCLUSIVE":
			scores.InconclusiveByC++
		}
	}

	for _, f := range ordered {
		f.FinalStatus = finalStatus(f.CDisposition, f.BDisposition)
	}

	for _, nf := range c.NewFindings {
		ordered = append(ordered, &Finding{
			ID:          nf.ID,
			Title:       nf.Title,
			Severity:    nf.Severity,
			Confidence:  nf.Confidence,
			FinalStatus: StatusNewUnverified,
		})
	}
	scores.NewFindingsByC = len(c.NewFindings)

	findings := Findings{SchemaVersion: "evaluation-findings/v1", RunID: runID, CaseID: caseID}
	for _, f := range ordered {
		findings.Findings = append(findings.Findings, *f)
	}

	return scores, findings, nil
}

func finalStatus(cDisposition, bDisposition string) string {
	disposition := cDisposition
	if disposition == "" {
		disposition = bDisposition
	}
	switch disposition {
	case "CONFIRMED":
		return StatusConfirmed
	case "NARROWED":
		return StatusNarrowed
	case "REJECTED":
		return StatusRejected
	case "INCONCLUSIVE":
		return StatusInconclusive
	default:
		return StatusUnassessed
	}
}
