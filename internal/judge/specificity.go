package judge

import (
	"fmt"
	"math"
	"sort"
)

// The pilot-v1 cohort is 7 positives and 3 DESIGNED NEGATIVES - PRs chosen
// because no corrective commit followed them in a ~2-year window (backlog
// 1.2, "the cohort is now fully covered - 7 positives and 3 negatives").
//
// The negatives were originally excluded from every judge denominator on the
// reasoning that "no evidence is not no defect". That reasoning is right for
// an accidental gap and wrong for a control class: it discarded the only
// cases that can measure correct suppression, and produced the claim that
// this benchmark "can demonstrate harm but never benefit". That claim was an
// artifact of the exclusion, not a property of the data.
//
// They stay excluded from the anticipation and recall denominators, which do
// require a fix to compare against. They belong in specificity, which does
// not.

// NegativeCases are the cohort's designed negatives.
var NegativeCases = map[string]bool{
	"case-3a74a3a25bda9041": true,
	"case-9ba8fca704a95e70": true,
	"case-c96a113c3301c4fa": true,
}

// ClassifiedFinding is one finding placed in its case class, for specificity.
// It carries no judge verdict: the judge never saw the negative cases, so the
// negative class has no mechanism-agreement data at all.
type ClassifiedFinding struct {
	Arm         string
	CaseID      string
	Negative    bool
	Disposition string // final (stage 3) disposition
	NewByStage3 bool   // discovered by stage 3, so it has no b_disposition
}

// Survived reports whether the pipeline let a finding through. CONFIRMED and
// NARROWED survive; REJECTED and INCONCLUSIVE do not. A stage-3 discovery has
// no disposition at all and is treated as surviving, because the pipeline
// emitted it.
func (c ClassifiedFinding) Survived() bool {
	switch c.Disposition {
	case "CONFIRMED", "NARROWED":
		return true
	case "REJECTED", "INCONCLUSIVE":
		return false
	default:
		return true
	}
}

// SpecificityReport is survival on clean PRs against survival on buggy ones.
//
// A finding that survives on a designed negative is a false positive the
// pipeline failed to suppress. Unlike the anticipation rate, this needs no
// oracle and no fix - which is why it can measure benefit where the
// mechanism-agreement metric structurally cannot.
type SpecificityReport struct {
	NegFindings int `json:"negative_findings"`
	NegSurvived int `json:"negative_survived"`
	PosFindings int `json:"positive_findings"`
	PosSurvived int `json:"positive_survived"`

	NegCases int `json:"negative_cases"`
	PosCases int `json:"positive_cases"`

	// PRs is the per-PR breakdown, and it is what makes the primary test
	// possible. Findings are NOT independent samples: they cluster within
	// PRs, and with only 3 clean PRs a single PR can carry an entire
	// result. The independent unit is the PR.
	PRs []PRStats `json:"prs"`
}

// PRStats is one PR's suppression, the unit the primary test permutes.
type PRStats struct {
	CaseID     string `json:"case_id"`
	Negative   bool   `json:"negative"`
	Findings   int    `json:"findings"`
	Suppressed int    `json:"suppressed"`
}

// Rate is this PR's suppression rate.
func (p PRStats) Rate() float64 { return ratio(p.Suppressed, p.Findings) / 100 }

// SurvivalRates returns both class rates together and only together. A
// specificity figure quoted without the rate it is being contrasted against
// says nothing: 61% survival on clean PRs is alarming or unremarkable
// depending entirely on what the buggy PRs did.
func (s SpecificityReport) SurvivalRates() (negative, positive float64) {
	return ratio(s.NegSurvived, s.NegFindings), ratio(s.PosSurvived, s.PosFindings)
}

// SuppressionRates is the complement, in the direction people will quote.
func (s SpecificityReport) SuppressionRates() (negative, positive float64) {
	return ratio(s.NegFindings-s.NegSurvived, s.NegFindings),
		ratio(s.PosFindings-s.PosSurvived, s.PosFindings)
}

