// Package contamscan implements the pre-registration's section 8 post-hoc
// contamination scan: every sealed review document is searched for the
// fixing commit SHA, the fixing PR number, CVE identifiers, and phrases
// lifted from post-merge discussion. Any hit voids the run for that arm.
//
// This is evaluator-side code. The keys it matches against are answer-key
// data - the fixing commit IS the outcome - so nothing on the review path
// may import this package, and it in turn imports nothing from the review
// path. It reads sealed results, a keys file, and at most the
// reviewer-visible half of a prospective bundle (for the exclusion set).
//
// Every rule the scanner applies is written into its output. A hit that
// cannot be traced to a stated rule is not a hit.
package contamscan

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	KeysSchemaVersion = "contamination-keys/v1"
	ScanSchemaVersion = "contamination-scan/v1"

	// MinSHAPrefix is section 8's "any prefix >= 7".
	MinSHAPrefix = 7

	// ShingleTokens is the operational definition of "a phrase lifted
	// from post-merge discussion": this many consecutive tokens in common.
	// Section 8 does not define the phrase test; this value is recorded in
	// every output and is the thing to register.
	ShingleTokens = 8
)

// Keys is contamination-keys/v1: what a reviewer could only know by having
// seen the future of this change.
type Keys struct {
	SchemaVersion   string       `json:"schema_version"`
	CaseID          string       `json:"case_id"`
	FixingSHAs      []string     `json:"fixing_shas"`
	FixingPRNumbers []int        `json:"fixing_pr_numbers"`
	CVEIDs          []string     `json:"cve_ids"`
	Discussion      []Discussion `json:"discussion"`
	Provenance      any          `json:"provenance"`
}

type Discussion struct {
	Source string `json:"source"`
	Body   string `json:"body"`
}

// Rules is the rule set as applied, written into every scan.
type Rules struct {
	SHAMinPrefix  int      `json:"sha_min_prefix"`
	PRPatterns    []string `json:"pr_patterns"`
	CVEPattern    string   `json:"cve_pattern"`
	ShingleTokens int      `json:"shingle_tokens"`
	// ExclusionSources lists what the discussion rule subtracted before
	// matching: the reviewer-visible diff and metadata, plus every
	// repository file the review cited (reviewer/repository/<path>).
	ExclusionSources []string `json:"exclusion_sources"`
	// DiscussionSkipsPaths names the JSON paths the discussion rule does
	// not scan: excerpts are verbatim file copies by contract, so matching
	// them against discussion could only manufacture a void. The other
	// three rules still scan them.
	DiscussionSkipsPaths []string `json:"discussion_skips_paths"`
}

// Hit is one match. Kind is sha, pr, cve, or discussion.
type Hit struct {
	Kind     string `json:"kind"`
	Stage    string `json:"stage"`
	JSONPath string `json:"json_path"`
	Matched  string `json:"matched"`
	// KeyRef names what it matched: the SHA, the PR number, the CVE id (or
	// "other" for a CVE not in the keys), or the discussion source.
	KeyRef string `json:"key_ref"`
	// Unexcluded is set on discussion hits made without the bundle's
	// exclusion set, which cannot tell a lifted phrase from a quoted diff.
	Unexcluded bool `json:"unexcluded,omitempty"`
}

// Scan is contamination-scan/v1 for one sealed run.
type Scan struct {
	SchemaVersion    string   `json:"schema_version"`
	RunID            string   `json:"run_id"`
	CaseID           string   `json:"case_id"`
	Arm              string   `json:"arm"`
	KeysSHA256       string   `json:"keys_sha256"`
	Rules            Rules    `json:"rules"`
	ExclusionApplied bool     `json:"exclusion_applied"`
	StagesScanned    []string `json:"stages_scanned"`
	Hits             []Hit    `json:"hits"`
	Void             bool     `json:"void"`
}

var (
	hexToken   = regexp.MustCompile(`(?i)\b[0-9a-f]{7,40}\b`)
	cvePattern = regexp.MustCompile(`(?i)\bCVE-\d{4}-\d{4,7}\b`)
	// PR-shaped contexts only: a bare number in review text is a line
	// number far more often than a PR.
	prPatterns = []*regexp.Regexp{
		regexp.MustCompile(`#(\d+)\b`),
		regexp.MustCompile(`(?i)\bPR[ -]?#?(\d+)\b`),
		regexp.MustCompile(`(?i)\bpull/(\d+)\b`),
		regexp.MustCompile(`(?i)\bpull request #?(\d+)\b`),
	}
	nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)
)

