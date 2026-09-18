package h1score

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Render turns a scored report into the H1 results document.
//
// The point of rendering from the scored JSON rather than writing the
// numbers by hand is that nothing in the document can disagree with what
// the scorer produced. Every figure here is read from the report; nothing
// is recomputed, rounded by hand, or filled in from memory.
//
// Two habits are deliberate and load-bearing:
//
//   - A figure that could not be computed prints as "absent" WITH ITS
//     REASON, never as 0, "-", or a blank cell. Four metrics on this
//     project were retired because a number read as stronger than it was;
//     a blank that looks like a zero is the same failure in miniature.
//   - A stratum whose covariate the labels file does not carry prints as
//     "not recorded", never as one bucket containing everything. A single
//     bucket reads as "no variation on this dimension", which is a
//     different and much stronger claim than "we did not record it".
func Render(r Report, labels []Label, meta RenderMeta) string {
	var b strings.Builder
	w := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }

	w("# H1 results")
	w("")
	if r.Provisional {
		w("> **PROVISIONAL — not a result.** %s", r.ProvisionalReason)
		w("")
	}
	if meta.Draft {
		w("> **DRAFT SCAFFOLD.** Rendered before the arms ran; every number below is")
		w("> from fixture or partial data and means nothing yet.")
		w("")
	}
	w("Cohort %d cases (%d positive, %d negative). Generated from `%s` by `h1score -render`.",
		r.CohortSize, r.Positives, r.Negatives, meta.SourceFile)
	w("")

	renderCriteria(w, r, meta)
	renderArms(w, r)
	renderPairwise(w, r)
	renderHitRate(w, r)
	renderReasonMatch(w, r)
	renderRecallByClass(w, r)
	renderStrata(w, r, labels)
	renderVoided(w, r)
	renderCost(w, r, meta)
	renderFingerprints(w, r, meta)
	renderLimitations(w)
	return b.String()
}

// RenderMeta carries what the scorer does not know: which artifacts
// produced it, and whether this is a real result.
type RenderMeta struct {
	SourceFile     string
	EngineCommit   string
	CohortRecords  string // cohort manifest records_sha256
	ArmFileHash    string
	ProtocolHashes map[string]string // arm -> protocol hash
	PromptHashes   map[string]string // arm/stage -> prompt hash
	CostNote       string
	// ProvenanceNotes are free-text lines recorded under Provenance. This
	// document is generated, so a fact written into it by hand is lost on
	// the next render; anything that must survive belongs here, passed in
	// at render time.
	ProvenanceNotes []string
	// Draft marks a scaffold render: the document says so at the top, so a
	// half-filled template cannot be mistaken for a finding.
	Draft bool
}

func rateCell(r Rate) string {
	if r.Value == nil {
		if r.Absent != "" {
			return "absent (" + r.Absent + ")"
		}
		return "absent"
	}
	return fmt.Sprintf("%.3f (%d/%d)", *r.Value, r.Numerator, r.Denominator)
}

func floatCell(p *float64, format string) string {
	if p == nil {
		return "absent"
	}
	return fmt.Sprintf(format, *p)
}

