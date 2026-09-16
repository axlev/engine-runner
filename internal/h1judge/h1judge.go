// Package h1judge wires the pilot's mechanism-agreement judge to H1's
// section 6 reason match.
//
// It reuses internal/judge wholesale - the opaque pooling, the four
// verdicts, the two prompts, the scoring - because the alternative is a
// second judge that can drift from the one whose numbers are already
// published. What changes is the input, and two things about it matter:
//
// ONE FINDING PER ARM, NOT ALL OF THEM. Section 6 judges "the case's
// highest-ranked finding". The pilot pooled every finding a reviewer
// produced; H1 pools exactly one per arm - the primary from verdict.json,
// which under T is stage B's first surviving assessment and under G is
// findings[0]. That is a stricter test than the pilot's: a reviewer cannot
// be credited for burying the right answer at position nine, and ranking
// badly costs the same as not finding it.
//
// TRUE POSITIVES ONLY. Section 6 scores "each RISKY true positive", so a
// case is judged only where the arm said RISKY and the label says positive.
// A RISKY call on a negative is a false positive and is already counted in
// precision; sending it to the judge would ask "does this match the fix?"
// about a case with no fix.
//
// Evaluator-side: it reads the fixing diff, which is the answer.
package h1judge

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/axlev/engine-runner/internal/judge"
)

const SchemaVersion = "h1-judge/v1"

// Candidate is one arm's primary finding on one case, eligible for judging.
type Candidate struct {
	CaseID    string
	Arm       string
	RunID     string
	FindingID string
	Mechanism string
	File      string
	Line      int
}

// Report is h1-judge/v1 for one case: the judged verdicts for every arm's
// primary finding, plus the provenance needed to read them.
type Report struct {
	SchemaVersion string `json:"schema_version"`
	CaseID        string `json:"case_id"`
	JudgeModel    string `json:"judge_model"`
	// InFamily records that the judge shares a model family with the
	// treatment arm. Section 6 requires this stated on every figure
	// derived from it, so it travels with the data rather than relying on
	// whoever writes the prose to remember.
	InFamily     bool              `json:"in_family"`
	PromptHashes map[string]string `json:"prompt_hashes"`
	FixSHA256    string            `json:"fix_patch_sha256"`
	// FixSHAs are the commit SHAs the judged patch actually contains, so a
	// verdict can be traced to the commits it was judged against without
	// re-reading the patch.
	FixSHAs []string `json:"fix_shas"`

	// Mechanisms are the fix descriptions, written without sight of any
	// finding.
	Mechanisms []Mechanism `json:"mechanisms"`

	// Verdicts are keyed by arm, since each arm contributes exactly one
	// finding. The opaque id the judge saw is recorded so a verdict can be
	// traced back without re-deriving the pool.
	Verdicts map[string]ArmVerdict `json:"verdicts"`
}

type Mechanism struct {
	FixID       string `json:"fix_id"`
	Description string `json:"description"`
}

type ArmVerdict struct {
	Arm       string `json:"arm"`
	RunID     string `json:"run_id"`
	FindingID string `json:"finding_id"`
	OpaqueID  string `json:"opaque_id"`
	Agreement string `json:"agreement"`
	FixID     string `json:"fix_id,omitempty"`
	Rationale string `json:"rationale"`
}

// Summary is the reason-match figure h1score consumes.
type Summary struct {
	SchemaVersion string                `json:"schema_version"`
	JudgeModel    string                `json:"judge_model"`
	InFamily      bool                  `json:"in_family"`
	Arms          map[string]ArmSummary `json:"arms"`
	Note          string                `json:"note"`
}

type ArmSummary struct {
	Judged int `json:"judged"`
	// Counts per verdict, all four reported. MECHANISM alone is the §2
	// criterion; LOCALITY_ONLY is reported beside it as the rationalisation
	// tell, because a high MECHANISM count with near-zero LOCALITY_ONLY is
	// evidence the judge is agreeing too easily rather than that reviewers
	// are right.
	Mechanism    int      `json:"mechanism"`
	LocalityOnly int      `json:"locality_only"`
	None         int      `json:"none"`
	TooVague     int      `json:"too_vague"`
	ReasonMatch  *float64 `json:"reason_match_rate"`
	LocalityRate *float64 `json:"locality_only_rate"`
	Absent       string   `json:"absent,omitempty"`
}

// EligibleCandidates selects the arm/case pairs section 6 judges: the arm
// called RISKY and the case is a labelled positive.
//
// verdictOf returns the arm's sealed verdict for a case; isPositive reports
// the label. Both are injected so this package does not depend on h1score
// and cannot be tempted to reach into the results tree itself.
func EligibleCandidates(caseIDs []string, arms []string,
	verdictOf func(arm, caseID string) (Candidate, bool, bool),
	isPositive func(caseID string) bool) []Candidate {

	var out []Candidate
	for _, id := range caseIDs {
		if !isPositive(id) {
			continue
		}
		for _, arm := range arms {
			c, risky, ok := verdictOf(arm, id)
			if !ok || !risky {
				continue
			}
			if strings.TrimSpace(c.Mechanism) == "" {
				// A RISKY verdict with no primary finding cannot happen
				// through the scorer, which only says RISKY on the strength
				// of one. Refusing to invent a blank finding here means a
				// malformed result shows up as a missing verdict rather
				// than as a TOO_VAGUE the reviewer never earned.
				continue
			}
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CaseID != out[j].CaseID {
			return out[i].CaseID < out[j].CaseID
		}
		return out[i].Arm < out[j].Arm
	})
	return out
}