// PerCaseSurvival is surviving findings per PR, which is the figure that
// answers "how much noise does a reviewer put in front of someone reading a
// clean patch".
func (s SpecificityReport) PerCaseSurvival() (negative, positive float64) {
	var n, p float64
	if s.NegCases > 0 {
		n = float64(s.NegSurvived) / float64(s.NegCases)
	}
	if s.PosCases > 0 {
		p = float64(s.PosSurvived) / float64(s.PosCases)
	}
	return n, p
}

// PValues returns the PR-level exact permutation p FIRST and the
// finding-level Fisher p second, and only together.
//
// There is deliberately no way to obtain the Fisher figure alone. It treats
// findings as independent samples when they cluster within PRs, which
// inflates significance - on the pilot cohort it reported p = 0.048 where the
// PR-level test reports 0.19. That is the same defect class as a precision
// figure obtainable without its vagueness rate, and it is closed the same
// way: the honest number travels with the misleading one.
//
// prPermutation is two-sided. Use PermutationOneSided for the directional
// question.
func (s SpecificityReport) PValues() (prPermutation, findingFisher float64) {
	return s.permutation(true), fisherExact(
		s.NegFindings-s.NegSurvived, s.NegSurvived,
		s.PosFindings-s.PosSurvived, s.PosSurvived)
}

// PermutationOneSided asks only whether clean PRs are suppressed MORE.
func (s SpecificityReport) PermutationOneSided() float64 { return s.permutation(false) }

// Gap is the observed statistic: mean per-PR suppression rate on clean PRs
// minus the same on buggy ones. Mean-of-PR-rates rather than a pooled rate,
// so that a PR with 23 findings does not outvote one with 7 - the whole point
// of moving to the PR as the unit.
func (s SpecificityReport) Gap() float64 {
	return gapOf(s.PRs, negMask(s.PRs))
}

// permutation enumerates every way of labelling the same number of PRs as
// clean. C(10,3) = 120 on this cohort, so it is exact rather than sampled.
func (s SpecificityReport) permutation(twoSided bool) float64 {
	n := len(s.PRs)
	k := 0
	for _, p := range s.PRs {
		if p.Negative {
			k++
		}
	}
	if n == 0 || k == 0 || k == n {
		return 1
	}
	obs := s.Gap()
	total, extreme := 0, 0
	var rec func(start int, chosen []int)
	rec = func(start int, chosen []int) {
		if len(chosen) == k {
			mask := make([]bool, n)
			for _, i := range chosen {
				mask[i] = true
			}
			g := gapOf(s.PRs, mask)
			total++
			if (twoSided && math.Abs(g) >= math.Abs(obs)-1e-12) || (!twoSided && g >= obs-1e-12) {
				extreme++
			}
			return
		}
		for i := start; i < n; i++ {
			rec(i+1, append(chosen, i))
		}
	}
	rec(0, nil)
	if total == 0 {
		return 1
	}
	return float64(extreme) / float64(total)
}

func negMask(prs []PRStats) []bool {
	m := make([]bool, len(prs))
	for i, p := range prs {
		m[i] = p.Negative
	}
	return m
}

func gapOf(prs []PRStats, clean []bool) float64 {
	var cs, bs float64
	var cn, bn int
	for i, p := range prs {
		if clean[i] {
			cs += p.Rate()
			cn++
		} else {
			bs += p.Rate()
			bn++
		}
	}
	if cn == 0 || bn == 0 {
		return 0
	}
	return cs/float64(cn) - bs/float64(bn)
}

func (s SpecificityReport) String() string {
	ns, ps := s.SurvivalRates()
	nsup, psup := s.SuppressionRates()
	perm, fish := s.PValues()
	return fmt.Sprintf(
		"clean PRs: %d/%d survived (%.0f%%, %.0f%% suppressed) | buggy PRs: %d/%d survived (%.0f%%, %.0f%% suppressed)\n"+
			"          gap %+.3f | PR-level permutation p = %.3f (PRIMARY, n=%d PRs) | finding-level Fisher p = %.4f (INFLATED by clustering, do not quote alone)",
		s.NegSurvived, s.NegFindings, ns, nsup,
		s.PosSurvived, s.PosFindings, ps, psup,
		s.Gap(), perm, len(s.PRs), fish)
}

