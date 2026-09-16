package h1score

import (
	"strings"
	"testing"
)

func renderFixture(t *testing.T, mutate func(*Report, *[]Label)) string {
	t.Helper()
	l := cohort(2)
	T := verdicts(map[string]bool{"posa": true, "nega": false, "posb": true, "negb": true})
	G := verdicts(map[string]bool{"posa": true, "nega": true, "posb": false, "negb": false})
	r, err := Score(Options{Labels: l, ArmIDs: map[string]string{"T": "h1-t-v1", "G": "h1-g-v1"},
		Verdicts: map[string]map[string]CaseVerdict{"T": T, "G": G}, Threshold: 15})
	if err != nil {
		t.Fatal(err)
	}
	labels := l.Cases
	if mutate != nil {
		mutate(&r, &labels)
	}
	return Render(r, labels, RenderMeta{SourceFile: "h1-score.json", EngineCommit: "abc1234", Draft: true})
}

// The document must never present an uncomputed figure as a number, and
// must say plainly that H1 is unevaluated until the judge pass runs.
func TestRenderMarksAbsentFiguresAndUnevaluatedCriteria(t *testing.T) {
	doc := renderFixture(t, nil)
	for _, want := range []string{
		"# H1 results",
		"DRAFT SCAFFOLD",
		"## Pre-registered criteria",
		"absent (judge pass has not run)",
		"H1 has not been evaluated",
		"≥ 60%",
		"≥ 50%",
		"≥ +15 points",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("document lacks %q", want)
		}
	}
	// No bare zero standing in for an absent figure.
	if strings.Contains(doc, "| 0.000 (0/0) |") {
		t.Error("an undefined rate rendered as 0.000 rather than absent")
	}
}

// A stratum whose covariate is absent must say so, not collapse to one
// bucket that reads as "no variation".
func TestRenderSaysNotRecordedRatherThanInventingStrata(t *testing.T) {
	doc := renderFixture(t, nil)
	for _, section := range []string{
		"Fallback pairs (A9(vi))",
		"Source window (A11(iii))",
		"Fix before/after model cutoff (A11(vi))",
	} {
		if !strings.Contains(doc, section) {
			t.Fatalf("missing section %q", section)
		}
	}
	if !strings.Contains(doc, "**Not recorded.**") {
		t.Error("absent covariates must be reported as not recorded")
	}
	if !strings.Contains(doc, "stronger\nclaim than \"not recorded\"") && !strings.Contains(doc, "not recorded\"") {
		t.Error("the document must explain why one bucket would be misleading")
	}

	// With the covariates present, real buckets appear.
	withCovariates := renderFixture(t, func(_ *Report, labels *[]Label) {
		yes, no := true, false
		for i := range *labels {
			(*labels)[i].SourceWindow = "2026H1"
			(*labels)[i].Subsystem = "bgpd"
			if i%2 == 0 {
				(*labels)[i].MatchKeyUsed = []string{"subsystem"}
				(*labels)[i].FixBeforeCutoff = &yes
			} else {
				(*labels)[i].MatchKeyUsed = []string{"category", "subsystem"}
				(*labels)[i].FixBeforeCutoff = &no
			}
		}
	})
	for _, want := range []string{"fallback", "fully matched", "2026H1", "before cutoff", "after cutoff"} {
		if !strings.Contains(withCovariates, want) {
			t.Errorf("with covariates present, expected bucket %q", want)
		}
	}
}

// Registered limitations are static: a good result must not be able to ship
// without them.
func TestRenderAlwaysCarriesRegisteredLimitations(t *testing.T) {
	doc := renderFixture(t, nil)
	for _, want := range []string{
		"Staging confound",
		"Arm H is a floor, not a competitor",
		"Fallback pairs (A9(vi))",
		"In-family judge",
		"estimate of equivalent API cost",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("limitations section lacks %q", want)
		}
	}
}

func TestRenderShowsProbeExclusionsAndProvisionalBanner(t *testing.T) {
	l := cohort(2)
	T := verdicts(map[string]bool{"posa": true, "nega": false, "posb": true, "negb": false})
	r, err := Score(Options{Labels: l, ArmIDs: map[string]string{"T": "t", "G": "g"},
		Verdicts:    map[string]map[string]CaseVerdict{"T": T, "G": T},
		ProbeVoided: []string{"posa"}, ProbeUnresolved: []string{"posb"}, Threshold: 15})
	if err != nil {
		t.Fatal(err)
	}
	doc := Render(r, l.Cases, RenderMeta{SourceFile: "x.json"})
	if !strings.Contains(doc, "PROVISIONAL — not a result") {
		t.Error("an unresolved probe must banner the document")
	}
	if !strings.Contains(doc, "posa") || !strings.Contains(doc, "excluded from every arm") {
		t.Error("probe-voided cases must be named with the reason")
	}
	if !strings.Contains(doc, "not an acquittal") {
		t.Error("the unresolved explanation must be present")
	}
}

func TestRenderRecordsProvenance(t *testing.T) {
	l := cohort(1)
	T := verdicts(map[string]bool{"posa": true, "nega": false})
	r, _ := Score(Options{Labels: l, ArmIDs: map[string]string{"T": "t", "G": "g"},
		Verdicts: map[string]map[string]CaseVerdict{"T": T, "G": T}, Threshold: 15})
	doc := Render(r, l.Cases, RenderMeta{
		SourceFile:     "x.json",
		EngineCommit:   "deadbee",
		CohortRecords:  "b2279b9c",
		ProtocolHashes: map[string]string{"h1-t-v1": "25c90be0"},
		PromptHashes:   map[string]string{"h1-t-v1/reasoner-1": "02d6a84b"},
	})
	for _, want := range []string{"deadbee", "b2279b9c", "25c90be0", "02d6a84b", "Rules as applied"} {
		if !strings.Contains(doc, want) {
			t.Errorf("provenance lacks %q", want)
		}
	}
}
