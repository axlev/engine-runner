# Mapping stage 1 to a disposition, so A→C can be computed

`docs/reading-results.md` says the increment worth measuring is **A→C**:
`review-a.json` is what one reviewer produces in one pass, `review-c.json`
is the same review after evidence and adversarial verification. Every number
this project has actually reported is **B→C**, because `evaluation/findings.json`
carries `b_disposition` and `c_disposition` and nothing else.

A→C cannot be computed without deciding what stage 1's *disposition* is, and
stage 1 does not assign one. Its findings carry `confidence` (0–1),
`severity` (low/medium/high/critical) and `uncertainty` (free text). So A→C
requires inventing a disposition-equivalent, which is a metric definition
rather than a schema chore — this document is the input to that decision, not
the decision.

It is written against the 109 stage-1 findings already on disk (19 sonnet,
41 opus-narrow, 49 opus-wide; 29 of 30 runs are replayable, `opus-c21d519d`
excepted because it died at reasoner-2). Every number below is measured, not
projected, and needs no new runs.

## Why this needs care rather than a quick choice

Two metric definitions have already failed here, and they failed the same way.

1. **Disposition-label deltas.** Adopted by default, and it manufactured a
   spend-predicts-falsification correlation that survived two increments
   before a ninth case killed it.
2. **Sub-disposition movement.** Adopted to fix the first, and it turned out
   to measure stage 2's error rate rather than stage 3's capability — three
   of four opus sub-moves were contingent on stage 2 having erred, and the
   fourth was stage 3 restoring a qualification stage 1 had already made and
   stage 2 had dropped.

The common shape: **both counted stage 3 changing something, and neither
asked whether the review got better.** Any mapping that inherits that shape
fails the same way regardless of whether its arithmetic is right. That is the
test to apply below, ahead of any question about thresholds.

## What stage 1's own signal actually looks like

This constrains the options more than expected:

| arm | min | p25 | median | p75 | max |
|---|---|---|---|---|---|
| sonnet | 0.25 | 0.30 | 0.45 | 0.60 | 0.80 |
| opus-narrow | 0.15 | 0.35 | 0.45 | 0.65 | 0.85 |
| opus-wide | 0.12 | 0.35 | 0.55 | 0.60 | 0.85 |

**No stage-1 finding in 109 has confidence above 0.85, and the medians sit
near 0.5.** There is no natural high-confidence band to map onto CONFIRMED.
Any threshold is therefore invented rather than discovered, which matters for
the candidates that need one.

## Candidate M1 — confidence threshold

A finding with `confidence >= T` is A-CONFIRMED, below it A-INCONCLUSIVE.
A→C moves when C disagrees with that.

*For:* `confidence` is the nearest thing stage 1 has to a belief about
whether the defect is real.

*Against:* the threshold does all the work. On the same 109 findings:

| T | sonnet | opus-narrow | opus-wide |
|---|---|---|---|
| 0.3 | 47% | 61% | 73% |
| 0.5 | 58% | 68% | 78% |
| 0.7 | 79% | 78% | 92% |
| 0.8 | 89% | 88% | 96% |

The sonnet headline moves from 47% to 89% on identical data, by choosing a
number. A metric with a free parameter that swings the result by 42 points is
one that can be tuned to whatever conclusion is wanted, which is how the
first failure happened.

## Candidate M2 — every reported finding is an assertion

Stage 1 reporting a finding *is* its claim that the defect is real, so every
A finding is A-CONFIRMED. `confidence` is a hedge it attaches, not a
disposition it assigns.

*For:* no free parameter, so it cannot be tuned. It also matches the stated
semantics of the baseline: a single-pass reviewer's output is its finding
list, and that is what the A→C comparison is against.

*Against:* discards stage 1's uncertainty signal entirely.

Measured:

| arm | A→C | B→C |
|---|---|---|
| sonnet | 8/19 = 42.1% | 0.0% |
| opus-narrow | 27/41 = 65.9% | 7.3% |
| opus-wide | 36/49 = 73.5% | 10.2% |

