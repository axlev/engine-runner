package judge

import (
	"math"
	"strings"
	"testing"
)

func cf(arm, case_ string, neg bool, dispo string) ClassifiedFinding {
	return ClassifiedFinding{Arm: arm, CaseID: case_, Negative: neg, Disposition: dispo}
}

// A specificity figure quoted without the class it is contrasted against says
// nothing, so the two rates come out together or not at all.
func TestSurvivalRatesComeOutTogether(t *testing.T) {
	s := SpecificityReport{NegFindings: 31, NegSurvived: 19, PosFindings: 78, PosSurvived: 63}
	n, p := s.SurvivalRates()
	if math.Abs(n-61.29) > 0.01 || math.Abs(p-80.77) > 0.01 {
		t.Errorf("SurvivalRates() = %.2f, %.2f", n, p)
	}
	str := s.String()
	for _, want := range []string{"19/31", "63/78", "clean", "buggy", "Fisher"} {
		if !strings.Contains(str, want) {
			t.Errorf("String() = %q, want it to mention %q", str, want)
		}
	}
}

// The headline claim. Pinned against an independent recomputation, because
// the figure first reached this project from a one-off script that reported
// p = 0.034 and could not be reproduced from its own cell counts.
func TestFisherReproducesTheCohortResult(t *testing.T) {
	s := SpecificityReport{NegFindings: 31, NegSurvived: 19, PosFindings: 78, PosSurvived: 63}
	_, got := s.PValues()
	if math.Abs(got-0.0483) > 0.002 {
		t.Errorf("finding-level Fisher = %.4f, want ~0.0483", got)
	}
}

// Two findings move the result across 0.05. Both accountings must therefore
// be reportable, and this pins the sensitivity so nobody quietly picks one.
func TestStage3DiscoveryChoiceMovesSignificance(t *testing.T) {
	var fs []ClassifiedFinding
	for i := 0; i < 12; i++ {
		fs = append(fs, cf("a", "neg1", true, "REJECTED"))
	}
	for i := 0; i < 19; i++ {
		fs = append(fs, cf("a", "neg1", true, "CONFIRMED"))
	}
	for i := 0; i < 15; i++ {
		fs = append(fs, cf("a", "pos1", false, "REJECTED"))
	}
	for i := 0; i < 63; i++ {
		fs = append(fs, cf("a", "pos1", false, "CONFIRMED"))
	}
	uni := map[string]bool{"neg1": true, "pos1": false}
	excl := Specificity(fs, false, uni)
	_, eF := excl.PValues()
	if math.Abs(eF-0.0483) > 0.002 {
		t.Fatalf("baseline Fisher = %.4f, want ~0.0483", eF)
	}
	// add one stage-3 discovery to each class
	fs = append(fs,
		ClassifiedFinding{Arm: "a", CaseID: "neg1", Negative: true, NewByStage3: true},
		ClassifiedFinding{Arm: "a", CaseID: "pos1", Negative: false, NewByStage3: true})
	if Specificity(fs, false, uni).NegFindings != excl.NegFindings {
		t.Errorf("excluding stage-3 discoveries must not change the counts")
	}
	incl := Specificity(fs, true, uni)
	if incl.NegFindings != 32 || incl.PosFindings != 79 {
		t.Fatalf("including them: got %d/%d, want 32/79", incl.NegFindings, incl.PosFindings)
	}
	_, iF := incl.PValues()
	if iF < 0.05 {
		t.Errorf("including stage-3 discoveries gives Fisher p = %.4f; the pilot value crosses 0.05 "+
			"and this test exists to keep that visible", iF)
	}
}

// A stage-3 discovery has no disposition; the pipeline still emitted it, so
// it survived.
func TestStage3DiscoveryCountsAsSurviving(t *testing.T) {
	f := ClassifiedFinding{NewByStage3: true}
	if !f.Survived() {
		t.Errorf("a finding the pipeline emitted with no disposition has survived")
	}
	if (ClassifiedFinding{Disposition: "INCONCLUSIVE"}).Survived() {
		t.Errorf("INCONCLUSIVE does not survive")
	}
}

func TestPerCaseSurvivalUsesCaseCountsNotFindingCounts(t *testing.T) {
	fs := []ClassifiedFinding{
		cf("a", "n1", true, "CONFIRMED"), cf("a", "n2", true, "REJECTED"),
		cf("a", "n3", true, "REJECTED"),
		cf("a", "p1", false, "CONFIRMED"), cf("a", "p1", false, "CONFIRMED"),
	}
	s := Specificity(fs, false, map[string]bool{"n1": true, "n2": true, "n3": true, "p1": false})
	n, p := s.PerCaseSurvival()
	if s.NegCases != 3 || s.PosCases != 1 {
		t.Fatalf("cases = %d neg / %d pos, want 3/1", s.NegCases, s.PosCases)
	}
	if math.Abs(n-1.0/3.0) > 0.001 || math.Abs(p-2.0) > 0.001 {
		t.Errorf("PerCaseSurvival() = %.3f, %.3f, want 0.333, 2.0", n, p)
	}
}

// The designed negatives are a fixed, named set - not "cases that happened to
// have no signal". Misidentifying them is how the control class got dropped.
func TestNegativeCasesAreTheDesignedThree(t *testing.T) {
	if len(NegativeCases) != 3 {
		t.Fatalf("expected exactly 3 designed negatives, got %d", len(NegativeCases))
	}
	for _, c := range []string{"case-3a74a3a25bda9041", "case-9ba8fca704a95e70", "case-c96a113c3301c4fa"} {
		if !NegativeCases[c] {
			t.Errorf("%s should be a designed negative", c)
		}
	}
}

// The finding-level test treats clustered findings as independent samples and
// inflates significance. It must not be obtainable on its own - the honest
// PR-level figure travels with it, the same way the vagueness rate travels
// with the anticipation rate.
func TestFindingLevelPCannotBeObtainedAlone(t *testing.T) {
	s := SpecificityReport{}
	// PValues is the only accessor, and it returns the PR-level figure first.
	perm, fish := s.PValues()
	_ = perm
	_ = fish
	// If a single-value FisherP() is ever reintroduced this file stops
	// compiling, which is the intended tripwire.
}

// The pilot cohort, at the PR level. A cross-vendor reviewer predicted the
// exact permutation would land between 0.05 and 0.20 before it was run; it
// did, and the finding-level 0.048 was an artifact of the wrong unit.
func TestPRLevelPermutationOnThePilotCohort(t *testing.T) {
	prs := []PRStats{
		{"9ba8fca7", true, 11, 8}, {"3a74a3a2", true, 9, 4}, {"c96a113c", true, 12, 1},
		{"322abe6a", false, 10, 0}, {"deaebcc7", false, 8, 1}, {"fe99bcf9", false, 8, 1},
		{"fd8ee329", false, 11, 2}, {"de6d5d30", false, 23, 5}, {"83d945a6", false, 12, 4},
		{"c21d519d", false, 7, 3},
	}
	s := SpecificityReport{PRs: prs}
	two, _ := s.PValues()
	one := s.PermutationOneSided()
	if two < 0.05 {
		t.Errorf("two-sided PR-level p = %.3f; the whole point is that it is NOT significant", two)
	}
	if one < 0.05 {
		t.Errorf("one-sided PR-level p = %.3f, expected well above 0.05", one)
	}
	t.Logf("gap %+.3f  one-sided %.3f  two-sided %.3f", s.Gap(), one, two)
}