const discussionSkipSuffix = ".excerpt"

func rules(exclusionSources []string) Rules {
	pats := make([]string, 0, len(prPatterns))
	for _, p := range prPatterns {
		pats = append(pats, p.String())
	}
	return Rules{
		SHAMinPrefix:         MinSHAPrefix,
		PRPatterns:           pats,
		CVEPattern:           cvePattern.String(),
		ShingleTokens:        ShingleTokens,
		ExclusionSources:     exclusionSources,
		DiscussionSkipsPaths: []string{"*" + discussionSkipSuffix},
	}
}

// LoadKeys reads and checks a contamination-keys/v1 file, returning it
// with the file's sha256.
func LoadKeys(path string) (Keys, string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Keys{}, "", err
	}
	var k Keys
	if err := json.Unmarshal(raw, &k); err != nil {
		return Keys{}, "", fmt.Errorf("contamscan: parsing %s: %w", path, err)
	}
	if k.SchemaVersion != KeysSchemaVersion {
		return Keys{}, "", fmt.Errorf("contamscan: %s is %q, want %s", path, k.SchemaVersion, KeysSchemaVersion)
	}
	if k.CaseID == "" {
		return Keys{}, "", fmt.Errorf("contamscan: %s has no case_id", path)
	}
	for _, s := range k.FixingSHAs {
		if !regexp.MustCompile(`^[0-9a-f]{40}$`).MatchString(strings.ToLower(s)) {
			return Keys{}, "", fmt.Errorf("contamscan: %s: fixing sha %q is not a full 40-hex commit", path, s)
		}
	}
	sum := sha256.Sum256(raw)
	return k, hex.EncodeToString(sum[:]), nil
}

// stringValue is one string found in a review document.
type stringValue struct {
	Path  string
	Value string
}

// collectStrings walks a decoded JSON document and returns every string
// value with its path, in deterministic order.
func collectStrings(node any, path string, out *[]stringValue) {
	switch v := node.(type) {
	case string:
		*out = append(*out, stringValue{Path: path, Value: v})
	case map[string]any:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			collectStrings(v[k], path+"."+k, out)
		}
	case []any:
		for i, item := range v {
			collectStrings(item, fmt.Sprintf("%s[%d]", path, i), out)
		}
	}
}

// tokens lowercases and splits on non-alphanumerics.
func tokens(s string) []string {
	parts := nonAlnum.Split(strings.ToLower(s), -1)
	out := parts[:0]
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// shingles returns every ShingleTokens-long window of s, as joined strings.
func shingles(s string) []string {
	t := tokens(s)
	if len(t) < ShingleTokens {
		return nil
	}
	out := make([]string, 0, len(t)-ShingleTokens+1)
	for i := 0; i+ShingleTokens <= len(t); i++ {
		out = append(out, strings.Join(t[i:i+ShingleTokens], " "))
	}
	return out
}

// Exclusion is the set of shingles a reviewer could have produced by
// quoting what it was legitimately shown.
type Exclusion struct {
	Sources  []string
	shingles map[string]bool
}

// exclusionFor builds the exclusion set from the reviewer-visible half of a
// prospective bundle: diff.patch, metadata.json, and every repository file
// the review cited. The reviewer copies excerpts from those files by
// instruction, and a fix PR's discussion quotes the same lines, so without
// them a quoted line reads as a lifted phrase. Files the review cites that
// are not in the snapshot are skipped: the evidence check at run time
// already failed any citation outside it.
func exclusionFor(bundleRoot string, cited []string) (Exclusion, error) {
	ex := Exclusion{shingles: map[string]bool{}}
	add := func(rel string, required bool) error {
		raw, err := os.ReadFile(filepath.Join(bundleRoot, filepath.FromSlash(rel)))
		if err != nil {
			if required {
				return fmt.Errorf("contamscan: exclusion source %s: %w", rel, err)
			}
			return nil
		}
		for _, sh := range shingles(string(raw)) {
			ex.shingles[sh] = true
		}
		ex.Sources = append(ex.Sources, rel)
		return nil
	}
	for _, name := range []string{"reviewer/diff.patch", "reviewer/metadata.json"} {
		if err := add(name, true); err != nil {
			return Exclusion{}, err
		}
	}
	for _, c := range cited {
		clean := filepath.ToSlash(filepath.Clean(c))
		if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
			continue
		}
		if err := add("reviewer/repository/"+clean, false); err != nil {
			return Exclusion{}, err
		}
	}
	return ex, nil
}