func renderCriteria(w func(string, ...any), r Report, meta RenderMeta) {
	w("## Pre-registered criteria (§2)")
	w("")
	w("All three must hold. Recall is reported, not criterial.")
	w("")
	w("| Criterion | Threshold | Observed | Verdict |")
	w("|---|---|---|---|")

	// Reason match: only the judge pass can fill this.
	rmObs, rmVerdict := "absent (judge pass has not run)", "cannot be evaluated"
	if sum, ok := parseReasonMatch(r.ReasonMatch); ok {
		if a, have := sum.Arms[ArmT]; have && a.ReasonMatch != nil {
			rmObs = fmt.Sprintf("%.3f (%d/%d)", *a.ReasonMatch, a.Mechanism, a.Judged)
			rmVerdict = passFail(*a.ReasonMatch >= reasonMatchThreshold)
		} else {
			rmObs = "absent (no judged treatment findings)"
		}
	}
	w("| Reason match, treatment RISKY true positives | ≥ 60%% MECHANISM | %s | %s |", rmObs, rmVerdict)

	hit := "absent"
	verdict := "cannot be evaluated"
	if t, ok := r.Arms[ArmT]; ok && t.HitRate != nil {
		hit = rateCell(t.HitRate.Rate)
		if t.HitRate.Rate.Value != nil {
			verdict = passFail(*t.HitRate.Rate.Value >= 0.50)
		}
	}
	w("| Recommended-validation hit rate (T) | ≥ 50%% | %s | %s |", hit, verdict)

	for _, cmp := range []string{"T-G", "T-H"} {
		p := findPairwise(r, cmp)
		obs, v := "absent", "cannot be evaluated"
		if p != nil {
			obs = floatCell(p.DeltaPrecision, "%+.3f")
			if p.DeltaPrecision != nil {
				obs = fmt.Sprintf("%+.1f points", *p.DeltaPrecision*100)
			}
			if p.MeetsPrecision != nil {
				v = passFail(*p.MeetsPrecision)
			}
			if p.Absent != "" {
				obs = "absent (" + p.Absent + ")"
			}
		}
		w("| Case-level precision, %s | ≥ +15 points | %s | %s |", cmp, obs, v)
	}
	w("")
	w("Precision is reported conditional on the 1:1 positive/negative ratio.")
	if r.Underpowered["recall"] {
		w("")
		w("**Underpowered at this cohort size:** with %d positives, a +15-point recall or", r.Positives)
		w("reason-match difference cannot reach significance. Those are reported as")
		w("\"consistent with H1, underpowered\", never as a pass.")
	}
	w("")
}

func passFail(ok bool) string {
	if ok {
		return "**meets**"
	}
	return "does not meet"
}

func findPairwise(r Report, comparison string) *Pairwise {
	for i := range r.Pairwise {
		if r.Pairwise[i].Comparison == comparison {
			return &r.Pairwise[i]
		}
	}
	return nil
}

func renderArms(w func(string, ...any), r Report) {
	w("## Per arm")
	w("")
	w("| Arm | Protocol | Cases | TP | FP | FN | TN | Precision | Recall | vs chance (Fisher, 2-sided) |")
	w("|---|---|---|---|---|---|---|---|---|---|")
	for _, name := range []string{ArmT, ArmG, ArmH} {
		a, ok := r.Arms[name]
		if !ok {
			continue
		}
		vs := "not applied"
		if a.VsChance != nil {
			if a.VsChance.TwoSided != nil {
				vs = fmt.Sprintf("%.4f", *a.VsChance.TwoSided)
			} else if a.VsChance.Absent != "" {
				vs = "absent (" + a.VsChance.Absent + ")"
			}
		}
		w("| %s | `%s` | %d | %d | %d | %d | %d | %s | %s | %s |",
			name, a.ArmID, a.Cases, a.Counts.TP, a.Counts.FP, a.Counts.FN, a.Counts.TN,
			rateCell(a.Precision), rateCell(a.Recall), vs)
	}
	w("")
	for _, name := range []string{ArmT, ArmG, ArmH} {
		a, ok := r.Arms[name]
		if !ok {
			continue
		}
		if len(a.Absent) > 0 {
			w("Arm %s did not score %d case(s): %s.", name, len(a.Absent), strings.Join(a.Absent, ", "))
			for _, id := range a.Absent {
				if reason := a.AbsentReasons[id]; reason != "" {
					w("  - `%s`: %s", id, reason)
				}
			}
		}
		if a.Voided > 0 {
			w("Arm %s excluded %d run(s) voided by the post-hoc scan: %s.",
				name, a.Voided, strings.Join(a.VoidedCases, ", "))
		}
	}
	w("")
}

