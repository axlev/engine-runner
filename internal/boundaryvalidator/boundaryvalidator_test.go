package boundaryvalidator

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v5"
)

const goodBundle = "../../fixtures/cases/happy-path/prospective"

// copyBundle copies the committed clean bundle into a temp dir so each test
// can contaminate exactly one thing and see which rule fires.
func copyBundle(t *testing.T) string {
	t.Helper()
	dst := t.TempDir()
	err := filepath.WalkDir(goodBundle, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(goodBundle, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatalf("copying bundle: %v", err)
	}
	return dst
}

func hasViolation(r Report, rule string) bool {
	for _, v := range r.Violations {
		if v.Rule == rule {
			return true
		}
	}
	return false
}

func validate(t *testing.T, bundle string) Report {
	t.Helper()
	r, err := Validate("test-case", bundle)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	return r
}

func TestCleanBundlePasses(t *testing.T) {
	r := validate(t, goodBundle)
	if !r.Passed() {
		t.Fatalf("clean fixture bundle should pass, got violations: %s", r.Summary())
	}
	if len(r.Checks) != len(ruleOrder) {
		t.Errorf("len(Checks) = %d, want %d (one per rule)", len(r.Checks), len(ruleOrder))
	}
	if r.Summary() != "" {
		t.Errorf("Summary() = %q, want empty for a passing bundle", r.Summary())
	}
}

func TestUnapprovedFileInReviewerRootIsRejected(t *testing.T) {
	bundle := copyBundle(t)
	if err := os.WriteFile(filepath.Join(bundle, "reviewer", "notes.txt"), []byte("stray"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	r := validate(t, bundle)
	if r.Passed() {
		t.Fatalf("expected failure for an unapproved reviewer/ entry")
	}
	if !hasViolation(r, RuleAllowedPaths) {
		t.Errorf("expected %s violation, got: %s", RuleAllowedPaths, r.Summary())
	}
}

func TestSymlinkIsRejected(t *testing.T) {
	bundle := copyBundle(t)
	outside := filepath.Join(t.TempDir(), "oracle-data.json")
	if err := os.WriteFile(outside, []byte(`{"answer":"yes"}`), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	link := filepath.Join(bundle, "reviewer", "repository", "notes.go")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatalf("setup: %v", err)
	}
	r := validate(t, bundle)
	if !hasViolation(r, RuleNoIrregularFiles) {
		t.Errorf("expected %s violation for a symlink, got: %s", RuleNoIrregularFiles, r.Summary())
	}
}

func TestGitMetadataIsRejected(t *testing.T) {
	bundle := copyBundle(t)
	gitDir := filepath.Join(bundle, "reviewer", "repository", ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "packed-refs"), []byte("ref\n"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	r := validate(t, bundle)
	if !hasViolation(r, RuleNoGitMetadata) {
		t.Errorf("expected %s violation, got: %s", RuleNoGitMetadata, r.Summary())
	}
}

func TestOracleShapedPathIsRejected(t *testing.T) {
	bundle := copyBundle(t)
	// The classic accident: an oracle file copied one directory too high.
	if err := os.WriteFile(filepath.Join(bundle, "reviewer", "repository", "oracle.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	r := validate(t, bundle)
	if !hasViolation(r, RuleOracleShaped) {
		t.Errorf("expected %s violation, got: %s", RuleOracleShaped, r.Summary())
	}
}

func writeMetadata(t *testing.T, bundle string, fields map[string]any) {
	t.Helper()
	data, err := json.MarshalIndent(fields, "", "  ")
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "reviewer", "metadata.json"), data, 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
}

func TestForbiddenMetadataFieldIsRejected(t *testing.T) {
	bundle := copyBundle(t)
	writeMetadata(t, bundle, map[string]any{
		"schema_version":   "reviewer-metadata/v1",
		"repository":       "example/widget-service",
		"title":            "Add pagination",
		"cutoff_timestamp": "2026-01-01T00:00:00Z",
		// Not reviewer-visible: this is the PR's final state.
		"merged": true,
	})
	r := validate(t, bundle)
	if !hasViolation(r, RuleMetadataFields) {
		t.Errorf("expected %s violation for a non-reviewer-visible field, got: %s", RuleMetadataFields, r.Summary())
	}
}

func TestRetrospectiveMetadataValueIsRejected(t *testing.T) {
	bundle := copyBundle(t)
	writeMetadata(t, bundle, map[string]any{
		"schema_version":   "reviewer-metadata/v1",
		"repository":       "example/widget-service",
		"title":            "Add pagination",
		"description":      "This was later reverted after a production incident.",
		"cutoff_timestamp": "2026-01-01T00:00:00Z",
	})
	r := validate(t, bundle)
	if !hasViolation(r, RuleMetadataFields) {
		t.Errorf("expected %s violation for an outcome-carrying value, got: %s", RuleMetadataFields, r.Summary())
	}
}

func TestWrongMetadataSchemaVersionIsRejected(t *testing.T) {
	bundle := copyBundle(t)
	writeMetadata(t, bundle, map[string]any{
		"schema_version":   "reviewer-metadata/v2",
		"repository":       "example/widget-service",
		"cutoff_timestamp": "2026-01-01T00:00:00Z",
	})
	r := validate(t, bundle)
	if !hasViolation(r, RulePinnedSchemas) {
		t.Errorf("expected %s violation for an unpinned schema version, got: %s", RulePinnedSchemas, r.Summary())
	}
}

func TestChecksumMismatchIsRejected(t *testing.T) {
	bundle := copyBundle(t)
	// Modify a file without updating the manifest: the classic sign that a
	// bundle was edited after export.
	target := filepath.Join(bundle, "reviewer", "diff.patch")
	if err := os.WriteFile(target, []byte("tampered\n"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	r := validate(t, bundle)
	if !hasViolation(r, RuleChecksumManifest) {
		t.Errorf("expected %s violation, got: %s", RuleChecksumManifest, r.Summary())
	}
}

func TestFileListedButMissingIsRejected(t *testing.T) {
	bundle := copyBundle(t)
	if err := os.Remove(filepath.Join(bundle, "reviewer", "diff.patch")); err != nil {
		t.Fatalf("setup: %v", err)
	}
	r := validate(t, bundle)
	if !hasViolation(r, RuleChecksumManifest) {
		t.Errorf("expected %s violation for a listed-but-absent file, got: %s", RuleChecksumManifest, r.Summary())
	}
}

func TestWrongControlManifestSchemaVersionIsRejected(t *testing.T) {
	bundle := copyBundle(t)
	data := []byte(`{"schema_version":"engine-manifest/v99","case_id":"x"}`)
	if err := os.WriteFile(filepath.Join(bundle, "control", "manifest.json"), data, 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	r := validate(t, bundle)
	if !hasViolation(r, RulePinnedSchemas) {
		t.Errorf("expected %s violation, got: %s", RulePinnedSchemas, r.Summary())
	}
}

func TestMissingBundleIsAnError(t *testing.T) {
	if _, err := Validate("x", filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatalf("expected an error for a missing bundle root")
	}
}

// TestReportIsDeterministic pins the reproducibility property: the same
// bundle must produce the same report, including violation ordering, since
// the report is itself a benchmark artifact.
func TestReportIsDeterministic(t *testing.T) {
	bundle := copyBundle(t)
	if err := os.WriteFile(filepath.Join(bundle, "reviewer", "zzz.txt"), []byte("a"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(bundle, "reviewer", "aaa.txt"), []byte("b"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	first := validate(t, bundle)
	second := validate(t, bundle)
	first.ValidatedAt, second.ValidatedAt = "", "" // the only field that legitimately differs

	a, _ := json.Marshal(first)
	b, _ := json.Marshal(second)
	if string(a) != string(b) {
		t.Errorf("reports differ across runs:\n%s\nvs\n%s", a, b)
	}
}

// TestReportSatisfiesItsSchema closes the loop between the Go type and
// schemas/boundary-validation.schema.json.
func TestReportSatisfiesItsSchema(t *testing.T) {
	schema, err := jsonschema.Compile("../../schemas/boundary-validation.schema.json")
	if err != nil {
		t.Fatalf("compiling schema: %v", err)
	}

	bundle := copyBundle(t)
	if err := os.WriteFile(filepath.Join(bundle, "reviewer", "oracle.json"), []byte("{}"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	for name, report := range map[string]Report{
		"passing": validate(t, goodBundle),
		"failing": validate(t, bundle),
	} {
		t.Run(name, func(t *testing.T) {
			raw, err := json.Marshal(report)
			if err != nil {
				t.Fatalf("marshaling report: %v", err)
			}
			var doc any
			if err := json.Unmarshal(raw, &doc); err != nil {
				t.Fatalf("unmarshaling report: %v", err)
			}
			if err := schema.Validate(doc); err != nil {
				t.Errorf("report does not satisfy its schema: %v\n%s", err, raw)
			}
		})
	}
}
