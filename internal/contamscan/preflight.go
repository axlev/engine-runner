package contamscan

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// The preflight is the EXACT counterpart of the section 8 post-hoc scan.
// Section 8 asks whether a review OUTPUT names the fix; this asks whether a
// bundle's reviewer-visible INPUT does, before any reviewer sees it.
//
// It exists because the boundary validator cannot answer that question. The
// validator runs without keys - by design, since it is on the path a
// reviewer's bundle takes - so it can only match the SHAPE of an
// identifier, and under reviewer-metadata/v2 a shape match is no longer
// evidence of anything: the description is admitted only when its edit
// history proves it pre-merge, and pre-merge prose legitimately carries
// commit SHAs and PR numbers. This check has the keys, so it matches the
// case's ACTUAL fixing identifiers and rejects on a real leak.
//
// It is therefore load-bearing, not advisory: once the validator records
// rather than rejects v2 identifier hits, this is the only thing standing
// between a leaked fixing SHA and a reviewer. A cohort must not be
// published until it passes.
//
// Evaluator-side, and it must stay there: it reads contamination keys,
// which quote the fix. cmd/bench must never invoke it.

const PreflightSchemaVersion = "contamination-preflight/v1"

// Preflight is contamination-preflight/v1 for one case.
type Preflight struct {
	SchemaVersion string `json:"schema_version"`
	CaseID        string `json:"case_id"`
	KeysSHA256    string `json:"keys_sha256"`
	Rules         Rules  `json:"rules"`
	// Sources are the reviewer-visible files scanned, relative to the
	// bundle, so the report says what was covered rather than implying it.
	Sources []string `json:"sources_scanned"`
	Hits    []Hit    `json:"hits"`
	Fail    bool     `json:"fail"`
}

// diffHeaderPrefixes are git's own metadata lines. Their hex is blob and
// tree ids, not commit ids, and they are present in every diff by
// construction - scanning them would fail every bundle on a prefix
// coincidence while telling us nothing about whether the FIX leaked. The
// diff BODY is still scanned: a fixing SHA quoted in a code comment or a
// commit message inside the patch is a real leak.
var diffHeaderPrefixes = []string{
	"index ",
	"diff --git ",
	"similarity index ",
	"dissimilarity index ",
	"old mode ",
	"new mode ",
	"deleted file mode ",
	"new file mode ",
	"rename from ",
	"rename to ",
	"copy from ",
	"copy to ",
	"GIT binary patch",
}

func isDiffHeader(line string) bool {
	for _, p := range diffHeaderPrefixes {
		if strings.HasPrefix(line, p) {
			return true
		}
	}
	return false
}

// stripDiffHeaders removes git metadata lines from a patch.
func stripDiffHeaders(patch string) string {
	var kept []string
	for _, line := range strings.Split(patch, "\n") {
		if isDiffHeader(line) {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

// PreflightCase scans one bundle's reviewer-visible text against that
// case's keys. bundleRoot is the case directory (containing reviewer/).
func PreflightCase(bundleRoot string, keys Keys, keysSHA string) (Preflight, error) {
	p := Preflight{
		SchemaVersion: PreflightSchemaVersion,
		CaseID:        keys.CaseID,
		KeysSHA256:    keysSHA,
		Rules:         rules(nil),
		Sources:       []string{},
		Hits:          []Hit{},
	}
	// The discussion rule does not apply to inputs: there is nothing the
	// reviewer was "shown" to subtract, and a bundle quoting its own PR
	// description is not a leak. Record that so the report is not read as
	// having applied it.
	p.Rules.ShingleTokens = 0
	p.Rules.DiscussionSkipsPaths = nil

	metaPath := filepath.Join(bundleRoot, "reviewer", "metadata.json")
	if raw, err := os.ReadFile(metaPath); err == nil {
		p.Sources = append(p.Sources, "reviewer/metadata.json")
		p.Hits = append(p.Hits, ScanIdentifiers("metadata", string(raw), keys)...)
	} else if !os.IsNotExist(err) {
		return Preflight{}, fmt.Errorf("contamscan: %s: %w", metaPath, err)
	}

	diffPath := filepath.Join(bundleRoot, "reviewer", "diff.patch")
	raw, err := os.ReadFile(diffPath)
	if err != nil {
		return Preflight{}, fmt.Errorf("contamscan: %s: %w", diffPath, err)
	}
	p.Sources = append(p.Sources, "reviewer/diff.patch (git metadata lines excluded)")
	p.Hits = append(p.Hits, ScanIdentifiers("diff", stripDiffHeaders(string(raw)), keys)...)

	p.Fail = len(p.Hits) > 0
	return p, nil
}

// PreflightSummary aggregates cases.
type PreflightSummary struct {
	SchemaVersion string   `json:"schema_version"`
	Scanned       int      `json:"scanned"`
	Failed        []string `json:"failed_cases"`
	Skipped       []string `json:"skipped_cases_without_keys"`
	Note          string   `json:"note"`
}

func SummarisePreflight(results []Preflight, skipped []string) PreflightSummary {
	s := PreflightSummary{
		SchemaVersion: "contamination-preflight-summary/v1",
		Scanned:       len(results),
		Failed:        []string{},
		Skipped:       skipped,
		Note: "a failing case's reviewer-visible text contains one of its own fixing identifiers; it must not be published. " +
			"A case with no keys file is NOT cleared - it was not checked.",
	}
	if s.Skipped == nil {
		s.Skipped = []string{}
	}
	for _, r := range results {
		if r.Fail {
			s.Failed = append(s.Failed, r.CaseID)
		}
	}
	sort.Strings(s.Failed)
	return s
}
