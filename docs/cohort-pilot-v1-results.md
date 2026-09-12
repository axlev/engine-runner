# FRR pilot-v1 cohort: results summary

Ten real FRRouting changes, three reasoning stages each, Claude Sonnet under
the frozen `pilot-v1` protocol. Run 2026-09-10/11. **Status: `Integrated`** —
exercised end to end against real inputs. Not `Verified` as a measurement,
for reasons in "How to read this" below.

The sealed run trees stay in `build/` and are never committed
(`AGENTS.md`, Layout rules). This file records the numbers and what follows
from them.

## Outcome

All ten completed. **$20.64, 6,599s of stage time (1.8h), 19 findings.**

| run | cost | time | found | reasoner-2 | reasoner-3 |
|---|---:|---:|---:|---|---|
| `frr-deaebcc7` | $3.50 | 895s | 2 | c1 n0 r1 i0 | c1 n0 r1 i0 |
| `frr-3a74a3a2` | $2.94 | 801s | 1 | c0 n0 r1 i0 | c0 n0 r1 i0 |
| `frr-c21d519d` | $2.87 | 721s | 1 | c0 n0 r1 i0 | c0 n0 r1 i0 |
| `frr-de6d5d30` | $2.77 | 889s | 5 | c4 n0 r1 i0 | c4 n0 r1 i0 |
| `frr-83d945a6` | $2.68 | 995s | 3 | c2 n1 r0 i0 | c2 n1 r0 i0 |
| `frr-fd8ee329` | $2.19 | 846s | 3 | c1 n0 r1 i1 | c1 n0 r1 i1 |
| `frr-fe99bcf9` | $1.48 | 472s | 1 | c1 n0 r0 i0 | c1 n0 r0 i0 |
| `frr-322abe6a` | $1.29 | 592s | 2 | c1 n1 r0 i0 | c1 n1 r0 i0 |
| `frr-9ba8fca7` | $0.58 | 295s | 1 | c1 n0 r0 i0 | c1 n0 r0 i0 |
| `frr-c96a113c` | $0.33 | 92s | 0 | c0 n0 r0 i0 | c0 n0 r0 i0 |
`c`/`n`/`r`/`i` = confirmed, narrowed, rejected, inconclusive.

True spend was ~$21.50: `frr-de6d5d30` burned $0.88 on a first attempt that a
vendor quota killed between stages, and that run tree no longer exists.

## Cost by stage

| stage | cost | share | time |
|---|---:|---:|---:|
| reasoner-1 (discovery) | $8.25 | 40% | 2,790s |
| reasoner-2 (evidence) | $6.87 | 33% | 2,191s |
| reasoner-3 (verification) | $5.51 | 27% | 1,618s |

**Staging is 60% of spend.** `review-a.json` is the single-pass baseline — one
reviewer, one pass — and it costs $8.25. The other $12.38 buys stages 2 and 3.

## The result that matters

**Reasoner-3 moved no disposition label. Nineteen findings judged, zero labels
moved, zero new findings, ten cases out of ten.**

> **Amended.** The paragraph that followed claimed stage 3's output set was
> identical to stage 2's, and therefore that scoring B and C against ground
> truth "would return identical numbers by construction." **That is false.**
> It was true only of disposition *labels*, which is the metric this document
> originally used and which was later retired for undercounting by 40%. Under
> the adopted rule — label *or* claim change — sonnet is **1 of 19**, not 0.
>
> In `frr-fd8ee329` f1, stage 2 rejected the finding on the grounds that
> `zlog_file_set_fd()` during `zlog_init()` reaches `zlog_file_cycle()`'s
> `fd = dup(zcf->fd)`. Stage 3 showed that is not what happens:
> `zlog_targets.c:193` breaks on `if (zcf->prio_min == ZLOG_DISABLED)` and the
> `dup()` sits at :196, *after* the break, while `log_vty.c:53` initialises
> that target with `.prio_min = ZLOG_DISABLED`. The first call breaks out
> before the dup; the real dup happens later, when `--log stdout` is
> processed. Same `REJECTED` verdict, **different mechanism** — so B and C
> would not score identically against an oracle, and the "by construction"
> claim does not hold.

On this cohort stage 3 altered one finding's reasoning and no finding's label,
for $5.51 — 27% of spend.

