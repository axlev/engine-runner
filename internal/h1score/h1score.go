// Package h1score is the cross-run aggregate for H1 (#5b): given sealed
// runs from arms T and G, the evaluator's label file, and optionally the
// history baseline H, the contamination scans and the fixing-commit paths,
// it reports precision and recall per arm with counts, the pairwise deltas
// with a PR-level exact permutation p, the recommended-validation hit rate,
// recall by class, and everything stratified by admission.
//
// Evaluator-side. Every input is named in the output with its sha256, every
// rule applied is written down, and a figure that cannot be computed is
// reported as absent with the reason, never as zero.
package h1score

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	SchemaVersion = "h1-score/v1"

	labelsSchema  = "h1-labels/v1"
	historySchema = "history-baseline/v1"
	fixingSchema  = "h1-fixing-paths/v1"
	verdictSchema = "h1-verdict/v1"
	scanSchema    = "contamination-scan/v1"

	ArmT = "T"
	ArmG = "G"
	ArmH = "H"

	// exactLimit is the number of discordant pairs up to which the paired
	// permutation is enumerated exhaustively (2^n outcomes). Above it a
	// seeded Monte Carlo is used and the output says so.
	exactLimit      = 20
	monteCarloDraws = 200000
	monteCarloSeed  = 20260915
)

// ---- inputs ----

type Labels struct {
	SchemaVersion        string  `json:"schema_version"`
	CohortManifestSHA256 string  `json:"cohort_manifest_sha256"`
	Cases                []Label `json:"cases"`
}

type Label struct {
	CaseID       string `json:"case_id"`
	Label        string `json:"label"`
	PairID       string `json:"pair_id"`
	Class        string `json:"class"`
	FollowupDays int    `json:"followup_days"`
	Admission    struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	} `json:"admission"`

	// The fields below are OPTIONAL covariates the pre-registration asks
	// to be reported as strata. They are not part of the original
	// h1-labels/v1 contract, so a labels file may omit them - and when one
	// does, the renderer says the stratum is unavailable rather than
	// silently reporting a single bucket, which would read as "no
	// variation" when it means "not recorded".
	//
	//   Subsystem      - A9(v), code-first subsystem.
	//   MatchKeyUsed   - A9(vi); ["subsystem"] marks a fallback pair.
	//   SourceWindow   - A11(iii), "2024" or "2026H1".
	//   FixBeforeCutoff- A11(vi), fix before/after the model cutoff.
	Subsystem    string   `json:"subsystem,omitempty"`
	MatchKeyUsed []string `json:"match_key_used,omitempty"`
	// FallbackPair is the evaluator's own flag. Both spellings are
	// accepted: the flag when the labels file states it, and otherwise
	// derived from match_key_used. Tolerating both means a labels file
	// written to either shape scores identically, rather than silently
	// losing the stratum because of a field name.
	FallbackPair *bool `json:"fallback_pair,omitempty"`
	// Source / SourceWindow: A11(iii)'s window, "2024" or "2026H1".
	Source          string `json:"source,omitempty"`
	SourceWindow    string `json:"source_window,omitempty"`
	FixBeforeCutoff *bool  `json:"fix_before_cutoff,omitempty"`
}

// SourceOf returns the A11(iii) window under either field name.
func (l Label) SourceOf() string {
	if l.Source != "" {
		return l.Source
	}
	return l.SourceWindow
}

// IsFallbackPair reports A9(vi) fallback matching: the pair was matched on
// subsystem alone because its (category, subsystem) cell had no
// counted-clean negative. The explicit flag wins when present.
func (l Label) IsFallbackPair() bool {
	if l.FallbackPair != nil {
		return *l.FallbackPair
	}
	return len(l.MatchKeyUsed) == 1 && strings.EqualFold(l.MatchKeyUsed[0], "subsystem")
}

// HasFallbackInfo reports whether the labels file said anything about
// fallback matching at all. Without it the stratum is "not recorded", which
// is not the same as "no fallback pairs".
func (l Label) HasFallbackInfo() bool {
	return l.FallbackPair != nil || len(l.MatchKeyUsed) > 0
}

// StrataDimensions are the covariate breakdowns the pre-registration asks
// to be reported. Each returns "" for a case whose labels do not carry the
// covariate, and such cases are left out of that dimension entirely rather
// than bucketed as unknown - a bucket of unknowns invites reading it as a
// category.
var StrataDimensions = []struct {
	Name   string
	Bucket func(Label) string
}{
	{"admission_description", func(l Label) string { return l.Admission.Description }},
	{"subsystem", func(l Label) string { return l.Subsystem }},
	{"source", func(l Label) string { return l.SourceOf() }},
	{"fallback_pair", func(l Label) string {
		if !l.HasFallbackInfo() {
			return ""
		}
		if l.IsFallbackPair() {
			return "fallback"
		}
		return "fully matched"
	}},
	{"fix_before_cutoff", func(l Label) string {
		if l.FixBeforeCutoff == nil {
			return ""
		}
		if *l.FixBeforeCutoff {
			return "before cutoff"
		}
		return "after cutoff"
	}},
}

