package orchestrator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
)

// Fingerprints is what makes a run reproducible and auditable after the
// fact, per design principle 5: "Every prompt, model, budget, handoff rule,
// adapter version, schema, and stopping rule is versioned." It is computed
// once per run from the exact files the orchestrator actually used - never
// from the protocol's declared version strings alone, which could drift
// from the file on disk without anyone noticing.
type Fingerprints struct {
	ProtocolHash string `json:"protocol_hash"`

	// EngineCommit is this engine's own git revision, taken from the
	// binary's embedded build info rather than supplied by a caller, so a
	// sealed run records the code that actually produced it and not a
	// claim typed alongside it. A build from a dirty tree is suffixed
	// "-dirty": an uncommitted engine is exactly the provenance a reader
	// most needs to see. Empty when the binary carries no VCS stamp (`go
	// run`, or a test binary), which is why it is omitempty rather than
	// an invented value.
	EngineCommit string `json:"engine_commit,omitempty"`

	AgentConfigHashes map[string]string `json:"agent_config_hashes,omitempty"`
	SchemaVersions    map[string]string `json:"schema_versions"`
	PromptHashes      map[string]string `json:"prompt_hashes"`
	AdapterVersions   map[string]string `json:"adapter_versions"`
	ContainerDigests  map[string]string `json:"container_digests,omitempty"`
	InputChecksums    map[string]string `json:"input_checksums"`

	// BundleSchemaVersions records, verbatim, the schema_version each of
	// the bundle's pinned documents carried (reviewer/metadata.json,
	// control/manifest.json). Not normalised: a cohort mixing
	// reviewer-metadata/v1 and /v2 bundles must be visible as such here.
	BundleSchemaVersions map[string]string `json:"bundle_schema_versions,omitempty"`

	// ToolSets records the RESOLVED tool grant per stage. The arm config
	// hash already covers a `tools:` key that is present, but not a
	// default applied in code when the key is absent - so without this a
	// change to defaultTools would alter what every existing arm file
	// means while every fingerprint stayed identical. Recording the
	// resolved value makes the grant auditable regardless of where it came
	// from.
	ToolSets map[string][]string `json:"tool_sets,omitempty"`

	// BoundaryWaivers lists the paths this run's protocol waived from the
	// oracle-name heuristic. It belongs in the fingerprints because a
	// waiver weakens a contamination check: someone auditing the result
	// needs to see it here, alongside the protocol hash, not have to go
	// find it in the protocol file.
	BoundaryWaivers []string `json:"boundary_waivers,omitempty"`
}

