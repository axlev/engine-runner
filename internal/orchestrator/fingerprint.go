package orchestrator

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Fingerprints is what makes a run reproducible and auditable after the
// fact, per design principle 5: "Every prompt, model, budget, handoff rule,
// adapter version, schema, and stopping rule is versioned." It is computed
// once per run from the exact files the orchestrator actually used - never
// from the protocol's declared version strings alone, which could drift
// from the file on disk without anyone noticing.
type Fingerprints struct {
	ProtocolHash     string            `json:"protocol_hash"`
	SchemaVersions   map[string]string `json:"schema_versions"`
	PromptHashes     map[string]string `json:"prompt_hashes"`
	AdapterVersions  map[string]string `json:"adapter_versions"`
	ContainerDigests map[string]string `json:"container_digests,omitempty"`
	InputChecksums   map[string]string `json:"input_checksums"`
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
func computeStaticFingerprints(protocolPath, repoRoot, bundleRoot string, protocol *Protocol) (Fingerprints, error) {
	fp := Fingerprints{
		SchemaVersions:  map[string]string{},
		PromptHashes:    map[string]string{},
		AdapterVersions: map[string]string{},
		InputChecksums:  map[string]string{},
	}

	protoHash, err := sha256HexFile(protocolPath)
	if err != nil {
		return Fingerprints{}, err
	}
	fp.ProtocolHash = protoHash

	for _, stage := range stageOrder {
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

	checksumsPath := filepath.Join(bundleRoot, "control", "checksums.sha256")
	data, err := os.ReadFile(checksumsPath)
	if err != nil {
		return Fingerprints{}, fmt.Errorf("reading bundle checksums %s: %w", checksumsPath, err)
	}
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return Fingerprints{}, fmt.Errorf("malformed checksum line in %s: %q", checksumsPath, line)
		}
		fp.InputChecksums[fields[1]] = fields[0]
	}

	return fp, nil
}
