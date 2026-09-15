package orchestrator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Fingerprints is what makes a run reproducible and auditable after the
// fact, per design principle 5: "Every prompt, model, budget, handoff rule,
// adapter version, schema, and stopping rule is versioned." It is computed
// once per run from the exact files the orchestrator actually used - never
// from the protocol's declared version strings alone, which could drift
// from the file on disk without anyone noticing.
type Fingerprints struct {
	ProtocolHash      string            `json:"protocol_hash"`
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
