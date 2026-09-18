package evaluation

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
)

// Case-level scoring for the H1 protocols (miner/docs/h1-preregistration.md
// section 5). Where the three-stage evaluator above counts what stages 2
// and 3 did with stage 1's findings, this one answers the pre-registered
// per-change question: is the case RISKY or CLEAN, and which finding is
// the case's primary claim.
//
// The rule is fixed by the pre-registration and reproduced here verbatim
// in code; the output carries the rule it applied (threshold, disposition
// set, confidence threshold, rule_version) so a scored result can never be
// separated from what scored it. No model emits a verdict - the schemas
// have no such field - and this scorer reads only the run's own stage
// outputs, so it is as blind as the reviewers were.

const (
	VerdictSchemaVersion = "h1-verdict/v1"

	// RuleVersion names the pre-registration section this scorer
	// implements. The section is amended in place until the first arm
	// runs, so a commit pin would drift; the rule is pinned instead by the
	// sha256 of its text (RuleText), which moves exactly when the rule
	// does. TestRuleTextMatchesPreregistration compares RuleText to the
	// miner's copy when that checkout is present.
	RuleVersion = "h1-preregistration s5"

	VerdictRisky = "RISKY"
	VerdictClean = "CLEAN"

	ArmRuleT = "T"
	ArmRuleG = "G"

	severityThreshold   = "medium"
	confidenceThreshold = 0.6
)

// RuleText is pre-registration section 5, verbatim. It is the rule; the
// code below is its transcription, and RuleTextSHA256 is what the output
// carries so a verdict can be matched to the exact wording that produced
// it.
const RuleText = `## 5. Verdict mapping — fixed now

A case is **RISKY** in arms T and G iff at least one finding has severity ≥ ` + "`medium`" + ` and, in T, stage-B disposition ` + "`CONFIRMED`" + ` or ` + "`NARROWED`" + `; in G, confidence ≥ 0.6. Otherwise CLEAN. ` + "`INCONCLUSIVE`" + ` and ` + "`REJECTED`" + ` do not count.

A continuous risk score (max over findings of severity-weight × confidence) is also recorded for ROC reporting, but the pre-registered comparison uses the binary rule above.
`

// RuleTextSHA256 returns the hex sha256 of RuleText.
func RuleTextSHA256() string {
	sum := sha256.Sum256([]byte(RuleText))
	return hex.EncodeToString(sum[:])
}

// severityRank orders the h1-review-a severity enum. Used only for the
// ">= medium" comparison; it is not a weight.
var severityRank = map[string]int{"low": 1, "medium": 2, "high": 3, "critical": 4}

// SeverityWeights are pre-registration s12 A6 (miner d20eeee): linear, so
// ROC ordering is by severity and then by confidence within severity. Used
// only for the continuous risk score; the binary rule is unaffected.
var SeverityWeights = map[string]float64{"low": 0.25, "medium": 0.5, "high": 0.75, "critical": 1.0}

var countingDispositions = []string{"CONFIRMED", "NARROWED"}

// Rule is the scoring rule as applied, written into every verdict.
type Rule struct {
	RuleVersion         string   `json:"rule_version"`
	RuleTextSHA256      string   `json:"rule_text_sha256"`
	SeverityThreshold   string   `json:"severity_threshold"`
	DispositionSet      []string `json:"disposition_set,omitempty"`
	ConfidenceThreshold *float64 `json:"confidence_threshold,omitempty"`
	// SeverityWeights are A6's weights as applied.
	SeverityWeights map[string]float64 `json:"severity_weights"`
	// RiskScoreOver names the finding set risk_score ranges over.
	RiskScoreOver string `json:"risk_score_over"`
}

// PrimaryFinding is the case's highest-ranked surviving claim - what the
// reason-match judge compares to the fixing diff (section 6). Under T the
// mechanism and recommended_validation are stage B's; under G they are
// discovery's.
type PrimaryFinding struct {
	ID                    string                 `json:"id"`
	File                  string                 `json:"file"`
	Line                  int                    `json:"line"`
	Class                 string                 `json:"class"`
	Severity              string                 `json:"severity"`
	Confidence            float64                `json:"confidence"`
	Disposition           string                 `json:"disposition,omitempty"`
	Mechanism             string                 `json:"mechanism"`
	RecommendedValidation *RecommendedValidation `json:"recommended_validation,omitempty"`
}

type RecommendedValidation struct {
	Kind        string   `json:"kind"`
	Paths       []string `json:"paths"`
	Description string   `json:"description"`
}

