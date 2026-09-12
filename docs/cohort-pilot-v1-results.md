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

**Reasoner-3 changed nothing. Nineteen findings judged, zero dispositions
moved, zero new findings, ten cases out of ten.**

Its output set is identical to reasoner-2's. That has a consequence which
needs no oracle: scoring B and C against ground truth would return **identical
numbers by construction**, because there is nothing to distinguish. On this
cohort, $5.51 — 27% of spend — altered no outcome.

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

0 of 14 on the weaker arms; 2 of 2 on opus. The plausible mechanism is that
adversarial verification needs enough capability to construct a genuine
falsification, and that a weaker model can only re-read stage 2's chain and
agree with it.

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

Opus runs much closer to its per-stage `max_cost_usd` than sonnet did. Through
eight cases the high-water marks are `opus-fe99bcf9` reasoner-2 at $4.61 of
$5.00 (92.1%), `opus-3a74a3a2` reasoner-1 at $3.51 of $4.00 (87.8%), and
`opus-3a74a3a2` reasoner-2 at $4.16 of $5.00 (83.2%). Because `max_cost_usd`
is the one bound the vendor enforces *before* the fact, a stage that reaches
it is cut off mid-work — and a truncated stage presents as weak reasoning,
not as a budget artifact.

The caps were nonetheless **held at their sonnet-arm values for the whole opus
arm**. `configs/agents/opus.yaml` is hashed into every run's fingerprint, so
raising caps partway would make the opus arm non-uniform against a sonnet arm
that ran uniformly throughout. That trades a *hypothetical* truncation
confound for a *certain* fingerprint confound, in a comparison whose entire
value is that only the arm changed. That argument is about experimental
validity alone, and it is the only argument holding the caps.

It is explicitly **not** supported by a claim that cost is unrelated to
falsification. An earlier draft of this section argued that it was, on the
strength of the single most expensive case having produced no movement. Eight
cases do not support that, and in fact point the other way. Ranked by total
case cost:

| case | total | moved |
|---|---|---|
| `opus-3a74a3a2` | $9.76 | — |
| `opus-83d945a6` | $9.34 | yes |
| `opus-fe99bcf9` | $9.19 | yes |
| `opus-c96a113c` | $7.95 | yes |
| `opus-deaebcc7` | $7.86 | — |
| `opus-fd8ee329` | $5.51 | — |
| `opus-9ba8fca7` | $4.40 | — |
| `opus-322abe6a` | $3.42 | — |

All three disposition moves sit in the four most expensive cases; none of the
four cheapest moved. With n=8 and three events this is a pattern, not a
result — but it is the opposite of the pattern the earlier draft asserted, and
it means spend is a live candidate explanation for falsification rather than a
ruled-out one. Anything read off this arm should be read with that open.

The mitigation instead is to record stage cost against cap for every stage,
and to treat any stage above ~95% of its cap as **SUSPECT** — excluded from
"stage 3 found nothing" conclusions — rather than read it as a weak result.
No stage has crossed that line through eight cases.

One case does show why the *converse* is worth watching. In `opus-fe99bcf9`
reasoner-2 ran at 92.1% of cap and left finding f3 `INCONCLUSIVE`, saying
explicitly that it lacked the enable/disable bodies; reasoner-3, with more
headroom (80.3%), read those bodies and resolved it to `REJECTED`. So a
disposition move can come from one stage having budget that an earlier stage
had exhausted, not only from stage 3 being more capable. Budget pressure
should be read per stage before any A→C increment is credited to capability.

**Proposed future work, and the cost ranking above raises its priority:** re-run
the cohort at higher caps as its own arm with its own fingerprint (a new
`configs/agents/opus-wide.yaml`), never as a patch to this one. That answers the
truncation question without retroactively splitting the arm now in flight. If
the moves really do track spend, a wide-cap arm is not a robustness check on
this result — it is the experiment that tells us whether stage 3's increment is
a capability at all or just a budget we had not yet granted.

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
