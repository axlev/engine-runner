// Package boundaryvalidator verifies a prospective bundle before any
// reasoner runs, per docs/system-design.md section 6.2.
//
// It is deterministic software, and it is not a reviewer: it never judges
// whether a review finding is correct. It answers one question — is this
// bundle admissible as prospective evidence, or is it contaminated with
// information a reviewer at the cutoff could not have had?
//
// It fails closed and allow-lists: a bundle passes only when every file in
// it is one the contract expects. That matters because the threat here is
// not a hostile miner but an ordinary mistake — an oracle file copied one
// directory too high, a .git directory left in a snapshot, a metadata field
// carrying the outcome. Any of those silently invalidates a benchmark
// result that will otherwise look perfectly credible.
//
// Section 12: a boundary-validation failure invalidates the case before any
// LLM cost is incurred.
package boundaryvalidator

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Rule names. These are the closed set in
// schemas/boundary-validation.schema.json.
const (
	RuleAllowedPaths     = "allowed_paths"
	RuleNoIrregularFiles = "no_irregular_files"
	RuleNoGitMetadata    = "no_git_metadata"
	RuleMetadataFields   = "reviewer_metadata_fields"
	RuleOracleShaped     = "oracle_shaped_content"
	RuleChecksumManifest = "checksum_manifest"
	RulePinnedSchemas    = "pinned_schema_versions"
)

// ruleOrder fixes the order checks appear in a report, so two validations of
// the same bundle produce byte-identical output.
var ruleOrder = []string{
	RuleAllowedPaths,
	RuleNoIrregularFiles,
	RuleNoGitMetadata,
	RuleMetadataFields,
	RuleOracleShaped,
	RuleChecksumManifest,
	RulePinnedSchemas,
}

type Violation struct {
	Rule   string `json:"rule"`
	Path   string `json:"path,omitempty"`
	Detail string `json:"detail"`
}

type Check struct {
	Rule           string `json:"rule"`
	Result         string `json:"result"`
	ViolationCount int    `json:"violation_count"`
}

type Report struct {
	SchemaVersion string      `json:"schema_version"`
	CaseID        string      `json:"case_id"`
	BundleRoot    string      `json:"bundle_root"`
	ValidatedAt   string      `json:"validated_at"`
	Result        string      `json:"result"`
	Checks        []Check     `json:"checks"`
	Violations    []Violation `json:"violations,omitempty"`
}

// Passed reports whether the bundle is admissible.
func (r Report) Passed() bool { return r.Result == "pass" }

// Summary renders the violations as a single line, for a run's
// invalidation_reason.
func (r Report) Summary() string {
	if r.Passed() {
		return ""
	}
	parts := make([]string, 0, len(r.Violations))
	for _, v := range r.Violations {
		if v.Path != "" {
			parts = append(parts, fmt.Sprintf("%s (%s): %s", v.Rule, v.Path, v.Detail))
			continue
		}
		parts = append(parts, fmt.Sprintf("%s: %s", v.Rule, v.Detail))
	}
	return strings.Join(parts, "; ")
}

// allowedReviewerEntries are the only top-level entries permitted under
// <bundle>/reviewer, matching section 7's contract. Anything else is a
// violation, rather than something to be quietly ignored — an unexpected
// file is exactly the shape contamination arrives in.
var allowedReviewerEntries = map[string]bool{
	"repository":    true,
	"diff.patch":    true,
	"metadata.json": true,
}

// allowedMetadataFields are the only reviewer-visible metadata fields
// section 7 permits. A field outside this set can carry outcome information
// (state, merged_at, fix_commit, ...) even when it looks innocuous.
var allowedMetadataFields = map[string]bool{
	"schema_version":   true,
	"repository":       true,
	"title":            true,
	"description":      true,
	"cutoff_timestamp": true,
}

// gitMetadataNames are filesystem names that indicate git history,
// alternates, submodule or worktree leakage. The prospective snapshot
// format is a plain directory tree, so none of these has any business
// being present at all - which makes this check simple and total.
var gitMetadataNames = map[string]bool{
	".git":        true,
	".gitmodules": true,
	"packed-refs": true,
	"HEAD":        true,
	"ORIG_HEAD":   true,
	"FETCH_HEAD":  true,
	"MERGE_HEAD":  true,
	"shallow":     true,
	"objects":     true,
	"refs":        true,
	"reflogs":     true,
	"worktrees":   true,
	"alternates":  true,
}

// oracleShapedSubstrings are path fragments that suggest retrospective or
// outcome material. Matching is on the lowercased path, so "Expected.md",
// "ORACLE/", and "ground_truth.json" all trip it.
var oracleShapedSubstrings = []string{
	"oracle",
	"ground_truth",
	"groundtruth",
	"expected_finding",
	"expected-finding",
	"answer_key",
	"answerkey",
	"solution",
	"postmortem",
	"post_mortem",
	"regression_report",
	"fix_commit",
	"final_state",
	"review_comments",
	"ci_result",
	"verdict",
}

