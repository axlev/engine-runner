package h1judge

import (
	"strings"
	"testing"

	"github.com/axlev/engine-runner/internal/judge"
)

func cand(caseID, arm, mech string) Candidate {
	return Candidate{CaseID: caseID, Arm: arm, RunID: arm + "-" + caseID,
		FindingID: "f1", Mechanism: mech, File: "bgpd/bgp_evpn.c", Line: 42}
}

// Section 6 judges RISKY TRUE POSITIVES only: not negatives (no fix to
// match), not CLEAN calls (nothing claimed).
func TestEligibleCandidatesAreRiskyTruePositivesOnly(t *testing.T) {
	cases := []string{"pos1", "pos2", "neg1"}
	positive := map[string]bool{"pos1": true, "pos2": true}
	risky := map[string]bool{"T|pos1": true, "G|pos1": true, "T|pos2": false, "T|neg1": true, "G|neg1": true}

	got := EligibleCandidates(cases, []string{"T", "G"},
		func(arm, id string) (Candidate, bool, bool) {
			return cand(id, arm, "mechanism text"), risky[arm+"|"+id], true
		},
		func(id string) bool { return positive[id] })

	if len(got) != 2 {
		t.Fatalf("want 2 candidates (both arms on pos1), got %d: %+v", len(got), got)
	}
	for _, c := range got {
		if c.CaseID != "pos1" {
			t.Errorf("a negative or a CLEAN call was sent to the judge: %+v", c)
		}
	}
}

// A RISKY verdict with an empty mechanism is a malformed result, not a
// vague finding: it must go missing, not be judged TOO_VAGUE.
func TestEmptyMechanismIsNotJudged(t *testing.T) {
	got := EligibleCandidates([]string{"pos1"}, []string{"T"},
		func(arm, id string) (Candidate, bool, bool) {
			c := cand(id, arm, "   ")
			return c, true, true
		},
		func(string) bool { return true })
	if len(got) != 0 {
		t.Errorf("an empty mechanism must not be judged: %+v", got)
	}
}

// The judge must not be able to tell which arm produced a finding.
func TestPoolHidesTheArm(t *testing.T) {
	p := PoolFor("pos1", []Candidate{
		cand("pos1", "T", "T's mechanism, which is distinctive"),
		cand("pos1", "G", "G's mechanism, also distinctive"),
	})
	if err := p.CheckOpaque(); err != nil {
		t.Fatalf("pool leaks provenance: %v", err)
	}
	for _, v := range p.View() {
		blob := v.ID + v.Title + v.Body + strings.Join(v.Files, ",")
		for _, leak := range []string{"T-pos1", "G-pos1", `"arm"`} {
			if strings.Contains(blob, leak) {
				t.Errorf("judge view contains %q: %+v", leak, v)
			}
		}
	}
	// Both arms present, distinguishable only by content.
	if len(p.View()) != 2 {
		t.Fatalf("want both arms pooled, got %d", len(p.View()))
	}
	idx := p.Index()
	arms := map[string]bool{}
	for _, f := range idx {
		arms[f.Arm] = true
	}
	if !arms["T"] || !arms["G"] {
		t.Errorf("index lost an arm: %+v", arms)
	}
}