// Verdict is evaluation/verdict.json.
type Verdict struct {
	SchemaVersion string `json:"schema_version"`
	RunID         string `json:"run_id"`
	CaseID        string `json:"case_id"`

	ArmRule string `json:"arm_rule"`
	Rule    Rule   `json:"rule"`

	Verdict string `json:"verdict"`

	// RiskScore is section 5's continuous score with A6's weights: max of
	// severity-weight x confidence over the findings the verdict rule
	// considers (T: stage-B survivors; G: all findings). 0 when none.
	RiskScore *float64 `json:"risk_score"`
	// RiskScoreAllDiscovered is the same statistic over every discovery
	// finding regardless of stage-B disposition - the other reading of
	// "max over findings", recorded so either ROC can be drawn without a
	// re-run. Identical to RiskScore under G.
	RiskScoreAllDiscovered *float64 `json:"risk_score_all_discovered"`

	PrimaryFinding *PrimaryFinding `json:"primary_finding"`
	// DecidingFinding is set only when the finding that satisfied the
	// rule is not the primary - a lower-severity survivor ranked first.
	// Recorded rather than reconciled.
	DecidingFinding *PrimaryFinding `json:"deciding_finding,omitempty"`

	// RecommendedPaths is the union of recommended_validation.paths over
	// the findings that count under the rule plus the primary, for the
	// oracle-side hit-rate matcher. The engine does no matching.
	RecommendedPaths []string `json:"recommended_paths"`

	FindingsDiscovered int  `json:"findings_discovered"`
	Survivors          *int `json:"survivors,omitempty"`
	FindingsAtOrAbove  int  `json:"findings_at_or_above_threshold"`

	// QualifyingFindings are every finding at or above threshold, in rank
	// order. FindingsAtOrAbove is their count; this is the set itself,
	// which anything attempting to overturn the verdict needs, since one
	// surviving qualifier keeps the case RISKY.
	QualifyingFindings []PrimaryFinding `json:"qualifying_findings,omitempty"`
}

type h1Finding struct {
	ID                    string                 `json:"id"`
	File                  string                 `json:"file"`
	Line                  int                    `json:"line"`
	Mechanism             string                 `json:"mechanism"`
	Class                 string                 `json:"class"`
	Severity              string                 `json:"severity"`
	Confidence            float64                `json:"confidence"`
	RecommendedValidation *RecommendedValidation `json:"recommended_validation"`
}

type h1ReviewA struct {
	SchemaVersion string      `json:"schema_version"`
	Findings      []h1Finding `json:"findings"`
}

type h1Assessment struct {
	FindingID             string                 `json:"finding_id"`
	Disposition           string                 `json:"disposition"`
	Mechanism             string                 `json:"mechanism"`
	RecommendedValidation *RecommendedValidation `json:"recommended_validation"`
}

type h1ReviewB struct {
	SchemaVersion string         `json:"schema_version"`
	Assessments   []h1Assessment `json:"assessments"`
}

