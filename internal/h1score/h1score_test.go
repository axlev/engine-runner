package h1score

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func lbl(id, label, pair, class string) Label {
	l := Label{CaseID: id, Label: label, PairID: pair, Class: class}
	l.Admission.Title = "admitted"
	l.Admission.Description = "admitted"
	return l
}

func cohort(n int) Labels {
	var l Labels
	l.SchemaVersion = labelsSchema
	for i := 0; i < n; i++ {
		p := string(rune('a' + i))
		l.Cases = append(l.Cases, lbl("pos"+p, "positive", "pair"+p, "config-interaction"), lbl("neg"+p, "negative", "pair"+p, ""))
	}
	return l
}

func verdicts(m map[string]bool) map[string]CaseVerdict {
	out := map[string]CaseVerdict{}
	for id, risky := range m {
		out[id] = CaseVerdict{CaseID: id, RunID: "run-" + id, Risky: risky}
	}
	return out
}

func f(p *float64) float64 {
	if p == nil {
		return math.NaN()
	}
	return *p
}

func TestCountsPrecisionRecall(t *testing.T) {
	l := cohort(2) // posa, nega, posb, negb
	T := verdicts(map[string]bool{"posa": true, "nega": false, "posb": false, "negb": true})
	G := verdicts(map[string]bool{"posa": true, "nega": true, "posb": true, "negb": true})
	r, err := Score(Options{Labels: l, ArmIDs: map[string]string{"T": "h1-t-v1", "G": "h1-g-v1"},
		Verdicts: map[string]map[string]CaseVerdict{"T": T, "G": G}, Threshold: 15})
	if err != nil {
		t.Fatal(err)
	}
	at := r.Arms["T"]
	if at.Counts != (Counts{TP: 1, FP: 1, FN: 1, TN: 1}) || f(at.Precision.Value) != 0.5 || f(at.Recall.Value) != 0.5 {
		t.Errorf("T: %+v", at)
	}
	ag := r.Arms["G"]
	if ag.Counts != (Counts{TP: 2, FP: 2}) || f(ag.Precision.Value) != 0.5 || f(ag.Recall.Value) != 1 {
		t.Errorf("G: %+v", ag)
	}
	if r.Arms["H"].Cases != 0 || len(r.Arms["H"].Absent) != 4 {
		t.Errorf("H with no files must be absent for every case: %+v", r.Arms["H"])
	}
	if r.Arms["T"].RecallByClass["config-interaction"].Numerator != 1 {
		t.Errorf("recall by class: %+v", r.Arms["T"].RecallByClass)
	}
	if !r.Underpowered["recall"] {
		t.Error("2 positives is underpowered")
	}
}

// Hand-computed: three discordant cases, all positives, T RISKY and G CLEAN
// on each; concordant rest. Recall delta = +3/3 - 0 = 1. Under swaps, delta
// >= 1 only when no case is swapped: one-sided p = 1/8.
func TestPairedPermutationMatchesHandComputation(t *testing.T) {
	l := cohort(3)
	T := verdicts(map[string]bool{"posa": true, "posb": true, "posc": true, "nega": false, "negb": false, "negc": false})
	G := verdicts(map[string]bool{"posa": false, "posb": false, "posc": false, "nega": false, "negb": false, "negc": false})
	labels := map[string]Label{}
	for _, c := range l.Cases {
		labels[c.CaseID] = c
	}
	pw := pairwise("T-G", labels, T, G, 15)
	if pw.Discordant != 3 || pw.CommonCases != 6 {
		t.Fatalf("%+v", pw)
	}
	if f(pw.DeltaRecall) != 1 || f(pw.PRecall.OneSided) != 0.125 || pw.PRecall.Draws != 8 {
		t.Errorf("recall: delta=%v p1=%v draws=%d", f(pw.DeltaRecall), f(pw.PRecall.OneSided), pw.PRecall.Draws)
	}
	// G called nothing RISKY, so G's precision is undefined and the
	// precision delta is absent rather than zero.
	if pw.DeltaPrecision != nil || pw.PPrecision.OneSided != nil {
		t.Errorf("precision delta must be absent when the baseline has no RISKY calls: %+v", pw)
	}
	if pw.MeetsRecall == nil || !*pw.MeetsRecall {
		t.Error("+100 points meets +15")
	}
}

func TestPairwiseUsesOnlyCasesBothArmsScored(t *testing.T) {
	l := cohort(2)
	T := verdicts(map[string]bool{"posa": true, "nega": false, "posb": true, "negb": false})
	H := verdicts(map[string]bool{"posa": false, "nega": false})
	labels := map[string]Label{}
	for _, c := range l.Cases {
		labels[c.CaseID] = c
	}
	pw := pairwise("T-H", labels, T, H, 15)
	if pw.CommonCases != 2 || pw.Discordant != 1 {
		t.Errorf("%+v", pw)
	}
}

