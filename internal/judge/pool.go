package judge

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
)

// PooledFinding is one reviewer finding plus the provenance the judge must
// never see.
//
// Arm, RunID and FindingID are held OUTSIDE the judge call and rejoined
// during scoring. Two of the three arms are opus and the natural judge model
// is also opus, on a benchmark whose headline question is whether opus beats
// sonnet - so a judge able to identify the arm has a self-preference problem
// with no clean outside option, since every arm is a Claude model. The
// defence is that it cannot identify the arm, which is worth more than an
// instruction telling it not to care.
type PooledFinding struct {
	// OpaqueID is what the judge sees. It is a truncated hash, carrying no
	// arm, run or ordinal information.
	OpaqueID string

	Arm       string
	RunID     string
	CaseID    string
	FindingID string

	Title string
	Body  string
	Files []string
}

// JudgeView is the finding as the judge receives it: text and an opaque id,
// nothing else. Separate type rather than a marshalling tag so that adding a
// provenance field to PooledFinding cannot silently leak it into a prompt.
type JudgeView struct {
	ID    string   `json:"id"`
	Title string   `json:"title"`
	Body  string   `json:"body"`
	Files []string `json:"files"`
}

// Pool is one case's findings from every arm, judged together in a single
// call.
//
// Pooling is not only a bias defence. Judged per arm, the judge would see a
// different number of findings per call and its calibration could drift with
// list length; pooled, every case is judged once under identical conditions
// and the arms differ only in which findings are present. It is also cheaper:
// one call per case rather than one per case per arm.
type Pool struct {
	CaseID   string
	Findings []PooledFinding
}

// NewPool assigns opaque ids and orders by them.
//
// The id is a truncated SHA-256 of the case, arm and finding id. Ordering by
// that hash decorrelates position from arm while staying deterministic, which
// a random shuffle would not: the same inputs must produce the same pool, or
// a judge run cannot be reproduced from its fingerprints.
func NewPool(caseID string, findings []PooledFinding) Pool {
	out := make([]PooledFinding, 0, len(findings))
	for _, f := range findings {
		f.CaseID = caseID
		f.OpaqueID = opaqueID(caseID, f.Arm, f.FindingID)
		out = append(out, f)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].OpaqueID < out[j].OpaqueID })
	return Pool{CaseID: caseID, Findings: out}
}

func opaqueID(caseID, arm, findingID string) string {
	sum := sha256.Sum256([]byte(caseID + "\x00" + arm + "\x00" + findingID))
	return "f" + hex.EncodeToString(sum[:])[:10]
}

// View returns exactly what goes into the judge prompt.
func (p Pool) View() []JudgeView {
	out := make([]JudgeView, 0, len(p.Findings))
	for _, f := range p.Findings {
		out = append(out, JudgeView{ID: f.OpaqueID, Title: f.Title, Body: f.Body, Files: f.Files})
	}
	return out
}

// Index maps opaque id back to provenance, for scoring after the judge has
// run.
func (p Pool) Index() map[string]PooledFinding {
	m := make(map[string]PooledFinding, len(p.Findings))
	for _, f := range p.Findings {
		m[f.OpaqueID] = f
	}
	return m
}

// Arms lists the arms represented in the pool, sorted.
func (p Pool) Arms() []string {
	seen := map[string]bool{}
	var out []string
	for _, f := range p.Findings {
		if !seen[f.Arm] {
			seen[f.Arm] = true
			out = append(out, f.Arm)
		}
	}
	sort.Strings(out)
	return out
}

// CheckOpaque reports any way the judge's view could betray provenance. It is
// a test helper promoted to the package because the property it checks is the
// whole point of pooling, and a leak would be silent.
//
// It asserts the id IS the hash, rather than scanning for provenance
// substrings. A substring scan is the obvious implementation and it is wrong:
// a 10-character hex id contains short strings like "f1" by coincidence
// often, so the check fails at random on findings whose ids are short. Worse,
// it would pass by luck rather than by construction. Deriving the id purely
// from a hash and verifying that derivation is the property actually wanted.
//
// Arm and RunID are still scanned because those are long, meaningful strings
// whose appearance could not be coincidence.
func (p Pool) CheckOpaque() error {
	for _, f := range p.Findings {
		want := opaqueID(p.CaseID, f.Arm, f.FindingID)
		if f.OpaqueID != want {
			return fmt.Errorf("finding %s/%s has id %q, which is not its hash %q",
				f.Arm, f.FindingID, f.OpaqueID, want)
		}
		if f.Arm != "" && containsFold(f.OpaqueID, f.Arm) {
			return fmt.Errorf("opaque id %q contains the arm name %q", f.OpaqueID, f.Arm)
		}
		if f.RunID != "" && containsFold(f.OpaqueID, f.RunID) {
			return fmt.Errorf("opaque id %q contains the run id %q", f.OpaqueID, f.RunID)
		}
	}
	return nil
}

func containsFold(haystack, needle string) bool {
	if needle == "" {
		return false
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if equalFold(haystack[i:i+len(needle)], needle) {
			return true
		}
	}
	return false
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		x, y := a[i], b[i]
		if 'A' <= x && x <= 'Z' {
			x += 'a' - 'A'
		}
		if 'A' <= y && y <= 'Z' {
			y += 'a' - 'A'
		}
		if x != y {
			return false
		}
	}
	return true
}