// ScoreCase applies the section 5 rule under armRule (ArmRuleT or
// ArmRuleG), which the caller derives from the protocol the run DECLARED,
// never from which files happen to exist: a T run whose stage B was
// skipped must fail here, not be scored as G. reviewBPath is required under
// T and forbidden under G; either disagreement is an error.
//
// Stated limitation: under T, severity is discovery's. NARROWED tightens
// the mechanism but cannot lower a finding out of the threshold, so
// narrowing is never a route to CLEAN - only REJECTED and INCONCLUSIVE
// are. That is the pre-registered rule as written, and it is the
// conservative direction for the treatment arm.
func ScoreCase(runID, caseID, armRule, reviewAPath, reviewBPath string) (Verdict, error) {
	switch armRule {
	case ArmRuleT:
		if reviewBPath == "" {
			return Verdict{}, fmt.Errorf("evaluation: arm rule T declared but no stage-B output was supplied; refusing to score a T run as G")
		}
	case ArmRuleG:
		if reviewBPath != "" {
			return Verdict{}, fmt.Errorf("evaluation: arm rule G declared but a stage-B output was supplied")
		}
	default:
		return Verdict{}, fmt.Errorf("evaluation: unknown arm rule %q", armRule)
	}

	var a h1ReviewA
	if err := readJSON(reviewAPath, &a); err != nil {
		return Verdict{}, err
	}
	if a.SchemaVersion != "h1-review-a/v1" {
		return Verdict{}, fmt.Errorf("evaluation: %s is %q, not an h1-review-a/v1 document", reviewAPath, a.SchemaVersion)
	}

	v := Verdict{
		SchemaVersion: VerdictSchemaVersion,
		RunID:         runID,
		CaseID:        caseID,
		ArmRule:       armRule,
		Rule: Rule{RuleVersion: RuleVersion, RuleTextSHA256: RuleTextSHA256(), SeverityThreshold: severityThreshold,
			SeverityWeights: SeverityWeights},
		Verdict:            VerdictClean,
		RecommendedPaths:   []string{},
		FindingsDiscovered: len(a.Findings),
	}
	byID := map[string]h1Finding{}
	for _, f := range a.Findings {
		byID[f.ID] = f
	}

	// candidates are the findings in rank order with the mechanism and
	// validation the rule reads; counts marks those satisfying the rule.
	var candidates []PrimaryFinding
	var counts []bool

	if armRule == ArmRuleG {
		v.Rule.RiskScoreOver = "all discovery findings"
		ct := confidenceThreshold
		v.Rule.ConfidenceThreshold = &ct
		for _, f := range a.Findings {
			candidates = append(candidates, primaryFrom(f, "", f.Mechanism, f.RecommendedValidation))
			counts = append(counts, atOrAbove(f.Severity) && f.Confidence >= confidenceThreshold)
		}
	} else {
		var b h1ReviewB
		if err := readJSON(reviewBPath, &b); err != nil {
			return Verdict{}, err
		}
		if b.SchemaVersion != "h1-review-b/v1" {
			return Verdict{}, fmt.Errorf("evaluation: %s is %q, not an h1-review-b/v1 document", reviewBPath, b.SchemaVersion)
		}
		v.Rule.RiskScoreOver = "stage-B survivors (CONFIRMED or NARROWED)"
		v.Rule.DispositionSet = append([]string(nil), countingDispositions...)
		survivors := 0
		for _, as := range b.Assessments {
			if !survives(as.Disposition) {
				continue
			}
			f, ok := byID[as.FindingID]
			if !ok {
				// The orchestrator's ancestry check makes this unreachable
				// on a sealed run; refuse rather than score a phantom.
				return Verdict{}, fmt.Errorf("evaluation: assessment %q has no discovery finding", as.FindingID)
			}
			survivors++
			candidates = append(candidates, primaryFrom(f, as.Disposition, as.Mechanism, as.RecommendedValidation))
			counts = append(counts, atOrAbove(f.Severity))
		}
		v.Survivors = &survivors
	}

	paths := map[string]bool{}
	for i, c := range candidates {
		if !counts[i] {
			continue
		}
		v.FindingsAtOrAbove++
		// Every finding that makes the case RISKY, not just the first.
		// A case is RISKY if ANY qualifying finding stands, so anything
		// asking "could this verdict be overturned" has to see all of
		// them: rejecting the primary alone leaves the case RISKY
		// whenever a second qualifier survives, which is the majority of
		// RISKY cases in practice.
		v.QualifyingFindings = append(v.QualifyingFindings, c)
		if v.Verdict == VerdictClean {
			v.Verdict = VerdictRisky
			deciding := c
			if i != 0 {
				v.DecidingFinding = &deciding
			}
		}
		addPaths(paths, c.RecommendedValidation)
	}
	if len(candidates) > 0 {
		p := candidates[0]
		v.PrimaryFinding = &p
		addPaths(paths, p.RecommendedValidation)
	}
	for p := range paths {
		v.RecommendedPaths = append(v.RecommendedPaths, p)
	}
	sort.Strings(v.RecommendedPaths)

	rs := 0.0
	for _, c := range candidates {
		rs = math.Max(rs, SeverityWeights[c.Severity]*c.Confidence)
	}
	all := 0.0
	for _, f := range a.Findings {
		all = math.Max(all, SeverityWeights[f.Severity]*f.Confidence)
	}
	v.RiskScore, v.RiskScoreAllDiscovered = &rs, &all
	return v, nil
}

func primaryFrom(f h1Finding, disposition, mechanism string, rv *RecommendedValidation) PrimaryFinding {
	return PrimaryFinding{
		ID: f.ID, File: f.File, Line: f.Line, Class: f.Class,
		Severity: f.Severity, Confidence: f.Confidence,
		Disposition: disposition, Mechanism: mechanism, RecommendedValidation: rv,
	}
}

func atOrAbove(severity string) bool {
	return severityRank[severity] >= severityRank[severityThreshold]
}

func survives(disposition string) bool {
	for _, d := range countingDispositions {
		if d == disposition {
			return true
		}
	}
	return false
}

func addPaths(into map[string]bool, rv *RecommendedValidation) {
	if rv == nil {
		return
	}
	for _, p := range rv.Paths {
		into[p] = true
	}
}