func TestFailClosed(t *testing.T) {
	l := cohort(1)
	T := verdicts(map[string]bool{"posa": true, "nega": false, "ghost": true})
	if _, err := Score(Options{Labels: l, ArmIDs: map[string]string{"T": "t", "G": "g"}, Verdicts: map[string]map[string]CaseVerdict{"T": T, "G": {}}}); err == nil {
		t.Error("a run with no label must fail")
	}
	T2 := verdicts(map[string]bool{"posa": true})
	if _, err := Score(Options{Labels: l, ArmIDs: map[string]string{"T": "t", "G": "g"}, Verdicts: map[string]map[string]CaseVerdict{"T": T2, "G": {}}}); err == nil {
		t.Error("a label with no run in any arm must fail")
	}
}

func TestLoadLabelsChecks(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "cohort.json")
	_ = os.WriteFile(manifest, []byte(`{"cohort":"x"}`), 0o644)
	sum, _ := fileSHA256(manifest)
	write := func(name string, l Labels) string {
		l.SchemaVersion = labelsSchema
		l.CohortManifestSHA256 = sum
		b, _ := json.Marshal(l)
		p := filepath.Join(dir, name)
		_ = os.WriteFile(p, b, 0o644)
		return p
	}
	if _, _, err := LoadLabels(write("ok.json", cohort(2)), manifest); err != nil {
		t.Errorf("valid labels refused: %v", err)
	}
	bad := cohort(1)
	bad.Cases[0].Class = ""
	if _, _, err := LoadLabels(write("noclass.json", bad), manifest); err == nil {
		t.Error("positive without class must fail")
	}
	unpaired := cohort(1)
	unpaired.Cases = unpaired.Cases[:1]
	if _, _, err := LoadLabels(write("unpaired.json", unpaired), manifest); err == nil {
		t.Error("unpaired case must fail")
	}
	wrong := cohort(1)
	wrong.CohortManifestSHA256 = "deadbeef"
	b, _ := json.Marshal(struct {
		Labels
		SchemaVersion string `json:"schema_version"`
	}{wrong, labelsSchema})
	p := filepath.Join(dir, "wrongsum.json")
	_ = os.WriteFile(p, b, 0o644)
	if _, _, err := LoadLabels(p, manifest); err == nil {
		t.Error("manifest sha mismatch must fail")
	}
}

func TestHitRateMatchesPathsOrDirectories(t *testing.T) {
	l := cohort(2)
	labels := map[string]Label{}
	for _, c := range l.Cases {
		labels[c.CaseID] = c
	}
	T := map[string]CaseVerdict{
		"posa": {CaseID: "posa", Risky: true, RecommendedPaths: []string{"tests/topotests/ospf_nssa_topo1"}},
		"posb": {CaseID: "posb", Risky: true, RecommendedPaths: []string{"bgpd/bgp_evpn_test.c"}},
		"nega": {CaseID: "nega", Risky: true, RecommendedPaths: []string{"x"}}, // FP: never in the hit denominator
		"negb": {CaseID: "negb", Risky: false},
	}
	fixing := map[string][]string{
		"posa": {"tests/topotests/ospf_nssa_topo1/test_ospf_nssa_topo1.py"},
		"posb": {"bgpd/bgp_evpn.c"},
	}
	r := armReport("T", "t", labels, T, nil, fixing, true)
	if r.HitRate == nil || f(r.HitRate.Rate.Value) != 0.5 || r.HitRate.Rate.Denominator != 2 {
		t.Fatalf("%+v", r.HitRate)
	}
	if !r.HitRate.PerCase[0].Hit || r.HitRate.PerCase[0].MatchedBy == "" || r.HitRate.PerCase[1].Hit {
		t.Errorf("%+v", r.HitRate.PerCase)
	}
	r2 := armReport("T", "t", labels, T, nil, map[string][]string{}, true)
	if r2.HitRate.NoFixingPaths != 2 || r2.HitRate.Rate.Absent == "" {
		t.Errorf("without fixing paths the rate is absent, not zero: %+v", r2.HitRate)
	}
}

func TestLoadVerdictsRefusesDuplicateAndExcludesVoided(t *testing.T) {
	root := t.TempDir()
	mk := func(run, caseID, proto string) {
		d := filepath.Join(root, run)
		_ = os.MkdirAll(filepath.Join(d, "evaluation"), 0o755)
		_ = os.WriteFile(filepath.Join(d, "run.json"), []byte(`{"run_id":"`+run+`","case_id":"`+caseID+`","protocol_version":"`+proto+`","status":"completed"}`), 0o644)
		_ = os.WriteFile(filepath.Join(d, "evaluation", "verdict.json"), []byte(`{"schema_version":"h1-verdict/v1","verdict":"RISKY","recommended_paths":[]}`), 0o644)
	}
	mk("r1", "c1", "h1-t-v1")
	mk("r2", "c1", "h1-t-v1")
	arms := map[string]string{"T": "h1-t-v1", "G": "h1-g-v1"}
	if _, _, err := LoadVerdicts(root, arms, nil); err == nil {
		t.Error("two runs of one case in one arm must fail")
	}
	v, voided, err := LoadVerdicts(root, arms, map[string]bool{"r2": true})
	if err != nil || len(v["T"]) != 1 || len(voided["T"]) != 1 || voided["T"][0] != "c1" {
		t.Errorf("voided run must be excluded and its case listed: %v %v %v", v, voided, err)
	}
}

