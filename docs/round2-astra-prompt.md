# Round two: engage with the other reviewer's position

Three things first, then their position, then your task.

**Your clustering objection was correct and has been acted on.** You argued a
finding-level Fisher test treats observations as independent when findings share
PRs and there are only ten PR clusters, three of them negative. The exact
PR-level permutation test over all 120 labellings of 3-of-10 gives **p =
0.19-0.22 two-sided, 0.12 one-sided**. The p = 0.034 is retracted. Per-PR rates
show one negative PR at 0.73 carrying the effect and another at 0.08, below four
of the seven positives. The brief now states that the project has no result at
p < 0.05.

**Your arithmetic catch was correct.** The negative rows summed to 31 against a
stated 32, the positive rows to 78 against 79, and arm B's negative row to 14
against 15. Cause: 2 findings in the cohort carry no final disposition at all,
and the column was dropped while the totals were kept. Verified against the
sealed results and fixed. You found that from the brief text alone, with no
access to the data.

**The other reviewer independently made the same clustering objection**, and
pre-registered a prediction that the PR-level test would land between 0.05 and
0.20 — which it did. It did not raise your multiple-comparisons point.

---

## The other reviewer's position, verbatim and complete

<<<
**Position**

The three-stage pipeline is the wrong shape. Its two verification stages spend ~40% of the budget on a candidate set that is missing nearly half the truth, and they buy almost nothing back. Delete stage 3 outright. Demote stage 2 from a gate to an annotator (its REJECTED verdict should reorder, never remove). Reinvest in more, more diverse, single-stage discoverers, and union their output. The product is not a PR-level traffic light — the data says that signal does not exist — it is a short list of located, evidence-attached pointers a reviewer spends three minutes each on.

**Reasoning**

The load-bearing number is **8 of 18**: real, later-fixed defects that no arm described at any stage. Add the infinite loop found by nobody in the pipeline, and discovery is the binding constraint by a wide margin. Misses outnumber over-filtering 8:1. No amount of adjudication downstream touches that 8.

Second, the filtering stages do almost nothing on buggy PRs, and I can put a number on it. The 28.6% figure (22/77) is computed over all findings, rejected ones included. After stage 2's gate, 19 true findings survive among ~61 — about 31%. So gating on the positive class moves precision from 28.6% to ~31% and costs 3 true findings, one of which lost its only coverage (recall 11/18 → 10/18). Across both classes the gate moves precision from roughly 20% (22/111) to roughly 23% (19/82). That is the entire benefit of stage 2 as a filter, for ~$3/PR.

Third, stage 3 touched 4/77 and rescued 0/3. What actually rescued 2 of the 3 wrongly-killed findings was *another arm's discovery*. Cross-model redundancy at stage 1 did the job stage 3 was designed for. That is the same-weights explanation confirmed from the other side: verification within a model is inert; diversity across models is not.

Fourth, diversity is buying recall almost linearly: best single arm 6/18, three pooled 10/18, and only 4 of 14 arm-matches are duplicates. That last number also kills the "agreement between independent reviewers is the signal" framing: with so little overlap, requiring two arms to agree would cap recall at ~4/18. The union is the asset; the intersection is worthless.

**On the p = 0.034 result**

It is real evidence that stage 2 does something, and I want to be precise about what. Raw stage-1 output per PR is essentially identical on clean and buggy PRs: 32/9 = 3.56 versus 79/21 = 3.76. Stage 2 then suppresses 41% versus 20%, leaving 2.11 versus 3.0 survivors per PR. So stage 2 is the *only* component producing any clean/buggy discrimination at all — and the discrimination it produces is a ratio of ~1.4, and for arms B and C specifically 1.1 and 1.3 (3.0 vs 3.29, 3.0 vs 4.0). Stated as a filter: on the positive class stage 2 passes ~86% of true findings and ~72% of false ones; on the clean class it passes 59% of false ones. A likelihood ratio around 1.3 is a nudge, not a gate.

Two under-analysed points. (a) The test unit is wrong: 111 findings from 10 PRs, tested as if independent. With 3 clean PRs, one small clean PR whose findings were all trivially rejected could carry the result. An exact permutation over the 120 possible 3-of-10 labelings costs nothing to run. (b) Arm A's 5.1 ratio is 1 finding versus 12 — that is a filter that finds nothing looking good on a discrimination metric, exactly the path-correspondence trap. I am explicitly not building on it.