func renderPairwise(w func(string, ...any), r Report) {
	w("## Pairwise")
	w("")
	w("| Comparison | Subset | Common | Discordant | Δ precision | p (1-sided) | p (2-sided) | Δ recall | p (1-sided) | p (2-sided) |")
	w("|---|---|---|---|---|---|---|---|---|---|")
	for _, p := range r.Pairwise {
		subset := p.Subset
		if subset == "" {
			subset = "all scored cases"
		}
		if p.Absent != "" {
			w("| %s | %s | — | — | absent (%s) | | | | | |", p.Comparison, subset, p.Absent)
			continue
		}
		w("| %s | %s | %d | %d | %s | %s | %s | %s | %s | %s |",
			p.Comparison, subset, p.CommonCases, p.Discordant,
			floatCell(p.DeltaPrecision, "%+.3f"),
			floatCell(p.PPrecision.OneSided, "%.4f"), floatCell(p.PPrecision.TwoSided, "%.4f"),
			floatCell(p.DeltaRecall, "%+.3f"),
			floatCell(p.PRecall.OneSided, "%.4f"), floatCell(p.PRecall.TwoSided, "%.4f"))
	}
	w("")
	if len(r.Pairwise) > 0 {
		w("Test: %s.", r.Pairwise[0].PPrecision.Method)
		if r.Pairwise[0].PPrecision.Seed != 0 {
			w("Monte Carlo draws %d, seed %d.", r.Pairwise[0].PPrecision.Draws, r.Pairwise[0].PPrecision.Seed)
		}
	}
	w("")
}

func renderHitRate(w func(string, ...any), r Report) {
	w("## Recommended-validation hit rate")
	w("")
	w("Path match against the fixing commit, no judge call.")
	w("")
	for _, name := range []string{ArmT, ArmG} {
		a, ok := r.Arms[name]
		if !ok || a.HitRate == nil {
			continue
		}
		w("**Arm %s:** %s", name, rateCell(a.HitRate.Rate))
		if a.HitRate.NoFixingPaths > 0 {
			w("")
			w("%d RISKY true positive(s) had no `h1-fixing-paths/v1` file and are excluded from the denominator, not counted as misses.", a.HitRate.NoFixingPaths)
		}
		if len(a.HitRate.PerCase) > 0 {
			w("")
			w("| Case | Hit | Matched path | Matched by |")
			w("|---|---|---|---|")
			for _, c := range a.HitRate.PerCase {
				mark := "no"
				if c.Hit {
					mark = "yes"
				}
				w("| `%s` | %s | %s | %s |", c.CaseID, mark, code(c.MatchedPath), c.MatchedBy)
			}
		}
		w("")
	}
}

func code(s string) string {
	if s == "" {
		return "—"
	}
	return "`" + s + "`"
}

func renderReasonMatch(w func(string, ...any), r Report) {
	w("## Reason match (§6)")
	w("")
	if r.ReasonMatch == nil {
		w("**Absent.** %s", r.ReasonMatchAbsent)
		w("")
		w("This is the §2 criterion that cannot be computed without a paid judge pass.")
		w("Until it runs, H1 has not been evaluated, whatever the other numbers say.")
		w("")
		return
	}
	sum, ok := parseReasonMatch(r.ReasonMatch)
	if !ok {
		w("**Present but unreadable.** The report carries a reason-match value that is not a")
		w("judge summary, so no rate is shown rather than a guessed one.")
		w("")
		return
	}
	if r.ReasonMatchNote != "" {
		w("%s", r.ReasonMatchNote)
		w("")
	}
	w("| Arm | Judged | MECHANISM | LOCALITY_ONLY | NONE | TOO_VAGUE | Reason match | LOCALITY_ONLY rate |")
	w("|---|---|---|---|---|---|---|---|")
	for _, arm := range sortedKeys(sum.Arms) {
		a := sum.Arms[arm]
		w("| %s | %d | %d | %d | %d | %d | %s | %s |", arm, a.Judged, a.Mechanism,
			a.LocalityOnly, a.None, a.TooVague, pct(a.ReasonMatch), pct(a.LocalityRate))
	}
	w("")
	w("LOCALITY_ONLY is reported beside the match rate deliberately: a high MECHANISM")
	w("count with near-zero LOCALITY_ONLY is evidence the judge is agreeing too easily,")
	w("not that reviewers are right.")
	w("")
}

// reasonMatchThreshold is the §2 criterion: at least 60%% of the treatment
// arm's judged RISKY true positives must be MECHANISM.
const reasonMatchThreshold = 0.60

