// Package judge scores mechanism agreement: whether a reviewer anticipated a
// defect that maintainers later corrected.
//
// EVALUATOR-ONLY. This package reads the answer key. internal/oracle's
// isolation tests keep the review path away from both.
//
// IT MEASURES ANTICIPATION, NOT PRECISION. The headline figure is the share
// of findings that named a defect maintainers later fixed. It is NOT
// precision, and calling it precision would overstate it in a specific,
// knowable direction:
//
// A NONE verdict is three things this data cannot tell apart - a genuine
// false positive, a real defect nobody ever fixed, and a real defect whose
// fix the oracle never captured. Only the first is an error. So the true
// precision is GREATER THAN OR EQUAL TO the anticipation rate, by an unknown
// margin, and no number derived from this oracle can close that gap.
//
// The same asymmetry bounds what THIS ORACLE-BASED METRIC can say about
// staging: a rejected finding that matched a real fix is a PROVABLY wrong
// rejection, while a rejection that correctly killed a non-defect is
// unprovable for exactly the reason above. So mechanism agreement can
// demonstrate harm and cannot demonstrate benefit.
//
// That limit is the metric's, not the benchmark's. Correct suppression IS
// measurable on the cohort's designed negatives, where no fix is needed to
// know a surviving finding was a false positive - see specificity.go. The
// two metrics cover different populations and neither checks the other.
package judge

import "fmt"

// Agreement is one verdict's outcome. LOCALITY_ONLY exists as a distinct
// value rather than being folded into NONE because it is the diagnostic: a
// judge rationalising matches awards MECHANISM where it should award
// LOCALITY_ONLY, so a high MECHANISM count with near-zero LOCALITY_ONLY is
// visible in the totals without reading a single rationale.
type Agreement string

const (
	AgreementMechanism Agreement = "MECHANISM"
	AgreementLocality  Agreement = "LOCALITY_ONLY"
	AgreementNone      Agreement = "NONE"
	AgreementTooVague  Agreement = "TOO_VAGUE"
)

// Verdict is one finding's ruling, as emitted by judge call 2.
type Verdict struct {
	FindingID   string    `json:"finding_id"`
	Agreement   Agreement `json:"agreement"`
	FixID       string    `json:"fix_id,omitempty"`
	CaseAgainst string    `json:"case_against"`
	Rationale   string    `json:"rationale"`
}

// CaseVerdicts is one case's judged output plus the fixes it was judged
// against. FixIDs is required rather than derived from the verdicts: recall
// needs the fixes nobody matched, which by definition appear in no verdict.
type CaseVerdicts struct {
	CaseID   string    `json:"case_id"`
	FixIDs   []string  `json:"fix_ids"`
	Verdicts []Verdict `json:"verdicts"`
}

// ExcludedCase is a case that carries no corrective evidence, so its findings
// cannot be judged at all.
//
// These are EXCLUDED from every denominator, never scored as unmatched.
// Counting "no evidence" as "wrong" is the denominator error that
// disqualified the correspondence metric, where including unscoreable cases
// dropped the headline from 78.6% to 50.5% while saying nothing about review
// quality. They are reported so the scoreable base can never be mistaken for
// the cohort.
type ExcludedCase struct {
	CaseID   string `json:"case_id"`
	Findings int    `json:"findings"`
	Reason   string `json:"reason"`
}

// AnticipationReport deliberately exposes no way to obtain the anticipation
// rate on its own.
//
// Excluding TOO_VAGUE from the denominator is correct - an unfalsifiable
// finding is not a claim at all - but it opens the failure this project has
// now hit three times: a number that improves when the system gets worse. A
// reviewer writing vaguer findings has them removed from the denominator
// rather than counted as misses, so the rate RISES.
//
// The defence is that the vagueness rate travels with the headline figure and
// cannot be detached from it. Rates() returns both, so a caller cannot read
// one without receiving the other, and String() renders both. There is no
// single-value accessor and there must not be one.
type AnticipationReport struct {
	Mechanism int `json:"mechanism"`
	Locality  int `json:"locality_only"`
	None      int `json:"none"`
	TooVague  int `json:"too_vague"`
}

// Judged is every finding that produced a verdict, vague ones included.
func (p AnticipationReport) Judged() int {
	return p.Mechanism + p.Locality + p.None + p.TooVague
}

// Denominator is the anticipation base: TOO_VAGUE is excluded.
func (p AnticipationReport) Denominator() int {
	return p.Mechanism + p.Locality + p.None
}

// Rates returns the anticipation rate and the vagueness rate together, and
// only together. Two return values is the mechanism: obtaining the headline
// figure alone requires explicitly discarding the vagueness rate with `_`,
// which is visible to a reader in a way a silently-unused struct field is
// not.
func (p AnticipationReport) Rates() (anticipation, vagueness float64) {
	return ratio(p.Mechanism, p.Denominator()), ratio(p.TooVague, p.Judged())
}

// String renders the pair, and names the figure for what it is. "84.0%" on
// its own is not reportable, and neither is the word "precision".
func (p AnticipationReport) String() string {
	ant, vague := p.Rates()
	return fmt.Sprintf("%.1f%% anticipated (%d/%d) [true precision >= this], %d of %d findings TOO_VAGUE (%.1f%%)",
		ant, p.Mechanism, p.Denominator(), p.TooVague, p.Judged(), vague)
}

