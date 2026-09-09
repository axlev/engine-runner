package boundaryvalidator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
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

// TestCutoffProvableMetadataFieldsAreAdmissible pins the widening made
// after reading the miner's own reviewer-metadata schema: base_branch and
// commit_messages are things a reviewer at the cutoff could genuinely see,
// and the miner emits them only when their as-of-cutoff value is provable.
// Rejecting them would make the benchmark measure a harder task than the
// real one.
func TestCutoffProvableMetadataFieldsAreAdmissible(t *testing.T) {
	bundle := copyBundle(t)
	writeMetadata(t, bundle, map[string]any{
		"schema_version":   "reviewer-metadata/v1",
		"repository":       "example/widget-service",
		"title":            "Add pagination",
		"description":      "Introduces cursor-based pagination.",
		"cutoff_timestamp": "2026-01-01T00:00:00Z",
		"base_branch":      "main",
		"commit_messages":  []string{"add paginate helper", "wire into list endpoint"},
	})
	r := validate(t, bundle)
	if hasViolation(r, RuleMetadataFields) {
		t.Errorf("base_branch/commit_messages must be admissible, got: %s", r.Summary())
	}
}

// TestOutcomeCarryingFieldsAreStillRejected guards the widening above from
// having quietly opened the door: the fields that actually carry outcome
// information must still be refused.
func TestOutcomeCarryingFieldsAreStillRejected(t *testing.T) {
	for _, field := range []string{"merged", "state", "merged_at", "fix_commit", "closed_at"} {
		t.Run(field, func(t *testing.T) {
			bundle := copyBundle(t)
			writeMetadata(t, bundle, map[string]any{
				"schema_version":   "reviewer-metadata/v1",
				"repository":       "example/widget-service",
				"cutoff_timestamp": "2026-01-01T00:00:00Z",
				field:              "whatever",
			})
			r := validate(t, bundle)
			if !hasViolation(r, RuleMetadataFields) {
				t.Errorf("field %q must still be rejected, got: %s", field, r.Summary())
			}
		})
	}
}

// TestThirdPartySourceVocabularyIsNotAFalsePositive is the regression test
// for the demonstrated false positive: a clean snapshot containing Oracle
// *the database* (or a .sln, or a solver) must not invalidate the case.
// The words in reviewer/repository/ belong to the upstream project, not to
// this benchmark's vocabulary.
func TestThirdPartySourceVocabularyIsNotAFalsePositive(t *testing.T) {
	for _, name := range []string{
		"oracle_dialect.go",
		"OracleConnection.java",
		"solution_builder.go",
		"verdict_reporter.go",
		"ground_truth_labels.py",
		"final_state_machine.go",
	} {
		t.Run(name, func(t *testing.T) {
			bundle := copyBundle(t)
			target := filepath.Join(bundle, "reviewer", "repository", name)
			if err := os.WriteFile(target, []byte("package db\n"), 0o644); err != nil {
				t.Fatalf("setup: %v", err)
			}
			rehashBundle(t, bundle)

			r := validate(t, bundle)
			if hasViolation(r, RuleOracleShaped) {
				t.Errorf("%s is ordinary third-party source and must not trip the oracle heuristic: %s", name, r.Summary())
			}
		})
	}
}

// TestOracleArtifactFilenamesStillCaughtInsideSnapshot is the other half:
// narrowing the check must not have lost the accident it exists for. An
// oracle bundle's own artifact, dropped into the snapshot, is still caught
// by exact basename.
func TestOracleArtifactFilenamesStillCaughtInsideSnapshot(t *testing.T) {
	for _, name := range []string{"oracle.json", "ground_truth.json", "expected_findings.json", "retrospective.json"} {
		t.Run(name, func(t *testing.T) {
			bundle := copyBundle(t)
			if err := os.WriteFile(filepath.Join(bundle, "reviewer", "repository", name), []byte("{}"), 0o644); err != nil {
				t.Fatalf("setup: %v", err)
			}
			rehashBundle(t, bundle)

			r := validate(t, bundle)
			if !hasViolation(r, RuleOracleShaped) {
				t.Errorf("%s inside the snapshot must still be caught: %s", name, r.Summary())
			}
		})
	}
}

