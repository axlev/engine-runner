package orchestrator

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/axlev/engine-runner/internal/adapters"
	"github.com/axlev/engine-runner/internal/contextbuilder"
)

// writeProtocol materialises a manifest so that LoadProtocol's validation
// and resolution actually run on it. Tests must not construct a Protocol
// struct by hand and expect resolved order or grants: those exist only
// after load.
func writeProtocol(t *testing.T, text string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "protocol.yaml")
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatalf("writing protocol: %v", err)
	}
	return p
}

const declaredHeader = `protocol_version: test-v1
retry_policy:
  max_attempts: 1
`

// pilot-v1's file predates stage_order and must never be edited. An
// unchanged file has to resolve to exactly what the hardcoded pipeline did:
// three stages in the original order, reasoner-2 receiving review-a,
// reasoner-3 receiving review-a and review-b.
func TestPilotV1ResolvesToTheLegacyOrderAndHandoffs(t *testing.T) {
	p := loadPilotV1(t)
	if p.StageOrder != nil {
		t.Fatalf("pilot-v1.yaml must not declare stage_order (the file must stay unchanged), got %v", p.StageOrder)
	}
	wantOrder := []adapters.Stage{adapters.StageReasoner1, adapters.StageReasoner2, adapters.StageReasoner3}
	if got := p.Order(); len(got) != 3 || got[0] != wantOrder[0] || got[1] != wantOrder[1] || got[2] != wantOrder[2] {
		t.Errorf("Order() = %v, want %v", got, wantOrder)
	}
	if g := p.HandoffsFor(adapters.StageReasoner1); len(g) != 0 {
		t.Errorf("reasoner-1 must receive nothing, got %v", g)
	}
	wantB := []contextbuilder.HandoffGrant{{From: adapters.StageReasoner1, As: "review-a.json"}}
	if g := p.HandoffsFor(adapters.StageReasoner2); len(g) != 1 || g[0] != wantB[0] {
		t.Errorf("reasoner-2 grants = %v, want %v", g, wantB)
	}
	wantC := []contextbuilder.HandoffGrant{
		{From: adapters.StageReasoner1, As: "review-a.json"},
		{From: adapters.StageReasoner2, As: "review-b.json"},
	}
	if g := p.HandoffsFor(adapters.StageReasoner3); len(g) != 2 || g[0] != wantC[0] || g[1] != wantC[1] {
		t.Errorf("reasoner-3 grants = %v, want %v", g, wantC)
	}
}

// The legacy switch still means what it meant: with enable_a_to_b false,
// reasoner-2 receives nothing and reasoner-3 receives review-b only.
func TestLegacyDisabledHandoffResolvesToReviewBOnly(t *testing.T) {
	src, err := os.ReadFile(pilotV1Path)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(string(src), "enable_a_to_b: true", "enable_a_to_b: false", 1)
	p, err := LoadProtocol(writeProtocol(t, text))
	if err != nil {
		t.Fatalf("LoadProtocol: %v", err)
	}
	if g := p.HandoffsFor(adapters.StageReasoner2); len(g) != 0 {
		t.Errorf("reasoner-2 must receive nothing when a_to_b is disabled, got %v", g)
	}
	if g := p.HandoffsFor(adapters.StageReasoner3); len(g) != 1 || g[0].From != adapters.StageReasoner2 {
		t.Errorf("reasoner-3 must receive review-b only, got %v", g)
	}
}

func TestDeclaredOneStageProtocolLoads(t *testing.T) {
	p, err := LoadProtocol(filepath.Join(repoRoot, "fixtures", "protocols", "single-stage.yaml"))
	if err != nil {
		t.Fatalf("LoadProtocol: %v", err)
	}
	if got := p.Order(); len(got) != 1 || got[0] != adapters.StageReasoner1 {
		t.Errorf("Order() = %v, want [reasoner-1]", got)
	}
	if g := p.HandoffsFor(adapters.StageReasoner1); len(g) != 0 {
		t.Errorf("a single stage receives nothing, got %v", g)
	}
}

func TestDeclaredTwoStageProtocolLoadsWithExplicitHandoff(t *testing.T) {
	p, err := LoadProtocol(filepath.Join(repoRoot, "fixtures", "protocols", "two-stage-a-to-b.yaml"))
	if err != nil {
		t.Fatalf("LoadProtocol: %v", err)
	}
	if got := p.Order(); len(got) != 2 || got[1] != adapters.StageReasoner2 {
		t.Errorf("Order() = %v, want [reasoner-1 reasoner-2]", got)
	}
	g := p.HandoffsFor(adapters.StageReasoner2)
	if len(g) != 1 || g[0] != (contextbuilder.HandoffGrant{From: adapters.StageReasoner1, As: "review-a.json"}) {
		t.Errorf("reasoner-2 grants = %v, want review-a from reasoner-1", g)
	}
	if g := p.HandoffsFor(adapters.StageReasoner3); len(g) != 0 {
		t.Errorf("an undeclared stage has no grants, got %v", g)
	}
}