// forbiddenMetadataValueSubstrings catch outcome information smuggled into
// an otherwise-allowed metadata field's value, e.g. a title amended after
// the fact to read "Fixes #123 (merged, reverted later)".
var forbiddenMetadataValueSubstrings = []string{
	"reverted",
	"regression introduced",
	"root cause was",
}

const (
	pinnedReviewerMetadataSchema = "reviewer-metadata/v1"
	pinnedControlManifestSchema  = "engine-manifest/v1"
)

// Validate checks the prospective bundle rooted at bundleRoot (the
// directory containing reviewer/ and control/) and returns a report. An
// error is returned only when the bundle could not be inspected at all; a
// contaminated-but-readable bundle is a report with Result "fail", not an
// error.
func Validate(caseID, bundleRoot string) (Report, error) {
	report := Report{
		SchemaVersion: "boundary-validation/v1",
		CaseID:        caseID,
		BundleRoot:    bundleRoot,
		ValidatedAt:   time.Now().UTC().Format(time.RFC3339),
	}

	info, err := os.Stat(bundleRoot)
	if err != nil {
		return Report{}, fmt.Errorf("boundaryvalidator: reading bundle %s: %w", bundleRoot, err)
	}
	if !info.IsDir() {
		return Report{}, fmt.Errorf("boundaryvalidator: bundle %s is not a directory", bundleRoot)
	}

	byRule := map[string][]Violation{}
	add := func(rule, path, detail string) {
		byRule[rule] = append(byRule[rule], Violation{Rule: rule, Path: path, Detail: detail})
	}

	reviewerRoot := filepath.Join(bundleRoot, "reviewer")
	if _, err := os.Stat(reviewerRoot); err != nil {
		add(RuleAllowedPaths, "reviewer", "bundle has no reviewer/ directory")
	} else {
		checkReviewerTree(reviewerRoot, add)
	}

	checkMetadata(filepath.Join(reviewerRoot, "metadata.json"), add)
	checkChecksums(bundleRoot, reviewerRoot, add)
	checkControlManifest(filepath.Join(bundleRoot, "control", "manifest.json"), add)

	// Assemble in fixed rule order, with violations sorted within each rule,
	// so the report is reproducible.
	for _, rule := range ruleOrder {
		violations := byRule[rule]
		sort.Slice(violations, func(i, j int) bool {
			if violations[i].Path != violations[j].Path {
				return violations[i].Path < violations[j].Path
			}
			return violations[i].Detail < violations[j].Detail
		})
		byRule[rule] = violations

		result := "pass"
		if len(violations) > 0 {
			result = "fail"
		}
		report.Checks = append(report.Checks, Check{Rule: rule, Result: result, ViolationCount: len(violations)})
		report.Violations = append(report.Violations, violations...)
	}

	report.Result = "pass"
	if len(report.Violations) > 0 {
		report.Result = "fail"
	}
	return report, nil
}

// checkReviewerTree walks reviewer/ once, applying the path-shaped rules:
// the top-level allow-list, irregular file types, git metadata, and
// oracle-shaped names.
func checkReviewerTree(reviewerRoot string, add func(rule, path, detail string)) {
	entries, err := os.ReadDir(reviewerRoot)
	if err != nil {
		add(RuleAllowedPaths, "reviewer", fmt.Sprintf("cannot read reviewer directory: %v", err))
		return
	}
	for _, e := range entries {
		if !allowedReviewerEntries[e.Name()] {
			add(RuleAllowedPaths, filepath.Join("reviewer", e.Name()),
				"entry is not one of the reviewer-visible entries the export contract allows (repository, diff.patch, metadata.json)")
		}
	}

	_ = filepath.WalkDir(reviewerRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			add(RuleAllowedPaths, path, fmt.Sprintf("cannot walk: %v", err))
			return nil
		}
		rel, relErr := filepath.Rel(filepath.Dir(reviewerRoot), path)
		if relErr != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)
		if rel == "reviewer" {
			return nil
		}

		// Irregular files: symlinks, sockets, devices, FIFOs. A symlink can
		// point anywhere, including at the oracle bundle, so it is rejected
		// rather than resolved.
		info, infoErr := d.Info()
		if infoErr == nil {
			mode := info.Mode()
			switch {
			case mode&os.ModeSymlink != 0:
				add(RuleNoIrregularFiles, rel, "symlink is not admissible in a prospective bundle")
			case mode&os.ModeSocket != 0:
				add(RuleNoIrregularFiles, rel, "socket is not admissible in a prospective bundle")
			case mode&os.ModeDevice != 0:
				add(RuleNoIrregularFiles, rel, "device node is not admissible in a prospective bundle")
			case mode&os.ModeNamedPipe != 0:
				add(RuleNoIrregularFiles, rel, "named pipe is not admissible in a prospective bundle")
			}
		}

		if gitMetadataNames[d.Name()] {
			add(RuleNoGitMetadata, rel,
				fmt.Sprintf("%q indicates git history, alternates, submodule or worktree leakage; the snapshot format is a plain directory tree", d.Name()))
		}

		lower := strings.ToLower(rel)
		for _, frag := range oracleShapedSubstrings {
			if strings.Contains(lower, frag) {
				add(RuleOracleShaped, rel,
					fmt.Sprintf("path contains %q, which suggests retrospective or outcome material", frag))
				break
			}
		}
		return nil
	})
}

