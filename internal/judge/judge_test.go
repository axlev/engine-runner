package judge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func pooled(arm, findingID, title, body string, files ...string) PooledFinding {
	return PooledFinding{Arm: arm, RunID: arm + "-run", FindingID: findingID,
		Title: title, Body: body, Files: files}
}

// The judge must not be able to tell which arm produced a finding. Two of
// three arms are opus and the natural judge model is opus, so an identifiable
// arm is a self-preference problem with no clean outside option.
func TestJudgeViewCarriesNoProvenance(t *testing.T) {
	p := NewPool("case-x", []PooledFinding{
		pooled("opus-wide", "f1", "null deref", "body", "a.c"),
		pooled("sonnet", "f1", "leak", "body", "a.c"),
	})
	if err := p.CheckOpaque(); err != nil {
		t.Fatalf("CheckOpaque: %v", err)
	}
	blob, err := json.Marshal(p.View())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, leak := range []string{"sonnet", "opus", "wide", "-run"} {
		if strings.Contains(strings.ToLower(string(blob)), leak) {
			t.Errorf("judge view leaks %q: %s", leak, blob)
		}
	}
}

// Two arms producing the same finding id must not collide, or one arm's
// verdict would overwrite the other's during scoring.
func TestOpaqueIDsAreDistinctAcrossArms(t *testing.T) {
	p := NewPool("case-x", []PooledFinding{
		pooled("sonnet", "f1", "a", "b"),
		pooled("opus-narrow", "f1", "a", "b"),
		pooled("opus-wide", "f1", "a", "b"),
	})
	seen := map[string]bool{}
	for _, f := range p.Findings {
		if seen[f.OpaqueID] {
			t.Fatalf("duplicate opaque id %s", f.OpaqueID)
		}
		seen[f.OpaqueID] = true
	}
}

// A run must be reproducible from its fingerprints, so pooling is
// deterministic rather than randomly shuffled.
func TestPoolOrderIsDeterministicButNotByArm(t *testing.T) {
	in := []PooledFinding{
		pooled("sonnet", "f1", "a", "b"),
		pooled("opus-wide", "f2", "c", "d"),
		pooled("opus-narrow", "f3", "e", "f"),
	}
	a, b := NewPool("case-x", in), NewPool("case-x", in)
	for i := range a.Findings {
		if a.Findings[i].OpaqueID != b.Findings[i].OpaqueID {
			t.Fatalf("pool order is not deterministic at %d", i)
		}
	}
}

// TOO_VAGUE is excluded from the precision denominator - an unfalsifiable
// finding is not a false positive, it is not a claim. That exclusion makes
// vagueness free, which is the failure shape this project has hit three
// times, so the vagueness rate must be impossible to detach from precision.
func TestPrecisionCannotBeReadWithoutVagueness(t *testing.T) {
	p := PrecisionReport{Mechanism: 3, Locality: 1, None: 1, TooVague: 5}

	if got := p.Denominator(); got != 5 {
		t.Errorf("Denominator = %d, want 5 (TOO_VAGUE excluded)", got)
	}
	prec, vague := p.Rates()
	if prec != 60 {
		t.Errorf("precision = %v, want 60", prec)
	}
	if vague != 50 {
		t.Errorf("vagueness = %v, want 50", vague)
	}
	// The rendered form must carry both. "60.0% agreement" alone is not a
	// reportable figure.
	s := p.String()
	for _, want := range []string{"60.0%", "TOO_VAGUE", "5 of 10"} {
		if !strings.Contains(s, want) {
			t.Errorf("String() = %q, want it to mention %q", s, want)
		}
	}
}

// Vaguer findings must not raise the score without that being visible. This
// is the gaming hole the adjacency rule exists to close.
func TestVaguenessRaisesPrecisionButIsVisible(t *testing.T) {
	honest := PrecisionReport{Mechanism: 3, Locality: 3, None: 4, TooVague: 0}
	hedged := PrecisionReport{Mechanism: 3, Locality: 1, None: 1, TooVague: 5}

	hp, hv := honest.Rates()
	gp, gv := hedged.Rates()
	if !(gp > hp) {
		t.Fatalf("expected hedging to raise precision (%v -> %v); if it does not, this test's premise is wrong", hp, gp)
	}
	if !(gv > hv) {
		t.Errorf("hedging raised precision without raising the vagueness rate (%v -> %v); the tell is missing", hv, gv)
	}
}