// RecallReport is over FIXES, not findings, so the finding count cannot move
// it. Only MECHANISM covers a fix: a LOCALITY_ONLY or TOO_VAGUE finding
// describes something else, or nothing, and cannot cover anything.
type RecallReport struct {
	Fixes   int `json:"fixes"`
	Covered int `json:"covered"`
}

func (r RecallReport) Rate() float64 { return ratio(r.Covered, r.Fixes) }

func (r RecallReport) String() string {
	return fmt.Sprintf("%.1f%% of fixes anticipated (%d/%d)", r.Rate(), r.Covered, r.Fixes)
}

// ArmReport is one arm's scores, or the pooled total. Anticipation and Recall
// are separate fields and are never combined into one rate: a single ratio
// over a finding set whose size the reviewer controls is gameable by changing
// that size, which is exactly what made correspondence rank the arm with the
// fewest findings highest.
type ArmReport struct {
	Anticipation AnticipationReport `json:"anticipation"`
	Recall       RecallReport       `json:"recall"`
}

func (a ArmReport) String() string { return a.Anticipation.String() + " | " + a.Recall.String() }

// Report is the whole scored result.
//
// ByArm is produced by rejoining opaque finding ids to their provenance
// AFTER judging. The judge saw one pooled list per case and could not tell
// the arms apart, so any difference between arms here is a difference in the
// findings, not in how they were judged.
type Report struct {
	Overall  ArmReport            `json:"overall"`
	ByArm    map[string]ArmReport `json:"by_arm"`
	Excluded []ExcludedCase       `json:"excluded_cases"`
	Cases    int                  `json:"scoreable_cases"`
}

// ExcludedFindings is how many findings sit in cases that could not be
// judged. Reported alongside the rates so the scoreable base is never
// mistaken for the cohort.
func (r Report) ExcludedFindings() int {
	n := 0
	for _, e := range r.Excluded {
		n += e.Findings
	}
	return n
}

func (r Report) String() string {
	return fmt.Sprintf("%s | %d scoreable cases, %d cases excluded holding %d findings",
		r.Overall, r.Cases, len(r.Excluded), r.ExcludedFindings())
}

// Score aggregates judged cases, holding excluded ones out of every
// denominator, and attributes each verdict back to the arm that produced it.
//
// index maps opaque finding id to provenance. A verdict whose id is not in
// the index is counted in the overall totals but attributed to no arm, and
// reported by UnattributedVerdicts - it means the judge invented or mangled
// an id, which must not silently vanish.
//
// A verdict naming a fix_id that was not supplied for that case counts toward
// the anticipation rate but never toward recall: it is a judge error, and crediting a fix
// that does not exist would inflate recall.
//
// Per-arm recall asks whether THAT arm's findings covered the fix, so a fix
// covered by two arms counts once for each. Overall recall asks whether any
// finding did.
func Score(cases []CaseVerdicts, index map[string]PooledFinding, excluded []ExcludedCase) (Report, int) {
	rep := Report{Excluded: excluded, Cases: len(cases), ByArm: map[string]ArmReport{}}
	unattributed := 0

	// Every arm present in the index gets a row even if it scored nothing,
	// so an arm that matched zero fixes is visibly zero rather than absent.
	armsSeen := map[string]bool{}
	for _, f := range index {
		armsSeen[f.Arm] = true
	}
	for arm := range armsSeen {
		rep.ByArm[arm] = ArmReport{}
	}

	for _, c := range cases {
		known := map[string]bool{}
		for _, f := range c.FixIDs {
			known[f] = true
		}
		coveredOverall := map[string]bool{}
		coveredByArm := map[string]map[string]bool{}

		for _, v := range c.Verdicts {
			prov, ok := index[v.FindingID]
			if !ok {
				unattributed++
			}
			arm := prov.Arm

			bump := func(p *AnticipationReport) {
				switch v.Agreement {
				case AgreementMechanism:
					p.Mechanism++
				case AgreementLocality:
					p.Locality++
				case AgreementNone:
					p.None++
				case AgreementTooVague:
					p.TooVague++
				}
			}
			bump(&rep.Overall.Anticipation)
			if ok {
				a := rep.ByArm[arm]
				bump(&a.Anticipation)
				rep.ByArm[arm] = a
			}

			if v.Agreement == AgreementMechanism && known[v.FixID] {
				coveredOverall[v.FixID] = true
				if ok {
					if coveredByArm[arm] == nil {
						coveredByArm[arm] = map[string]bool{}
					}
					coveredByArm[arm][v.FixID] = true
				}
			}
		}

		rep.Overall.Recall.Fixes += len(c.FixIDs)
		rep.Overall.Recall.Covered += len(coveredOverall)
		for arm := range armsSeen {
			a := rep.ByArm[arm]
			a.Recall.Fixes += len(c.FixIDs)
			a.Recall.Covered += len(coveredByArm[arm])
			rep.ByArm[arm] = a
		}
	}
	return rep, unattributed
}

// LocalityRatio is the primary health check on a real run, ahead of the
// anticipation figure. A judge that rationalises matches awards MECHANISM where
// LOCALITY_ONLY belongs, so a value near zero alongside a high match count is
// the signature of that failure - detectable without reading any rationale.
//
// Returns the share of location-related verdicts that were held to be
// locality only, and whether there were any such verdicts at all.
func (p AnticipationReport) LocalityRatio() (float64, bool) {
	total := p.Mechanism + p.Locality
	if total == 0 {
		return 0, false
	}
	return ratio(p.Locality, total), true
}

func ratio(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b) * 100
}