// reasonMatchSummary is the part of h1judge.Summary this document reads.
//
// It is parsed from JSON rather than type-asserted because the value
// arrives two ways: as the judge summary itself when Score is called
// directly, and as a generic map when -render reads a sealed h1-score.json
// off disk. A type assertion would silently miss the second case, which is
// the one the results document is actually produced from.
type reasonMatchSummary struct {
	Arms map[string]struct {
		Judged       int      `json:"judged"`
		Mechanism    int      `json:"mechanism"`
		LocalityOnly int      `json:"locality_only"`
		None         int      `json:"none"`
		TooVague     int      `json:"too_vague"`
		ReasonMatch  *float64 `json:"reason_match_rate"`
		LocalityRate *float64 `json:"locality_only_rate"`
	} `json:"arms"`
}

func parseReasonMatch(v any) (reasonMatchSummary, bool) {
	var s reasonMatchSummary
	if v == nil {
		return s, false
	}
	b, err := json.Marshal(v)
	if err != nil {
		return s, false
	}
	if err := json.Unmarshal(b, &s); err != nil || len(s.Arms) == 0 {
		return s, false
	}
	return s, true
}

func pct(v *float64) string {
	if v == nil {
		return "absent"
	}
	return fmt.Sprintf("%.3f", *v)
}

func renderRecallByClass(w func(string, ...any), r Report) {
	w("## Recall by failure class (§7)")
	w("")
	classes := map[string]bool{}
	for _, name := range []string{ArmT, ArmG, ArmH} {
		for c := range r.Arms[name].RecallByClass {
			classes[c] = true
		}
	}
	if len(classes) == 0 {
		w("No positives scored, so recall by class is absent.")
		w("")
		return
	}
	var ordered []string
	for c := range classes {
		ordered = append(ordered, c)
	}
	sort.Strings(ordered)
	w("| Class | T | G | H |")
	w("|---|---|---|---|")
	for _, c := range ordered {
		w("| `%s` | %s | %s | %s |", c,
			rateCell(r.Arms[ArmT].RecallByClass[c]),
			rateCell(r.Arms[ArmG].RecallByClass[c]),
			rateCell(r.Arms[ArmH].RecallByClass[c]))
	}
	w("")
	w("Reported, not criterial. The pre-registration expects T to do worst on")
	w("`timing-race` and `resource-exhaustion`, which are deliberately not lensed.")
	w("")
}

// stratum describes one reported breakdown and how to bucket a case.
type stratum struct {
	title     string
	note      string
	bucket    func(Label) string // "" means this case has no value recorded
	required  string             // the label field that must be present
	dimension string             // key into ArmReport.Strata
}

// renderArmStrata prints per-arm precision and recall within one
// dimension's buckets, when the scorer computed them.
func renderArmStrata(w func(string, ...any), r Report, dimension string) {
	type row struct {
		arm, bucket string
		s           Stratum
	}
	var rows []row
	for _, arm := range []string{ArmT, ArmG, ArmH} {
		byBucket, ok := r.Arms[arm].Strata[dimension]
		if !ok {
			continue
		}
		for _, b := range sortedKeys(byBucket) {
			rows = append(rows, row{arm, b, byBucket[b]})
		}
	}
	if len(rows) == 0 {
		return
	}
	w("| Arm | Bucket | Cases | TP | FP | FN | TN | Precision | Recall |")
	w("|---|---|---|---|---|---|---|---|---|")
	for _, x := range rows {
		w("| %s | %s | %d | %d | %d | %d | %d | %s | %s |",
			x.arm, x.bucket, x.s.Cases, x.s.Counts.TP, x.s.Counts.FP, x.s.Counts.FN, x.s.Counts.TN,
			rateCell(x.s.Precision), rateCell(x.s.Recall))
	}
	w("")
}

