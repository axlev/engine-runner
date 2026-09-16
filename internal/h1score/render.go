package h1score

import (
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
	w("| Reason match, treatment RISKY true positives | ≥ 60%% MECHANISM | %s | %s |",
		"absent (judge pass has not run)", "cannot be evaluated")

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
	w("| Comparison | Common | Discordant | Δ precision | p (1-sided) | p (2-sided) | Δ recall | p (1-sided) | p (2-sided) |")
	w("|---|---|---|---|---|---|---|---|---|")
	for _, p := range r.Pairwise {
		if p.Absent != "" {
			w("| %s | — | — | absent (%s) | | | | | |", p.Comparison, p.Absent)
			continue
		}
		w("| %s | %d | %d | %s | %s | %s | %s | %s | %s |",
			p.Comparison, p.CommonCases, p.Discordant,
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
	w("%v", r.ReasonMatch)
	w("")
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
	title    string
	note     string
	bucket   func(Label) string // "" means this case has no value recorded
	required string             // the label field that must be present
}

func renderStrata(w func(string, ...any), r Report, labels []Label) {
	w("## Strata")
	w("")
	strata := []stratum{
		{
			title:    "Admission of title/description",
			required: "admission.description",
			bucket:   func(l Label) string { return l.Admission.Description },
		},
		{
			title:    "Fallback pairs (A9(vi))",
			note:     "A fallback pair was matched on subsystem alone, so the stateful category is unbalanced within the pair on a dimension the arms can see in the diff. §2's pairwise deltas are to be given with and without these.",
			required: "match_key_used",
			bucket: func(l Label) string {
				if len(l.MatchKeyUsed) == 0 {
					return ""
				}
				if l.IsFallbackPair() {
					return "fallback (subsystem only)"
				}
				return "matched on " + strings.Join(l.MatchKeyUsed, "+")
			},
		},
		{
			title:    "Source window (A11(iii))",
			required: "source_window",
			bucket:   func(l Label) string { return l.SourceWindow },
		},
		{
			title:    "Fix before/after model cutoff (A11(vi))",
			note:     "Recorded as a covariate per positive; the model cutoff is May 2026.",
			required: "fix_before_cutoff",
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
			title:    "Subsystem (A9(v))",
			required: "subsystem",
			bucket:   func(l Label) string { return l.Subsystem },
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
		if s.required == "admission.description" {
			w("*Per-arm precision and recall within these buckets are in the scored JSON")
			w("under `arms.<arm>.by_admission_description`.*")
		} else {
			w("*Case counts only. Per-arm precision and recall within these buckets are")
			w("not computed yet: the scorer strata only `admission.description`. Extending")
			w("it is a scorer change, not a rendering one.*")
		}
		w("")
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
	w("**In-family judge (§6).** Reason match is adjudicated by Opus, the same model")
	w("family as the treatment arm. This is stated on every figure derived from it.")
	w("Cross-vendor re-judging is desirable and not required for this")
	w("pre-registration.")
	w("")
	w("**Cost figures are estimates.** The credential is a subscription token, so")
	w("`cost_usd` is the CLI's estimate of equivalent API cost, not an amount billed.")
	w("")
}