// citedFiles returns every distinct "file" string value in the documents,
// sorted.
func citedFiles(docs []any) []string {
	seen := map[string]bool{}
	var walk func(any)
	walk = func(node any) {
		switch v := node.(type) {
		case map[string]any:
			if f, ok := v["file"].(string); ok && f != "" {
				seen[f] = true
			}
			for _, child := range v {
				walk(child)
			}
		case []any:
			for _, item := range v {
				walk(item)
			}
		}
	}
	for _, d := range docs {
		walk(d)
	}
	out := make([]string, 0, len(seen))
	for f := range seen {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

// ScanRun scans one sealed run directory against keys. bundleRoot is the
// case's prospective bundle, used only for the exclusion set; when empty,
// discussion hits are made without exclusion and say so.
func ScanRun(runDir string, keys Keys, keysSHA string, bundleRoot string) (Scan, error) {
	var manifest struct {
		RunID           string `json:"run_id"`
		CaseID          string `json:"case_id"`
		ProtocolVersion string `json:"protocol_version"`
	}
	raw, err := os.ReadFile(filepath.Join(runDir, "run.json"))
	if err != nil {
		return Scan{}, fmt.Errorf("contamscan: %s has no run.json: %w", runDir, err)
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return Scan{}, fmt.Errorf("contamscan: parsing run.json in %s: %w", runDir, err)
	}
	if manifest.CaseID != keys.CaseID {
		return Scan{}, fmt.Errorf("contamscan: run %s is case %q but keys are for %q", manifest.RunID, manifest.CaseID, keys.CaseID)
	}

	// Read every review first: the exclusion set depends on what they cite.
	type review struct {
		stage string
		doc   any
	}
	var reviews []review
	paths, _ := filepath.Glob(filepath.Join(runDir, "stages", "*", "review-*.json"))
	sort.Strings(paths)
	var docs []any
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			return Scan{}, err
		}
		var doc any
		if err := json.Unmarshal(raw, &doc); err != nil {
			return Scan{}, fmt.Errorf("contamscan: parsing %s: %w", path, err)
		}
		reviews = append(reviews, review{stage: filepath.Base(filepath.Dir(path)), doc: doc})
		docs = append(docs, doc)
	}

	var ex *Exclusion
	if bundleRoot != "" {
		e, err := exclusionFor(bundleRoot, citedFiles(docs))
		if err != nil {
			return Scan{}, err
		}
		ex = &e
	}
	var sources []string
	if ex != nil {
		sources = ex.Sources
	}
	scan := Scan{
		SchemaVersion:    ScanSchemaVersion,
		RunID:            manifest.RunID,
		CaseID:           manifest.CaseID,
		Arm:              manifest.ProtocolVersion,
		KeysSHA256:       keysSHA,
		Rules:            rules(sources),
		ExclusionApplied: ex != nil,
		StagesScanned:    []string{},
		Hits:             []Hit{},
	}

	// Discussion shingles, minus what the reviewer was shown.
	discussion := map[string]string{} // shingle -> source
	for _, d := range keys.Discussion {
		for _, sh := range shingles(d.Body) {
			if ex != nil && ex.shingles[sh] {
				continue
			}
			if _, seen := discussion[sh]; !seen {
				discussion[sh] = d.Source
			}
		}
	}
	shas := make([]string, 0, len(keys.FixingSHAs))
	for _, s := range keys.FixingSHAs {
		shas = append(shas, strings.ToLower(s))
	}
	prs := map[string]bool{}
	for _, n := range keys.FixingPRNumbers {
		prs[fmt.Sprint(n)] = true
	}
	cves := map[string]bool{}
	for _, c := range keys.CVEIDs {
		cves[strings.ToUpper(c)] = true
	}

	for _, r := range reviews {
		scan.StagesScanned = append(scan.StagesScanned, r.stage)
		var values []stringValue
		collectStrings(r.doc, "$", &values)
		for _, sv := range values {
			scan.Hits = append(scan.Hits, scanString(r.stage, sv, shas, prs, cves, discussion, ex == nil)...)
		}
	}
	scan.Void = len(scan.Hits) > 0
	return scan, nil
}

