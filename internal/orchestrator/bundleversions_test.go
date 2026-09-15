package orchestrator

import "testing"

// The bundle's own schema versions travel in the fingerprint verbatim.
func TestFingerprintRecordsBundleSchemaVersions(t *testing.T) {
	p := loadPilotV1(t)
	fp, err := computeStaticFingerprints(pilotV1Path, fixtureAgentsDir, repoRoot, happyPathBundle, p, loadFixtureAgents(t))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"reviewer/metadata.json": "reviewer-metadata/v1", "control/manifest.json": "engine-manifest/v1"}
	for k, v := range want {
		if fp.BundleSchemaVersions[k] != v {
			t.Errorf("bundle_schema_versions[%s] = %q, want %q", k, fp.BundleSchemaVersions[k], v)
		}
	}
}