Reasoner-2 is the opposite: it altered **7 of 19** findings (4 rejected, 2
narrowed, 1 inconclusive). Whether those changes were improvements is exactly
what ground truth answers and nothing else can.

## What this does and does not establish

It does **not** say whether any finding is correct. `internal/evaluation`
computes reviewer-side metrics only and never reads an oracle, so a correct
rejection and a wrong one are indistinguishable here (backlog Phase 2).

It does establish, without any oracle, that stage 3's contribution to the
finding set was nil.

## How to read this

- **Run-to-run variance is large.** `case-83d945a6dd4e5785` was run twice
  under identical conditions and gave 1 finding, then 3. A single run is a
  sample. Nothing resting on a one-or-two-finding difference is safe.
- **n=10, one arm, one protocol.** No statistical claim is supported.
- **Counts are not the substance.** Whether stage 3 was rubber-stamping or
  genuinely failing to falsify is answerable only by reading its `rationale`
  fields — see `analysis-session-prompt.md`.
- Contamination was probed, not assumed: `contamination-probe-pilot-v1.md`.

## The null may be about sonnet, not about staging

Every instance of reasoner-3 contributing anything, across every run ever
made, was on **opus**:

| run | arm | what reasoner-3 did |
|---|---|---|
| `arm-opus-1` | opus | contributed **2 new findings** |
| `tlv-opus-1` | opus | **overturned a rejection** (B `r1` → C `c2`) |
| 14 others | haiku, sonnet | nothing, in any run |

0 of 14 on the weaker arms; 2 of 2 on opus. The proposed mechanism was that
adversarial verification needs enough capability to construct a genuine
falsification, and that a weaker model can only re-read stage 2's chain and
agree with it.

> **The second half of that mechanism is refuted by the completed arms.**
> Sonnet's stage 3 is not re-reading and agreeing. Its rationales are
> multi-branch, specific, and independently verifiable: `c21d519d` f1 traces
> five construction paths to close a rejection, and `3a74a3a2` f1 states that
> it went further than stage 2 by checking hook consumers for deferred use of
> the adjacency pointer. Both arms genuinely attempt falsification. What
> differs is how often they *overturn* — and whether that difference is real
> is settled below, not here.

**This cohort was run on sonnet on my recommendation**, argued from a
ceiling-effect worry — that too strong a model would find everything in stage
1 and leave no increment to measure. That was flagged at the time as a guess
rather than a measurement, and the data points the other way: opus is not the
arm where staging has no room, it is the only arm where staging did anything.

So the headline null above may be a property of **the arm**, not of the
pipeline. Re-running the cohort on opus (~$60, reachable on the current
token, no comparability caveat) tests that directly, and gates everything
downstream including whether the judge is worth building. `fable.yaml` exists
as a second strong arm for the same question; its cost is uncalibrated.

Caveats: n=2 for opus, both on synthetic fixtures rather than real FRR cases.
Suggestive, not established. And it does not rescue stage 3 — it says only
that the wrong arm may have been measured.

## Opus arm: budget caps were deliberately not raised

Opus runs much closer to its per-stage `max_cost_usd` than sonnet did. Because
`max_cost_usd` is the one bound the vendor enforces *before* the fact, a stage
that reaches it is cut off mid-work rather than reported after it.

The caps were **held at their sonnet-arm values for the whole opus arm**.
`configs/agents/opus.yaml` is hashed into every run's fingerprint, so raising
caps partway would make the opus arm non-uniform against a sonnet arm that ran
uniformly throughout. Experimental validity was the only argument for holding
them, and it remains the only one — the cost argument an earlier draft of this
section also offered is retracted below.

**The truncation risk was not hypothetical: it fired twice.**

`opus-c21d519d` failed outright. Its reasoner-2 was killed by the cost cap on
both attempts ($5.37 and $5.02 against $5.00), no review-b was produced, and
$13.72 bought nothing. The arm is therefore **9 sealed of 10**.

`opus-de6d5d30` hit **the same cap on its first reasoner-2 attempt** ($5.31,
106% of $5.00), was killed the same way, retried, and the retry came in at
$4.59 and succeeded. It is sealed `completed` and was scored in this document
as a clean case.