func sha256HexFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("hashing %s: %w", path, err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// computeStaticFingerprints hashes the protocol file itself and every stage
// prompt and output schema it references, and carries forward the
// prospective bundle's own checksums.sha256 verbatim. It does not read
// AdapterVersions - those are only known once stages have actually run, and
// are filled in separately from the run's attempt records.
func computeStaticFingerprints(protocolPath, agentSetPath, repoRoot, bundleRoot string, protocol *Protocol, agents AgentSet) (Fingerprints, error) {
	fp := Fingerprints{
		EngineCommit:      engineCommit(),
		SchemaVersions:    map[string]string{},
		PromptHashes:      map[string]string{},
		AdapterVersions:   map[string]string{},
		AgentConfigHashes: map[string]string{},
		InputChecksums:    map[string]string{},
		ToolSets:          map[string][]string{},
	}

	for _, stage := range protocol.Order() {
		if tools := agents[stage].Tools; len(tools) > 0 {
			fp.ToolSets[string(stage)] = append([]string(nil), tools...)
		}
	}

	protoHash, err := sha256HexFile(protocolPath)
	if err != nil {
		return Fingerprints{}, err
	}
	fp.ProtocolHash = protoHash

	if waived := protocol.BoundaryValidation.WaivedOracleShapedPaths; len(waived) > 0 {
		fp.BoundaryWaivers = append([]string(nil), waived...)
		sort.Strings(fp.BoundaryWaivers)
	}

	// The agent set is hashed alongside the protocol: a run's vendor binding
	// is as much a part of what produced its numbers as the experiment is,
	// and the two vary independently.
	//
	// One arm is now one file, so every stage's binding hashes to the same
	// value. Still recorded per stage rather than once: the map says "this
	// is where THIS stage's binding came from", which stays true if a
	// future arm splits stages across files, and keeps the fingerprint
	// shape stable for anything already reading it.
	agentHash, err := sha256HexFile(agentSetPath)
	if err != nil {
		return Fingerprints{}, err
	}
	for _, stage := range protocol.Order() {
		fp.AgentConfigHashes[string(stage)] = agentHash
	}

	for _, stage := range protocol.Order() {
		sp := protocol.Stages[string(stage)]

		promptHash, err := sha256HexFile(filepath.Join(repoRoot, sp.Prompt))
		if err != nil {
			return Fingerprints{}, err
		}
		fp.PromptHashes[string(stage)] = promptHash

		schemaHash, err := sha256HexFile(filepath.Join(repoRoot, sp.OutputSchema))
		if err != nil {
			return Fingerprints{}, err
		}
		fp.SchemaVersions[sp.OutputSchema] = schemaHash
	}

	// Carrying the bundle's own checksums forward is recording, not
	// verifying: the boundary validator is the authority on whether they
	// are complete and correct, and it has already run (or is about to)
	// with a far stricter check than re-reading this file would be. So an
	// unreadable or malformed manifest is left as an empty map rather than
	// an error - that lets a run rejected by boundary validation still
	// record which protocol and prompts were in play, instead of failing
	// here with a less useful diagnosis than the one the validator gives.
	fp.BundleSchemaVersions = map[string]string{}
	for _, rel := range []string{"reviewer/metadata.json", "control/manifest.json"} {
		raw, err := os.ReadFile(filepath.Join(bundleRoot, filepath.FromSlash(rel)))
		if err != nil {
			continue // metadata.json is optional; the validator reports a missing manifest
		}
		var doc struct {
			SchemaVersion string `json:"schema_version"`
		}
		if json.Unmarshal(raw, &doc) == nil && doc.SchemaVersion != "" {
			fp.BundleSchemaVersions[rel] = doc.SchemaVersion
		}
	}

	checksumsPath := filepath.Join(bundleRoot, "control", "checksums.sha256")
	data, err := os.ReadFile(checksumsPath)
	if err != nil {
		return fp, nil
	}
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		fp.InputChecksums[fields[1]] = fields[0]
	}

	return fp, nil
}

// engineCommit identifies the engine that produced this run. Nothing is
// passed at the command line and nothing can be mistyped, which is the
// point: the run records the code that ran it rather than a claim made
// beside it. A dirty tree is suffixed "-dirty" - an uncommitted engine is
// exactly the provenance a reader most needs to see.
//
// Two sources, because one alone is silently empty on the path that matters.
// `go build` stamps vcs.revision into the binary, but `go run` does not
// (verified), and cmd/h1run launches every sealed case with `go run
// ./cmd/bench`. So when there is no stamp we ask git directly: under `go
// run` the working tree IS the engine that ran, which makes git the
// accurate source there, not a guess at one.
//
// Memoised: this shells out, and a batch seals many runs per process.
var engineCommitOnce struct {
	sync.Once
	value string
}

func engineCommit() string {
	engineCommitOnce.Do(func() { engineCommitOnce.value = resolveEngineCommit() })
	return engineCommitOnce.value
}

func resolveEngineCommit() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		var rev, modified string
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				rev = s.Value
			case "vcs.modified":
				modified = s.Value
			}
		}
		if rev != "" {
			if modified == "true" {
				return rev + "-dirty"
			}
			return rev
		}
	}
	return gitCommit()
}

// gitCommit reports HEAD of the tree the process is running in, or "" if
// there is no git, no repository, or anything else unexpected. Provenance
// that cannot be established is absent, never approximated.
func gitCommit() string {
	rev, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	head := strings.TrimSpace(string(rev))
	if head == "" {
		return ""
	}
	status, err := exec.Command("git", "status", "--porcelain").Output()
	if err != nil {
		// HEAD is known but cleanliness is not; say so rather than imply
		// a clean tree.
		return head + "-unknown"
	}
	if len(strings.TrimSpace(string(status))) > 0 {
		return head + "-dirty"
	}
	return head
}