func checkMetadata(metadataPath string, add func(rule, path, detail string)) {
	raw, err := os.ReadFile(metadataPath)
	if err != nil {
		// metadata.json is optional per section 7 ("if a field cannot be
		// reconstructed, the miner omits it" - and a bundle may legitimately
		// carry no reviewer-visible metadata at all).
		return
	}
	rel := "reviewer/metadata.json"

	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		add(RulePinnedSchemas, rel, fmt.Sprintf("is not valid JSON: %v", err))
		return
	}

	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		if !allowedMetadataFields[name] {
			add(RuleMetadataFields, rel,
				fmt.Sprintf("field %q is not reviewer-visible metadata; only schema_version, repository, title, description and cutoff_timestamp are permitted", name))
			continue
		}
		value, ok := fields[name].(string)
		if !ok {
			continue
		}
		lower := strings.ToLower(value)
		for _, frag := range forbiddenMetadataValueSubstrings {
			if strings.Contains(lower, frag) {
				add(RuleMetadataFields, rel,
					fmt.Sprintf("field %q contains %q, which reads as retrospective outcome information", name, frag))
				break
			}
		}
		for _, frag := range oracleShapedSubstrings {
			if strings.Contains(lower, frag) {
				add(RuleOracleShaped, rel,
					fmt.Sprintf("field %q contains %q, which suggests retrospective or outcome material", name, frag))
				break
			}
		}
	}

	version, _ := fields["schema_version"].(string)
	if version != pinnedReviewerMetadataSchema {
		add(RulePinnedSchemas, rel,
			fmt.Sprintf("schema_version is %q, want the pinned %q", version, pinnedReviewerMetadataSchema))
	}
}

// checkChecksums recomputes every digest in control/checksums.sha256 and
// compares it, and cross-checks that the manifest and the reviewer tree
// describe exactly the same set of files. A file present but unlisted is as
// much a contamination signal as a digest mismatch.
func checkChecksums(bundleRoot, reviewerRoot string, add func(rule, path, detail string)) {
	manifestPath := filepath.Join(bundleRoot, "control", "checksums.sha256")
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		add(RuleChecksumManifest, "control/checksums.sha256", fmt.Sprintf("cannot read checksum manifest: %v", err))
		return
	}

	listed := map[string]string{}
	for _, line := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) != 2 {
			add(RuleChecksumManifest, "control/checksums.sha256", fmt.Sprintf("malformed line %q", line))
			continue
		}
		listed[filepath.ToSlash(parts[1])] = parts[0]
	}

	present := map[string]bool{}
	_ = filepath.WalkDir(reviewerRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil || !info.Mode().IsRegular() {
			// Irregular files are reported by their own rule; digesting them
			// here would only produce a confusing second complaint.
			return nil
		}
		rel, relErr := filepath.Rel(filepath.Dir(reviewerRoot), path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		present[rel] = true

		want, ok := listed[rel]
		if !ok {
			add(RuleChecksumManifest, rel, "file is present in the bundle but absent from control/checksums.sha256")
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			add(RuleChecksumManifest, rel, fmt.Sprintf("cannot read for digest: %v", readErr))
			return nil
		}
		sum := sha256.Sum256(data)
		if got := hex.EncodeToString(sum[:]); got != want {
			add(RuleChecksumManifest, rel, fmt.Sprintf("sha256 is %s, manifest says %s", got, want))
		}
		return nil
	})

	missing := make([]string, 0)
	for rel := range listed {
		if !present[rel] {
			missing = append(missing, rel)
		}
	}
	sort.Strings(missing)
	for _, rel := range missing {
		add(RuleChecksumManifest, rel, "file is listed in control/checksums.sha256 but absent from the bundle")
	}
}

func checkControlManifest(manifestPath string, add func(rule, path, detail string)) {
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		add(RulePinnedSchemas, "control/manifest.json", fmt.Sprintf("cannot read control manifest: %v", err))
		return
	}
	var manifest map[string]any
	if err := json.Unmarshal(raw, &manifest); err != nil {
		add(RulePinnedSchemas, "control/manifest.json", fmt.Sprintf("is not valid JSON: %v", err))
		return
	}
	version, _ := manifest["schema_version"].(string)
	if version != pinnedControlManifestSchema {
		add(RulePinnedSchemas, "control/manifest.json",
			fmt.Sprintf("schema_version is %q, want the pinned %q", version, pinnedControlManifestSchema))
	}
}