> **Correction.** An earlier version of this section claimed the failure mode
> was benign — "it failed loudly… no sealed result is contaminated." **That is
> only half right, and the half that is wrong is the one that mattered.**
> `c21d519d` failed loudly. `de6d5d30` failed *silently*: one killed attempt,
> one cheaper retry, and a sealed record indistinguishable from a clean run
> except for an `attempts: 2` counter and an empty failure `detail`. The only
> thing separating the visible failure from the invisible one is whether the
> retry happened to land under the cap.
>
> So the price of holding the caps was one lost case **and** one silently
> budget-compromised case counted as clean — not one lost case and an
> otherwise uncorrupted arm.

Two related properties of the bound, both measured:

- **`max_cost_usd` is not a hard ceiling.** A deliberate probe at a $0.002 cap
  recorded $0.005638 — a 2.8× overshoot. The CLI stops shortly *after*
  exceeding, not at, which is why killed attempts read 106% and 107% of cap
  rather than exactly 100%. Caps bound runaway spend; they do not bound a
  single stage precisely, and cap arithmetic should not be read as a spend
  guarantee.
- **The ≥95% SUSPECT rule was the right shape but was applied to the wrong
  denominator.** These utilisations are **per attempt**, not per stage, which
  is correct for a cap rule — the vendor kills one invocation — but the
  distinction was not stated. Per-attempt wall clock, recoverable from
  `telemetry.json`: `c21d519d` r2 attempt 1 at 865s (96% of 900s),
  `de6d5d30` r2 attempt 1 at 834s (93%). Both were near the time bound as well
  as over the cost bound, so raising only `max_cost_usd` would have relocated
  these failures rather than removed them.

### Retracted: spend does not predict falsification

An earlier draft of this section claimed cost was unrelated to falsification.
A later one claimed the opposite, on n=8. **Both are withdrawn.** At n=9,
sealed cases ranked by total cost, with movement scored two ways.

Costs here are **true spend including killed attempts**, not the figures
`run.json` reports. `internal/results/results.go:208` assigns
`doc.Usage = winning.Result.Usage`, so a stage that burned an attempt records
only the attempt that succeeded. Durations are summed across attempts
eighteen lines earlier, at `results.go:189`, with the comment that "a stage
that burned a repair attempt really did cost that time, and a per-case total
that ignored it would understate the cohort" — an argument that applies
verbatim to dollars and is not applied to them. Only the two retried cases
differ; the other seven are identical either way.

| case | total | findings | label move | any move |
|---|---|---|---|---|
| `opus-de6d5d30` | **$15.75** | 8 | — | — |
| `opus-3a74a3a2` | $9.76 | 4 | — | — |
| `opus-83d945a6` | $9.34 | 5 | yes | yes |
| `opus-fe99bcf9` | $9.19 | 4 | yes | yes |
| `opus-c96a113c` | $7.95 | 6 | yes | yes |
| `opus-deaebcc7` | $7.86 | 3 | — | — |
| `opus-fd8ee329` | $5.51 | 3 | — | yes |
| `opus-9ba8fca7` | $4.40 | 4 | — | yes |
| `opus-322abe6a` | $3.42 | 4 | — | — |

Moved cases average $7.28, unmoved **$9.20** — unmoved are the *more*
expensive group, and correcting `de6d5d30` to its true spend widened that gap
rather than narrowing it. Top four by cost: 2 of 4 moved. Bottom five: 3 of 5
moved. The most expensive case in the arm moved nothing, at $15.75.

**The instructive part is why the n=8 signal existed at all.** Under
label-only scoring the split is 3/4 top versus 0/4 bottom, which looks
striking. Under any-movement scoring it is 2/4 versus 3/5, which is nothing.
Both cheap "unmoved" cases — `fd8ee329` and `9ba8fca7` — contain
sub-disposition moves that label-delta scoring discards. The coarse metric
manufactured the correlation. A scoring choice that loses information did not
merely undercount the result; it produced a spurious one, and it survived two
increments before the ninth case exposed it.

### Tested and not supported: reasoner-2 budget starvation

The hypothesis was that stage-3 moves come from stage 2 having exhausted a
budget stage 3 still had — making the A→C increment a budget artifact rather
than a capability. Measured across the arm, it comes out **inverted**: moved
cases average −1.7pp B-vs-C squeeze, unmoved +16.2pp. The two most-squeezed
cases (`3a74a3a2` +30.9pp, `de6d5d30` +19.9pp) moved nothing, and two of the
three label-moves had stage 2 holding *more* headroom than stage 3.