// Specificity builds the report. includeStage3Discoveries decides whether
// findings stage 3 invented (which carry no b_disposition) are counted.
//
// THE CHOICE MATTERS AND MUST BE REPORTED BOTH WAYS. On the pilot cohort it
// is two findings, and it moves the headline p-value across 0.05 - 0.0483
// excluding, 0.0515 including. Reporting only the favourable one would be the
// fifth metric on this project to be chosen by which answer it gave.
// universe is caseID -> isNegative for every case in the cohort. It is
// required rather than derived from the findings, because an arm that
// reviewed a clean PR and produced NOTHING is the best possible outcome and
// must still count in that arm's denominator. Deriving case counts from the
// findings present silently drops those PRs and flatters the arm that stayed
// quiet - the same denominator error, in miniature, as excluding the
// negatives in the first place.
func Specificity(findings []ClassifiedFinding, includeStage3Discoveries bool, universe map[string]bool) SpecificityReport {
	var r SpecificityReport
	negCases, posCases := map[string]bool{}, map[string]bool{}
	for c, neg := range universe {
		if neg {
			negCases[c] = true
		} else {
			posCases[c] = true
		}
	}
	for _, f := range findings {
		if f.NewByStage3 && !includeStage3Discoveries {
			continue
		}
		if f.Negative {
			r.NegFindings++
			if f.Survived() {
				r.NegSurvived++
			}
		} else {
			r.PosFindings++
			if f.Survived() {
				r.PosSurvived++
			}
		}
	}
	r.NegCases, r.PosCases = len(negCases), len(posCases)

	agg := map[string]*PRStats{}
	var order []string
	for c, neg := range universe {
		agg[c] = &PRStats{CaseID: c, Negative: neg}
		order = append(order, c)
	}
	for _, f := range findings {
		if f.NewByStage3 && !includeStage3Discoveries {
			continue
		}
		p, ok := agg[f.CaseID]
		if !ok {
			p = &PRStats{CaseID: f.CaseID, Negative: f.Negative}
			agg[f.CaseID] = p
			order = append(order, f.CaseID)
		}
		p.Findings++
		if !f.Survived() {
			p.Suppressed++
		}
	}
	sort.Strings(order)
	for _, c := range order {
		r.PRs = append(r.PRs, *agg[c])
	}
	return r
}

// SpecificityByArm returns one report per arm, sorted by arm name.
func SpecificityByArm(findings []ClassifiedFinding, includeStage3Discoveries bool, universe map[string]bool) map[string]SpecificityReport {
	byArm := map[string][]ClassifiedFinding{}
	for _, f := range findings {
		byArm[f.Arm] = append(byArm[f.Arm], f)
	}
	out := map[string]SpecificityReport{}
	for arm, fs := range byArm {
		out[arm] = Specificity(fs, includeStage3Discoveries, universe)
	}
	return out
}

// ArmNames is the sorted arm list of a per-arm map.
func ArmNames(m map[string]SpecificityReport) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// fisherExact is the two-tailed exact test on a 2x2 table.
func fisherExact(a, b, c, d int) float64 {
	n := a + b + c + d
	if n == 0 {
		return 1
	}
	p := func(a, b, c, d int) float64 {
		return math.Exp(lchoose(a+b, a) + lchoose(c+d, c) - lchoose(n, a+c))
	}
	obs := p(a, b, c, d)
	total := 0.0
	rowA, colA := a+b, a+c
	for i := 0; i <= min(rowA, colA); i++ {
		j := rowA - i
		k := colA - i
		l := d - (a - i)
		if j < 0 || k < 0 || l < 0 {
			continue
		}
		if q := p(i, j, k, l); q <= obs*(1+1e-9) {
			total += q
		}
	}
	if total > 1 {
		return 1
	}
	return total
}

func lchoose(n, k int) float64 {
	if k < 0 || k > n {
		return math.Inf(-1)
	}
	lg, _ := math.Lgamma(float64(n) + 1)
	lk, _ := math.Lgamma(float64(k) + 1)
	lnk, _ := math.Lgamma(float64(n-k) + 1)
	return lg - lk - lnk
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
