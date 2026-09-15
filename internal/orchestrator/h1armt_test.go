package orchestrator

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/axlev/engine-runner/internal/adapters"
)

const lensBegin, lensEnd = "<!-- LENSES:BEGIN -->", "<!-- LENSES:END -->"

// stripLensSlot returns T's discovery prompt with the owner-authored block
// removed, and the block itself.
func stripLensSlot(t *testing.T, p string) (rest, slot string) {
	t.Helper()
	b, e := strings.Index(p, lensBegin), strings.Index(p, lensEnd)
	if b < 0 || e < 0 || e < b {
		t.Fatalf("lens slot markers missing or misordered (begin=%d end=%d)", b, e)
	}
	if strings.Count(p, lensBegin) != 1 || strings.Count(p, lensEnd) != 1 {
		t.Fatal("lens slot markers must appear exactly once")
	}
	end := e + len(lensEnd)
	// The slot owns one trailing blank line so removing it leaves G's
	// spacing intact.
	if strings.HasPrefix(p[end:], "\n\n") {
		end += 2
	}
	return p[:b] + p[end:], p[b:end]
}

// Brief s10.5: "T = G + the slot filled. Same file minus one section, so the
// diff between the two prompts IS the hypothesis."
func TestArmTIsArmGPlusTheLensSlot(t *testing.T) {
	g := readPrompt(t, "prompts/h1-g-v1/reasoner-1.md")
	tp := readPrompt(t, "prompts/h1-t-v1/reasoner-1.md")
	rest, _ := stripLensSlot(t, tp)
	if rest != g {
		t.Errorf("arm T outside the lens slot is not byte-identical to arm G")
	}
	// The slot sits at the declared seam: after Scope, before the finding
	// definition, so the lenses direct discovery without altering the
	// shared output contract that follows.
	if strings.Index(tp, "## Scope") > strings.Index(tp, lensBegin) || strings.Index(tp, lensEnd) > strings.Index(tp, "## What counts as a finding") {
		t.Error("lens slot is not between Scope and What counts as a finding")
	}
	checkH1DiscoveryPrompt(t, "prompts/h1-t-v1/reasoner-1.md")
}

// Brief s3: five lenses, each mapped to its s7 class as a heading.
func TestArmTLensSlotHasTheFiveLenses(t *testing.T) {
	_, slot := stripLensSlot(t, readPrompt(t, "prompts/h1-t-v1/reasoner-1.md"))
	for _, want := range []string{
		"### State and ordering (`ordering-state-machine`)",
		"### Invariants across modules (`cross-module-invariant`)",
		"### Configuration surfaces (`config-interaction`)",
		"### Error and lifecycle paths (`error-path-lifecycle`)",
		"### Restart and upgrade (`restart-upgrade`)",
	} {
		if !strings.Contains(slot, want) {
			t.Errorf("lens slot lacks heading %q", want)
		}
	}
	// Brief s3 last paragraph: no thin sixth lens for the unlensed classes.
	for _, bad := range []string{"`timing-race`", "`resource-exhaustion`"} {
		if strings.Contains(slot, bad) {
			t.Errorf("lens slot directs at %s, which the brief says not to lens", bad)
		}
	}
}

// The slot is owner-authored. While placeholders remain, T is a scaffold
// and this test says so rather than passing silently; once filled, every
// lens must carry real text.
func TestArmTLensSlotIsFilled(t *testing.T) {
	_, slot := stripLensSlot(t, readPrompt(t, "prompts/h1-t-v1/reasoner-1.md"))
	if strings.Contains(slot, "<!-- owner:") {
		t.Skip("arm T lens slot still holds owner placeholders; T is a scaffold and must not run on a paid arm yet")
	}
	sections := regexp.MustCompile(`(?m)^### `).Split(slot, -1)[1:]
	for _, s := range sections {
		body := strings.TrimSpace(strings.SplitN(s, "\n", 2)[1])
		if len(body) < 80 {
			t.Errorf("lens %q has no substantive text", strings.SplitN(s, "\n", 2)[0])
		}
	}
}

