package orchestrator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Evidence citations are the substance of a staged review: reasoner-2's
// entire job is building causal chains, and a chain is only worth something
// if its steps point at code that exists. The schemas require only "file",
// leaving start_line, end_line and excerpt unchecked - so a stage could
// cite a plausible-looking location it never read and be scored as having
// substantiated a finding.
//
// This closes that: every citation must name a file present in the
// reviewer's own snapshot, and any line range must fall inside it.
//
// Citations are found by walking the whole document rather than by reading
// known paths (findings[].evidence[], assessments[].causal_chain[],
// new_findings[].evidence[]). A generic walk covers all three schemas today
// and any citation-bearing field added later, which a hand-listed traversal
// would silently skip - and silently skipping is the failure this exists to
// prevent.

// evidenceCitation is one reference from a review document to source.
type evidenceCitation struct {
	File      string
	StartLine int
	EndLine   int
	Excerpt   string
}

// collectCitations walks any decoded review document and returns every
// object carrying a "file" string. Order is deterministic: maps are visited
// by sorted key, so two runs over the same document report the same
// problems in the same order, which a benchmark artifact requires.
func collectCitations(node interface{}, out *[]evidenceCitation) {
	switch v := node.(type) {
	case map[string]interface{}:
		if file, ok := v["file"].(string); ok && file != "" {
			c := evidenceCitation{File: file}
			if n, ok := v["start_line"].(float64); ok {
				c.StartLine = int(n)
			}
			if n, ok := v["end_line"].(float64); ok {
				c.EndLine = int(n)
			}
			if s, ok := v["excerpt"].(string); ok {
				c.Excerpt = s
			}
			*out = append(*out, c)
		}
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			collectCitations(v[k], out)
		}
	case []interface{}:
		for _, item := range v {
			collectCitations(item, out)
		}
	}
}

// validateEvidence checks every citation in a stage's output against the
// snapshot the reviewer was given.
//
// It returns a fatal error for a citation that cannot be true - a file that
// is absent, or a line range outside the file - because those are claims
// about code that does not exist, and the retry policy exists to give a
// stage another attempt at exactly that kind of mistake.
//
// Excerpt mismatches come back as warnings instead. A model that quotes
// with different whitespace, or elides the middle of a hunk, has still
// pointed at the right place; failing a paid stage over formatting would
// repeat the mistake that --json-schema was added to fix.
func validateEvidence(outputPath, bundleRoot string) (error, []string) {
	raw, err := os.ReadFile(outputPath)
	if err != nil {
		return fmt.Errorf("evidence: reading %s: %w", outputPath, err), nil
	}
	var doc interface{}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("evidence: parsing %s: %w", outputPath, err), nil
	}

	var citations []evidenceCitation
	collectCitations(doc, &citations)

	snapshot := filepath.Join(bundleRoot, "reviewer", "repository")
	var warnings []string

	for _, c := range citations {
		// Citations are repository-relative. A path that escapes the
		// snapshot is rejected outright rather than resolved: "../" in a
		// citation is not a typo worth accommodating.
		clean := filepath.Clean(c.File)
		if filepath.IsAbs(clean) || strings.HasPrefix(clean, "..") {
			return fmt.Errorf("evidence: citation %q is not a path inside the reviewer snapshot", c.File), warnings
		}

		body, err := os.ReadFile(filepath.Join(snapshot, clean))
		if err != nil {
			return fmt.Errorf("evidence: cites %s, which is not in the reviewer snapshot", c.File), warnings
		}

		if c.StartLine == 0 && c.EndLine == 0 {
			continue // file-level citation; the schemas allow it
		}

		lines := strings.Split(string(body), "\n")
		end := c.EndLine
		if end == 0 {
			end = c.StartLine
		}
		if c.StartLine < 1 || end < c.StartLine || end > len(lines) {
			return fmt.Errorf("evidence: cites %s:%d-%d, outside a %d-line file",
				c.File, c.StartLine, end, len(lines)), warnings
		}

		if c.Excerpt == "" {
			continue
		}
		actual := strings.Join(lines[c.StartLine-1:end], "\n")
		if strings.TrimSpace(actual) != strings.TrimSpace(c.Excerpt) {
			warnings = append(warnings, fmt.Sprintf(
				"citation %s:%d-%d quotes text that does not match the file", c.File, c.StartLine, end))
		}
	}

	return nil, warnings
}