Reasoner-2 is the most budget-pressured stage in absolute terms — mean
utilisation 56.9% against reasoner-3's 52.4%, and it holds the arm's three
highest **per-attempt** marks (`c21d519d` 107%, `fe99bcf9` 92.1%,
`de6d5d30` 106% on its killed first attempt) — but
~4.5pp is far too small to carry an explanation. The mechanism is real in
exactly one case: in `opus-fe99bcf9`, stage 2 at 92.1% left f3 `INCONCLUSIVE`
citing missing material, and stage 3 at 80.3% read that material and rejected
it. That is a single-case story. It is recorded here as tested and not
supported, **not** as a standing limitation of the design.

### The harness is demonstrably binding, and that is measurable

Reviewers cannot enumerate the filesystem: the adapter hard-sets `--tools Read`
as a prompt-injection defence, there is no Grep or Glob, and directory reads
fail with `EISDIR`. So a reviewer can only open paths it already knows from the
diff or the handoffs, and cannot discover consumers of an API.

This is no longer a theoretical ceiling. In `opus-de6d5d30` f8, stage 3 wrote
out its own decision procedure — does `enum zclient_send_status` have a
negative enumerator? if yes the pointer conversion is clean, if no `-Werror`
fires — then paged blindly through `lib/zclient.h` at eight offsets, stopped at
line 745, and returned `INCONCLUSIVE` for not having located the definition
within budget. The enum is at **line 764**, nineteen lines further on, and it
contains `ZCLIENT_SEND_FAILURE = -1`. By the stage's own stated logic the
finding was a `REJECT`. One grep would have closed it.

> **This is evidence that the harness was binding, but not clean evidence that
> *search* was the binding constraint.** `de6d5d30` is the most
> budget-compromised case in the arm: its review-b is the output of a retry
> after a cap kill, and the f8 rationale says it abandoned the `zclient.h`
> search "inside budget" — naming budget, not tooling, as what stopped it. So
> f8 has two candidate causes, no-search and budget exhaustion, and this arm
> cannot separate them. Worse, `opus-wide` raised tools *and* budgets in one
> fingerprint change, so if it resolves f8 to `REJECT` that result is
> confounded the same way. The recoverable test is per-case: a wide-arm move
> whose winning attempt cost less than the old $5.00 cap cannot be explained
> by the extra budget, which leaves search.

At least one `INCONCLUSIVE` in this arm is therefore a harness artifact rather
than a model limitation, which makes the movement figures below undercounts of
what the *pipeline design* can do. **This arm measures opus-under-this-harness,
and the harness binds.** The gap was deliberately left unfixed mid-arm, for the
same fingerprint-uniformity reason as the caps.

### The arm's result depends on the metric

**Adopted rule: label *or* sub-disposition movement** — stage 3 is credited when
it changed a finding's disposition or its actual claim, and not when it only
added evidence behind an unchanged claim. That gives the arm **5 of 9 runs**.

Across **41** stage-2→stage-3 finding pairs in the 9 sealed cases: **3 label
moves, 4 sub-disposition moves, 34 no movement**, plus 1 new finding
(`c96a113c`, valid `discovered_via`).

| scoring rule | runs | findings |
|---|---|---|
| disposition label changed | 3 of 9 | 3 of 41 (7.3%) |
| label **or** sub-disposition changed — *adopted* | 5 of 9 | 7 of 41 (17.1%) |
| stage 3 contributed new verifiable evidence | 7 of 9 | 11 of 41 (26.8%) |

**Report the per-finding rate alongside the per-run rate, and prefer it for
cross-arm comparison.** Per-run movement scales with how many findings a run
has available to move, and the arms differ sharply there: sonnet found 19
findings across 10 cases (mean 1.9), opus 41 across 9 (mean 4.6). One sonnet
case found *zero* findings and so cannot show movement under any rule, making
its scoreable denominator 9, not 10. A per-run comparison between arms of
unequal finding density measures discovery volume as much as falsification.