func TestArmTSubstantiationPromptMeetsTheBrief(t *testing.T) {
	p := readPrompt(t, "prompts/h1-t-v1/reasoner-2.md")
	if !strings.Contains(p, briefTestsStep) {
		t.Error("brief s4 tests step is not present verbatim")
	}
	for _, want := range []string{
		"/workspace/input/handoff/review-a.json",
		`"discovery_mechanism"`, `"recommended_validation"`, `"notes"`,
		"does not become a finding", "copied verbatim", "consequence if real",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("substantiation prompt lacks %q", want)
		}
	}
	if regexp.MustCompile(`(?i)favou?r recall`).MatchString(p) {
		t.Error("substantiation prompt claims discovery favoured recall; under H1 both arms are calibrated")
	}
	if strings.Contains(p, "narrowed_description") || strings.Contains(p, "proposed_test") {
		t.Error("substantiation prompt uses pilot-v1 field names")
	}
}

func TestArmTProtocolAndArmFileLoad(t *testing.T) {
	p, err := LoadProtocol(filepath.Join(repoRoot, "configs", "protocols", "h1-t-v1.yaml"))
	if err != nil {
		t.Fatalf("LoadProtocol: %v", err)
	}
	if got := p.Order(); len(got) != 2 || got[0] != "reasoner-1" || got[1] != "reasoner-2" {
		t.Fatalf("order = %v", got)
	}
	grants := p.HandoffsFor("reasoner-2")
	if len(grants) != 1 || grants[0].From != "reasoner-1" || grants[0].As != "review-a.json" {
		t.Errorf("reasoner-2 grants = %+v", grants)
	}
	if len(p.HandoffsFor("reasoner-1")) != 0 {
		t.Error("reasoner-1 must receive nothing")
	}

	set, err := LoadAgentSet(filepath.Join(repoRoot, "configs", "agents", "h1-opus-wide.yaml"))
	if err != nil {
		t.Fatalf("LoadAgentSet: %v", err)
	}
	for _, stage := range []string{"reasoner-1", "reasoner-2"} {
		cfg := set[adapters.Stage(stage)]
		if cfg.Model != "claude-opus-5" || cfg.Adapter != "claude" {
			t.Errorf("%s bound to %s/%s", stage, cfg.Adapter, cfg.Model)
		}
		if strings.Join(cfg.Tools, ",") != "Read,Grep,Glob" {
			t.Errorf("%s tools = %v", stage, cfg.Tools)
		}
	}
	if set["reasoner-2"].Budget.MaxCostUSD != 9.00 || set["reasoner-1"].Budget.MaxCostUSD != 6.00 {
		t.Errorf("budgets not the opus-wide caps: r1=%v r2=%v", set["reasoner-1"].Budget.MaxCostUSD, set["reasoner-2"].Budget.MaxCostUSD)
	}

	// T, G and pilot-v1 are three different experiments.
	fpT, err := computeStaticFingerprints(filepath.Join(repoRoot, "configs", "protocols", "h1-t-v1.yaml"), fixtureAgentsDir, repoRoot, happyPathBundle, p, loadFixtureAgents(t))
	if err != nil {
		t.Fatal(err)
	}
	g, _ := LoadProtocol(filepath.Join(repoRoot, "configs", "protocols", "h1-g-v1.yaml"))
	fpG, err := computeStaticFingerprints(filepath.Join(repoRoot, "configs", "protocols", "h1-g-v1.yaml"), fixtureAgentsDir, repoRoot, happyPathBundle, g, loadFixtureAgents(t))
	if err != nil {
		t.Fatal(err)
	}
	if fpT.ProtocolHash == fpG.ProtocolHash || fpT.ProtocolHash == pilotV1ProtocolHashBefore {
		t.Error("h1-t-v1 must fingerprint apart from h1-g-v1 and pilot-v1")
	}
	if len(fpT.PromptHashes) != 2 {
		t.Errorf("h1-t-v1 fingerprinted %d prompts, want 2", len(fpT.PromptHashes))
	}
}

func TestArmTRunsEndToEndOnFixtures(t *testing.T) {
	adapter := newFixtureAdapter(t)
	o := newOrchFor(t, filepath.Join(repoRoot, "configs", "protocols", "h1-t-v1.yaml"), adapter)
	outcome, err := o.Run(context.Background(), "run-t", "h1-two-stage", happyPathBundle)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if outcome.Status != "completed" || outcome.ReviewAPath == "" || outcome.ReviewBPath == "" || outcome.ReviewCPath != "" {
		t.Fatalf("status %q A=%q B=%q C=%q", outcome.Status, outcome.ReviewAPath, outcome.ReviewBPath, outcome.ReviewCPath)
	}
}