func renderStrata(w func(string, ...any), r Report, labels []Label) {
	w("## Strata")
	w("")
	strata := []stratum{
		{
			title:     "Admission of title/description",
			dimension: "admission_description",
			required:  "admission.description",
			bucket:    func(l Label) string { return l.Admission.Description },
		},
		{
			title:     "Fallback pairs (A9(vi))",
			dimension: "fallback_pair",
			note:      "A fallback pair was matched on subsystem alone, so the stateful category is unbalanced within the pair on a dimension the arms can see in the diff. §2's pairwise deltas are to be given with and without these.",
			required:  "fallback_pair (or match_key_used)",
			bucket: func(l Label) string {
				if !l.HasFallbackInfo() {
					return ""
				}
				if l.IsFallbackPair() {
					return "fallback"
				}
				return "fully matched"
			},
		},
		{
			title:     "Source window (A11(iii))",
			dimension: "source",
			required:  "source (or source_window)",
			bucket:    func(l Label) string { return l.SourceOf() },
		},
		{
			title:     "Fix before/after model cutoff (A11(vi))",
			dimension: "fix_before_cutoff",
			note:      "Recorded as a covariate per positive; the model cutoff is May 2026.",
			required:  "fix_before_cutoff",
			bucket: func(l Label) string {
				if l.FixBeforeCutoff == nil {
					return ""
				}
				if *l.FixBeforeCutoff {
					return "fix before cutoff"
				}
				return "fix after cutoff"
			},
		},
		{
			title:     "Subsystem (A9(v))",
			dimension: "subsystem",
			required:  "subsystem",
			bucket:    func(l Label) string { return l.Subsystem },
		},
	}

	for _, s := range strata {
		w("### %s", s.title)
		w("")
		if s.note != "" {
			w("%s", s.note)
			w("")
		}
		counts := map[string]int{}
		recorded := 0
		for _, l := range labels {
			v := s.bucket(l)
			if v == "" {
				continue
			}
			recorded++
			counts[v]++
		}
		if recorded == 0 {
			w("**Not recorded.** The labels file carries no `%s`, so this breakdown is", s.required)
			w("unavailable. It is reported as missing rather than as a single bucket: one")
			w("bucket would read as \"no variation on this dimension\", which is a stronger")
			w("claim than \"not recorded\".")
			w("")
			continue
		}
		if recorded < len(labels) {
			w("Recorded for %d of %d cases; the rest are omitted from this breakdown.", recorded, len(labels))
			w("")
		}
		var keys []string
		for k := range counts {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		w("| Bucket | Cases |")
		w("|---|---|")
		for _, k := range keys {
			w("| %s | %d |", k, counts[k])
		}
		w("")
		renderArmStrata(w, r, s.dimension)
	}
}

func renderVoided(w func(string, ...any), r Report) {
	w("## Excluded cases")
	w("")
	if len(r.ProbeVoidedCases) == 0 && len(r.ProbeUnresolvedCases) == 0 {
		anyScanVoid := false
		for _, name := range []string{ArmT, ArmG} {
			if r.Arms[name].Voided > 0 {
				anyScanVoid = true
			}
		}
		if !anyScanVoid {
			w("None.")
			w("")
			return
		}
	}
	if len(r.ProbeVoidedCases) > 0 {
		w("**Voided by the contamination probe (A11), excluded from every arm:** %s.",
			strings.Join(r.ProbeVoidedCases, ", "))
		w("")
		w("The model already knew how these changes were fixed, so no arm's answer on")
		w("them means anything.")
		w("")
	}
	if len(r.ProbeUnresolvedCases) > 0 {
		w("**Probe unresolved — result is provisional:** %s.", strings.Join(r.ProbeUnresolvedCases, ", "))
		w("")
		w("Their mechanical scan was clean, but no evaluator has read the probe responses.")
		w("A clean mechanical scan is not an acquittal: the rules cannot see")
		w("mechanism-level recall.")
		w("")
	}
	for _, name := range []string{ArmT, ArmG} {
		a := r.Arms[name]
		if a.Voided > 0 {
			w("**Arm %s, voided by the §8 post-hoc scan:** %s.", name, strings.Join(a.VoidedCases, ", "))
			w("")
		}
	}
}

func renderCost(w func(string, ...any), r Report, meta RenderMeta) {
	w("## Cost")
	w("")
	note := meta.CostNote
	if note == "" {
		note = "Figures are the CLI's ESTIMATE of equivalent API cost under a subscription token, not amounts billed."
	}
	w("%s", note)
	w("")
}

func renderFingerprints(w func(string, ...any), r Report, meta RenderMeta) {
	w("## Provenance")
	w("")
	w("| Artifact | Value |")
	w("|---|---|")
	if meta.EngineCommit != "" {
		w("| engine commit | `%s` |", meta.EngineCommit)
	}
	if meta.CohortRecords != "" {
		w("| cohort `records_sha256` | `%s` |", meta.CohortRecords)
	}
	if meta.ArmFileHash != "" {
		w("| arm file | `%s` |", meta.ArmFileHash)
	}
	for _, k := range sortedKeys(meta.ProtocolHashes) {
		w("| protocol `%s` | `%s` |", k, meta.ProtocolHashes[k])
	}
	for _, k := range sortedKeys(meta.PromptHashes) {
		w("| prompt `%s` | `%s` |", k, meta.PromptHashes[k])
	}
	for _, k := range sortedKeys(r.Inputs) {
		if v := r.Inputs[k]; v != "" {
			w("| input `%s` | `%s` |", k, v)
		}
	}
	if rv, ok := r.Rules["rule_version"]; ok {
		w("| verdict rule | `%v` |", rv)
	}
	w("")
	for _, n := range meta.ProvenanceNotes {
		if n != "" {
			w("- %s", n)
		}
	}
	if len(meta.ProvenanceNotes) > 0 {
		w("")
	}
	w("### Rules as applied")
	w("")
	for _, k := range sortedKeys(anyMapToString(r.Rules)) {
		w("- `%s`: %v", k, r.Rules[k])
	}
	w("")
}

func sortedKeys[V any](m map[string]V) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func anyMapToString(m map[string]any) map[string]string {
	out := map[string]string{}
	for k := range m {
		out[k] = ""
	}
	return out
}

// renderLimitations is static text: these are registered limitations, not
// observations, so they belong in every rendering of this document
// regardless of how the numbers come out. Writing them here rather than
// leaving them to a human means a good result cannot quietly ship without
// them.
func renderLimitations(w func(string, ...any)) {
	w("## Registered limitations")
	w("")
	w("These are pre-registered, not discovered after the fact. They apply whatever")
	w("the numbers above show.")
	w("")
	w("**Staging confound (brief §2).** T has a substantiation stage and G does not,")
	w("so T differs from G in two declared ways: the discovery prompt's five domain")
	w("lenses, and the second stage. A T-over-G result is therefore prompt *plus*")
	w("staging, and cannot be attributed to the prompt alone. A fourth arm (generic")
	w("prompt + substantiation) would decompose it and is deferred.")
	w("")
	w("**Arm H is a floor, not a competitor (A10).** Under subsystem matching a")
	w("positive and its negative share a subsystem and near-identical windows, so H")
	w("gives both the same verdict by construction: its precision is pinned near the")
	w("base rate and its recall near 1.0. T−H answers \"does the treatment beat")
	w("knowing the subsystem\". The §2 pairwise criterion is carried by T−G.")
	w("")
	w("**Fallback pairs (A9(vi)).** A positive with no counted-clean negative in its")
	w("(category, subsystem) cell is matched on subsystem alone. Within such a pair")
	w("the stateful category is unbalanced on a dimension the arms can see in the")
	w("diff — every HIGH bgpd positive is a fallback pair by construction. Pairwise")
	w("deltas are to be reported with and without them.")
	w("")
	w("**The judge reads one finding per arm, not all of them (§6).** The judged")
	w("pool carries each arm's HIGHEST-RANKED finding only — under T stage B's first")
	w("surviving assessment, under G `findings[0]`. This differs from the pilot's")
	w("judge, which pooled every finding a reviewer produced, and the difference is")
	w("deliberate: §6 scores \"the case's highest-ranked finding\" and the treatment")
	w("brief §5 makes ranking a scored output for exactly this reason. It is the")
	w("stricter test — a reviewer earns nothing for burying the right answer at")
	w("position nine, and ranking badly costs the same as not finding the defect at")
	w("all. Reason-match figures here are therefore NOT comparable with the pilot's")
	w("anticipation rate, which used the looser pool.")
	w("")
	w("**In-family judge (§6).** Reason match is adjudicated by Opus, the same model")
	w("family as the treatment arm. This is stated on every figure derived from it.")
	w("Cross-vendor re-judging is desirable and not required for this")
	w("pre-registration.")
	w("")
	w("**Cost figures are estimates.** The credential is a subscription token, so")
	w("`cost_usd` is the CLI's estimate of equivalent API cost, not an amount billed.")
	w("")
}