// PoolFor builds the opaque pool for one case from its candidates. The
// judge sees the mechanism text and the file, never the arm.
func PoolFor(caseID string, candidates []Candidate) judge.Pool {
	var findings []judge.PooledFinding
	for _, c := range candidates {
		files := []string{}
		if c.File != "" {
			files = append(files, c.File)
		}
		findings = append(findings, judge.PooledFinding{
			Arm:       c.Arm,
			RunID:     c.RunID,
			CaseID:    c.CaseID,
			FindingID: c.FindingID,
			Title:     firstLine(c.Mechanism),
			Body:      c.Mechanism,
			Files:     files,
		})
	}
	return judge.NewPool(caseID, findings)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '.'); i > 0 && i < 120 {
		return strings.TrimSpace(s[:i+1])
	}
	if len(s) > 120 {
		return strings.TrimSpace(s[:120])
	}
	return strings.TrimSpace(s)
}

// Summarise turns per-case reports into the reason-match figure.
//
// The denominator is judged candidates, not cohort cases: a case no arm
// called RISKY contributes nothing, and counting it as a miss would be the
// denominator error that disqualified the correspondence metric.
func Summarise(reports []Report, judgeModel string, inFamily bool) Summary {
	s := Summary{
		SchemaVersion: SchemaVersion,
		JudgeModel:    judgeModel,
		InFamily:      inFamily,
		Arms:          map[string]ArmSummary{},
		Note: "Denominator is RISKY true positives judged, not cohort cases. " +
			"MECHANISM alone counts toward the section 2 criterion; LOCALITY_ONLY is " +
			"reported beside it as the rationalisation tell.",
	}
	for _, r := range reports {
		for arm, v := range r.Verdicts {
			a := s.Arms[arm]
			a.Judged++
			switch judge.Agreement(v.Agreement) {
			case judge.AgreementMechanism:
				a.Mechanism++
			case judge.AgreementLocality:
				a.LocalityOnly++
			case judge.AgreementTooVague:
				a.TooVague++
			default:
				a.None++
			}
			s.Arms[arm] = a
		}
	}
	for arm, a := range s.Arms {
		if a.Judged == 0 {
			a.Absent = "no RISKY true positive was judged for this arm"
		} else {
			m := float64(a.Mechanism) / float64(a.Judged)
			l := float64(a.LocalityOnly) / float64(a.Judged)
			a.ReasonMatch, a.LocalityRate = &m, &l
		}
		s.Arms[arm] = a
	}
	return s
}

// LoadReports reads h1-judge/v1 files from a directory.
func LoadReports(dir string, caseIDs []string) ([]Report, error) {
	var out []Report
	if dir == "" {
		return out, nil
	}
	for _, id := range caseIDs {
		p := filepath.Join(dir, id+".h1-judge.json")
		raw, err := os.ReadFile(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var r Report
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, fmt.Errorf("h1judge: parsing %s: %w", p, err)
		}
		if r.SchemaVersion != SchemaVersion || r.CaseID != id {
			return nil, fmt.Errorf("h1judge: %s is not a %s file for %s", p, SchemaVersion, id)
		}
		out = append(out, r)
	}
	return out, nil
}

// keyedSuffix marks the correlator-attributed reference set. It is NOT what
// the judge may read: it carries every commit the correlator attributed to a
// case (up to thirty), where the judged set is the evaluator-verified fixing
// commits. Judging against the wider set would let a reviewer match any of
// thirty commits and call it anticipation.
const keyedSuffix = "-keyed"

// RefuseKeyedFixes rejects a fixes directory that is the reference set.
func RefuseKeyedFixes(fixesDir string) error {
	clean := strings.TrimRight(filepath.ToSlash(filepath.Clean(fixesDir)), "/")
	if strings.HasSuffix(filepath.Base(clean), keyedSuffix) {
		return fmt.Errorf("h1judge: refusing -fixes %s: a directory ending %q is the correlator-attributed reference set, not the evaluator-verified fixing commits; judging against it would let a finding match any attributed commit", fixesDir, keyedSuffix)
	}
	return nil
}

// shaLine matches the commit SHAs a formatted patch names in its headers.
var shaLine = regexp.MustCompile(`(?im)^(?:From|commit)\s+([0-9a-f]{7,40})\b`)

// FixSHAsIn extracts the commit SHAs a patch declares, in order, deduped.
func FixSHAsIn(patch string) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range shaLine.FindAllStringSubmatch(patch, -1) {
		sha := strings.ToLower(m[1])
		if !seen[sha] {
			seen[sha] = true
			out = append(out, sha)
		}
	}
	return out
}