// HistoryBaseline is history-baseline/v1, produced in the miner (Alex's
// decision, 2026-09-15). Only the fields read are pinned; unknown
// top-level keys (detail, reason, merge_base, ...) are tolerated, and
// rule is checked only for being an object. risky may be null, with a
// reason, for a case whose base commit could not be resolved: that case
// is absent for H, like a missing file, never CLEAN.
type HistoryBaseline struct {
	SchemaVersion string         `json:"schema_version"`
	CaseID        string         `json:"case_id"`
	Risky         *bool          `json:"risky"`
	Reason        string         `json:"reason"`
	Score         float64        `json:"score"`
	Subsystem     string         `json:"subsystem"`
	Tercile       int            `json:"tercile"`
	Rule          map[string]any `json:"rule"`
	Provenance    any            `json:"provenance"`
}

type FixingPaths struct {
	SchemaVersion string   `json:"schema_version"`
	CaseID        string   `json:"case_id"`
	Paths         []string `json:"paths"`
}

// CaseVerdict is one arm's answer for one case.
type CaseVerdict struct {
	CaseID           string
	RunID            string
	Risky            bool
	RecommendedPaths []string
	Class            string // reviewer-assigned class of the primary finding, if any

	// The primary finding is what section 6's judge reads: under T stage
	// B's first surviving assessment, under G findings[0]. Carried here so
	// the judge does not re-open the sealed run and risk reading a
	// different finding than the one the scorer used.
	PrimaryFindingID string
	PrimaryMechanism string
	PrimaryFile      string
	PrimaryLine      int
}

// ---- output ----

type Counts struct {
	TP int `json:"tp"`
	FP int `json:"fp"`
	FN int `json:"fn"`
	TN int `json:"tn"`
}

type Rate struct {
	Value       *float64 `json:"value"`
	Numerator   int      `json:"numerator"`
	Denominator int      `json:"denominator"`
	Absent      string   `json:"absent,omitempty"`
}

type ArmReport struct {
	Arm         string   `json:"arm"`
	ArmID       string   `json:"arm_id"`
	Cases       int      `json:"cases"`
	Voided      int      `json:"voided"`
	VoidedCases []string `json:"voided_cases"`
	Absent      []string `json:"absent_cases"`
	// AbsentReasons carries, for arm H, the file\'s own reason for a null
	// verdict (an unresolvable base commit); absent files have no entry.
	AbsentReasons map[string]string `json:"absent_reasons,omitempty"`
	Counts        Counts            `json:"counts"`
	Precision     Rate              `json:"precision"`
	Recall        Rate              `json:"recall"`
	RecallByClass map[string]Rate   `json:"recall_by_class"`
	HitRate       *HitRate          `json:"recommended_validation_hit_rate,omitempty"`
	// VsChance is A8\'s per-arm test: case-level Fisher exact, two-sided, on
	// the arm\'s RISKY/CLEAN x positive/negative table.
	VsChance *VsChance `json:"vs_chance"`
	// Strata are per-arm rates within each covariate bucket, keyed
	// dimension -> bucket. A dimension the labels do not carry is absent
	// from the map entirely, so a reader cannot mistake "not recorded" for
	// "one bucket".
	Strata map[string]map[string]Stratum `json:"strata"`
}

type VsChance struct {
	Test     string   `json:"test"`
	TwoSided *float64 `json:"two_sided"`
	Absent   string   `json:"absent,omitempty"`
}

// fisherExactTwoSided is the permutation distribution of the 2x2 table
// [[a b] [c d]] under fixed margins: the sum of the probabilities of every
// table at least as improbable as the observed one (A8). Case-level, so
// the pilot's clustering objection does not apply.
func fisherExactTwoSided(a, b, c, d int) float64 {
	r1, r2, c1, n := a+b, c+d, a+c, a+b+c+d
	if n == 0 {
		return 1
	}
	lg := func(x int) float64 { l, _ := math.Lgamma(float64(x + 1)); return l }
	logC := func(n, k int) float64 { return lg(n) - lg(k) - lg(n-k) }
	prob := func(x int) float64 { return math.Exp(logC(r1, x) + logC(r2, c1-x) - logC(n, c1)) }
	obs := prob(a)
	lo, hi := 0, c1
	if c1-r2 > lo {
		lo = c1 - r2
	}
	if r1 < hi {
		hi = r1
	}
	p := 0.0
	for x := lo; x <= hi; x++ {
		if px := prob(x); px <= obs*(1+1e-9) {
			p += px
		}
	}
	return math.Min(p, 1)
}

func vsChance(c Counts) *VsChance {
	v := &VsChance{Test: "case-level Fisher exact, two-sided (A8)"}
	if c.TP+c.FP+c.FN+c.TN == 0 {
		v.Absent = "no cases scored"
		return v
	}
	p := fisherExactTwoSided(c.TP, c.FP, c.FN, c.TN)
	v.TwoSided = &p
	return v
}

type Stratum struct {
	Cases     int    `json:"cases"`
	Counts    Counts `json:"counts"`
	Precision Rate   `json:"precision"`
	Recall    Rate   `json:"recall"`
}

type HitRate struct {
	Rate          Rate      `json:"rate"`
	NoFixingPaths int       `json:"true_positives_without_fixing_paths"`
	PerCase       []HitCase `json:"per_case"`
}

