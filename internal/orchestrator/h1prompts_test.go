package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// docs/h1-treatment-prompt-brief.md section 6: "This paragraph goes into
// G's prompt word for word." Section 4 is the tests step, also shared. Both
// are pinned here as the brief states them, so a prompt edit that drifts
// either one fails a test rather than quietly changing the experiment.
const briefCalibrationParagraph = `A change with no system-level risk is a normal result. Roughly half the
changes you review will be clean. Do not manufacture a finding to have
something to report; an empty finding list with a stated reason ("the
change is confined to X, touches no shared state, no config surface, no
lifecycle path") is a complete review.`

const briefTestsStep = `For each finding, search ` + "`tests/topotests`" + ` and the daemon's ` + "`tests/`" + `
directory for a test that exercises the path you have identified. Use
Grep on the function names and config commands involved. If a test
exists, name its path and say whether it would catch this defect as
written. If none exists, say so, and describe the scenario a test would
need to construct.`

// The s7 enum, as the reviewer must see it (brief s10 decision 3).
var preregClasses = []string{
	"ordering-state-machine", "cross-module-invariant", "config-interaction",
	"error-path-lifecycle", "restart-upgrade", "protocol-parsing",
	"classical-memory-safety", "timing-race", "resource-exhaustion", "other",
}

func readPrompt(t *testing.T, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot, rel))
	if err != nil {
		t.Fatalf("reading %s: %v", rel, err)
	}
	return string(raw)
}

// checkH1DiscoveryPrompt holds everything the brief requires of a discovery
// prompt in EITHER arm. T's scaffold runs through the same function.
func checkH1DiscoveryPrompt(t *testing.T, rel string) {
	t.Helper()
	p := readPrompt(t, rel)

	if !strings.Contains(p, briefCalibrationParagraph) {
		t.Errorf("%s: brief s6 calibration paragraph is not present verbatim", rel)
	}
	if !strings.Contains(p, briefTestsStep) {
		t.Errorf("%s: brief s4 tests step is not present verbatim", rel)
	}
	for _, c := range preregClasses {
		if !strings.Contains(p, "`"+c+"`") {
			t.Errorf("%s: s7 class %q is not shown to the reviewer", rel, c)
		}
	}
	for _, field := range []string{`"mechanism"`, `"class"`, `"severity"`, `"confidence"`, `"file"`, `"line"`, `"recommended_validation"`, `"empty_reason"`} {
		if !strings.Contains(p, field) {
			t.Errorf("%s: output contract lacks %s", rel, field)
		}
	}

	// Pre-registration s4: G has no domain content, and T's domain content
	// is the lens slot alone. A positive example of a finding is a hint
	// toward a class - the first draft's four examples paraphrased the
	// brief's pilot evidence for lenses 1-4 - so the finding definition
	// may carry only the negative list. Enforced on both arms: T's
	// direction lives in its slot, not here.
	if strings.Contains(p, "These are findings") {
		t.Errorf("%s: carries positive finding examples; the finding definition may only say what is NOT a finding", rel)
	}
	sec := p[strings.Index(p, "## What counts as a finding"):]
	sec = sec[:strings.Index(sec, "\n## ")]
	allowedBullets := map[string]bool{
		`- "consider adding error handling" with no path that fails`:         true,
		`- any style, naming, or readability preference`:                     true,
		`- "add tests for this" as a standalone item`:                        true,
		`- "this might have performance implications" with no specific cost`: true,
		`- restating what the diff does`:                                     true,
	}
	for _, line := range strings.Split(sec, "\n") {
		if strings.HasPrefix(line, "- ") && !allowedBullets[line] {
			t.Errorf("%s: bullet under What counts as a finding is not in the negative list: %q", rel, line)
		}
	}
	// Brief s3 lens 5 in four words does not belong in the shared text.
	if strings.Contains(strings.ToLower(p[:strings.Index(p, "## What you can read")]), "restart") {
		t.Errorf("%s: preamble names restart, which is lens 5", rel)
	}

	// Brief s2: the recall instruction is the confound G exists to remove.
	if regexp.MustCompile(`(?i)favou?r recall`).MatchString(p) {
		t.Errorf("%s: instructs the reviewer to favour recall", rel)
	}
	// Brief s7: no invitation to remember; no historical identifiers.
	for _, bad := range []*regexp.Regexp{
		regexp.MustCompile(`(?i)have you seen`),
		regexp.MustCompile(`(?i)what happened to this code later`),
		regexp.MustCompile(`(?i)\bCVE-\d`),
		regexp.MustCompile(`(?i)\bPR ?#\d`),
		regexp.MustCompile(`\b[0-9a-f]{12,40}\b`),
	} {
		if bad.MatchString(p) {
			t.Errorf("%s: matches prohibited pattern %s", rel, bad)
		}
	}
	// Brief s7: no reliance on a PR description. The word may appear only
	// to say one may be absent.
	if strings.Contains(p, "the PR description") || strings.Contains(p, "pull request description") {
		t.Errorf("%s: assumes a PR description exists", rel)
	}
	// Brief s5: ranking rule and severity-by-consequence must be stated.
	if !strings.Contains(p, "consequence if real") || !strings.Contains(p, "confidence**") {
		t.Errorf("%s: ranking rule (consequence if real x confidence) not stated", rel)
	}
	if !strings.Contains(p, "consequence at runtime") {
		t.Errorf("%s: severity is not defined by consequence at runtime", rel)
	}
}

