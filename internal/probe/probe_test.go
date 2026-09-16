package probe

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axlev/engine-runner/internal/contamscan"
)

const fixSHA = "0123456789abcdef0123456789abcdef01234567"

func keys() contamscan.Keys {
	return contamscan.Keys{
		SchemaVersion:   contamscan.KeysSchemaVersion,
		CaseID:          "c1",
		FixingSHAs:      []string{fixSHA},
		FixingPRNumbers: []int{4242},
		CVEIDs:          []string{"CVE-2026-0001"},
	}
}

func reportWith(answers ...string) Report {
	r := Report{SchemaVersion: ReportSchemaVersion, CaseID: "c1"}
	names := []string{ProbeA, ProbeB}
	for i, a := range answers {
		r.Responses = append(r.Responses, Response{Probe: names[i%2], Answer: a})
	}
	return r
}

// The probe prompts are the registered instrument: they must ask for memory
// and must not invite analysis or name the case.
func TestPromptsAskForMemoryAndNameNothing(t *testing.T) {
	for name, p := range map[string]string{ProbeA: PromptA, ProbeB: PromptB} {
		if !strings.Contains(p, "from memory") {
			t.Errorf("prompt %s does not ask for memory", name)
		}
		if !strings.Contains(p, "I do not recognise this") {
			t.Errorf("prompt %s does not make a miss an acceptable answer", name)
		}
		if !strings.Contains(p, "Do not speculate") {
			t.Errorf("prompt %s does not forbid speculation", name)
		}
		for _, bad := range []string{"CVE-2", "FRR", "frrouting", "bgpd", "ospf"} {
			if strings.Contains(strings.ToLower(p), strings.ToLower(bad)) {
				t.Errorf("prompt %s names %q; the prompt must not identify the case", name, bad)
			}
		}
	}
	// Probe B must forbid analysis - the registered method is explicit
	// that the question is memory, not deduction.
	if !strings.Contains(PromptB, "must not analyse the code") {
		t.Error("probe B does not forbid analysing the code")
	}
	if !strings.Contains(PromptB, "blob hashes") {
		t.Error("probe B does not show blob hashes, which are a recognition signal")
	}
	h := PromptHashes()
	if len(h[ProbeA]) != 64 || len(h[ProbeB]) != 64 || h[ProbeA] == h[ProbeB] {
		t.Errorf("prompt hashes malformed: %v", h)
	}
}

func TestPromptForBuildsAndRefusesEmptyInput(t *testing.T) {
	in := Input{CaseID: "c1", ProbeASymptom: "the daemon spins after a peer flap"}
	a, err := PromptFor(ProbeA, in, "")
	if err != nil || !strings.HasSuffix(strings.TrimSpace(a), "the daemon spins after a peer flap") {
		t.Errorf("probe A prompt: %v / %q", err, a)
	}
	b, err := PromptFor(ProbeB, in, "diff --git a/x b/x\nindex abc..def\n")
	if err != nil || !strings.Contains(b, "index abc..def") {
		t.Errorf("probe B must carry the diff verbatim: %v", err)
	}
	if _, err := PromptFor(ProbeB, in, ""); err == nil {
		t.Error("probe B with no diff must fail")
	}
	if _, err := PromptFor(ProbeA, Input{CaseID: "c1"}, ""); err == nil {
		t.Error("probe A with no symptom must fail")
	}
	if _, err := PromptFor("C", in, "d"); err == nil {
		t.Error("unknown probe must fail")
	}
}

func TestScanVoidsOnIdentifiersInTheAnswer(t *testing.T) {
	cases := map[string]struct {
		answer string
		void   bool
	}{
		"a clean miss":   {"I do not recognise this change.", false},
		"names the sha":  {"I think this was fixed in " + fixSHA[:9], true},
		"names the pr":   {"Addressed in #4242 later that year.", true},
		"names a cve":    {"This resembles CVE-2020-1234.", true},
		"mechanism only": {"I recall the loop bound being corrected to exclusive.", false},
	}
	for name, c := range cases {
		r := reportWith(c.answer)
		r.Scan(keys(), "sum")
		if r.Void != c.void {
			t.Errorf("%s: void=%v want %v (hits %+v)", name, r.Void, c.void, r.MechanicalHits)
		}
		if r.Void && r.VoidReason != "mechanical" {
			t.Errorf("%s: reason %q", name, r.VoidReason)
		}
		if r.KeysSHA256 != "sum" {
			t.Errorf("%s: keys sha not recorded", name)
		}
	}
}

// The decisive property: a clean mechanical scan with no evaluator reading
// is UNRESOLVED, never clean. Mechanism-level recall is invisible to the
// rules, so scoring on their silence is the failure A11 exists to prevent.
func TestDecideTreatsAnUnreadCleanScanAsUnresolved(t *testing.T) {
	clean := reportWith("I do not recognise this change.")
	clean.Scan(keys(), "sum")

	d := Decide(clean, Verdict{}, false)
	if d.Void || !d.Unresolved {
		t.Errorf("no verdict yet must be unresolved, not clean: %+v", d)
	}
	d = Decide(clean, Verdict{Hit: false, Reason: "read, recalls nothing"}, true)
	if d.Void || d.Unresolved {
		t.Errorf("evaluator cleared it: %+v", d)
	}
	d = Decide(clean, Verdict{Hit: true, Reason: "names the mechanism exactly"}, true)
	if !d.Void || !strings.Contains(d.Reason, "evaluator") {
		t.Errorf("evaluator hit must void: %+v", d)
	}

	hit := reportWith("fixed in " + fixSHA)
	hit.Scan(keys(), "sum")
	d = Decide(hit, Verdict{Hit: false}, true)
	if !d.Void || !strings.Contains(d.Reason, "mechanical") {
		t.Errorf("a mechanical hit voids even if the evaluator disagrees: %+v", d)
	}
}

func TestLoadInputAndVerdictsValidate(t *testing.T) {
	dir := t.TempDir()
	w := func(name, body string) string {
		p := filepath.Join(dir, name)
		_ = os.WriteFile(p, []byte(body), 0o644)
		return p
	}
	good := w("c1.json", `{"schema_version":"contamination-probe-input/v1","case_id":"c1","probe_a_symptom":"spins"}`)
	if in, err := LoadInput(good); err != nil || in.CaseID != "c1" {
		t.Fatalf("%v", err)
	}
	for _, bad := range []string{
		`{"schema_version":"contamination-probe-input/v2","case_id":"c1","probe_a_symptom":"s"}`,
		`{"schema_version":"contamination-probe-input/v1","case_id":"c1"}`,
		`{"schema_version":"contamination-probe-input/v1","probe_a_symptom":"s"}`,
	} {
		if _, err := LoadInput(w("bad.json", bad)); err == nil {
			t.Errorf("expected refusal for %s", bad)
		}
	}

	vdir := t.TempDir()
	vb, _ := json.Marshal(Verdict{SchemaVersion: VerdictSchemaVersion, CaseID: "c1", Hit: true, Reason: "recalls the fix"})
	_ = os.WriteFile(filepath.Join(vdir, "c1.json"), vb, 0o644)
	vs, err := LoadVerdicts(vdir, []string{"c1", "c2"})
	if err != nil || len(vs) != 1 || !vs["c1"].Hit {
		t.Fatalf("%v %+v", err, vs)
	}
	if _, ok := vs["c2"]; ok {
		t.Error("a missing verdict must be absent, not fabricated")
	}
}
