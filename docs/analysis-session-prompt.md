# Cohort analysis session

Read this as your instructions. Work in `/home/alex/repos/engine-runner`.
(If you are a human: paste this file, or point an agent at it.)

You are helping me read the results of a staged code-review benchmark. I am
the decision-maker; your job is to help me see what is actually in the data
and to disagree with me when I am reading it wrong.

## What this system does

Three isolated LLM reviewers look at one real code change from FRRouting:

- **reasoner-1 (A)** — discovery, high recall. Sees only the change and the
  source tree.
- **reasoner-2 (B)** — evidence. Gets A's findings and builds causal chains,
  confirming, narrowing or rejecting each.
- **reasoner-3 (C)** — adversarial verification. Gets A's and B's output and
  actively tries to falsify each finding.

Ten cases ran on Claude Sonnet under a frozen protocol (`pilot-v1`). Nine
completed; one failed. Total spend $18.75.

## The question I need answered

**Did stages 2 and 3 earn their cost?**

The single-pass baseline is already in every run: `review-a.json` is what one
reviewer produces in one pass. `review-c.json` is the same review after
evidence and adversarial verification. The increment is A → C. If the three
stages mostly agree with each other and add no new reasoning, the pipeline is
expensive theatre. If dispositions change for stated, checkable reasons, it is
doing real work.

## Start with this, because it is the striking result

Across all nine completed runs, **14 findings were judged by both B and C.
Stage 3 changed none of them, and produced zero new findings.** Not one
disposition moved.

Do not take that as a verdict — take it as the thing to investigate. The two
readings are very different:

- **Stage 3 is doing nothing.** Its rationales restate B's reasoning, no real
  falsification was attempted, and 40% of the cost buys agreement.
- **Stage 3 is doing its job and B was right.** Its rationales describe
  specific falsification attempts that were made and failed, which is exactly
  what "confirmed" is supposed to mean.

Read C's `rationale` and `falsification_attempt` fields and tell me which one
this is. Quote the text — do not summarise it away.

## How to read the files

`docs/reading-results.md` documents the result tree, the fields in each
stage's output, and working jq commands. Read it first.

**Everything you need is in two places**, and neither leaks anything:
`build/results/frr-*/` (the sealed runs) and, for each case, the
`reviewer/` directory of its bundle under
`/home/alex/repos/miner/output/frr-pilot-cohort/case-<id>/` — that is what the
reviewer itself was given, so reading it puts you in exactly the reviewer's
position and no further.

Runs are `build/results/frr-*/`. For any finding, trace it through:
`stages/reasoner-1/review-a.json` (the claim and its evidence), then
`stages/reasoner-2/review-b.json` (`causal_chain`, `rejection_reason`,
`narrowed_description`, `proposed_test`), then
`stages/reasoner-3/review-c.json` (`rationale`, `falsification_attempt`,
`new_findings`).

To go from a run to its bundle, read `case_id` out of the run itself — never
from the cohort document:

```bash
jq -r .case_id build/results/frr-83d945a6/run.json   # -> case-83d945a6dd4e5785
```

The code each finding cites is in the bundle at
`/home/alex/repos/miner/output/frr-pilot-cohort/case-<id>/reviewer/` — the
diff under review is `diff.patch`, the full source tree is `repository/`.
**Check the cited lines against the real code.** A causal chain that
misquotes the source is worth more to me than any summary.

## Hard rules

1. **Do not read any of these. They contain the answer key.**

   | Path | What it leaks |
   |---|---|
   | `*-evaluator-only/` under the cohort bundles | the corrective commits — what the real defect was |
   | `tmp/frr-pilot-v1-cohort.md` | a **Positives / Negatives** split listing which case IDs contain a real defect |
   | `/home/alex/data` | raw mined data and oracle bundles |
   | anything in `/home/alex/repos/miner` outside a case's `reviewer/` directory | selection rationale and retrospective signal counts |

   `docs/reading-results.md` mentions the cohort document as the place the
   PR↔case mapping lives. **Do not follow that pointer.** You do not need to
   know which PR a run is; you need to know whether its reasoning holds.

   If you read any of these, say so immediately and stop. An analysis produced
   after seeing ground truth is worthless to me, and I would rather lose the
   session than trust a contaminated read.

2. **Do not ask me which cases have real defects, and if I let it slip,
   ignore it.** Same reason. If I start describing what a case "really" was,
   tell me to stop.
3. **You cannot conclude whether any finding is correct.** No oracle scoring
   exists yet. You can judge whether reasoning is sound, whether evidence
   matches the code, and whether stages added anything. You cannot judge
   right or wrong. Say "I cannot tell" and mean it.
4. **Do not flatter the pipeline.** I built it. I need to know if it is not
   working, and a favourable reading I cannot trust is worse than a harsh one
   I can.

## Known limits, so you read the numbers correctly

- **Run-to-run variance is large.** One case was run twice under identical
  conditions and produced 1 finding, then 3. A single run is a sample, not a
  measurement. Do not build an argument on a one-or-two-finding difference.
- **n=9.** Nothing here supports a statistical claim.
- **One case failed** (`frr-de6d5d30`, the largest diff) and has no
  evaluation. Say so rather than quietly analysing nine as if it were ten.
- **Counts are not the substance.** `confirmed_by_c: 2` tells me almost
  nothing. What C actually tried, and whether the cited code says what the
  chain claims, tells me everything.

## How to work with me

Go case by case, not all at once. For each: what A claimed, what B did to it
and on what evidence, what C actually tried. Show me the text. Flag anything
that looks like reasoning about code that is not there, evidence pointing at
the wrong lines, or a stage restating rather than testing.

At the end I want your answer to one question: **on this evidence, is the A→C
increment real enough to be worth measuring precisely?** If yes, the next step
is building oracle-based scoring. If no, that is a more valuable finding and
saves the work.
