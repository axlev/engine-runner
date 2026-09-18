package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axlev/engine-runner/internal/h1judge"
	"github.com/axlev/engine-runner/internal/h1score"
)

// writeJudge seals one h1-judge/v1 report the way cmd/h1judge does.
func writeJudge(t *testing.T, dir, caseID, model string, inFamily bool, verdicts map[string]h1judge.ArmVerdict) {
	t.Helper()
	r := h1judge.Report{
		SchemaVersion: h1judge.SchemaVersion,
		CaseID:        caseID,
		JudgeModel:    model,
		InFamily:      inFamily,
		Verdicts:      verdicts,
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, caseID+".h1-judge.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// cases builds labelled POSITIVES by default: section 6 judges RISKY true
// positives, so that is the eligible shape. Negatives are built explicitly
// where a test needs one.
func cases(ids ...string) []h1score.Label {
	out := make([]h1score.Label, 0, len(ids))
	for _, id := range ids {
		out = append(out, h1score.Label{CaseID: id, Label: "positive"})
	}
	return out
}

func TestLoadReasonMatchAbsentWithoutJudgeDir(t *testing.T) {
	got, _, err := loadReasonMatch("", cases("c1"), nil, nil, nil)
	if err != nil || got != nil {
		t.Fatalf("no -judge must yield (nil, nil), got (%v, %v)", got, err)
	}
}

func TestLoadReasonMatchSummarisesJudgedPrimaries(t *testing.T) {
	dir := t.TempDir()
	writeJudge(t, dir, "c1", "claude-opus-5", true, map[string]h1judge.ArmVerdict{
		"T": {Arm: "T", FindingID: "f-t-1", Agreement: "MECHANISM"},
		"G": {Arm: "G", FindingID: "f-g-1", Agreement: "LOCALITY_ONLY"},
	})
	verdicts := map[string]map[string]h1score.CaseVerdict{
		"T": {"c1": {CaseID: "c1", Risky: true, PrimaryFindingID: "f-t-1"}},
		"G": {"c1": {CaseID: "c1", Risky: true, PrimaryFindingID: "f-g-1"}},
	}
	got, _, err := loadReasonMatch(dir, cases("c1"), verdicts, nil, nil)
	if err != nil {
		t.Fatalf("well-formed judge output refused: %v", err)
	}
	s, ok := got.(h1judge.Summary)
	if !ok {
		t.Fatalf("expected an h1judge.Summary, got %T", got)
	}
	if s.JudgeModel != "claude-opus-5" || !s.InFamily {
		t.Errorf("judge model/in-family not carried: %+v", s)
	}
	if s.Arms["T"].Mechanism != 1 || s.Arms["G"].LocalityOnly != 1 {
		t.Errorf("verdict counts not summarised: %+v", s.Arms)
	}
}

// The judge is handed the primary from verdict.json precisely so it cannot
// read a different finding than the scorer counts. If they diverge the
// figure describes something this report does not score, so it must stop.
func TestLoadReasonMatchFailsClosedOnFindingMismatch(t *testing.T) {
	dir := t.TempDir()
	writeJudge(t, dir, "c1", "claude-opus-5", true, map[string]h1judge.ArmVerdict{
		"T": {Arm: "T", FindingID: "f-t-9", Agreement: "MECHANISM"},
	})
	verdicts := map[string]map[string]h1score.CaseVerdict{
		"T": {"c1": {CaseID: "c1", PrimaryFindingID: "f-t-1"}},
	}
	_, _, err := loadReasonMatch(dir, cases("c1"), verdicts, nil, nil)
	if err == nil {
		t.Fatal("a judged finding that is not the scored primary was accepted")
	}
	for _, want := range []string{"f-t-9", "f-t-1", "c1"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should name %q, got: %v", want, err)
		}
	}
}

func TestLoadReasonMatchFailsClosedOnUnscoredArm(t *testing.T) {
	dir := t.TempDir()
	writeJudge(t, dir, "c1", "claude-opus-5", true, map[string]h1judge.ArmVerdict{
		"T": {Arm: "T", FindingID: "f-t-1", Agreement: "MECHANISM"},
	})
	_, _, err := loadReasonMatch(dir, cases("c1"), map[string]map[string]h1score.CaseVerdict{}, nil, nil)
	if err == nil {
		t.Fatal("a judged arm with no scored verdict was accepted")
	}
}

// Section 6 requires in-family stated on every figure derived from the
// judge. One figure cannot carry two answers.
func TestLoadReasonMatchFailsClosedOnDisagreeingJudges(t *testing.T) {
	dir := t.TempDir()
	v := map[string]h1judge.ArmVerdict{"T": {Arm: "T", FindingID: "f", Agreement: "MECHANISM"}}
	writeJudge(t, dir, "c1", "claude-opus-5", true, v)
	writeJudge(t, dir, "c2", "claude-sonnet-5", false, v)
	verdicts := map[string]map[string]h1score.CaseVerdict{
		"T": {"c1": {Risky: true, PrimaryFindingID: "f"}, "c2": {Risky: true, PrimaryFindingID: "f"}},
	}
	_, _, err := loadReasonMatch(dir, cases("c1", "c2"), verdicts, nil, nil)
	if err == nil {
		t.Fatal("reports from two different judges were accepted into one figure")
	}
}

func TestLoadReasonMatchRefusesAnEmptyJudgeDir(t *testing.T) {
	if _, _, err := loadReasonMatch(t.TempDir(), cases("c1"), nil, nil, nil); err == nil {
		t.Fatal("an empty -judge directory was accepted; the criterion would render as if judged")
	}
}

// Section 6 judges RISKY true positives only. A verdict on anything else
// means the judge ran against verdicts other than the ones being scored,
// and the rate would describe a different denominator than §2 names.
func TestLoadReasonMatchFailsClosedOnNonRiskyCase(t *testing.T) {
	dir := t.TempDir()
	writeJudge(t, dir, "c1", "claude-opus-5", true, map[string]h1judge.ArmVerdict{
		"T": {Arm: "T", FindingID: "f-t-1", Agreement: "MECHANISM"},
	})
	verdicts := map[string]map[string]h1score.CaseVerdict{
		"T": {"c1": {CaseID: "c1", Risky: false, PrimaryFindingID: "f-t-1"}},
	}
	_, _, err := loadReasonMatch(dir, cases("c1"), verdicts, nil, nil)
	if err == nil {
		t.Fatal("a judged case the arm did not call RISKY was accepted")
	}
	if !strings.Contains(err.Error(), "c1") || !strings.Contains(err.Error(), "RISKY") {
		t.Errorf("error should name the case and the rule, got: %v", err)
	}
}

func TestLoadReasonMatchFailsClosedOnLabelledNegative(t *testing.T) {
	dir := t.TempDir()
	writeJudge(t, dir, "c1", "claude-opus-5", true, map[string]h1judge.ArmVerdict{
		"T": {Arm: "T", FindingID: "f-t-1", Agreement: "MECHANISM"},
	})
	verdicts := map[string]map[string]h1score.CaseVerdict{
		"T": {"c1": {CaseID: "c1", Risky: true, PrimaryFindingID: "f-t-1"}},
	}
	negative := []h1score.Label{{CaseID: "c1", Label: "negative"}}
	_, _, err := loadReasonMatch(dir, negative, verdicts, nil, nil)
	if err == nil {
		t.Fatal("a RISKY call on a labelled negative was accepted into reason match; it is a false positive, already counted in precision")
	}
	if !strings.Contains(err.Error(), "c1") {
		t.Errorf("error should name the case, got: %v", err)
	}
}

// A voided run is dropped from every figure, so a judged pair on one cannot
// count here. h1judge pools by RISKY and label alone and has no scan input,
// so it legitimately judges runs the section 8 scan later voided; the
// scorer must skip those rather than fail on them.
func TestLoadReasonMatchSkipsScanVoidedPairs(t *testing.T) {
	dir := t.TempDir()
	writeJudge(t, dir, "c1", "claude-opus-5", true, map[string]h1judge.ArmVerdict{
		"T": {Arm: "T", FindingID: "f-t-1", Agreement: "MECHANISM"},
		"G": {Arm: "G", FindingID: "f-g-1", Agreement: "MECHANISM"},
	})
	verdicts := map[string]map[string]h1score.CaseVerdict{
		"T": {"c1": {CaseID: "c1", Risky: true, PrimaryFindingID: "f-t-1"}},
		// G's run was voided, so it is absent from the scored verdicts.
		"G": {},
	}
	scanVoided := map[string][]string{"G": {"c1"}}

	got, voided, err := loadReasonMatch(dir, cases("c1"), verdicts, scanVoided, nil)
	if err != nil {
		t.Fatalf("a scan-voided judged pair must be skipped, not fail: %v", err)
	}
	if len(voided) != 1 || voided[0].Arm != "G" || voided[0].CaseID != "c1" {
		t.Fatalf("the voided pair must be reported: %+v", voided)
	}
	if !strings.Contains(voided[0].Reason, "scan-voided") {
		t.Errorf("reason should name the rule, got %q", voided[0].Reason)
	}
	s := got.(h1judge.Summary)
	if s.Arms["T"].Judged != 1 {
		t.Errorf("the unvoided arm must still be judged: %+v", s.Arms["T"])
	}
	if s.Arms["G"].Judged != 0 {
		t.Errorf("the voided arm must not enter the denominator: %+v", s.Arms["G"])
	}
}

// The probe voids a case for EVERY arm, including arms whose runs are
// otherwise present and RISKY.
func TestLoadReasonMatchSkipsProbeVoidedCasesForEveryArm(t *testing.T) {
	dir := t.TempDir()
	writeJudge(t, dir, "c1", "claude-opus-5", true, map[string]h1judge.ArmVerdict{
		"T": {Arm: "T", FindingID: "f-t-1", Agreement: "MECHANISM"},
		"G": {Arm: "G", FindingID: "f-g-1", Agreement: "MECHANISM"},
	})
	verdicts := map[string]map[string]h1score.CaseVerdict{
		"T": {"c1": {CaseID: "c1", Risky: true, PrimaryFindingID: "f-t-1"}},
		"G": {"c1": {CaseID: "c1", Risky: true, PrimaryFindingID: "f-g-1"}},
	}
	got, voided, err := loadReasonMatch(dir, cases("c1"), verdicts, nil, []string{"c1"})
	if err != nil {
		t.Fatalf("a probe-voided case must be skipped, not fail: %v", err)
	}
	if len(voided) != 2 {
		t.Fatalf("a probe void drops every arm, got %d: %+v", len(voided), voided)
	}
	for _, v := range voided {
		if !strings.Contains(v.Reason, "probe-voided") {
			t.Errorf("reason should name the rule, got %q", v.Reason)
		}
	}
	s := got.(h1judge.Summary)
	if s.Arms["T"].Judged != 0 || s.Arms["G"].Judged != 0 {
		t.Errorf("no arm may count a probe-voided case: %+v", s.Arms)
	}
}

// The void skip must not become a blanket excuse: a judged pair that is
// neither voided nor RISKY-and-positive is still a hard failure.
func TestLoadReasonMatchStillFailsWhenNotVoidedAndNotEligible(t *testing.T) {
	dir := t.TempDir()
	writeJudge(t, dir, "c1", "claude-opus-5", true, map[string]h1judge.ArmVerdict{
		"T": {Arm: "T", FindingID: "f-t-1", Agreement: "MECHANISM"},
	})
	verdicts := map[string]map[string]h1score.CaseVerdict{
		"T": {"c1": {CaseID: "c1", Risky: false, PrimaryFindingID: "f-t-1"}},
	}
	// Voided for a DIFFERENT arm, so this pair is not excused.
	if _, _, err := loadReasonMatch(dir, cases("c1"), verdicts, map[string][]string{"G": {"c1"}}, nil); err == nil {
		t.Fatal("a non-RISKY judged pair was excused by an unrelated arm's void")
	}
}