Label-delta scoring undercounts runs where stage 3 altered a finding by 40%.
The four sub-moves, all citation-verified: `fd8ee329` f3 (stage 2's claim that
`assert()` text is lost overturned — `lib/assert/assert.h` shadows the system
header and expands to `_zlog_assert_failed()`); `9ba8fca7` f4 (stage 2's
secondary no-op argument shown wrong, rejection re-grounded on
unreachability); `fe99bcf9` f2 (impact tightened on semantic grounds);
`fe99bcf9` f4 (leak shown to predate the diff via `isis_te.c:499`).

The 7-of-9 row counts a further four cases where stage 3 added new verifiable
evidence without changing the claim; those were scored NO_MOVE conservatively.
**The headline swings from 3/9 to 7/9 on this choice alone**, so the judge
specification has to make it explicitly rather than inherit it from whichever
field is easiest to diff. Note also that the retracted cost correlation above
was an artifact of picking the coarsest of these three rules.

### Spend

True arm cost **$86.91** across all attempts — $67.89 in sealed stages, $13.72
on the failed case, the remainder retries. Sonnet's full ten cases cost $20.64.

### For `opus-wide.yaml`

Reasoner-2 needs the raise most: it is the stage that failed and it holds the
arm's three highest utilisations. Since minting that arm is a fingerprint
change anyway, granting the reviewers search tools in the same change costs
nothing extra and removes the confound measured above. Never patch either into
this arm.

## Opus vs sonnet: the comparison is underpowered

Both arms scored under the adopted rule, sonnet re-scored retroactively from
sealed results under identical criteria and the same conservative borderline
convention. `frr-c96a113c` produced zero findings and so has no pairs, making
sonnet's scoreable denominator 9 runs.

| rule | sonnet /run | sonnet /finding | opus /finding | Fisher 2-tailed |
|---|---|---|---|---|
| label only | 0 of 9 | 0/19 = 0% | 3/41 = 7.3% | p = 0.545 |
| label-or-sub — *adopted* | 1 of 9 | 1/19 = 5.3% | 7/41 = 17.1% | p = 0.416 |
| label-or-sub, per run | 1 of 9 | — | 5 of 9 | p = 0.131 |

**Opus is 3.2× sonnet per finding on the adopted rule, and that difference is
not distinguishable from chance at these sample sizes.** No test reaches
significance; the closest is the per-run comparison at p = 0.131, and per-run
is the basis the finding-density gap makes unsafe to compare on.

For scale on the power problem: against sonnet's 1/19, opus would have to
reach **12 of 41 (29%)** for p < 0.05. It reached 7 of 41. The observed effect
is well inside the range this design cannot resolve — which sits on top of the
run-to-run variance already documented above, where one case gave 1 finding
then 3 under identical conditions.

**So the project's central question is unresolved, not answered.** The
direction is consistent with the opus hypothesis across every measurement ever
taken, and the magnitude is unmeasured. Reporting "opus is 3.2× sonnet"
without this paragraph would repeat exactly the error retracted above: a
pattern that survives because nobody tested it.

### Rule 3 is rejected, and its failure is instructive

The third rule — crediting stage 3 for contributing new verifiable evidence
behind an unchanged claim — must not be codified. It is not merely loose; it
**inverts the result**. On opus only 4 of 26 no-moves are borderline under it,
but on sonnet 15–18 of 18 qualify, because nearly every sonnet stage-3
rationale cites a `file:line` stage 2 never touched and then confirms stage 2.

Scored that way the arms come out sonnet 16/19 (84%) against opus 11/41 (27%),
which is *significant in the opposite direction* (p < 0.001). A rule that
turns a 3.2× opus advantage into a significant sonnet advantage on a
definitional choice is measuring rationale verbosity, not falsification.

Define SUB_MOVE by **claim change**, as the adopted rule does. This is the
evidence for that choice.

## The sub-move metric is contingent on stage 2 erring

The adopted rule counts a sub-disposition move — stage 3 changing a finding's
claim without changing its label — and those four moves are the entire
difference between 3 of 9 runs and 5 of 9. Classifying them by what the
correction was *grounded in*:

| sub-move | what stage 3 did | grounded in |
|---|---|---|
| `fd8ee329` f3 | deleted stage 2's false claim that `assert()` text is lost, showing `lib/assert/assert.h` shadows the system header | **stage 2's error** |
| `9ba8fca7` f4 | "one correction to B's reasoning… its secondary 'no-op' argument is wrong" | **stage 2's error** |
| `fe99bcf9` f2 | tightened stage 2's impact framing as overstated | **stage 2's error** |
| `fe99bcf9` f4 | found `isis_link_params_update()` already calls the leaking helper at `isis_te.c:499`, so the leak predates the diff | **a fact about the code** |

