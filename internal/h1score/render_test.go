package h1score

import (
	"encoding/json"
	"github.com/axlev/engine-runner/internal/h1judge"
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

// The judge's divergence from the pilot must be in the document, not
// discovered later by someone comparing the two numbers.
func TestRenderRecordsTheJudgeDivergenceFromThePilot(t *testing.T) {
	doc := renderFixture(t, nil)
	for _, want := range []string{
		"one finding per arm",
		"differs from the pilot's",
		"NOT comparable with the pilot's",
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("limitations lack %q", want)
		}
	}
}

// The results document is generated, so a fact recorded in it by hand is
// lost on the next render. Notes passed at render time must survive.
func TestProvenanceNotesAreRendered(t *testing.T) {
	const note = "probes 1-19 delivered the prompt via argv; probe 20 and all arm runs via stdin; identical bytes"
	out := Render(Report{SchemaVersion: SchemaVersion}, nil, RenderMeta{
		ProvenanceNotes: []string{note},
	})
	if !strings.Contains(out, note) {
		t.Errorf("provenance note missing from the rendered document")
	}
}

func f64(v float64) *float64 { return &v }

// Criterion 1 must read the judge summary rather than announcing itself
// absent. The round-trip case is the one that matters: -render reads a
// sealed h1-score.json, so ReasonMatch arrives as a generic map and a type
// assertion would silently miss it.
func TestCriterionOneReadsReasonMatchInBothForms(t *testing.T) {
	summary := h1judge.Summary{
		SchemaVersion: "h1-judge/v1",
		JudgeModel:    "claude-opus-5",
		InFamily:      true,
		Arms: map[string]h1judge.ArmSummary{
			"T": {Judged: 10, Mechanism: 7, LocalityOnly: 2, None: 1,
				ReasonMatch: f64(0.70), LocalityRate: f64(0.20)},
			"G": {Judged: 8, Mechanism: 3, LocalityOnly: 4, None: 1,
				ReasonMatch: f64(0.375), LocalityRate: f64(0.50)},
		},
	}
	// As Score sets it, and as -render reads it back off disk.
	var roundTripped any
	b, err := json.Marshal(summary)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &roundTripped); err != nil {
		t.Fatal(err)
	}

	for name, value := range map[string]any{"typed": summary, "round-tripped": roundTripped} {
		out := Render(Report{SchemaVersion: SchemaVersion, ReasonMatch: value}, nil, RenderMeta{})
		if strings.Contains(out, "absent (judge pass has not run)") {
			t.Errorf("%s: criterion 1 still reports the judge pass as not run", name)
		}
		if !strings.Contains(out, "0.700 (7/10)") {
			t.Errorf("%s: criterion 1 does not carry the treatment rate", name)
		}
		if !strings.Contains(out, "**meets**") {
			t.Errorf("%s: 0.70 is over the 0.60 threshold and must meet the criterion", name)
		}
		// LOCALITY_ONLY must be reported beside the match rate.
		if !strings.Contains(out, "0.200") || !strings.Contains(out, "0.500") {
			t.Errorf("%s: LOCALITY_ONLY rates missing from the reason-match section", name)
		}
		// Both arms are reported, not just the treatment.
		if !strings.Contains(out, "0.375") {
			t.Errorf("%s: arm G reason match missing", name)
		}
	}
}

func TestCriterionOneFailsBelowThreshold(t *testing.T) {
	below := h1judge.Summary{Arms: map[string]h1judge.ArmSummary{
		"T": {Judged: 100, Mechanism: 59, ReasonMatch: f64(0.59)},
	}}
	out := Render(Report{SchemaVersion: SchemaVersion, ReasonMatch: below}, nil, RenderMeta{})
	if !strings.Contains(out, "0.590 (59/100)") {
		t.Error("observed rate missing")
	}
	if !strings.Contains(out, "does not meet") {
		t.Error("0.59 is under the 0.60 threshold and must not meet the criterion")
	}
}

// A judge pass that judged nothing for the treatment arm must say so, not
// render as a zero rate that reads like a measured result.
func TestCriterionOneAbsentWhenTreatmentWasNeverJudged(t *testing.T) {
	none := h1judge.Summary{Arms: map[string]h1judge.ArmSummary{
		"G": {Judged: 3, Mechanism: 1, ReasonMatch: f64(0.333)},
	}}
	out := Render(Report{SchemaVersion: SchemaVersion, ReasonMatch: none}, nil, RenderMeta{})
	if !strings.Contains(out, "absent (no judged treatment findings)") {
		t.Error("a summary with no treatment arm must render absent, not a rate")
	}
}