func TestArmGPromptMeetsTheBrief(t *testing.T) {
	checkH1DiscoveryPrompt(t, "prompts/h1-g-v1/reasoner-1.md")

	// Brief s3 / s10.2: G has NO lenses. Its section headings are the
	// generic reviewer's; none of the five lens names appears as a heading.
	p := readPrompt(t, "prompts/h1-g-v1/reasoner-1.md")
	for _, lens := range []string{"State and ordering", "Invariants across modules", "Configuration surfaces", "Error and lifecycle paths", "Restart and upgrade"} {
		if regexp.MustCompile(`(?m)^#+ .*` + regexp.QuoteMeta(lens)).MatchString(p) {
			t.Errorf("arm G carries lens heading %q; the lenses are the treatment", lens)
		}
	}
	if strings.Contains(p, "LENSES") {
		t.Error("arm G carries the lens slot marker")
	}
}

func TestArmGProtocolLoadsAndFingerprintsApartFromPilot(t *testing.T) {
	g, err := LoadProtocol(filepath.Join(repoRoot, "configs", "protocols", "h1-g-v1.yaml"))
	if err != nil {
		t.Fatalf("LoadProtocol: %v", err)
	}
	if got := g.Order(); len(got) != 1 || got[0] != "reasoner-1" {
		t.Fatalf("h1-g-v1 order = %v, want [reasoner-1]", got)
	}
	if g.Stages["reasoner-1"].OutputSchema != "schemas/h1-review-a.schema.json" {
		t.Errorf("h1-g-v1 reasoner-1 schema = %q", g.Stages["reasoner-1"].OutputSchema)
	}
	fp, err := computeStaticFingerprints(filepath.Join(repoRoot, "configs", "protocols", "h1-g-v1.yaml"),
		fixtureAgentsDir, repoRoot, happyPathBundle, g, loadFixtureAgents(t))
	if err != nil {
		t.Fatalf("computeStaticFingerprints: %v", err)
	}
	if fp.ProtocolHash == pilotV1ProtocolHashBefore {
		t.Error("h1-g-v1 hashes identically to pilot-v1")
	}
	if len(fp.PromptHashes) != 1 || len(fp.SchemaVersions) != 1 {
		t.Errorf("h1-g-v1 must fingerprint exactly one prompt and one schema, got %d/%d", len(fp.PromptHashes), len(fp.SchemaVersions))
	}
	if _, ok := fp.SchemaVersions["schemas/h1-review-a.schema.json"]; !ok {
		t.Errorf("h1-g-v1 fingerprint lacks its schema: %v", fp.SchemaVersions)
	}
}

func TestArmGRunsEndToEndOnFixtures(t *testing.T) {
	for _, caseID := range []string{"h1-single-stage", "h1-clean"} {
		adapter := newFixtureAdapter(t)
		o := newOrchFor(t, filepath.Join(repoRoot, "configs", "protocols", "h1-g-v1.yaml"), adapter)
		outcome, err := o.Run(context.Background(), "run-g-"+caseID, caseID, happyPathBundle)
		if err != nil {
			t.Fatalf("%s: %v", caseID, err)
		}
		if outcome.Status != "completed" || outcome.ReviewAPath == "" || outcome.ReviewBPath != "" {
			t.Fatalf("%s: status %q A=%q B=%q", caseID, outcome.Status, outcome.ReviewAPath, outcome.ReviewBPath)
		}
	}
}
