# Oracle bundle ingest contract

The counterpart to `prospective-bundle-contract.md`. That document describes
what the *engine* receives; this one describes what the *evaluator* receives,
and — as that one does — why the shape is what it is.

The headline, because it changes what can be built on top: **the evaluator
receives graded evidence that a later commit corrected the change under
review. It does not receive a list of defects, and it does not receive
labels.** An earlier draft of `a-to-c-mapping.md` assumed otherwise and had
to be corrected in public. The distinction is the whole subject of this
document.

## Layout

```
<case>-evaluator-only/          never under the bundle root
├── correlated-report.json      the complete selected correlated record
└── metadata-export-audit.json  per-field provenance for every metadata.json decision
```

Produced by the miner's `prospectiveexport` (`export.go:691`, which writes
the raw correlated record verbatim). `reviewer/` and `control/` — the
engine's inputs — are separate trees, and `<case>-evaluator-only/` is not
under the bundle root at all, so there is no filter to bypass rather than a
filter that must hold.

## What `correlated-report.json` contains

One `model.PRCandidateRecord`. Top-level keys, confirmed by keys-only
inspection of all ten pilot-v1 reports:

```
schema_version  original  stateful  retrospective  provenance
```

**`retrospective`** is the evidence, and it is evidence *about a later
commit*, not about a defect:

| tier | signal types | what it carries |
|---|---|---|
| `strong_signals` | `FIXES_SHA`, `FIXES_PR`, `EXPLICIT_REVERT`, `REGRESSION_MENTION` | `source_ref` (the later commit/PR), `raw_snippet` (the matched message line), `confidence`, `changed_paths` (paths *the fix* touched) |
| `medium_signals` | `SAME_FUNCTION_FIX`, `LINKED_ISSUE_FIX`, `REGRESSION_TEST_ADDED` | `function_or_path`, `context`, `changed_paths` |
| `weak_signals` | `SAME_FILE_MODIFICATION`, `SAME_SYMBOL_DIFFERENT_PATH` | `file_path` |

Plus `reversal` and lineage evidence: `reversal_type`, and `path_overlaps[]`
giving per-path counts of how much of the original change a later patch undid.

**`stateful`** is the miner's own heuristic — a score in [0,1], a
`HIGH`/`MEDIUM`/`LOW` category, matched path and keyword rules, matched
symbols. It is a *prediction*, not an observation, and must never be averaged
with the retrospective evidence. `internal/oracle` surfaces disagreement
between the two as `Evidence.StatefulConflict` rather than folding them
together, because a case where the heuristic says HIGH and nothing corrective
ever happened is unresolved, not middling.

**`original`** and **`provenance`** are PR metadata and provenance. Note
`original.labels` and `original.pre_merge_issue_refs` are GitHub labels and
issue references — they look defect-shaped in a recursive scan and are not.

## The granularity ceiling: per path

This is the constraint that determines what the benchmark can claim.

**Every line-level field in the record is an `int`.**
`original_removed_lines`, `reversed_line_overlap_count`,
`later_added_lines_matching_original_removals`, and the same fields under
`path_overlaps[]`, are all counts. There are no line numbers and no line
content anywhere. Within `path_overlaps[]` only `path` is a string.

So the finest available granularity is **per file path**, rankable by how
much of the original change the later fix reverted. That is slightly better
than bare path presence, and nowhere near a mechanism.