// Every fail-closed case rejects at load and names the stage, so a broken
// manifest is a construction-time diagnosis rather than a paid discovery.
func TestProtocolFailsClosed(t *testing.T) {
	stage := func(name string) string {
		return "  " + name + ":\n    prompt: fixtures/prompts/placeholder.md\n    output_schema: schemas/review-a.schema.json\n"
	}
	cases := []struct {
		name, text, wantSubstr string
	}{
		{"empty stage_order", declaredHeader + "stage_order: []\nstages:\n" + stage("reasoner-1"), "stage_order is empty"},
		{"unknown stage in order", declaredHeader + "stage_order: [reasoner-9]\nstages:\n" + stage("reasoner-9"), `"reasoner-9"`},
		{"duplicate in order", declaredHeader + "stage_order: [reasoner-1, reasoner-1]\nstages:\n" + stage("reasoner-1"), "twice"},
		{"ordered but not declared", declaredHeader + "stage_order: [reasoner-1, reasoner-2]\nstages:\n" + stage("reasoner-1"), `"reasoner-2"`},
		{"declared but not ordered", declaredHeader + "stage_order: [reasoner-1]\nstages:\n" + stage("reasoner-1") + stage("reasoner-2"), `"reasoner-2" is declared under stages but absent from stage_order`},
		{"missing prompt", declaredHeader + "stage_order: [reasoner-1]\nstages:\n  reasoner-1:\n    output_schema: schemas/review-a.schema.json\n", `"reasoner-1" missing prompt`},
		{"missing schema", declaredHeader + "stage_order: [reasoner-1]\nstages:\n  reasoner-1:\n    prompt: fixtures/prompts/placeholder.md\n", `"reasoner-1" missing output_schema`},
		{"receives from undeclared", declaredHeader + "stage_order: [reasoner-1, reasoner-2]\nstages:\n" + stage("reasoner-1") +
			"  reasoner-2:\n    prompt: fixtures/prompts/placeholder.md\n    output_schema: schemas/review-b.schema.json\n    receives:\n      - from: reasoner-3\n        as: x.json\n", `receives from "reasoner-3", which is not declared`},
		{"receives from a later stage", declaredHeader + "stage_order: [reasoner-1, reasoner-2]\nstages:\n" +
			"  reasoner-1:\n    prompt: fixtures/prompts/placeholder.md\n    output_schema: schemas/review-a.schema.json\n    receives:\n      - from: reasoner-2\n        as: x.json\n" + stage("reasoner-2"), `"reasoner-1" receives from "reasoner-2", which does not run before it`},
		{"receives from itself", declaredHeader + "stage_order: [reasoner-1]\nstages:\n" +
			"  reasoner-1:\n    prompt: fixtures/prompts/placeholder.md\n    output_schema: schemas/review-a.schema.json\n    receives:\n      - from: reasoner-1\n        as: x.json\n", "does not run before it"},
		{"receives without stage_order", "protocol_version: t\nretry_policy:\n  max_attempts: 1\nstages:\n" +
			stage("reasoner-1") + "  reasoner-2:\n    prompt: p\n    output_schema: s\n    receives:\n      - from: reasoner-1\n        as: x.json\n" + stage("reasoner-3"), `"reasoner-2" declares receives but the protocol has no stage_order`},
		{"enable_a_to_b alongside stage_order", declaredHeader + "handoffs:\n  enable_a_to_b: true\nstage_order: [reasoner-1]\nstages:\n" + stage("reasoner-1"), "enable_a_to_b has no meaning when stage_order is declared"},
		{"handoff name with a path", declaredHeader + "stage_order: [reasoner-1, reasoner-2]\nstages:\n" + stage("reasoner-1") +
			"  reasoner-2:\n    prompt: p\n    output_schema: s\n    receives:\n      - from: reasoner-1\n        as: ../escape.json\n", "bare file name"},
		{"duplicate handoff name", declaredHeader + "stage_order: [reasoner-1, reasoner-2, reasoner-3]\nstages:\n" + stage("reasoner-1") + stage("reasoner-2") +
			"  reasoner-3:\n    prompt: p\n    output_schema: s\n    receives:\n      - from: reasoner-1\n        as: same.json\n      - from: reasoner-2\n        as: same.json\n", `two handoffs as "same.json"`},
		{"legacy missing stage still rejected", "protocol_version: t\nretry_policy:\n  max_attempts: 1\nstages:\n" + stage("reasoner-1") + stage("reasoner-2"), `missing stage "reasoner-3"`},
		{"unknown stage key in legacy", "protocol_version: t\nretry_policy:\n  max_attempts: 1\nstages:\n" + stage("reasoner-1") + stage("reasoner-2") + stage("reasoner-3") + stage("reasoner-4"), `"reasoner-4" is not a stage this engine knows`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := LoadProtocol(writeProtocol(t, c.text))
			if err == nil {
				t.Fatalf("expected a load error")
			}
			if !strings.Contains(err.Error(), c.wantSubstr) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), c.wantSubstr)
			}
		})
	}
}
