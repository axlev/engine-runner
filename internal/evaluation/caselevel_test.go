package evaluation

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeDoc(t *testing.T, dir, name string, doc any) string {
	t.Helper()
	p := filepath.Join(dir, name)
	b, _ := json.Marshal(doc)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func h1Finding_(id, sev string, conf float64, paths ...string) map[string]any {
	return map[string]any{"id": id, "file": "x.c", "line": 1, "mechanism": "M-" + id, "class": "other",
		"severity": sev, "confidence": conf,
		"recommended_validation": map[string]any{"kind": "new_test", "paths": paths, "description": "d"}}
}

func h1ReviewADoc(findings ...map[string]any) map[string]any {
	return map[string]any{"schema_version": "h1-review-a/v1", "findings": findings}
}

func h1Assessment_(id, disp string, paths ...string) map[string]any {
	a := map[string]any{"finding_id": id, "disposition": disp, "mechanism": "B-" + id}
	if len(paths) > 0 {
		a["recommended_validation"] = map[string]any{"kind": "existing_test", "paths": paths, "description": "d"}
	}
	return a
}

func h1ReviewBDoc(as ...map[string]any) map[string]any {
	return map[string]any{"schema_version": "h1-review-b/v1", "assessments": as}
}

func TestArmGRule(t *testing.T) {
	cases := []struct {
		name    string
		a       map[string]any
		want    string
		primary string
	}{
		{"empty is CLEAN", h1ReviewADoc(), VerdictClean, ""},
		{"medium at 0.6 is RISKY", h1ReviewADoc(h1Finding_("f1", "medium", 0.6)), VerdictRisky, "f1"},
		{"medium at 0.59 is CLEAN", h1ReviewADoc(h1Finding_("f1", "medium", 0.59)), VerdictClean, "f1"},
		{"low at 1.0 is CLEAN", h1ReviewADoc(h1Finding_("f1", "low", 1.0)), VerdictClean, "f1"},
		{"critical at 0.6 is RISKY", h1ReviewADoc(h1Finding_("f1", "critical", 0.6)), VerdictRisky, "f1"},
		{"primary is index 0 even when CLEAN", h1ReviewADoc(h1Finding_("f1", "low", 0.9), h1Finding_("f2", "low", 0.9)), VerdictClean, "f1"},
	}
	for _, c := range cases {
		dir := t.TempDir()
		v, err := ScoreCase("r", "c", ArmRuleG, writeDoc(t, dir, "a.json", c.a), "")
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if v.Verdict != c.want || v.ArmRule != ArmRuleG {
			t.Errorf("%s: verdict %s arm %s", c.name, v.Verdict, v.ArmRule)
		}
		if (c.primary == "" && v.PrimaryFinding != nil) || (c.primary != "" && (v.PrimaryFinding == nil || v.PrimaryFinding.ID != c.primary)) {
			t.Errorf("%s: primary = %+v, want %q", c.name, v.PrimaryFinding, c.primary)
		}
		if v.Rule.ConfidenceThreshold == nil || *v.Rule.ConfidenceThreshold != 0.6 || v.Rule.DispositionSet != nil {
			t.Errorf("%s: G rule block wrong: %+v", c.name, v.Rule)
		}
		if v.RiskScore == nil || v.Rule.SeverityWeights["critical"] != 1.0 || v.Rule.RiskScoreOver == "" {
			t.Errorf("%s: risk score and A6 weights must be recorded", c.name)
		}
		if v.Rule.RuleVersion != RuleVersion || v.Rule.SeverityThreshold != "medium" {
			t.Errorf("%s: rule not self-described: %+v", c.name, v.Rule)
		}
	}
}

func TestArmTRule(t *testing.T) {
	a := h1ReviewADoc(h1Finding_("f1", "high", 0.9, "t/one"), h1Finding_("f2", "medium", 0.5, "t/two"), h1Finding_("f3", "low", 0.9, "t/three"))
	cases := []struct {
		name          string
		b             map[string]any
		want          string
		primary       string
		survivors     int
		deciding      string
		mechanism     string
		wantPathCount int
	}{
		{"CONFIRMED high is RISKY", h1ReviewBDoc(h1Assessment_("f1", "CONFIRMED", "t/b1")), VerdictRisky, "f1", 1, "", "B-f1", 1},
		{"NARROWED medium is RISKY, confidence irrelevant", h1ReviewBDoc(h1Assessment_("f2", "NARROWED", "t/b2")), VerdictRisky, "f2", 1, "", "B-f2", 1},
		{"REJECTED and INCONCLUSIVE never count", h1ReviewBDoc(h1Assessment_("f1", "REJECTED"), h1Assessment_("f2", "INCONCLUSIVE")), VerdictClean, "", 0, "", "", 0},
		{"survivor below threshold is CLEAN", h1ReviewBDoc(h1Assessment_("f3", "CONFIRMED", "t/b3")), VerdictClean, "f3", 1, "", "B-f3", 1},
		{"low survivor ranked first, deciding recorded", h1ReviewBDoc(h1Assessment_("f3", "CONFIRMED", "t/b3"), h1Assessment_("f1", "CONFIRMED", "t/b1")), VerdictRisky, "f3", 2, "f1", "B-f3", 2},
		{"empty assessments is CLEAN", h1ReviewBDoc(), VerdictClean, "", 0, "", "", 0},
	}
	for _, c := range cases {
		dir := t.TempDir()
		v, err := ScoreCase("r", "c", ArmRuleT, writeDoc(t, dir, "a.json", a), writeDoc(t, dir, "b.json", c.b))
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if v.Verdict != c.want || v.ArmRule != ArmRuleT || v.Survivors == nil || *v.Survivors != c.survivors {
			t.Errorf("%s: verdict %s arm %s survivors %v", c.name, v.Verdict, v.ArmRule, v.Survivors)
		}
		gotPrimary := ""
		if v.PrimaryFinding != nil {
			gotPrimary = v.PrimaryFinding.ID
			if v.PrimaryFinding.Mechanism != c.mechanism {
				t.Errorf("%s: primary mechanism %q, want B's %q", c.name, v.PrimaryFinding.Mechanism, c.mechanism)
			}
			if v.PrimaryFinding.Severity == "" || v.PrimaryFinding.Disposition == "" {
				t.Errorf("%s: primary must carry A's severity and B's disposition: %+v", c.name, v.PrimaryFinding)
			}
		}
		if gotPrimary != c.primary {
			t.Errorf("%s: primary %q, want %q", c.name, gotPrimary, c.primary)
		}
		gotDeciding := ""
		if v.DecidingFinding != nil {
			gotDeciding = v.DecidingFinding.ID
		}
		if gotDeciding != c.deciding {
			t.Errorf("%s: deciding %q, want %q", c.name, gotDeciding, c.deciding)
		}
		if len(v.RecommendedPaths) != c.wantPathCount {
			t.Errorf("%s: recommended_paths %v, want %d entries", c.name, v.RecommendedPaths, c.wantPathCount)
		}
		if v.Rule.ConfidenceThreshold != nil || len(v.Rule.DispositionSet) != 2 {
			t.Errorf("%s: T rule block wrong: %+v", c.name, v.Rule)
		}
	}
}

func TestScoreCaseRefusesWrongDocuments(t *testing.T) {
	dir := t.TempDir()
	pilot := writeDoc(t, dir, "pilot.json", map[string]any{"schema_version": "review-a/v1", "findings": []any{}})
	if _, err := ScoreCase("r", "c", ArmRuleG, pilot, ""); err == nil {
		t.Error("a pilot-v1 review-a must not be scored by the H1 rule")
	}
	a := writeDoc(t, dir, "a.json", h1ReviewADoc(h1Finding_("f1", "high", 0.9)))
	b := writeDoc(t, dir, "b.json", h1ReviewBDoc(h1Assessment_("ghost", "CONFIRMED", "t/x")))
	if _, err := ScoreCase("r", "c", ArmRuleT, a, b); err == nil {
		t.Error("an assessment with no discovery finding must be refused")
	}
	// The arm rule is declared by the protocol; file presence may not
	// override it in either direction.
	if _, err := ScoreCase("r", "c", ArmRuleT, a, ""); err == nil {
		t.Error("T without a stage-B output must be refused, not scored as G")
	}
	if _, err := ScoreCase("r", "c", ArmRuleG, a, b); err == nil {
		t.Error("G with a stage-B output must be refused")
	}
}

// The rule is pinned by the hash of its text. When the miner checkout is
// beside this repository, the text must still be the pre-registration's
// section 5 byte for byte; otherwise a verdict would carry a hash of
// wording nobody registered.
func TestRuleTextMatchesPreregistration(t *testing.T) {
	raw, err := os.ReadFile("/home/alex/repos/miner/docs/h1-preregistration.md")
	if err != nil {
		t.Skip("miner checkout not present; cannot cross-check RuleText")
	}
	if !strings.Contains(string(raw), RuleText) {
		t.Errorf("RuleText is not a verbatim section of the pre-registration; the rule moved or the transcription did")
	}
	if len(RuleTextSHA256()) != 64 {
		t.Error("rule hash malformed")
	}
}

// A6: risk_score = max severity-weight x confidence. Under T over the
// survivors; risk_score_all_discovered over every discovery finding.
func TestA6RiskScore(t *testing.T) {
	dir := t.TempDir()
	a := h1ReviewADoc(h1Finding_("f1", "critical", 0.5), h1Finding_("f2", "low", 1.0), h1Finding_("f3", "high", 0.8))
	v, err := ScoreCase("r", "c", ArmRuleG, writeDoc(t, dir, "a.json", a), "")
	if err != nil {
		t.Fatal(err)
	}
	near := func(got *float64, want float64) bool { return got != nil && math.Abs(*got-want) < 1e-9 }
	if !near(v.RiskScore, 0.6) || !near(v.RiskScoreAllDiscovered, 0.6) { // high 0.75 x 0.8
		t.Errorf("G: %v / %v", *v.RiskScore, *v.RiskScoreAllDiscovered)
	}
	b := h1ReviewBDoc(h1Assessment_("f1", "CONFIRMED", "t/x"), h1Assessment_("f3", "REJECTED"))
	v, err = ScoreCase("r", "c", ArmRuleT, writeDoc(t, dir, "a2.json", a), writeDoc(t, dir, "b.json", b))
	if err != nil {
		t.Fatal(err)
	}
	if !near(v.RiskScore, 0.5) || !near(v.RiskScoreAllDiscovered, 0.6) { // survivor f1: critical 1.0 x 0.5
		t.Errorf("T: %v / %v", *v.RiskScore, *v.RiskScoreAllDiscovered)
	}
	empty, _ := ScoreCase("r", "c", ArmRuleG, writeDoc(t, dir, "e.json", h1ReviewADoc()), "")
	if *empty.RiskScore != 0 {
		t.Errorf("empty: %v", *empty.RiskScore)
	}
}