// Section 8: a void is an expected, reported outcome. A labelled case whose
// only run was voided is absent in that arm - it does not make the
// aggregate refuse to run.
func TestVoidedOnlyRunIsAbsentNotFatal(t *testing.T) {
	l := cohort(1)
	G := verdicts(map[string]bool{"nega": false})
	r, err := Score(Options{Labels: l, ArmIDs: map[string]string{"T": "t", "G": "g"},
		Verdicts: map[string]map[string]CaseVerdict{"T": {}, "G": G},
		Voided:   map[string][]string{"T": {"posa"}}})
	if err != nil {
		t.Fatalf("a voided-only case must not be fatal: %v", err)
	}
	at := r.Arms["T"]
	if at.Voided != 1 || len(at.VoidedCases) != 1 || at.VoidedCases[0] != "posa" || at.Cases != 0 {
		t.Errorf("%+v", at)
	}
}

// history-baseline/v1 as the miner builds it: unknown top-level keys are
// tolerated, rule need only be an object, and risky: null with a reason
// makes the case absent for H - never CLEAN.
func TestLoadHistoryToleratesAdditionsAndTreatsNullAsAbsent(t *testing.T) {
	dir := t.TempDir()
	write := func(id, body string) {
		_ = os.WriteFile(filepath.Join(dir, id+".json"), []byte(body), 0o644)
	}
	write("c1", `{"schema_version":"history-baseline/v1","case_id":"c1","risky":true,"score":0.9,"subsystem":"bgpd","tercile":3,
	  "rule":{"window_months":24,"signal_tiers":["strong"],"risky_if":"top tercile","note":"x"},"provenance":{},
	  "detail":{"anything":1},"merge_base":"abc"}`)
	write("c2", `{"schema_version":"history-baseline/v1","case_id":"c2","risky":null,"reason":"base commit unresolvable",
	  "score":0,"subsystem":"","tercile":0,"rule":{},"provenance":{}}`)
	write("c4", `{"schema_version":"history-baseline/v1","case_id":"c4","risky":false,"rule":"not an object","provenance":{}}`)
	cases := []Label{{CaseID: "c1"}, {CaseID: "c2"}, {CaseID: "c3"}}
	h, reasons, err := LoadHistory(dir, cases)
	if err != nil {
		t.Fatal(err)
	}
	if v, ok := h["c1"]; !ok || !v.Risky {
		t.Errorf("c1 with extra keys must load risky=true: %+v", h)
	}
	if _, ok := h["c2"]; ok || reasons["c2"] != "base commit unresolvable" {
		t.Errorf("null risky must be absent with its reason: %v %v", h, reasons)
	}
	if _, ok := h["c3"]; ok || reasons["c3"] != "" {
		t.Errorf("missing file is absent with no reason entry: %v %v", h, reasons)
	}
	if _, _, err := LoadHistory(dir, []Label{{CaseID: "c4"}}); err == nil {
		t.Error("rule that is not an object must fail")
	}

	// The reason travels into the report; H's counts exclude the case.
	l := cohort(1)
	l.Cases[0].CaseID, l.Cases[1].CaseID = "c1", "c2"
	T := verdicts(map[string]bool{"c1": true, "c2": false})
	r, err := Score(Options{Labels: l, ArmIDs: map[string]string{"T": "t", "G": "g"},
		Verdicts: map[string]map[string]CaseVerdict{"T": T, "G": {}}, History: h, HistoryAbsentReasons: reasons})
	if err != nil {
		t.Fatal(err)
	}
	ah := r.Arms["H"]
	if ah.Cases != 1 || len(ah.Absent) != 1 || ah.Absent[0] != "c2" || ah.AbsentReasons["c2"] != "base commit unresolvable" {
		t.Errorf("%+v", ah)
	}
}

// A8: Fisher's tea-tasting table [[3 1] [1 3]] gives two-sided p = 0.4857.
func TestA8FisherExact(t *testing.T) {
	if p := fisherExactTwoSided(3, 1, 1, 3); math.Abs(p-0.485714) > 1e-4 {
		t.Errorf("p = %v, want 0.4857", p)
	}
	if p := fisherExactTwoSided(0, 0, 0, 0); p != 1 {
		t.Errorf("empty table p = %v", p)
	}
	l := cohort(2)
	T := verdicts(map[string]bool{"posa": true, "nega": false, "posb": true, "negb": false})
	r, err := Score(Options{Labels: l, ArmIDs: map[string]string{"T": "t", "G": "g"},
		Verdicts: map[string]map[string]CaseVerdict{"T": T, "G": T}, Threshold: 15})
	if err != nil {
		t.Fatal(err)
	}
	if v := r.Arms["T"].VsChance; v == nil || v.TwoSided == nil || math.Abs(*v.TwoSided-1.0/3) > 1e-9 {
		t.Errorf("[[2 0][0 2]] two-sided p should be 1/3, got %+v", v)
	}
	if v := r.Arms["H"].VsChance; v == nil || v.Absent == "" {
		t.Errorf("H with no cases: absent with a reason, got %+v", v)
	}
}