type HitCase struct {
	CaseID      string `json:"case_id"`
	Hit         bool   `json:"hit"`
	MatchedPath string `json:"matched_path,omitempty"`
	MatchedBy   string `json:"matched_by,omitempty"`
}

type Pairwise struct {
	Comparison string `json:"comparison"`
	// Subset names the case set this comparison ran over. "" is every
	// scored case; "excluding fallback pairs" is A9(vi)'s second reading,
	// which the pre-registration requires alongside the first because
	// within a fallback pair the stateful category is unbalanced on a
	// dimension the arms can see in the diff.
	Subset         string   `json:"subset,omitempty"`
	CommonCases    int      `json:"common_cases"`
	Discordant     int      `json:"discordant_cases"`
	DeltaPrecision *float64 `json:"delta_precision"`
	DeltaRecall    *float64 `json:"delta_recall"`
	PPrecision     PValue   `json:"p_precision"`
	PRecall        PValue   `json:"p_recall"`
	Threshold      float64  `json:"preregistered_threshold_points"`
	MeetsPrecision *bool    `json:"meets_threshold_precision"`
	MeetsRecall    *bool    `json:"meets_threshold_recall"`
	Absent         string   `json:"absent,omitempty"`
}

type PValue struct {
	OneSided *float64 `json:"one_sided_treatment_better"`
	TwoSided *float64 `json:"two_sided"`
	Method   string   `json:"method"`
	Draws    int      `json:"draws,omitempty"`
	Seed     int64    `json:"seed,omitempty"`
}

type Report struct {
	SchemaVersion string            `json:"schema_version"`
	Inputs        map[string]string `json:"inputs"`
	Rules         map[string]any    `json:"rules"`
	CohortSize    int               `json:"cohort_size"`
	Positives     int               `json:"positives"`
	Negatives     int               `json:"negatives"`

	// ProbeVoidedCases are voided for EVERY arm by the A11 contamination
	// probe - the model already knew how the change was fixed, so no arm's
	// answer on it means anything. Excluded from every figure below.
	ProbeVoidedCases []string `json:"probe_voided_cases"`
	// ProbeUnresolvedCases passed the probe's mechanical scan but no
	// evaluator has read the responses yet. The mechanical rules cannot see
	// mechanism-level recall, so their silence is not an acquittal: while
	// this list is non-empty the result is provisional.
	ProbeUnresolvedCases []string `json:"probe_unresolved_cases"`
	Provisional          bool     `json:"provisional"`
	ProvisionalReason    string   `json:"provisional_reason,omitempty"`

	Arms        map[string]ArmReport `json:"arms"`
	Pairwise    []Pairwise           `json:"pairwise"`
	ReasonMatch any                  `json:"reason_match"`
	// ReasonMatchVoided records judged (case, arm) pairs dropped from the
	// reason-match figure because the run was voided. A voided run is
	// dropped from every figure, so it cannot count here either - but it
	// was paid for and judged, so it is reported rather than silently
	// absent from the denominator.
	ReasonMatchVoided []VoidedJudged  `json:"reason_match_voided,omitempty"`
	ReasonMatchNote   string          `json:"reason_match_note,omitempty"`
	ReasonMatchAbsent string          `json:"reason_match_absent"`
	Underpowered      map[string]bool `json:"underpowered_at_this_cohort_size"`
}

// ---- loading ----

func readJSON(path string, into any) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("h1score: parsing %s: %w", path, err)
	}
	return nil
}