## Candidate M3 — confidence bands onto the four-way vocabulary

E.g. ≥0.8 CONFIRMED, 0.5–0.8 NARROWED, <0.5 INCONCLUSIVE.

**Reject this one.** NARROWED does not mean "medium confidence" — it means
"real but smaller in scope than claimed". Mapping a confidence band onto it
conflates *how sure* stage 1 was with *how big* the defect is, which are
independent. It would produce a number that looks richer than M2 while
measuring something incoherent, and the confidence distribution above means
the CONFIRMED band would be nearly empty anyway.

## Candidate M4 — no mapping; measure the set difference

Don't invent an A disposition. Report A→C as a decomposition: how many A
findings C rejected, how many it narrowed, how many it left inconclusive, and
how many C added that A never had.

*For:* invents nothing, and it is the honest shape of the question — "what
did the later stages do to the single-pass output".

*Against:* it is not a single number, so it cannot be put in a table cell or
compared across arms with a significance test. It also needs the scope-change
judgment that made sub-moves expensive.

## Which of these reproduce B→C

None reproduce its *magnitude* — A→C is 42–74% against B→C's 0–10%, because
stage 2 does most of the narrowing and B→C is blind to all of it by
construction.

But **every candidate, at every threshold, ranks the arms identically:
sonnet < opus-narrow < opus-wide.** So the arm ordering is robust to the
mapping choice. That cuts both ways: it means the mapping is not where the
risk lies, and it means no mapping will rescue the ordering from the
objection that already applies to it — the opus-vs-sonnet gap sits at Fisher
exact p = 0.416 and more decimal places will not change that.

It also exposes a conflation worth naming. A→C at 42–74% is mostly measuring
*stage 2 narrowing things*, not stage 3 doing anything. A→C and B→C answer
different questions and neither answers both:

- **"Does staging beat a single pass?"** → A→C. Needs a mapping.
- **"Does reasoner-3 earn its 27% of spend?"** → B→C. Already computable, and
  confounded by stage 2's quality, as the opus-wide arm demonstrated by
  scoring 0 sub-moves *because* its stage 2 was better.

## Recommendation

**Adopt M2, report it decomposed as in M4, and treat neither as the priority.**

M2 because it is the only candidate with no free parameter, and because the
confidence distribution makes every threshold arbitrary. Decomposed because a
single "73.5% moved" headline would be *worse* than B→C rather than better:
it hides that most of the movement is stage 2's narrowing, and it would
invite exactly the reading that produced the first two failures.

But the recommendation that matters is that **the mapping is not the
bottleneck, and choosing it will not produce a trustworthy number.** M2
still counts change, not improvement. It inherits the shape both prior
metrics died of. Shipping it as the project's headline would be the third
failure, with better arithmetic.

**What changes that is ground truth, and it already exists.** Every one of the
ten cases has a `correlated-report.json` under
`<case>-evaluator-only/`. Nothing in this engine reads it (backlog Phase 2).
With it, "C rejected this finding" becomes "C correctly rejected a false
positive" or "C wrongly rejected a real defect" — which is the difference
between measuring change and measuring improvement, and the only thing that
makes any A→C or B→C number worth publishing.

Two constraints on doing that, both real:

- The miner's export contract marks that directory **"evaluator-only; must
  never be exposed to an engine"** (`docs/export-contract.md`). So the judge
  has to be a separate evaluator process outside the review path, not a stage
  or a library the engine links. That is an architectural requirement, not a
  preference.
- Whoever analyses reviews by hand must not read it either. The oracle is
  reachable in two commands from this repo; the discipline that keeps blind
  analysis blind is a convention, not a mechanism, and it has already been
  relied on for every disposition judgment recorded about this cohort.

So the order I would recommend: wire the oracle first, then choose the
mapping with the oracle available to validate it. A mapping can be judged
against ground truth. Judged against nothing, it is a preference.