func TestSummariseRatesAndDenominator(t *testing.T) {
	reports := []Report{
		{CaseID: "a", Verdicts: map[string]ArmVerdict{
			"T": {Arm: "T", Agreement: string(judge.AgreementMechanism)},
			"G": {Arm: "G", Agreement: string(judge.AgreementLocality)},
		}},
		{CaseID: "b", Verdicts: map[string]ArmVerdict{
			"T": {Arm: "T", Agreement: string(judge.AgreementMechanism)},
			"G": {Arm: "G", Agreement: string(judge.AgreementNone)},
		}},
		{CaseID: "c", Verdicts: map[string]ArmVerdict{
			"T": {Arm: "T", Agreement: string(judge.AgreementTooVague)},
		}},
	}
	s := Summarise(reports, "claude-opus-5", true)
	tArm := s.Arms["T"]
	if tArm.Judged != 3 || tArm.Mechanism != 2 || tArm.TooVague != 1 {
		t.Errorf("T: %+v", tArm)
	}
	if tArm.ReasonMatch == nil || *tArm.ReasonMatch < 0.666 || *tArm.ReasonMatch > 0.667 {
		t.Errorf("T reason match should be 2/3: %+v", tArm.ReasonMatch)
	}
	gArm := s.Arms["G"]
	if gArm.Judged != 2 || gArm.LocalityOnly != 1 || gArm.None != 1 {
		t.Errorf("G: %+v", gArm)
	}
	if gArm.LocalityRate == nil || *gArm.LocalityRate != 0.5 {
		t.Errorf("G locality rate should be 1/2: %+v", gArm.LocalityRate)
	}
	if !s.InFamily || s.JudgeModel != "claude-opus-5" {
		t.Error("the in-family judge must be stated on the figure")
	}
	if !strings.Contains(s.Note, "not cohort cases") {
		t.Error("the denominator must be stated")
	}
}

func TestSummariseReportsAbsentArmRatherThanZero(t *testing.T) {
	s := Summarise([]Report{{CaseID: "a", Verdicts: map[string]ArmVerdict{
		"T": {Arm: "T", Agreement: string(judge.AgreementMechanism)},
	}}}, "claude-opus-5", true)
	if _, ok := s.Arms["G"]; ok {
		t.Error("an arm with nothing judged must be absent, not a zero row")
	}
	// An arm present but with nothing judged says so.
	empty := Summarise([]Report{{CaseID: "a", Verdicts: map[string]ArmVerdict{}}}, "m", true)
	if len(empty.Arms) != 0 {
		t.Errorf("no verdicts means no arms: %+v", empty.Arms)
	}
}

// The judge must be given the evaluator-verified fixing commits, not the
// correlator's wider attributed set: matching any of thirty attributed
// commits is not anticipation.
func TestRefuseKeyedFixes(t *testing.T) {
	for _, bad := range []string{
		"/x/frr-h1-cohort-evaluator-inputs/fixes-keyed",
		"/x/fixes-keyed/",
		"fixes-keyed",
	} {
		if err := RefuseKeyedFixes(bad); err == nil {
			t.Errorf("must refuse %q", bad)
		} else if !strings.Contains(err.Error(), "reference set") {
			t.Errorf("%q: unhelpful error %v", bad, err)
		}
	}
	for _, ok := range []string{"/x/evaluator-inputs/fixes", "fixes", "/x/fixes/"} {
		if err := RefuseKeyedFixes(ok); err != nil {
			t.Errorf("must allow %q: %v", ok, err)
		}
	}
}

func TestFixSHAsIn(t *testing.T) {
	patch := `From 1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b Mon Sep 17 00:00:00 2001
Subject: [PATCH] bgpd: fix the drain loop

diff --git a/bgpd/x.c b/bgpd/x.c
index abc1234..def5678 100644
--- a/bgpd/x.c
+++ b/bgpd/x.c
@@ -1 +1 @@
-old
+new

From 9f8e7d6c5b4a32100123456789abcdef01234567 Mon Sep 17 00:00:00 2001
Subject: [PATCH] follow-up
`
	got := FixSHAsIn(patch)
	if len(got) != 2 || got[0] != "1a2b3c4d5e6f7a8b9c0d1e2f3a4b5c6d7e8f9a0b" || got[1] != "9f8e7d6c5b4a32100123456789abcdef01234567" {
		t.Errorf("got %v", got)
	}
	// Blob ids in index lines are not commit SHAs and must not be listed.
	for _, s := range got {
		if strings.HasPrefix(s, "abc1234") || strings.HasPrefix(s, "def5678") {
			t.Errorf("blob id leaked into fix SHAs: %v", got)
		}
	}
}