**Three of the four are contingent on stage 2 having made a mistake.** That has
two consequences, and the second is worse than the first.

They cannot replicate unless stage 2 repeats the same error. Confirmed in the
wide arm: narrow `fd8ee329` f3 corrected a claim about `assert()` that wide's
stage 2 never made — the word does not appear in its assessment — so there was
nothing for wide's stage 3 to correct. That is not a failure to replicate; it
is a comparison that cannot be run.

And **a higher sub-move count can indicate a worse stage 2 rather than a better
stage 3.** The metric partly measures stage-2 error rate. Any cross-arm
comparison on sub-moves is therefore confounded by upstream quality, in the
same way per-run movement is confounded by finding density.

### Pre-registered prediction

**Recorded before `opuswide-fe99bcf9` was run** — it falls in pair 4, and pairs
1-3 were complete at the time of writing.

`fe99bcf9` f4 is the only sub-move in the arm grounded in a property of the
source rather than in stage 2's reasoning. The leak predates the diff whether
or not stage 2 says anything about it. So:

- **If wide rediscovers the ASLA-leak finding and independently notices the
  leak predates the diff** — that is the first replicated sub-move, and
  evidence that the category measures something real.
- **If wide rediscovers the finding and misses it** — the sub-move category is
  mostly noise, and the judge should weight label moves and new findings above
  it.

No outcome of this run changes the adopted rule retroactively; it decides how
sub-moves are *weighted* in the judge spec.

## Wide arm: replication against narrow

Scored per finding-pair, not per case — a case can replicate on one finding and
contradict on another, and a single per-case label hides that.

Through 4 of 10 cases: **REPLICATED 1 · NARROW-ONLY 2 · WIDE-ONLY 1 ·
NEITHER 1.**

**`c96a113c` replicated in the strong form.** Same finding, same stage-2
disposition (CONFIRMED), same stage-3 disposition (NARROWED), same mechanism —
stage 3 could not break the corruption claim itself, so it attacked
reachability via a sibling finding proving the socket never binds to port 4321.
Both arms independently discovered that sibling finding, so the cross-check was
genuinely available to both rather than lucky. This is the project's first
replication of anything.

**`83d945a6` is a direct contradiction, not a missing comparison.** Both arms
discovered the same defect and both stage 2s narrowed it; stage 3 then reached
`REJECTED` in narrow and `NARROWED` in wide — **from a matched starting point,
with better tooling in the arm that failed to move it.** This is a stronger
variance result than the non-overlapping finding sets elsewhere in the arm,
because the input was matched on the dimension that matters.

Two findings in that case moved in opposite directions: narrow-only on the
label move, wide-only on a new finding. Hence per-finding-pair scoring.

## Hypotheses this raises

Ranked by how cheaply each can be falsified.

1. **Agreement is anchoring, not correctness.** Reasoner-3 receives B's
   *assessments*, so it reviews a review rather than forming an independent
   opinion. 19/19 agreement is clean enough to be suspicious. Testable with
   the existing handoff controls: give C only A's findings and the same code.
   Disagreement would mean the design suppressed it; continued agreement would
   mean B was simply right.
2. **Model strength substitutes for staging.** Three Sonnet stages cost
   $20.64; one Opus pass over the same ten cases would cost roughly $10–13. On
   the `tlv-bounds` fixture Opus alone found 4 findings to Sonnet's 2. If one
   strong pass matches three weaker ones at half the price, staging loses on
   economics whether or not it "works". One config file and one run.
3. **The product is precision, not recall.** Stage 1 produces every finding;
   later stages only remove. If so the benchmark should score false positives
   suppressed rather than defects found — which changes what the judge must
   compute.
4. **Diminishing returns after one adversarial pass.** The plainest reading:
   two stages, not three. Requires no new experiment.

## Next

Blind analysis of the reasoning (`analysis-session-prompt.md`) settles
hypothesis 1 and costs nothing. Only 7 findings changed disposition across the
cohort, so stage 2 can be checked against ground truth by hand in about an
hour — which tells you whether mechanised scoring is worth building at all.