func fileSHA256(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// LoadLabels reads and checks h1-labels/v1 against the cohort manifest.
func LoadLabels(labelsPath, manifestPath string) (Labels, string, error) {
	var l Labels
	if err := readJSON(labelsPath, &l); err != nil {
		return Labels{}, "", err
	}
	if l.SchemaVersion != labelsSchema {
		return Labels{}, "", fmt.Errorf("h1score: %s is %q, want %s", labelsPath, l.SchemaVersion, labelsSchema)
	}
	manifestSum, err := fileSHA256(manifestPath)
	if err != nil {
		return Labels{}, "", fmt.Errorf("h1score: cohort manifest: %w", err)
	}
	if manifestSum != l.CohortManifestSHA256 {
		return Labels{}, "", fmt.Errorf("h1score: labels were written for cohort manifest %s but the manifest handed over hashes %s", l.CohortManifestSHA256, manifestSum)
	}
	seen := map[string]bool{}
	pairs := map[string][]Label{}
	for _, c := range l.Cases {
		if seen[c.CaseID] {
			return Labels{}, "", fmt.Errorf("h1score: case %s labelled twice", c.CaseID)
		}
		seen[c.CaseID] = true
		switch c.Label {
		case "positive":
			if c.Class == "" {
				return Labels{}, "", fmt.Errorf("h1score: positive %s has no class", c.CaseID)
			}
		case "negative":
		default:
			return Labels{}, "", fmt.Errorf("h1score: case %s has label %q", c.CaseID, c.Label)
		}
		if c.PairID == "" {
			return Labels{}, "", fmt.Errorf("h1score: case %s has no pair_id", c.CaseID)
		}
		pairs[c.PairID] = append(pairs[c.PairID], c)
	}
	for id, members := range pairs {
		if len(members) != 2 || members[0].Label == members[1].Label {
			return Labels{}, "", fmt.Errorf("h1score: pair %s is not one positive and one negative", id)
		}
	}
	labelsSum, _ := fileSHA256(labelsPath)
	return l, labelsSum, nil
}

// LoadVerdicts walks a results root and returns per-arm verdicts keyed by
// case, taking the arm from run.json's protocol_version. voided lists run
// ids voided by the contamination scan (excluded, counted). Two non-voided
// runs of one case in one arm is ambiguous and fails.
//
// The second return is the voided case ids per arm: a voided run is still
// evidence that the case was run, so the aggregate's fail-closed check
// counts it, while its verdict is excluded from every figure.
func LoadVerdicts(runsRoot string, armIDs map[string]string, voided map[string]bool) (map[string]map[string]CaseVerdict, map[string][]string, error) {
	byArm := map[string]map[string]CaseVerdict{}
	voidCases := map[string][]string{}
	idToArm := map[string]string{}
	for arm, id := range armIDs {
		idToArm[id] = arm
		byArm[arm] = map[string]CaseVerdict{}
	}
	manifests, _ := filepath.Glob(filepath.Join(runsRoot, "*", "run.json"))
	sort.Strings(manifests)
	for _, m := range manifests {
		var run struct {
			RunID           string `json:"run_id"`
			CaseID          string `json:"case_id"`
			ProtocolVersion string `json:"protocol_version"`
			Status          string `json:"status"`
		}
		if err := readJSON(m, &run); err != nil {
			return nil, nil, err
		}
		arm, ok := idToArm[run.ProtocolVersion]
		if !ok || run.Status != "completed" {
			continue
		}
		if voided[run.RunID] {
			voidCases[arm] = append(voidCases[arm], run.CaseID)
			continue
		}
		var v struct {
			SchemaVersion    string   `json:"schema_version"`
			Verdict          string   `json:"verdict"`
			RecommendedPaths []string `json:"recommended_paths"`
			Primary          *struct {
				ID        string `json:"id"`
				Class     string `json:"class"`
				Mechanism string `json:"mechanism"`
				File      string `json:"file"`
				Line      int    `json:"line"`
			} `json:"primary_finding"`
		}
		vp := filepath.Join(filepath.Dir(m), "evaluation", "verdict.json")
		if err := readJSON(vp, &v); err != nil {
			return nil, nil, fmt.Errorf("h1score: run %s (%s) has no readable verdict: %w", run.RunID, arm, err)
		}
		if v.SchemaVersion != verdictSchema {
			return nil, nil, fmt.Errorf("h1score: %s is %q, want %s", vp, v.SchemaVersion, verdictSchema)
		}
		if prev, dup := byArm[arm][run.CaseID]; dup {
			return nil, nil, fmt.Errorf("h1score: case %s has two non-voided runs in arm %s (%s, %s); which one counts is not the scorer's call", run.CaseID, arm, prev.RunID, run.RunID)
		}
		cv := CaseVerdict{CaseID: run.CaseID, RunID: run.RunID, Risky: v.Verdict == "RISKY", RecommendedPaths: v.RecommendedPaths}
		if v.Primary != nil {
			cv.Class = v.Primary.Class
			cv.PrimaryFindingID = v.Primary.ID
			cv.PrimaryMechanism = v.Primary.Mechanism
			cv.PrimaryFile = v.Primary.File
			cv.PrimaryLine = v.Primary.Line
		}
		byArm[arm][run.CaseID] = cv
	}
	for arm := range voidCases {
		sort.Strings(voidCases[arm])
	}
	return byArm, voidCases, nil
}

// LoadVoided reads a contamscan output directory and returns the voided
// run ids.
func LoadVoided(scansDir string) (map[string]bool, error) {
	voided := map[string]bool{}
	if scansDir == "" {
		return voided, nil
	}
	files, _ := filepath.Glob(filepath.Join(scansDir, "*.contamination-scan.json"))
	for _, f := range files {
		var s struct {
			SchemaVersion string `json:"schema_version"`
			RunID         string `json:"run_id"`
			Void          bool   `json:"void"`
		}
		if err := readJSON(f, &s); err != nil {
			return nil, err
		}
		if s.SchemaVersion != scanSchema {
			return nil, fmt.Errorf("h1score: %s is %q, want %s", f, s.SchemaVersion, scanSchema)
		}
		if s.Void {
			voided[s.RunID] = true
		}
	}
	return voided, nil
}

// LoadHistory reads <dir>/<case_id>.json history-baseline/v1 files for the
// labelled cases. Missing files make H absent for that case, not CLEAN.
//
// The second return maps case id to the file's stated reason when risky
// is null; those cases are not in the first return.
func LoadHistory(dir string, cases []Label) (map[string]CaseVerdict, map[string]string, error) {
	out := map[string]CaseVerdict{}
	reasons := map[string]string{}
	if dir == "" {
		return out, reasons, nil
	}
	for _, c := range cases {
		p := filepath.Join(dir, c.CaseID+".json")
		var h HistoryBaseline
		err := readJSON(p, &h)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		if h.SchemaVersion != historySchema || h.CaseID != c.CaseID {
			return nil, nil, fmt.Errorf("h1score: %s is not a %s file for %s", p, historySchema, c.CaseID)
		}
		if h.Rule == nil {
			return nil, nil, fmt.Errorf("h1score: %s: rule must be an object", p)
		}
		if h.Risky == nil {
			reason := h.Reason
			if reason == "" {
				reason = "risky is null and no reason given"
			}
			reasons[c.CaseID] = reason
			continue
		}
		out[c.CaseID] = CaseVerdict{CaseID: c.CaseID, RunID: "history:" + c.CaseID, Risky: *h.Risky}
	}
	return out, reasons, nil
}

// LoadFixingPaths reads <dir>/<case_id>.json h1-fixing-paths/v1 files.
func LoadFixingPaths(dir string, cases []Label) (map[string][]string, error) {
	out := map[string][]string{}
	if dir == "" {
		return out, nil
	}
	for _, c := range cases {
		p := filepath.Join(dir, c.CaseID+".json")
		var f FixingPaths
		err := readJSON(p, &f)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if f.SchemaVersion != fixingSchema || f.CaseID != c.CaseID {
			return nil, fmt.Errorf("h1score: %s is not a %s file for %s", p, fixingSchema, c.CaseID)
		}
		out[c.CaseID] = f.Paths
	}
	return out, nil
}

// ---- scoring ----

func rate(num, den int, absentWhy string) Rate {
	if den == 0 {
		return Rate{Numerator: num, Denominator: den, Absent: absentWhy}
	}
	v := float64(num) / float64(den)
	return Rate{Value: &v, Numerator: num, Denominator: den}
}

func countsFor(labels map[string]Label, verdicts map[string]CaseVerdict, only func(Label) bool) (Counts, int) {
	var c Counts
	n := 0
	for id, l := range labels {
		if only != nil && !only(l) {
			continue
		}
		v, ok := verdicts[id]
		if !ok {
			continue
		}
		n++
		switch {
		case v.Risky && l.Label == "positive":
			c.TP++
		case v.Risky:
			c.FP++
		case l.Label == "positive":
			c.FN++
		default:
			c.TN++
		}
	}
	return c, n
}

func precisionOf(c Counts) Rate {
	return rate(c.TP, c.TP+c.FP, "no case was called RISKY")
}
func recallOf(c Counts) Rate { return rate(c.TP, c.TP+c.FN, "no positives") }

// pathHit applies the section 6 match: a recommended path equals a fixing
// path, or a fixing path lies under a recommended directory.
func pathHit(recommended, fixing []string) (string, string, bool) {
	for _, r := range recommended {
		rc := strings.TrimSuffix(filepath.ToSlash(filepath.Clean(r)), "/")
		for _, f := range fixing {
			fc := filepath.ToSlash(filepath.Clean(f))
			if rc == fc {
				return fc, "exact", true
			}
			if strings.HasPrefix(fc, rc+"/") {
				return fc, "under recommended directory " + rc, true
			}
		}
	}
	return "", "", false
}

func armReport(arm, armID string, labels map[string]Label, verdicts map[string]CaseVerdict, voided []string, fixing map[string][]string, withHits bool) ArmReport {
	if voided == nil {
		voided = []string{}
	}
	r := ArmReport{Arm: arm, ArmID: armID, Voided: len(voided), VoidedCases: voided, RecallByClass: map[string]Rate{}, Strata: map[string]map[string]Stratum{}, Absent: []string{}}
	r.Counts, r.Cases = countsFor(labels, verdicts, nil)
	r.Precision, r.Recall = precisionOf(r.Counts), recallOf(r.Counts)
	r.VsChance = vsChance(r.Counts)
	classes := map[string]bool{}
	for id, l := range labels {
		if _, ok := verdicts[id]; !ok {
			r.Absent = append(r.Absent, id)
		}
		if l.Label == "positive" {
			classes[l.Class] = true
		}
	}
	sort.Strings(r.Absent)
	for cls := range classes {
		c, _ := countsFor(labels, verdicts, func(l Label) bool { return l.Label == "positive" && l.Class == cls })
		r.RecallByClass[cls] = recallOf(c)
	}
	for _, dim := range StrataDimensions {
		buckets := map[string]bool{}
		for _, l := range labels {
			if v := dim.Bucket(l); v != "" {
				buckets[v] = true
			}
		}
		if len(buckets) == 0 {
			continue // not recorded; absent from the map entirely
		}
		byBucket := map[string]Stratum{}
		for b := range buckets {
			bucket := b
			c, n := countsFor(labels, verdicts, func(l Label) bool { return dim.Bucket(l) == bucket })
			byBucket[bucket] = Stratum{Cases: n, Counts: c, Precision: precisionOf(c), Recall: recallOf(c)}
		}
		r.Strata[dim.Name] = byBucket
	}
	if withHits {
		h := &HitRate{PerCase: []HitCase{}}
		hits, den := 0, 0
		ids := make([]string, 0, len(labels))
		for id := range labels {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			l := labels[id]
			v, ok := verdicts[id]
			if !ok || !v.Risky || l.Label != "positive" {
				continue
			}
			fp, ok := fixing[id]
			if !ok {
				h.NoFixingPaths++
				continue
			}
			den++
			matched, by, hit := pathHit(v.RecommendedPaths, fp)
			if hit {
				hits++
			}
			h.PerCase = append(h.PerCase, HitCase{CaseID: id, Hit: hit, MatchedPath: matched, MatchedBy: by})
		}
		h.Rate = rate(hits, den, "no RISKY true positive with fixing paths")
		r.HitRate = h
	}
	return r
}

// pairedStat is precision or recall computed from per-case (risky, positive)
// pairs.
type caseOutcome struct {
	positive bool
	a, b     bool // risky under arm A (treatment) and arm B (baseline)
}

func precRec(cases []caseOutcome, useA []bool) (prec, rec float64, precOK, recOK bool) {
	var c Counts
	for i, o := range cases {
		risky := o.a
		if useA != nil && !useA[i] {
			risky = o.b
		}
		switch {
		case risky && o.positive:
			c.TP++
		case risky:
			c.FP++
		case o.positive:
			c.FN++
		default:
			c.TN++
		}
	}
	if c.TP+c.FP > 0 {
		prec, precOK = float64(c.TP)/float64(c.TP+c.FP), true
	}
	if c.TP+c.FN > 0 {
		rec, recOK = float64(c.TP)/float64(c.TP+c.FN), true
	}
	return
}

// pairwise compares treatment against baseline on the cases both scored.
// Under the null the two arms' verdicts on a case are exchangeable, so the
// reference distribution swaps them per case; concordant cases contribute
// nothing and only the discordant ones are enumerated. This is a different
// test from internal/judge's (which relabels which PRs are clean, an
// unpaired design) and is not interchangeable with it.
func pairwise(name string, labels map[string]Label, treat, base map[string]CaseVerdict, threshold float64) Pairwise {
	return pairwiseSubset(name, "", labels, treat, base, threshold)
}

func pairwiseSubset(name, subset string, labels map[string]Label, treat, base map[string]CaseVerdict, threshold float64) Pairwise {
	pw := Pairwise{Comparison: name, Subset: subset, Threshold: threshold}
	var cases []caseOutcome
	ids := make([]string, 0, len(labels))
	for id := range labels {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		t, okT := treat[id]
		b, okB := base[id]
		if !okT || !okB {
			continue
		}
		cases = append(cases, caseOutcome{positive: labels[id].Label == "positive", a: t.Risky, b: b.Risky})
	}
	pw.CommonCases = len(cases)
	if len(cases) == 0 {
		pw.Absent = "no case scored by both arms"
		return pw
	}
	var discordant []int
	for i, o := range cases {
		if o.a != o.b {
			discordant = append(discordant, i)
		}
	}
	pw.Discordant = len(discordant)

	// Statistic: treatment minus baseline. Baseline figure is precRec with
	// every case swapped.
	allB := make([]bool, len(cases))
	pT, rT, pTok, rTok := precRec(cases, nil)
	pB, rB, pBok, rBok := precRec(cases, allB)
	var obsP, obsR float64
	hasP, hasR := pTok && pBok, rTok && rBok
	if hasP {
		obsP = pT - pB
		pw.DeltaPrecision = &obsP
		m := obsP*100 >= threshold-1e-9
		pw.MeetsPrecision = &m
	}
	if hasR {
		obsR = rT - rB
		pw.DeltaRecall = &obsR
		m := obsR*100 >= threshold-1e-9
		pw.MeetsRecall = &m
	}

	// Reference distribution over swaps of the discordant cases.
	statAt := func(swap []bool) (float64, float64, bool, bool) {
		useA := make([]bool, len(cases))
		useB := make([]bool, len(cases))
		for i := range cases {
			useA[i], useB[i] = true, false
		}
		for k, i := range discordant {
			if swap[k] {
				useA[i], useB[i] = false, true
			}
		}
		p1, r1, p1ok, r1ok := precRec(cases, useA)
		p2, r2, p2ok, r2ok := precRec(cases, useB)
		return p1 - p2, r1 - r2, p1ok && p2ok, r1ok && r2ok
	}
	d := len(discordant)
	var draws int
	var extremeP1, extremeP2, extremeR1, extremeR2, totalP, totalR int
	consider := func(swap []bool) {
		dp, dr, okp, okr := statAt(swap)
		if okp && hasP {
			totalP++
			if dp >= obsP-1e-12 {
				extremeP1++
			}
			if math.Abs(dp) >= math.Abs(obsP)-1e-12 {
				extremeP2++
			}
		}
		if okr && hasR {
			totalR++
			if dr >= obsR-1e-12 {
				extremeR1++
			}
			if math.Abs(dr) >= math.Abs(obsR)-1e-12 {
				extremeR2++
			}
		}
	}
	method := "exact enumeration of 2^discordant arm swaps"
	if d <= exactLimit {
		swap := make([]bool, d)
		for mask := 0; mask < 1<<d; mask++ {
			for k := range swap {
				swap[k] = mask&(1<<k) != 0
			}
			consider(swap)
		}
		draws = 1 << d
	} else {
		method = "monte carlo over arm swaps, seeded"
		rng := rand.New(rand.NewSource(monteCarloSeed))
		swap := make([]bool, d)
		for n := 0; n < monteCarloDraws; n++ {
			for k := range swap {
				swap[k] = rng.Intn(2) == 1
			}
			consider(swap)
		}
		draws = monteCarloDraws
	}
	mk := func(has bool, e1, e2, total int) PValue {
		pv := PValue{Method: method, Draws: draws}
		if method != "exact enumeration of 2^discordant arm swaps" {
			pv.Seed = monteCarloSeed
		}
		if has && total > 0 {
			one, two := float64(e1)/float64(total), float64(e2)/float64(total)
			pv.OneSided, pv.TwoSided = &one, &two
		}
		return pv
	}
	pw.PPrecision = mk(hasP, extremeP1, extremeP2, totalP)
	pw.PRecall = mk(hasR, extremeR1, extremeR2, totalR)
	return pw
}

// Options are the inputs to Score, already loaded and checked.
type Options struct {
	Labels   Labels
	Inputs   map[string]string // name -> sha256 or path, recorded verbatim
	ArmIDs   map[string]string // T -> protocol id, G -> protocol id
	Verdicts map[string]map[string]CaseVerdict
	Voided   map[string][]string // arm -> voided case ids
	History  map[string]CaseVerdict
	// HistoryAbsentReasons is LoadHistory's second return.
	HistoryAbsentReasons map[string]string
	FixingPaths          map[string][]string
	Threshold            float64 // pre-registered points, 15

	// ProbeVoided are cases the A11 contamination probe voided: dropped
	// from every arm before counting. ProbeUnresolved are cases whose
	// mechanical scan was clean but which no evaluator has read yet; they
	// are counted, but they make the whole result provisional.
	ProbeVoided     []string
	ProbeUnresolved []string
	// ReasonMatch is the h1judge summary, when that pass has run.
	ReasonMatch any
	// ReasonMatchVoided are judged pairs excluded for being voided.
	ReasonMatchVoided []VoidedJudged
}

// Score computes the report. It fails closed on a run whose case has no
// label and on a label whose case has no run in any LLM arm. A voided run
// counts as a run for that check: section 8 voids are an expected,
// reported outcome, and an aggregate that refused to run because one
// case's only run was voided would hide exactly what the void is for.
func Score(o Options) (Report, error) {
	labels := map[string]Label{}
	for _, c := range o.Labels.Cases {
		labels[c.CaseID] = c
	}

	// A11: a probe-voided case is void for EVERY arm - the model already
	// knew how the change was fixed, so no arm's answer on it means
	// anything. Drop it here, before any counting, so that no figure
	// anywhere in the report includes it and no fail-closed check below
	// demands a run for a case that is no longer in the cohort.
	probeVoid := map[string]bool{}
	for _, id := range o.ProbeVoided {
		probeVoid[id] = true
		delete(labels, id)
	}
	if len(probeVoid) > 0 {
		for arm, vs := range o.Verdicts {
			kept := map[string]CaseVerdict{}
			for id, v := range vs {
				if !probeVoid[id] {
					kept[id] = v
				}
			}
			o.Verdicts[arm] = kept
		}
		kept := map[string]CaseVerdict{}
		for id, v := range o.History {
			if !probeVoid[id] {
				kept[id] = v
			}
		}
		o.History = kept
	}
	for arm, vs := range o.Verdicts {
		for id := range vs {
			if _, ok := labels[id]; !ok {
				return Report{}, fmt.Errorf("h1score: arm %s has a run for case %s, which has no label", arm, id)
			}
		}
	}
	for id := range labels {
		found := false
		for _, vs := range o.Verdicts {
			if _, ok := vs[id]; ok {
				found = true
			}
		}
		for _, ids := range o.Voided {
			for _, v := range ids {
				if v == id {
					found = true
				}
			}
		}
		if !found {
			return Report{}, fmt.Errorf("h1score: labelled case %s has no run in any arm", id)
		}
	}

	r := Report{
		SchemaVersion: SchemaVersion,
		Inputs:        o.Inputs,
		Rules: map[string]any{
			"arm_ids":                 o.ArmIDs,
			"precision":               "TP / (TP + FP) over labelled cases the arm scored; RISKY from evaluation/verdict.json",
			"recall":                  "TP / (TP + FN); reported, not criterial (pre-registration s2)",
			"hit_match":               "a recommended path equals a fixing path, or a fixing path lies under a recommended directory; over RISKY true positives with an h1-fixing-paths file",
			"pairwise_null":           "per case, the two arms' verdicts are exchangeable; reference distribution swaps them on the discordant cases",
			"pairwise_test":           "paired-exchangeable-verdicts, exact <=20 discordant, MC 200k seeded above (A8)",
			"per_arm_vs_chance":       "case-level Fisher exact, two-sided, one per arm, on the verdict x label table under fixed margins (A8); not criterial",
			"exact_limit_discordant":  exactLimit,
			"preregistered_threshold": o.Threshold,
			"voided_runs":             "excluded from the arm and counted; from contamination-scan/v1 void=true",
			"history_absent":          "a case with no history-baseline/v1 file is absent for H, not CLEAN",
			"probe_voided":            "A11: a case the contamination probe voided is dropped from EVERY arm before counting",
			"probe_unresolved":        "A11: a case whose probe scan was clean but which no evaluator has read makes the result provisional; a clean mechanical scan is not an acquittal",
		},
		Arms:              map[string]ArmReport{},
		Pairwise:          []Pairwise{},
		ReasonMatchAbsent: "the reason-match judge pass has not run; fed from cmd/h1judge in a later pass",
		Underpowered:      map[string]bool{},
	}
	r.ReasonMatchVoided = o.ReasonMatchVoided
	if o.ReasonMatch != nil {
		r.ReasonMatch = o.ReasonMatch
		r.ReasonMatchAbsent = ""
		// Section 6 requires in-family stated on every figure derived from
		// the judge. Read it from the summary rather than asserting it: a
		// hardcoded claim is false the first time the judge is not Opus,
		// and it would be false in the one place a reader trusts it.
		r.ReasonMatchNote = reasonMatchNote(o.ReasonMatch)
	}

	r.ProbeVoidedCases = sortedCopy(o.ProbeVoided)
	r.ProbeUnresolvedCases = sortedCopy(o.ProbeUnresolved)
	if len(r.ProbeUnresolvedCases) > 0 {
		r.Provisional = true
		r.ProvisionalReason = fmt.Sprintf(
			"%d case(s) have no %s file; the probe's mechanical rules cannot see mechanism-level recall, so a clean scan is not an acquittal (pre-registration A11)",
			len(r.ProbeUnresolvedCases), "contamination-probe-verdict/v1")
	}
	for _, c := range o.Labels.Cases {
		if probeVoid[c.CaseID] {
			continue
		}
		if c.Label == "positive" {
			r.Positives++
		} else {
			r.Negatives++
		}
	}
	r.CohortSize = len(o.Labels.Cases) - len(probeVoid)
	// Pre-registration s2: at 40 cases, +15 on recall (3 cases) cannot
	// reach significance; it is reported as consistent-with, not as a pass.
	r.Underpowered["recall"] = r.Positives <= 20
	r.Underpowered["reason_match"] = r.Positives <= 20

	r.Arms[ArmT] = armReport(ArmT, o.ArmIDs[ArmT], labels, o.Verdicts[ArmT], o.Voided[ArmT], o.FixingPaths, true)
	r.Arms[ArmG] = armReport(ArmG, o.ArmIDs[ArmG], labels, o.Verdicts[ArmG], o.Voided[ArmG], o.FixingPaths, true)
	h := armReport(ArmH, "history-baseline/v1", labels, o.History, nil, nil, false)
	if len(o.HistoryAbsentReasons) > 0 {
		h.AbsentReasons = o.HistoryAbsentReasons
	}
	r.Arms[ArmH] = h

	r.Pairwise = append(r.Pairwise, pairwise("T-G", labels, o.Verdicts[ArmT], o.Verdicts[ArmG], o.Threshold))
	r.Pairwise = append(r.Pairwise, pairwise("T-H", labels, o.Verdicts[ArmT], o.History, o.Threshold))

	// A9(vi): "§2's pairwise deltas are given with and without them."
	// Reported only when the labels actually say which pairs are fallback;
	// otherwise the second reading would silently duplicate the first and
	// look like corroboration.
	anyFallbackInfo, anyFallback := false, false
	fullyMatched := map[string]Label{}
	for id, l := range labels {
		if l.HasFallbackInfo() {
			anyFallbackInfo = true
			if l.IsFallbackPair() {
				anyFallback = true
				continue
			}
		}
		fullyMatched[id] = l
	}
	if anyFallbackInfo && anyFallback {
		const subset = "excluding fallback pairs (A9(vi))"
		r.Pairwise = append(r.Pairwise,
			pairwiseSubset("T-G", subset, fullyMatched, o.Verdicts[ArmT], o.Verdicts[ArmG], o.Threshold),
			pairwiseSubset("T-H", subset, fullyMatched, o.Verdicts[ArmT], o.History, o.Threshold))
	}
	return r, nil
}

// sortedCopy returns a sorted copy, never nil, so an absent list encodes as
// [] rather than null.
func sortedCopy(in []string) []string {
	out := append([]string{}, in...)
	sort.Strings(out)
	return out
}

// reasonMatchNote states the judge's provenance from the summary itself.
func reasonMatchNote(v any) string {
	sum, ok := parseReasonMatch(v)
	if !ok || sum.JudgeModel == "" {
		return "Judge provenance is not recorded in this summary; section 6 requires the judge model and whether it is in-family to be stated wherever these figures are used."
	}
	if sum.InFamily {
		return fmt.Sprintf("Judge model %s is IN-FAMILY with the treatment arm; section 6 requires that stated on every figure derived from it.", sum.JudgeModel)
	}
	return fmt.Sprintf("Judge model %s is NOT in-family with the treatment arm; section 6 requires that stated on every figure derived from it.", sum.JudgeModel)
}

// VoidedJudged is one judged (case, arm) pair that a void removed from the
// reason-match figure.
type VoidedJudged struct {
	CaseID string `json:"case_id"`
	Arm    string `json:"arm"`
	Reason string `json:"reason"`
}