// Only MECHANISM covers a fix. A LOCALITY_ONLY finding describes a different
// defect and a TOO_VAGUE one describes nothing.
func TestOnlyMechanismCoversAFix(t *testing.T) {
	p := NewPool("case-x", []PooledFinding{
		pooled("sonnet", "f1", "", ""),
		pooled("sonnet", "f2", "", ""),
	})
	idx := p.Index()
	var ids []string
	for id := range idx {
		ids = append(ids, id)
	}
	cases := []CaseVerdicts{{
		CaseID: "case-x", FixIDs: []string{"fix-1", "fix-2"},
		Verdicts: []Verdict{
			{FindingID: ids[0], Agreement: AgreementLocality, FixID: "fix-1"},
			{FindingID: ids[1], Agreement: AgreementTooVague},
		},
	}}
	rep, un := Score(cases, idx, nil)
	if un != 0 {
		t.Fatalf("unattributed = %d, want 0", un)
	}
	if rep.Overall.Recall.Covered != 0 {
		t.Errorf("Covered = %d, want 0: locality and vagueness cover nothing", rep.Overall.Recall.Covered)
	}
	if rep.Overall.Recall.Fixes != 2 {
		t.Errorf("Fixes = %d, want 2", rep.Overall.Recall.Fixes)
	}
}

// Crediting a fix the case never had would inflate recall.
func TestVerdictNamingAnUnknownFixDoesNotCountForRecall(t *testing.T) {
	p := NewPool("case-x", []PooledFinding{pooled("sonnet", "f1", "", "")})
	idx := p.Index()
	var id string
	for k := range idx {
		id = k
	}
	rep, _ := Score([]CaseVerdicts{{
		CaseID: "case-x", FixIDs: []string{"fix-1"},
		Verdicts: []Verdict{{FindingID: id, Agreement: AgreementMechanism, FixID: "fix-INVENTED"}},
	}}, idx, nil)

	if rep.Overall.Precision.Mechanism != 1 {
		t.Errorf("the verdict should still count toward precision")
	}
	if rep.Overall.Recall.Covered != 0 {
		t.Errorf("Covered = %d, want 0: the named fix does not exist", rep.Overall.Recall.Covered)
	}
}

// A judge that mangles or invents an id must not silently vanish from the
// totals.
func TestUnattributedVerdictsAreCounted(t *testing.T) {
	p := NewPool("case-x", []PooledFinding{pooled("sonnet", "f1", "", "")})
	rep, un := Score([]CaseVerdicts{{
		CaseID: "case-x", FixIDs: []string{"fix-1"},
		Verdicts: []Verdict{{FindingID: "fNOTREAL", Agreement: AgreementMechanism, FixID: "fix-1"}},
	}}, p.Index(), nil)
	if un != 1 {
		t.Errorf("unattributed = %d, want 1", un)
	}
	if rep.Overall.Precision.Mechanism != 1 {
		t.Errorf("an unattributed verdict still belongs in the overall totals")
	}
	if rep.ByArm["sonnet"].Precision.Mechanism != 0 {
		t.Errorf("an unattributed verdict must not be credited to an arm")
	}
}

// Excluded cases stay out of every denominator. Counting "no evidence" as
// "wrong" is the error that disqualified correspondence.
func TestExcludedCasesAreReportedNotScored(t *testing.T) {
	excluded := []ExcludedCase{
		{CaseID: "case-a", Findings: 4, Reason: "no corrective signals at any tier"},
		{CaseID: "case-b", Findings: 6, Reason: "no corrective signals at any tier"},
	}
	rep, _ := Score(nil, nil, excluded)
	if rep.Overall.Precision.Judged() != 0 {
		t.Errorf("excluded findings must not enter the precision base")
	}
	if rep.ExcludedFindings() != 10 {
		t.Errorf("ExcludedFindings = %d, want 10", rep.ExcludedFindings())
	}
	if !strings.Contains(rep.String(), "excluded") {
		t.Errorf("the rendered report must disclose exclusions: %q", rep.String())
	}
}

