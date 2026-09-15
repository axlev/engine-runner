package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/axlev/engine-runner/internal/adapters"
	"github.com/axlev/engine-runner/internal/adapters/fixture"
)

// newOrchFor wires an orchestrator over an arbitrary protocol FILE, so the
// fingerprint hashes that file rather than pilot-v1's.
func newOrchFor(t *testing.T, protocolPath string, adapter adapters.AgentAdapter) *Orchestrator {
	t.Helper()
	p, err := LoadProtocol(protocolPath)
	if err != nil {
		t.Fatalf("LoadProtocol(%q): %v", protocolPath, err)
	}
	o, err := New(Options{
		RepoRoot:      repoRoot,
		ProtocolPath:  protocolPath,
		Protocol:      p,
		AgentSetPath:  fixtureAgentsDir,
		AgentSet:      loadFixtureAgents(t),
		Adapters:      map[string]adapters.AgentAdapter{"fixture": adapter},
		WorkspaceRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return o
}

// These constants were captured from a pilot-v1 fixture run BEFORE the stage
// list became protocol-declared (run m2-before, 2026-09-15). pilot-v1's
// file, prompts and schemas are untouched by that change, so its fingerprint
// must not move: a moved value here means a sealed cohort can no longer be
// matched to the protocol it ran under.
const (
	pilotV1ProtocolHashBefore = "b78e7878c0116801431664b110ce4c8006d84e72a6fa33aa00b9d6f3a542ff09"
	pilotV1PromptR1Before     = "6484c249b4e2fa21219921b074df108cdc23b645997136c064cc02ea93e9c434"
	pilotV1PromptR2Before     = "c1e3a9c2a1d3e06f4229fbad1645dc308ed04ec383cd920e50c8f97a1a9ddf1a"
	pilotV1PromptR3Before     = "3d9d146e5bdbfe80c0d9445aef3ea1ea3ccbfe47d5df5b17483a32256a52c916"
	pilotV1SchemaABefore      = "3cd6d13fc842663748e1f86ad74341e402efc6098cdafaf7c5eb51d1b3602d63"
	pilotV1SchemaBBefore      = "73bc937a894d2d9337bdf947410baabdee766a3e90cb7ad847a3c3f55e07fb8c"
	pilotV1SchemaCBefore      = "3c5ea33fd880b5f972446c39aa0612c0b0f143c325292bf8cdfba434e8904d41"
)

func TestPilotV1FingerprintDidNotMove(t *testing.T) {
	p := loadPilotV1(t)
	fp, err := computeStaticFingerprints(pilotV1Path, fixtureAgentsDir, repoRoot, happyPathBundle, p, loadFixtureAgents(t))
	if err != nil {
		t.Fatalf("computeStaticFingerprints: %v", err)
	}
	if fp.ProtocolHash != pilotV1ProtocolHashBefore {
		t.Errorf("protocol_hash = %s, want %s (pilot-v1.yaml must be byte-identical)", fp.ProtocolHash, pilotV1ProtocolHashBefore)
	}
	for stage, want := range map[string]string{
		"reasoner-1": pilotV1PromptR1Before, "reasoner-2": pilotV1PromptR2Before, "reasoner-3": pilotV1PromptR3Before,
	} {
		if got := fp.PromptHashes[stage]; got != want {
			t.Errorf("prompt_hashes[%s] = %s, want %s", stage, got, want)
		}
	}
	for schema, want := range map[string]string{
		"schemas/review-a.schema.json": pilotV1SchemaABefore,
		"schemas/review-b.schema.json": pilotV1SchemaBBefore,
		"schemas/review-c.schema.json": pilotV1SchemaCBefore,
	} {
		if got := fp.SchemaVersions[schema]; got != want {
			t.Errorf("schema_versions[%s] = %s, want %s", schema, got, want)
		}
	}
	if len(fp.PromptHashes) != 3 || len(fp.ToolSets) != 3 || len(fp.AgentConfigHashes) != 3 {
		t.Errorf("pilot-v1 must fingerprint exactly three stages, got prompts=%d tools=%d agents=%d",
			len(fp.PromptHashes), len(fp.ToolSets), len(fp.AgentConfigHashes))
	}
}

// A one-stage protocol and a two-stage protocol must hash differently from
// each other and from pilot-v1: the declaration is what is fingerprinted.
func TestDeclaredProtocolsHaveDistinctFingerprints(t *testing.T) {
	hashes := map[string]string{}
	for _, name := range []string{"single-stage.yaml", "two-stage-a-to-b.yaml"} {
		path := filepath.Join(repoRoot, "fixtures", "protocols", name)
		p, err := LoadProtocol(path)
		if err != nil {
			t.Fatalf("LoadProtocol(%s): %v", name, err)
		}
		fp, err := computeStaticFingerprints(path, fixtureAgentsDir, repoRoot, happyPathBundle, p, loadFixtureAgents(t))
		if err != nil {
			t.Fatalf("fingerprints(%s): %v", name, err)
		}
		hashes[name] = fp.ProtocolHash
		if len(fp.PromptHashes) != len(p.Order()) {
			t.Errorf("%s: fingerprinted %d prompts for %d declared stages", name, len(fp.PromptHashes), len(p.Order()))
		}
	}
	if hashes["single-stage.yaml"] == hashes["two-stage-a-to-b.yaml"] || hashes["single-stage.yaml"] == pilotV1ProtocolHashBefore {
		t.Errorf("protocol hashes must be distinct: %v", hashes)
	}
}

func TestOneStageProtocolRunsEndToEnd(t *testing.T) {
	adapter := newFixtureAdapter(t)
	o := newOrchFor(t, filepath.Join(repoRoot, "fixtures", "protocols", "single-stage.yaml"), adapter)

	outcome, err := o.Run(context.Background(), "run-one-stage", "single-stage", happyPathBundle)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if outcome.Status != "completed" {
		t.Fatalf("Status = %q, want completed (%s)", outcome.Status, outcome.FailureReason)
	}
	calls := adapter.(*fixture.FixtureAdapter).Calls()
	if len(calls) != 1 || calls[0].Stage != adapters.StageReasoner1 {
		t.Fatalf("expected exactly one reasoner-1 call, got %d: %+v", len(calls), stagesOf(calls))
	}
	if outcome.ReviewAPath == "" || outcome.ReviewBPath != "" || outcome.ReviewCPath != "" {
		t.Errorf("only review-a should be recorded, got A=%q B=%q C=%q", outcome.ReviewAPath, outcome.ReviewBPath, outcome.ReviewCPath)
	}
	if _, err := os.Stat(filepath.Join(calls[0].WorkspacePath, "input", "handoff")); !os.IsNotExist(err) {
		t.Errorf("a single stage must have no handoff directory, got err=%v", err)
	}
}

func TestTwoStageProtocolHandsOffAAndNeverRunsC(t *testing.T) {
	adapter := newFixtureAdapter(t)
	o := newOrchFor(t, filepath.Join(repoRoot, "fixtures", "protocols", "two-stage-a-to-b.yaml"), adapter)

	outcome, err := o.Run(context.Background(), "run-two-stage", "two-stage", happyPathBundle)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if outcome.Status != "completed" {
		t.Fatalf("Status = %q, want completed (%s)", outcome.Status, outcome.FailureReason)
	}
	calls := adapter.(*fixture.FixtureAdapter).Calls()
	if len(calls) != 2 || calls[0].Stage != adapters.StageReasoner1 || calls[1].Stage != adapters.StageReasoner2 {
		t.Fatalf("expected reasoner-1 then reasoner-2 and nothing else, got %v", stagesOf(calls))
	}
	if _, err := os.Stat(filepath.Join(calls[1].WorkspacePath, "input", "handoff", "review-a.json")); err != nil {
		t.Errorf("reasoner-2 must receive review-a.json under the declared name: %v", err)
	}
	if outcome.ReviewCPath != "" {
		t.Errorf("reasoner-3 never ran, so ReviewCPath must be empty, got %q", outcome.ReviewCPath)
	}
	// The fixture scenario defines no reasoner-3 behaviour, so had the
	// orchestrator reached for a third stage the adapter would have failed
	// loudly rather than the run completing.
}

func stagesOf(calls []adapters.RunRequest) []adapters.Stage {
	var out []adapters.Stage
	for _, c := range calls {
		out = append(out, c.Stage)
	}
	return out
}
