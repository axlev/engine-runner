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
	"regexp"
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

	// ObservedSchemaVersions records the schema_version each pinned
	// document actually carried, verbatim, whether or not it was accepted.
	ObservedSchemaVersions map[string]string `json:"observed_schema_versions,omitempty"`

	// Warnings are findings recorded but not failed on. The metadata
	// vocabulary rule lives here: see checkMetadata.
	Warnings []Violation `json:"warnings,omitempty"`

	// Waivers records violations that were knowingly overridden by
	// protocol configuration. They are reported, never silently dropped:
	// the difference between "this cohort knowingly allowed
	// oracle_dialect.go" and "someone turned the check off" has to be
	// visible in the run's own artifacts.
	Waivers []Violation `json:"waivers,omitempty"`
}

// Config carries protocol-supplied validator settings.
type Config struct {
	// WaivedOracleShapedPaths are bundle-relative paths where the
	// oracle-name heuristic is knowingly overridden.
	//
	// Only oracle_shaped_content is waivable, and deliberately so: it is
	// the one rule built on a lexical guess about English words, so it is
	// the only one that can be wrong about a clean bundle. Every other
	// rule - symlinks, git metadata, checksum mismatches, forbidden
	// metadata fields - is structural and unambiguous, and a waiver
	// mechanism over those would just be a hole in the boundary.
	WaivedOracleShapedPaths []string
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
// permitted. A field outside this set can carry outcome information
// (state, merged_at, fix_commit, ...) even when it looks innocuous.
//
// This is a superset of section 7's example, which is labelled
// "recommended" rather than normative. base_branch and commit_messages
// were added after reading the miner's own reviewer-metadata schema: both
// are things a reviewer at the cutoff could genuinely see, and the miner
// emits them only when their as-of-cutoff value is positively provable
// (unchanged since cutoff, or reconstructed from a complete history).
// Admissibility is the test here, not brevity - excluding a field a
// reviewer legitimately had makes the benchmark measure a harder task
// than the real one.
var allowedMetadataFields = map[string]bool{
	"schema_version":   true,
	"repository":       true,
	"title":            true,
	"description":      true,
	"cutoff_timestamp": true,
	"base_branch":      true,
	"commit_messages":  true,
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
//
// These apply only to miner-authored surfaces: entries directly under
// reviewer/, and metadata values. They are deliberately NOT applied inside
// reviewer/repository/ - see checkOracleShapedPath below.
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

// The vocabulary problem: inside reviewer/repository/ the files are
// third-party upstream source, whose words this project does not control.
// "oracle" there means Oracle Database, "solution" means a .sln file or a
// solver, "verdict" means a TestNG result, "ground_truth" means training
// labels. Applying the substring list there rejects ordinary repositories
// wholesale - and a check that fires constantly on clean input gets turned
// off, which is worse than a narrower check that people trust.
//
// So inside the snapshot, two much tighter signals are used instead.

// oracleArtifactBasenames are exact filenames an oracle bundle's own
// artifacts would carry. An exact basename stays high-signal even in a
// source tree: real projects have oracle_dialect.go and OracleConnection
// .java, not a bare oracle.json.
var oracleArtifactBasenames = map[string]bool{
	"oracle.json":            true,
	"oracle.yaml":            true,
	"oracle.yml":             true,
	"ground_truth.json":      true,
	"groundtruth.json":       true,
	"answer_key.json":        true,
	"answerkey.json":         true,
	"expected_findings.json": true,
	"expected-findings.json": true,
	"retrospective.json":     true,
}

// highSignalOracleSubstrings are the few fragments specific enough to this
// benchmark's vocabulary that they are worth matching even inside
// third-party source. Everything ambiguous in ordinary code is excluded on
// purpose: "ground_truth" is omitted here despite being omitted-from-safe,
// because legitimate ML repositories use it constantly.
var highSignalOracleSubstrings = []string{
	"expected_finding",
	"expected-finding",
	"answer_key",
	"answerkey",
	"fix_commit",
}

// forbiddenMetadataValueSubstrings catch outcome information smuggled into
// an otherwise-allowed metadata field's value, e.g. a title amended after
// the fact to read "Fixes #123 (merged, reverted later)".
var forbiddenMetadataValueSubstrings = []string{
	"reverted",
	"regression introduced",
	"root cause was",
}

const pinnedControlManifestSchema = "engine-manifest/v1"

// acceptedReviewerMetadataSchemas are the metadata schema versions the
// engine admits. v2 (2026-09-15) differs from v1 only in how the MINER
// admits title and description - from each field's own edit history, last
// edit at or before the cutoff - which is recorded on the miner's evaluator
// audit, not something this validator can check. The wire shape is
// identical: same closed key set, same types, an inadmissible field absent
// rather than null. The engine's job is to accept the string and record it
// verbatim, so a cohort mixing v1 and v2 bundles is visible as such.
var acceptedReviewerMetadataSchemas = map[string]bool{
	"reviewer-metadata/v1": true,
	"reviewer-metadata/v2": true,
}

// metadataFieldTypes is the contract's type per field. A value of another
// type - null included - is a violation: the miner omits what it cannot
// admit, so a null is a field that should not be there at all.
var metadataFieldTypes = map[string]string{
	"schema_version":   "string",
	"repository":       "string",
	"title":            "string",
	"description":      "string",
	"cutoff_timestamp": "string",
	"base_branch":      "string",
	"commit_messages":  "array of strings",
}

// Validate checks the prospective bundle rooted at bundleRoot (the
// directory containing reviewer/ and control/) and returns a report. An
// error is returned only when the bundle could not be inspected at all; a
// contaminated-but-readable bundle is a report with Result "fail", not an
// error.
func Validate(caseID, bundleRoot string) (Report, error) {
	return ValidateWithConfig(caseID, bundleRoot, Config{})
}

// ValidateWithConfig is Validate with protocol-supplied settings.
func ValidateWithConfig(caseID, bundleRoot string, cfg Config) (Report, error) {
	waived := make(map[string]bool, len(cfg.WaivedOracleShapedPaths))
	for _, p := range cfg.WaivedOracleShapedPaths {
		waived[filepath.ToSlash(p)] = true
	}

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
	warn := func(rule, path, detail string) {
		report.Warnings = append(report.Warnings, Violation{Rule: rule, Path: path, Detail: detail})
	}

	reviewerRoot := filepath.Join(bundleRoot, "reviewer")
	if _, err := os.Stat(reviewerRoot); err != nil {
		add(RuleAllowedPaths, "reviewer", "bundle has no reviewer/ directory")
	} else {
		checkReviewerTree(reviewerRoot, add)
	}

	observed := map[string]string{}
	if v, present := checkMetadata(filepath.Join(reviewerRoot, "metadata.json"), add, warn); present {
		observed["reviewer/metadata.json"] = v
	}
	checkChecksums(bundleRoot, reviewerRoot, add)
	if v, present := checkControlManifest(filepath.Join(bundleRoot, "control", "manifest.json"), add); present {
		observed["control/manifest.json"] = v
	}
	report.ObservedSchemaVersions = observed

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

		// Waivers apply only to the lexical rule, and only to exactly the
		// paths the protocol named.
		if rule == RuleOracleShaped && len(waived) > 0 {
			kept := violations[:0:0]
			for _, v := range violations {
				if waived[v.Path] {
					report.Waivers = append(report.Waivers, v)
					continue
				}
				kept = append(kept, v)
			}
			violations = kept
			byRule[rule] = violations
		}

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

		// Irregular files: sockets, devices, FIFOs are never admissible.
		//
		// Symlinks are conditional. A blanket rejection was a proxy for the
		// property actually wanted - that no reference escapes the snapshot -
		// and the proxy was too broad to use: FRR carries ~77 in-tree
		// relative links under tests/topotests/ in every commit, so every
		// bundle mined from it failed on links no PR had touched. A link
		// that provably resolves inside reviewer/repository/ grants a
		// reviewer no reach it did not already have, since every file there
		// is already readable. See checkSnapshotSymlink for the conditions.
		info, infoErr := d.Info()
		if infoErr == nil {
			mode := info.Mode()
			switch {
			case mode&os.ModeSymlink != 0:
				checkSnapshotSymlink(reviewerRoot, path, rel, add)
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

		checkOracleShapedPath(rel, d.Name(), add)
		return nil
	})
}

// checkSnapshotSymlink admits a symlink only when it is provably confined to
// the reviewer's own source snapshot, and reports why when it is not.
//
// The conditions, all required:
//
//  1. It lives under reviewer/repository/. A link anywhere else in
//     reviewer/ - beside diff.patch or metadata.json - has no legitimate
//     purpose and is rejected outright.
//  2. Its target is relative. An absolute target is machine-specific and
//     cannot be reasoned about from the bundle alone.
//  3. Resolved against the link's own directory, it stays inside
//     reviewer/repository/. This is the property the old blanket rule was
//     approximating.
//  4. It resolves to something that exists.
//  5. Its target is not itself a symlink. Chains are rejected rather than
//     followed to a depth limit: FRR has none, and a rule with no traversal
//     loop has no traversal bug. This is deliberately stricter than the
//     depth-limited version that was proposed.
//
// Note what is NOT relied on: the target is not resolved with EvalSymlinks
// against the live filesystem beyond one Lstat, so a link cannot be walked
// out of the tree during checking.
func checkSnapshotSymlink(reviewerRoot, path, rel string, add func(rule, path, detail string)) {
	snapshotRoot := filepath.Join(reviewerRoot, "repository")

	// Condition 1: only the source snapshot may carry links.
	if !strings.HasPrefix(filepath.ToSlash(rel), snapshotPrefix) {
		add(RuleNoIrregularFiles, rel,
			"symlink outside reviewer/repository/ is not admissible in a prospective bundle")
		return
	}

	target, err := os.Readlink(path)
	if err != nil {
		add(RuleNoIrregularFiles, rel, fmt.Sprintf("cannot read symlink target: %v", err))
		return
	}

	// Condition 2: relative targets only.
	if filepath.IsAbs(target) {
		add(RuleNoIrregularFiles, rel,
			fmt.Sprintf("symlink target %q is absolute; only in-tree relative targets are admissible", target))
		return
	}

	// Condition 3: resolves inside the snapshot. Cleaned lexically against
	// the link's own directory - no filesystem traversal, so nothing can
	// escape during the check itself.
	resolved := filepath.Clean(filepath.Join(filepath.Dir(path), target))
	snapAbs, err1 := filepath.Abs(snapshotRoot)
	resAbs, err2 := filepath.Abs(resolved)
	if err1 != nil || err2 != nil {
		add(RuleNoIrregularFiles, rel, "cannot resolve symlink target for containment check")
		return
	}
	if resAbs != snapAbs && !strings.HasPrefix(resAbs, snapAbs+string(filepath.Separator)) {
		add(RuleNoIrregularFiles, rel,
			fmt.Sprintf("symlink target %q escapes reviewer/repository/", target))
		return
	}

	// Condition 4: the target exists. Condition 5: and is not itself a link.
	info, err := os.Lstat(resolved)
	if err != nil {
		add(RuleNoIrregularFiles, rel,
			fmt.Sprintf("symlink target %q does not exist in the snapshot", target))
		return
	}
	if info.Mode()&os.ModeSymlink != 0 {
		add(RuleNoIrregularFiles, rel,
			fmt.Sprintf("symlink target %q is itself a symlink; chains are not admissible", target))
		return
	}
}

// snapshotPrefix is the region whose vocabulary this project does not
// control.
const snapshotPrefix = "reviewer/repository/"

// checkOracleShapedPath applies the oracle-name heuristic with the strength
// appropriate to where the file sits: strict on miner-authored paths, and
// narrow inside the third-party source snapshot.
func checkOracleShapedPath(rel, basename string, add func(rule, path, detail string)) {
	lower := strings.ToLower(rel)
	lowerBase := strings.ToLower(basename)

	if strings.HasPrefix(lower, snapshotPrefix) {
		if oracleArtifactBasenames[lowerBase] {
			add(RuleOracleShaped, rel,
				fmt.Sprintf("%q is the filename of an oracle-bundle artifact; a source snapshot must not contain one", basename))
			return
		}
		for _, frag := range highSignalOracleSubstrings {
			if strings.Contains(lower, frag) {
				add(RuleOracleShaped, rel,
					fmt.Sprintf("path contains %q, which is specific enough to this benchmark's vocabulary to be suspicious even in third-party source", frag))
				return
			}
		}
		return
	}

	for _, frag := range oracleShapedSubstrings {
		if strings.Contains(lower, frag) {
			add(RuleOracleShaped, rel,
				fmt.Sprintf("path contains %q, which suggests retrospective or outcome material", frag))
			return
		}
	}
}

// checkMetadata returns the schema_version the file carried and whether
// the file was present at all.
// checkMetadata validates reviewer/metadata.json.
//
// The oracle-shaped rule is SPLIT here, and the split is the point.
// Identifier hits - a commit SHA, a PR number, a CVE - are the real oracle
// shape: a reviewer at the cutoff could not have seen them, so they stay a
// hard error. Vocabulary hits are different. The word list was written for
// PATHS, where the miner controls the names; under reviewer-metadata/v1 a
// description was almost never present, so the list was never tested
// against real PR prose. v2 admits title and description for nearly every
// case, and an author writing "this complicated the solution" before merge
// is writing ordinary review-time English, not leaking an outcome.
//
// This is the same reasoning already applied inside reviewer/repository/
// above: a check that fires constantly on clean input gets turned off, and
// that is worse than a narrower check people trust. So vocabulary hits on
// metadata VALUES are recorded as warnings - field and matched text, in the
// sealed report - and never fail the bundle. The word list is unchanged;
// tuning words in and out case by case would be fitting the rule to the
// cohort.
func checkMetadata(metadataPath string, add, warn func(rule, path, detail string)) (string, bool) {
	raw, err := os.ReadFile(metadataPath)
	if err != nil {
		// metadata.json is optional per section 7 ("if a field cannot be
		// reconstructed, the miner omits it" - and a bundle may legitimately
		// carry no reviewer-visible metadata at all).
		return "", false
	}
	rel := "reviewer/metadata.json"

	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		add(RulePinnedSchemas, rel, fmt.Sprintf("is not valid JSON: %v", err))
		return "", true
	}

	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		if !allowedMetadataFields[name] {
			add(RuleMetadataFields, rel,
				fmt.Sprintf("field %q is not reviewer-visible metadata; only schema_version, repository, title, description, cutoff_timestamp, base_branch and commit_messages are permitted", name))
			continue
		}
		if !hasMetadataType(fields[name], metadataFieldTypes[name]) {
			add(RuleMetadataFields, rel,
				fmt.Sprintf("field %q is not a %s; an inadmissible field is absent, never null or another type", name, metadataFieldTypes[name]))
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
		// Identifiers: a hard error, always.
		for _, hit := range identifierHits(value) {
			add(RuleOracleShaped, rel, fmt.Sprintf(
				"field %q contains %s, which a reviewer at the cutoff could not have seen", name, hit))
		}
		// Vocabulary: recorded, never fatal. See the doc comment.
		for _, frag := range oracleShapedSubstrings {
			if strings.Contains(lower, frag) {
				warn(RuleOracleShaped, rel, fmt.Sprintf(
					"field %q contains the word %q (matched text: %q); recorded, not failed - the vocabulary list was written for paths and fires on ordinary review-time prose",
					name, frag, excerptAround(value, frag)))
				break
			}
		}
	}

	version, _ := fields["schema_version"].(string)
	if !acceptedReviewerMetadataSchemas[version] {
		add(RulePinnedSchemas, rel,
			fmt.Sprintf("schema_version is %q, want one of the pinned %v", version, acceptedReviewerMetadataSchemaList()))
	}
	return version, true
}

// identifierHits reports commit SHAs (>= 7 hex at a word boundary), PR
// references and CVE ids in a metadata value. These are the shapes that
// cannot be innocent in reviewer-visible text: the description is written
// before merge, so a fixing SHA or CVE in it means the bundle was built
// from post-merge material.
//
// Deliberately NOT reusing contamscan's rules: those match against a
// specific case's known fixing identifiers, and the validator has no keys
// file - it must reject the SHAPE of an identifier, whatever its value.
func identifierHits(value string) []string {
	var out []string
	for _, m := range metadataSHAPattern.FindAllString(value, -1) {
		out = append(out, fmt.Sprintf("a commit-like hex string (%q)", m))
	}
	for _, p := range metadataPRPatterns {
		for _, m := range p.FindAllString(value, -1) {
			out = append(out, fmt.Sprintf("a pull-request reference (%q)", m))
		}
	}
	for _, m := range metadataCVEPattern.FindAllString(value, -1) {
		out = append(out, fmt.Sprintf("a CVE identifier (%q)", m))
	}
	return out
}

var (
	// A bare 7+ hex run. Anchored to word boundaries so ordinary words and
	// decimal numbers do not match.
	metadataSHAPattern = regexp.MustCompile(`(?i)\b[0-9a-f]{7,40}\b`)
	metadataCVEPattern = regexp.MustCompile(`(?i)\bCVE-\d{4}-\d{4,7}\b`)
	// PR-shaped only: a bare number in prose is a version or a count far
	// more often than a pull request.
	metadataPRPatterns = []*regexp.Regexp{
		regexp.MustCompile(`#\d+\b`),
		regexp.MustCompile(`(?i)\bPR[ -]?#?\d+\b`),
		regexp.MustCompile(`(?i)\bpull/\d+\b`),
		regexp.MustCompile(`(?i)\bpull request #?\d+\b`),
	}
)

// excerptAround returns a short window of text around a match, so the
// warning shows the phrase a reader needs to judge it.
func excerptAround(value, frag string) string {
	lower := strings.ToLower(value)
	i := strings.Index(lower, strings.ToLower(frag))
	if i < 0 {
		return ""
	}
	start, end := i-40, i+len(frag)+40
	if start < 0 {
		start = 0
	}
	if end > len(value) {
		end = len(value)
	}
	return strings.TrimSpace(value[start:end])
}

func acceptedReviewerMetadataSchemaList() []string {
	out := make([]string, 0, len(acceptedReviewerMetadataSchemas))
	for v := range acceptedReviewerMetadataSchemas {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

func hasMetadataType(v any, want string) bool {
	switch want {
	case "string":
		_, ok := v.(string)
		return ok
	case "array of strings":
		items, ok := v.([]any)
		if !ok {
			return false
		}
		for _, it := range items {
			if _, ok := it.(string); !ok {
				return false
			}
		}
		return true
	}
	return false
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

func checkControlManifest(manifestPath string, add func(rule, path, detail string)) (string, bool) {
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		add(RulePinnedSchemas, "control/manifest.json", fmt.Sprintf("cannot read control manifest: %v", err))
		return "", false
	}
	var manifest map[string]any
	if err := json.Unmarshal(raw, &manifest); err != nil {
		add(RulePinnedSchemas, "control/manifest.json", fmt.Sprintf("is not valid JSON: %v", err))
		return "", true
	}
	version, _ := manifest["schema_version"].(string)
	if version != pinnedControlManifestSchema {
		add(RulePinnedSchemas, "control/manifest.json",
			fmt.Sprintf("schema_version is %q, want the pinned %q", version, pinnedControlManifestSchema))
	}
	return version, true
}

// evaluatorOnlyDirSuffixes mark directories holding answer-key material:
// the miner's published probe inputs and contamination keys, and the
// retrospective bundles. A prospective bundle can never legitimately live
// under one - the probe inputs quote the symptom and the keys quote
// post-merge discussion, so a reviewer given either is not reviewing.
var evaluatorOnlyDirSuffixes = []string{"-evaluator-inputs", "-evaluator-only"}

// RefuseEvaluatorOnlyBundle fails closed when a bundle path resolves under
// a directory holding evaluator material.
//
// It lives here rather than in cmd/bench deliberately. The isolation test
// forbids review-path packages from naming evaluator-only paths at all -
// a mount built from a string reaches a model as surely as an import does -
// and a guard that refuses those paths must name them. Putting it with the
// other admissibility checks lets the caller enforce the boundary without
// naming it, and keeps the guard beside the rules it belongs with rather
// than in a main().
//
// The path is resolved through symlinks before matching, so a link into
// evaluator material cannot launder it.
func RefuseEvaluatorOnlyBundle(bundleRoot string) error {
	abs, err := filepath.Abs(bundleRoot)
	if err != nil {
		return fmt.Errorf("boundaryvalidator: resolving bundle path: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	for _, seg := range strings.Split(filepath.ToSlash(abs), "/") {
		for _, suffix := range evaluatorOnlyDirSuffixes {
			if strings.HasSuffix(seg, suffix) {
				return fmt.Errorf("boundaryvalidator: refusing bundle %s: it lies under %q, which holds evaluator-only material (probe inputs, contamination keys, history). A reviewer given that is not reviewing", bundleRoot, seg)
			}
		}
	}
	return nil
}