// TestMinerAuthoredPathsKeepTheStrictList: outside the snapshot the full
// substring list still applies, because those paths are miner-authored and
// this project does control that vocabulary.
func TestMinerAuthoredPathsKeepTheStrictList(t *testing.T) {
	bundle := copyBundle(t)
	if err := os.WriteFile(filepath.Join(bundle, "reviewer", "oracle-notes.md"), []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	r := validate(t, bundle)
	if !hasViolation(r, RuleOracleShaped) {
		t.Errorf("an oracle-shaped name directly under reviewer/ must still be caught: %s", r.Summary())
	}
}

func TestWaiverSuppressesViolationButRecordsIt(t *testing.T) {
	bundle := copyBundle(t)
	rel := "reviewer/oracle-notes.md"
	if err := os.WriteFile(filepath.Join(bundle, "reviewer", "oracle-notes.md"), []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	r, err := ValidateWithConfig("test-case", bundle, Config{WaivedOracleShapedPaths: []string{rel}})
	if err != nil {
		t.Fatalf("ValidateWithConfig: %v", err)
	}
	if hasViolation(r, RuleOracleShaped) {
		t.Errorf("waived path should not remain a violation: %s", r.Summary())
	}
	if len(r.Waivers) != 1 || r.Waivers[0].Path != rel {
		t.Fatalf("waiver must be recorded, got %+v", r.Waivers)
	}
}

// TestWaiversCannotSuppressStructuralRules is the important negative: the
// waiver mechanism is a relief valve for one lexical heuristic, not a
// general override. A checksum mismatch stays fatal no matter what the
// protocol lists.
func TestWaiversCannotSuppressStructuralRules(t *testing.T) {
	bundle := copyBundle(t)
	target := filepath.Join(bundle, "reviewer", "diff.patch")
	if err := os.WriteFile(target, []byte("tampered\n"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	r, err := ValidateWithConfig("test-case", bundle, Config{
		WaivedOracleShapedPaths: []string{"reviewer/diff.patch"},
	})
	if err != nil {
		t.Fatalf("ValidateWithConfig: %v", err)
	}
	if !hasViolation(r, RuleChecksumManifest) {
		t.Errorf("a checksum mismatch must not be waivable, got: %s", r.Summary())
	}
	if r.Passed() {
		t.Errorf("bundle with a tampered file must still fail")
	}
}

// rehashBundle regenerates control/checksums.sha256 so a test that adds a
// legitimate source file isolates the rule under test instead of also
// tripping the checksum manifest.
func rehashBundle(t *testing.T, bundle string) {
	t.Helper()
	var lines []string
	reviewerRoot := filepath.Join(bundle, "reviewer")
	err := filepath.WalkDir(reviewerRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(bundle, path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		lines = append(lines, hex.EncodeToString(sum[:])+"  "+filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("rehashing: %v", err)
	}
	sort.Strings(lines)
	out := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(bundle, "control", "checksums.sha256"), []byte(out), 0o644); err != nil {
		t.Fatalf("rehashing: %v", err)
	}
}

// TestFalsePositiveBundlePasses guards the second committed case the same
// way TestCleanBundlePasses guards the first: a fixture bundle that fails
// its own validator is worse than no fixture, because every test built on
// it then fails for the wrong reason.
func TestFalsePositiveBundlePasses(t *testing.T) {
	r, err := Validate("false-positive", "../../fixtures/cases/false-positive/prospective")
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !r.Passed() {
		t.Fatalf("the false-positive bundle must pass: %s", r.Summary())
	}
}