func scanString(stage string, sv stringValue, shas []string, prs, cves map[string]bool, discussion map[string]string, unexcluded bool) []Hit {
	var hits []Hit
	for _, tok := range hexToken.FindAllString(sv.Value, -1) {
		lower := strings.ToLower(tok)
		for _, sha := range shas {
			if len(lower) >= MinSHAPrefix && strings.HasPrefix(sha, lower) {
				hits = append(hits, Hit{Kind: "sha", Stage: stage, JSONPath: sv.Path, Matched: tok, KeyRef: sha})
				break
			}
		}
	}
	for _, p := range prPatterns {
		for _, m := range p.FindAllStringSubmatch(sv.Value, -1) {
			if prs[m[1]] {
				hits = append(hits, Hit{Kind: "pr", Stage: stage, JSONPath: sv.Path, Matched: m[0], KeyRef: m[1]})
			}
		}
	}
	for _, c := range cvePattern.FindAllString(sv.Value, -1) {
		ref := "other"
		if cves[strings.ToUpper(c)] {
			ref = strings.ToUpper(c)
		}
		hits = append(hits, Hit{Kind: "cve", Stage: stage, JSONPath: sv.Path, Matched: c, KeyRef: ref})
	}
	if len(discussion) > 0 && !strings.HasSuffix(sv.Path, discussionSkipSuffix) {
		hits = append(hits, discussionHits(stage, sv, discussion, unexcluded)...)
	}
	return hits
}

// discussionHits reports each maximal run of consecutive matching shingles
// as one hit whose Matched is the whole shared span, so a lifted sentence
// is one hit and not one per window.
func discussionHits(stage string, sv stringValue, discussion map[string]string, unexcluded bool) []Hit {
	t := tokens(sv.Value)
	if len(t) < ShingleTokens {
		return nil
	}
	var hits []Hit
	start := -1
	source := ""
	flush := func(end int) {
		if start < 0 {
			return
		}
		hits = append(hits, Hit{
			Kind: "discussion", Stage: stage, JSONPath: sv.Path,
			Matched: strings.Join(t[start:end+ShingleTokens-1], " "),
			KeyRef:  source, Unexcluded: unexcluded,
		})
		start = -1
	}
	for i := 0; i+ShingleTokens <= len(t); i++ {
		src, ok := discussion[strings.Join(t[i:i+ShingleTokens], " ")]
		if ok && start < 0 {
			start, source = i, src
		} else if !ok && start >= 0 {
			flush(i)
		}
	}
	flush(len(t) - ShingleTokens + 1)
	return hits
}

// Summary aggregates scans per arm.
type Summary struct {
	SchemaVersion string              `json:"schema_version"`
	KeysDir       string              `json:"keys_dir"`
	Arms          map[string]ArmCount `json:"arms"`
	Voided        []VoidedRun         `json:"voided"`
	Skipped       []string            `json:"skipped_runs_without_keys"`
}

type ArmCount struct {
	Scanned int `json:"scanned"`
	Voided  int `json:"voided"`
}

type VoidedRun struct {
	RunID  string   `json:"run_id"`
	CaseID string   `json:"case_id"`
	Arm    string   `json:"arm"`
	Kinds  []string `json:"kinds"`
}

func Summarise(scans []Scan, skipped []string, keysDir string) Summary {
	s := Summary{SchemaVersion: "contamination-summary/v1", KeysDir: keysDir, Arms: map[string]ArmCount{}, Voided: []VoidedRun{}, Skipped: skipped}
	for _, sc := range scans {
		c := s.Arms[sc.Arm]
		c.Scanned++
		if sc.Void {
			c.Voided++
			kinds := map[string]bool{}
			for _, h := range sc.Hits {
				kinds[h.Kind] = true
			}
			var ks []string
			for k := range kinds {
				ks = append(ks, k)
			}
			sort.Strings(ks)
			s.Voided = append(s.Voided, VoidedRun{RunID: sc.RunID, CaseID: sc.CaseID, Arm: sc.Arm, Kinds: ks})
		}
		s.Arms[sc.Arm] = c
	}
	if s.Skipped == nil {
		s.Skipped = []string{}
	}
	return s
}