// The primary health check on a real run: a judge that rationalises matches
// awards MECHANISM where LOCALITY_ONLY belongs, so near-zero locality
// alongside many matches is the signature.
func TestLocalityRatioIsTheRationalisationTell(t *testing.T) {
	if _, ok := (PrecisionReport{None: 5}).LocalityRatio(); ok {
		t.Errorf("with no location-related verdicts the ratio is undefined, not zero")
	}
	suspicious, ok := PrecisionReport{Mechanism: 20, Locality: 0}.LocalityRatio()
	if !ok || suspicious != 0 {
		t.Errorf("LocalityRatio = %v, want 0", suspicious)
	}
	healthy, _ := PrecisionReport{Mechanism: 10, Locality: 10}.LocalityRatio()
	if healthy != 50 {
		t.Errorf("LocalityRatio = %v, want 50", healthy)
	}
}

// Near-duplicates across arms are expected and each gets its own verdict.
// This test proves the HARNESS treats them independently; whether the MODEL
// lets a sharp finding lift a vague sibling is a model behaviour only
// observable on a real run, and is flagged as something to check there.
func TestNearDuplicatesAcrossArmsScoreIndependently(t *testing.T) {
	p := NewPool("case-x", []PooledFinding{
		pooled("sonnet", "f1", "vague memory concern", "something may leak"),
		pooled("opus-wide", "f7", "precise leak", "error path returns without freeing buf"),
	})
	idx := p.Index()
	var vagueID, sharpID string
	for id, f := range idx {
		if f.Arm == "sonnet" {
			vagueID = id
		} else {
			sharpID = id
		}
	}
	rep, _ := Score([]CaseVerdicts{{
		CaseID: "case-x", FixIDs: []string{"fix-1"},
		Verdicts: []Verdict{
			{FindingID: vagueID, Agreement: AgreementTooVague},
			{FindingID: sharpID, Agreement: AgreementMechanism, FixID: "fix-1"},
		},
	}}, idx, nil)

	if rep.ByArm["sonnet"].Precision.TooVague != 1 {
		t.Errorf("the vague finding must stay vague regardless of its sibling")
	}
	if rep.ByArm["opus-wide"].Precision.Mechanism != 1 {
		t.Errorf("the sharp finding should match")
	}
	// Recall is over fixes: the fix is covered overall and by opus-wide, but
	// NOT by sonnet, whose finding covered nothing.
	if rep.Overall.Recall.Covered != 1 {
		t.Errorf("overall recall should credit the fix as covered")
	}
	if rep.ByArm["sonnet"].Recall.Covered != 0 {
		t.Errorf("sonnet covered nothing and must not inherit its sibling's match")
	}
}

// Both prompts must exist and must not have drifted back into one: call 1's
// whole purpose is that it cannot see the findings.
func TestTwoPromptsExistAndCall1NeverMentionsFindings(t *testing.T) {
	mech, err := os.ReadFile(filepath.Join("prompt-mechanisms.md"))
	if err != nil {
		t.Fatalf("call 1 prompt missing: %v", err)
	}
	if _, err := os.ReadFile(filepath.Join("prompt-verdicts.md")); err != nil {
		t.Fatalf("call 2 prompt missing: %v", err)
	}
	// Call 1 may explain WHY it is not shown the findings; it must not ask
	// for a verdict.
	for _, banned := range []string{"MECHANISM`", "LOCALITY_ONLY", "TOO_VAGUE", "case_against"} {
		if strings.Contains(string(mech), banned) {
			t.Errorf("call 1 prompt mentions %q; it must describe fixes only, never judge", banned)
		}
	}
}