**The cheapest signal that changes behaviour**

Not a colour. A per-finding pointer: file:line, a one-sentence mechanism, the handful of repository lines forming the causal chain, and a concrete triggering condition ("called with NULL nexthop when the VRF is deleted mid-update"). Stage 2's chain-building is valuable *as this content*; its verdict is not. At ~3–4 pointers per PR and ~1 real one on 56% of buggy PRs, a reviewer trades ~10 minutes for a meaningful chance at a defect that otherwise ships and gets a `Fixes:` trailer two years later. A traffic light from arms B/C would be red on ~100% of PRs — that is not a signal anyone would act on.

**Strongest argument against me**

I am deleting the only component that discriminates. Stage 2 with the *same* model as stage 1 still produced the one significant result in the project; stage 2 with a different model, or run three times with a 2-of-3 non-rejection rule, might turn a 1.4 ratio into something usable, and I am proposing to throw it away before that was tried. Concretely checkable: run stage 2 alone over the existing stage-1 output for all 10 PRs with a different model than produced the findings (~$20 plus judge). If clean-PR suppression exceeds 60% while true-finding rejection stays at 1/22 or lower, stage 2 is a real gate and my position is wrong — the architecture was fine and the same-weights confound was the whole problem.

**Falsifiable prediction, with price**

Run four single-stage discoverers over the same 10 PRs — arm A's model read-only, arm C's model with grep/glob, arm C's model with a *task* variant ("enumerate every loop, pointer dereference and lock the diff adds; prove each terminates/is guarded"), and a fourth vendor's model. ~$2 × 4 × 10 = **$80**, plus ~$10 of judge. Union, dedupe by mechanism, score with the existing blind judge.

I predict pooled recall goes from 10/18 to at least 13/18, and union precision stays at or above 20%. If recall lands at 11/18 or below, the 8 misses are structurally unreachable by more discovery and the money belongs somewhere else entirely (test execution, not LLM reading); I am wrong about where the value is. If precision drops below 15%, union is the wrong combination rule and I am wrong about the product shape.

Free secondary prediction: the PR-level permutation test on the suppression result lands between 0.05 and 0.20. It survives as a lead, not a finding.

**Stop doing**

Stop adjudicating. Delete stage 3 — not swap its model, delete it; the redundancy that actually rescued findings lived in discovery. Stop letting stage 2's REJECTED remove anything from output. And stop pursuing the PR-level traffic light as the product; the 3.0-versus-3.29 number says the current pipeline cannot produce one, and building a metric that makes it look like it can is how you got four bad metrics.
>>>

---

## Your task

**Engage with its strongest point, not its weakest.**

Its strongest point against you is probably the **stage-2 ledger**: it computes
that stage 2's gate moves precision from 28.6% to ~31% on the positive class and
from ~20% to ~23% across both, at a cost of 3 true findings and ~$3/PR, and
concludes that a likelihood ratio around 1.3 is "a nudge, not a gate." You
argued the suppression pattern is the strongest evidence *for* retaining
substantiation. Its numbers say what that retention actually buys is
two-to-three precision points. Address that directly.

Its second strongest point is on **redundancy**: what rescued 2 of the 3
wrongly-killed findings was *another arm's discovery*, not any verification
stage. So the protection you want from stage 2 may be obtainable more cheaply by
running more discoverers.

Where you disagree, say what you would have to observe to change your mind.
Where it has changed your mind, say so plainly. A token concession is worse than
none — and so is defending a position you no longer hold.

Two specific questions, because you two diverge most there:

1. **What to measure next.** Its experiment costs ~$90 and measures pooled
   recall against known fixes. Yours costs ~$1,000 plus blinded human reviewers
   and independent adjudication, and measures whether a reviewer's action
   changed. Be aware of a practical constraint: **the asker may not have access
   to multiple blinded human reviewers at all.** If that is true, is your design
   adaptable, or does its cheap retrospective experiment become the only one
   that can actually be run? Say which you would do first given that constraint.

2. **Whether your position survives the retraction.** You cited the suppression
   pattern as the strongest evidence for retaining substantiation. It is now not
   significant at the correct unit of analysis — the unit you yourself
   identified. Does that weaken your own argument for keeping stage 2, and if
   so, does the honest answer become "neither of us has evidence either way"?

600-900 words. Do not restate your original position.