Consequences for the matching rule (backlog 2.3, alex's call):

- **same-file** — implementable. Expected to be near-vacuous: reviewers
  produce findings on files the diff already touches, and the fix usually
  touches those same files, so almost everything "matches".
- **file + symbol** — *partially* implementable. Only `medium_signals` carry
  `function_or_path`; strong signals carry paths and no symbol. So the rule
  would apply to the weaker half of the evidence and not the stronger.
- **file + symbol + mechanism** — **not implementable from this record.** It
  carries no mechanism. `raw_snippet` is a commit-message line and `context`
  is a message snippet; neither describes what the bug was.
- **mechanism-only, location-agnostic** — not implementable, same reason.

The record does contain the *pointer* to the mechanism: `source_ref` is the
fixing commit. Deriving a mechanism means materialising that commit's diff —
which `miner retrospective-export` already does; see below.

## The scoreable base is 18, not 174

`miner retrospective-export` was run against all ten cases
(`output/frr-pilot-retrospective-evaluator-only/`). **Materialization is
perfect — 174 of 174 commits, no `missing`, no `metadata_only`, no errors** —
so nothing was force-pushed away or stranded on an unmerged branch. That
answers "did materialization work". It is not the number that matters.

**Reach is 7 of 10 cases.** `3a74a3a2`, `9ba8fca7` and `c96a113c` carry zero
signals at any tier. No corrective evidence means no mechanism comparison, and
that is emphatically **not** "no defect" — it is no evidence either way.

**And the 174 is dominated by the weakest evidence.** Signals by tier across
the cohort:

| tier | count | what a signal means |
|---|---|---|
| strong | **18** | `FIXES_SHA` / `FIXES_PR` / `EXPLICIT_REVERT` / `REGRESSION_MENTION` — a maintainer stated this commit fixes that PR |
| medium | 153 | heuristic `function_or_path` match |
| weak | 72 | `SAME_FILE_MODIFICATION` / `SAME_SYMBOL_DIFFERENT_PATH` — a later commit touched the same file |

A weak signal asserts only that *some* later commit touched this file.
Comparing a reviewer's finding against such a commit's diff is not evidence;
it is noise shaped like evidence. So **the population that can support a
correctness claim is the strong tier: 18 signals across 7 cases**, at 1, 1, 3,
3, 2, 5, 3 per case.

**This also explains the cohort's skew, and reframes it.** `de6d5d30` holds 116
of the 174 commits (67%) and 53 of the 76 logical patches, against a median
scoreable case of 6. But it holds **105 of the cohort's 153 medium signals
(69%)**, and its strong-signal count is 3 — the same as two other cases. So the
concentration is a property of how many later commits touched those files, not
of how broken that PR was.

The consequence for rule design is that **tier selection matters more than
case weighting.** An unweighted correspondence rate over all tiers would be
dominated by one case and would reward findings in high-churn areas — a
quantitative restatement of correspondence-is-not-correctness. Scoring on the
strong tier alone largely dissolves the skew (3 of 18 rather than 116 of 174),
at the cost of an 18-item base.

Note also that strong signals are rare in *every* case (1-5). There is no case
where the strongest evidence is plentiful.

### Measured: correspondence is a coverage diagnostic, not a score

Tier sensitivity was measured across all three arms (109 findings). Two
results, and the second is the one that matters.

**The weak tier is exactly redundant.** `all-tiers` and `strong+medium` agree
in every cell, overall and per arm. Every path a weak signal names is already
named by a strong or medium signal, so for path matching the 72 weak signals
contribute nothing. The tier choice is two-way, not three-way, and the weak
tier can be dropped without loss.

**The tiers barely disagree.** On the scoreable base, strong-only is 78.6% and
strong+medium 92.3%, with a per-arm spread of 74-83% at strong-only — tight,
not variable, despite strong-signal bases of 1-5 per case. So the strong
signals land mostly on paths reviewers were already examining. That is evidence
the cohort's cases are well chosen, not evidence the metric works. Reach at
strong tier is **6 of 10** cases, not 7: one case's strong signals name no
`changed_paths` at all, and medium adds that case back.

**And correspondence penalises recall, which disqualifies it as a score.**

| arm | findings | all findings | scoreable base |
|---|---|---|---|
| sonnet | 19 | **84.2%** | 94.1% |
| opus-narrow | 41 | 61.0% | 92.6% |
| opus-wide | 49 | 63.3% | 91.2% |

**Sonnet ranks highest. Every other metric in this project ranked it worst.**
This one inverts the order — a 23.2pp spread on all findings, collapsing to
2.9pp on the scoreable base, which locates the effect in the denominator rather
than in review quality.

The mechanism is that extra findings land on more peripheral files, and
peripheral files are less likely to be ones a later fix touched. So a reviewer
reporting only its safest findings scores higher, and a reviewer that finds
more scores lower.

One honest qualification: pure finding count does not fully explain it.
`opus-wide` has *more* findings than `opus-narrow` (49 vs 41) and a slightly
*higher* rate, so count is not the only term. The large, robust effect is
sonnet against both opus arms.

**This is the third metric in this project to fail, and the three share a
shape: each can be improved by making the system worse.**

| metric | improved by |
|---|---|
| label-delta movement | a worse stage 2, leaving more for stage 3 to repair |
| sub-disposition movement | the same — demonstrated, wide's better stage 2 scored 0 |
| path correspondence | **reporting fewer findings** |

A benchmark scored on correspondence would tell you to find less. That is worse
than being silent about correctness; it is pointed the wrong way.

So the contract's position: **correspondence is a coverage diagnostic** — it
answers "did the reviewer look where the fix landed" — and must never be quoted
as a correctness number. If a tier must be selected, use **strong-only**: it is
the only tier where a signal means a maintainer stated this commit fixes that
PR, and it costs 14 points of a number that should not be quoted anyway.

### Two requirements this imposes on the judge

1. **Score correctness, via mechanism comparison.** All 174 fixing commits are
   materialized, so this is now buildable. It is the only remaining candidate
   that cannot be improved by degrading the system, because it asks whether a
   finding was *right*.
2. **Report precision *and* recall, never a single rate.** This follows
   directly from the inversion above: any single number over a variable-size
   finding set is gameable by changing the set size. Sonnet's 19 findings and
   opus-wide's 49 cannot be compared on one ratio, whatever that ratio
   measures.

### On measuring a threshold before choosing it

The two prior metric failures in this project both came from choosing a
threshold by convenience and discovering afterwards that the threshold carried
the result — label-delta scoring manufactured a spurious cost correlation, and
an A→C confidence threshold swung sonnet from 47% to 89% on identical data. The
tier threshold here is the same kind of knob.

So it is to be **measured, not picked**: report correspondence at strong-only,
strong+medium, and all-tiers, and see how far the answer moves. If the tiers
agree, the choice is free and the contract should say so. If they diverge, the
divergence is the finding and it belongs in front of whoever chooses.

**One caveat on who may run that measurement.** Tier sensitivity requires
matching reviewer findings against the paths the signals name, and those paths
say where the defect was. Any agent that reads per-case or per-finding output
is de-blinded for those cases. The measurement must therefore emit **aggregate
rates per tier only** if a blind analyst is to run it; per-case scoring is work
for an agent that is not doing blind analysis.

This is already load-bearing rather than hypothetical: reporting *which* cases
have zero corrective signals is itself weakly answer-adjacent, so the analyst
who produced the coverage table above is no longer fully blind on `3a74a3a2`,
`9ba8fca7` and `c96a113c`. Per-finding judgments are unaffected — nothing in a
zero-signal fact speaks to whether an individual finding was right — but the
limitation is recorded rather than assumed away.

## Where mechanism derivation should live

Input to that decision, since it determines which repository takes the work.

**The fetch belongs in the miner.** It already has the git object database
and materialises trees without a checkout (`internal/gitx/snapshot.go`), and
the oracle already names the fixing commit. The engine, by contrast, only
ever sees a sealed bundle and has no upstream git access — granting it that
access would breach the isolation the whole design rests on.

**The decisive argument is mutability, not convenience.** The upstream
repository is mutable: a commit can be force-pushed away, a branch deleted, a
fork vanish. If the engine derived mechanisms at judge time, the same sealed
run scored twice could produce different answers, and the score would depend
on when it was run. Derived in the miner it is computed once, exported as
evaluator-only data, and hashed like everything else in the bundle — so a
score becomes reproducible from artifacts rather than from the internet.

**The judgment belongs in the evaluator.** Deciding whether a reviewer's
described mechanism matches the fix's mechanism is a semantic comparison, not
a string match, and section 6.5 already anticipates an LLM evaluator for
exactly this kind of call. That half should not move into the miner, which is
deterministic by design and should stay that way.

So the split: **the miner exports the fixing commit's diff as evaluator-only
data; the judge compares it against reviewer findings.** It is a miner schema
change *and* an engine judge feature, not either/or.

## Isolation is a property of this contract, not a convention

The miner's export contract states that the evaluator directory "must never
be exposed to an engine". Until `internal/oracle` was written, that held for
an accidental reason: **no code read the oracle at all, so nothing could leak
it.** The guarantee was one `import` statement away from being false, and
nothing in the repository would have objected.

It is now enforced mechanically. `internal/oracle/isolation_test.go` scans
every `.go` file under `internal/adapters`, `internal/orchestrator`,
`internal/runner`, `internal/evaluation` and `cmd/bench`, and fails if any of
them so much as mentions this package's import path. The guard has been
verified to fail when violated, not merely to pass — a guard never seen to
fire is not a guard.

A second test asserts every fixture under `fixtures/oracle/` declares the
synthetic placeholder repository, so a real `correlated-report.json` cannot
be copied into the repository as test data. That would put the pilot cohort's
answer key in the tree, where anything globbing it would find it.

**The human half of the isolation has no mechanism.** Section 6.5 isolates
the judge from reasoners 1-3 because a judge that has seen the reasoning
cannot assess it independently. The same logic applies to whoever analyses
results: an analyst who has read the answer key cannot produce a blind
verdict afterwards. Every disposition and replication judgment recorded about
the pilot-v1 cohort was made blind, and that was preserved by convention —
by declining to open a file that is two commands away. `internal/oracle` was
developed and tested entirely against hand-authored fixtures for this reason.
Nothing enforces that, and anyone continuing this work should know that the
discipline is theirs to keep.

## Ingest

`internal/oracle.Load(path, caseID)` returns an `Evidence`: the record
normalised into a path-keyed view, with the strongest signal tier per path,
the signal types that named it, any symbols from medium signals, and the
reversal overlap count.

It deliberately implements **no matching rule**. `Evidence.ByPath` is the
coarsest correspondence primitive and is the one thing every candidate rule
needs; a same-file rule would be that lookup and nothing more. Choosing a
rule is 2.3.

There is no JSON schema to validate against, because the miner publishes none
for this record — its shape is defined by its Go types. `Load` therefore
rejects a record with no `schema_version`, on the grounds that an empty or
wrong file would otherwise score every case as "no evidence" silently.

## What this cannot support

Stated plainly, because the failure mode it guards against has already
happened twice on this project.

Two metrics have been retired here: disposition-label deltas, which
manufactured a spurious correlation, and sub-disposition movement, which
turned out to measure stage 2's error rate. Both counted whether a stage
*changed* something and neither asked whether the review got *better*.

This record does not let a judge ask the second question either. It supports
**correspondence** — did a reviewer finding land on a path a later fix
touched — and correspondence is not correctness. A finding can match the
corrected path by coincidence, and on a change touching three files it
usually will. A matching rule built on this alone inherits exactly the shape
both retired metrics died of.

That is not an argument against building the ingest, which is needed
regardless. It is an argument for being explicit that a correspondence score
is not a correctness score, wherever the resulting number ends up being
quoted.

## Mechanism agreement: what the judge measures, and what it does not

This section is written before the judge was implemented, deliberately. Three
metrics have failed on this project and each was quoted as something stronger
than it was. Naming this one's limits first is cheaper than retracting them
later — two of the three retractions were public.

### It is not correctness

**Mechanism agreement measures whether a reviewer predicted what maintainers
later corrected.** That is a better proxy than correspondence, which only
asked whether a finding landed on a file some later commit touched. It is
still a proxy.

Specifically, it is wrong in both directions and neither is a bug to be fixed:

- **A real defect nobody ever fixed scores as a false positive.** FRR is a
  live project; plenty of real bugs are never corrected, or are corrected
  outside the observation window, or are fixed silently inside an unrelated
  refactor. A reviewer that finds one of those is right and is scored wrong.
- **A finding about a real-but-unfixed problem is penalised for being right.**
  This is the same error stated from the reviewer's side, and it is the one
  that matters when comparing arms: an arm that finds more obscure real
  defects is penalised relative to one that finds only the obvious ones.

So the metric answers "did this reviewer anticipate the maintainers", and
that question is worth asking. It does not answer "was this reviewer right",
and no number derived from this data answers that. **Anywhere the resulting
figure is quoted, it is quoted with the word "agreement", never "accuracy"
and never "precision against ground truth".**

### Denominator rules

Both learned from metrics that have already been retired here.

**1. Cases with no corrective evidence are EXCLUDED, never scored as
all-false.** `3a74a3a25bda9041`, `9ba8fca704a95e70` and `c96a113c3301c4fa`
carry zero signals at any tier. Scoring their findings as unmatched would
count *no evidence* as *wrong* — the precise denominator error that
disqualified correspondence, where including unscoreable cases dropped the
overall rate from 78.6% to 50.5% and told you nothing about review quality.

Excluded cases are reported as excluded, with their finding counts, so the
scoreable base can never be mistaken for the cohort.

**2. Precision and recall are reported separately, never combined into one
rate.**

- *Precision* — of findings in scoreable cases, how many describe a defect
  that a later fix addressed.
- *Recall* — of strong-tier fixing commits, how many were described by at
  least one finding.

A single ratio over a finding set whose size the reviewer controls is
gameable by changing that size, and that is not hypothetical: correspondence
scored sonnet highest (84.2%) precisely because it produced 19 findings where
opus-wide produced 49. Reporting the two separately is what makes "found more,
matched a smaller fraction" legible instead of looking like a quality
difference.

### Judge design

Three requirements, each aimed at a specific way this would otherwise fail.

**It must argue both ways.** The judge sees the finding and the fix together,
which is ideal conditions for constructing a match after the fact. It
therefore borrows reasoner-3's structure: before ruling, it must state the
strongest case *against* the match. A judge asked only to look for agreement
will find it, and the resulting number will be a measure of its own
agreeableness.

**Same mechanism, not same area.** A match requires the finding to name the
failure mode the fix addressed. Same file is not a match; same function is not
a match. Locality matching is correspondence, which this project has already
retired — reproducing it inside an LLM call would make it more expensive and
less legible, not more valid.

**Vagueness earns nothing.** "There may be a memory issue here" must not match
a memory-leak fix. A finding earns a match by being specific enough that it
could have been wrong. This is the rule that stops the metric rewarding
hedging, in the same way the recall/precision split stops it rewarding
brevity.

### Pre-registered prediction

Recorded before any real run, because the one previous pre-registration on
this project (`fe99bcf9` f4) is the only reason that result carried weight.

**Prediction: precision will come in well below the 78.6% strong-tier
correspondence rate.** Correspondence needed only the right file; this needs
the right mechanism, and the gap between those is the entire reason for
building it.

**Falsifier, stated in advance:** if precision comes back *close* to 78.6%,
the likeliest explanation is that the judge is rationalising matches rather
than that the reviewers were that accurate. In that case the judge's
against-the-match reasoning gets read before the number is believed, and a
deliberately mismatched control pair — a finding and an unrelated fix — is run
to check it can say no at all.
