# Judge results, pilot-v1

Mechanism agreement between reviewer findings and the fixes maintainers
actually landed. This is the first metric in the project that measures whether
a finding was *right* rather than whether a later stage *changed* it.

Total spend: **$6.20** (7 cases two-call, 7 cases single-call control, 2-case
pilot). Companion documents: [`oracle-bundle-contract.md`](oracle-bundle-contract.md)
for what the evaluator receives and why, and
[`cohort-pilot-v1-results.md`](cohort-pilot-v1-results.md) for the arms
themselves.

**Everything here was judged by an in-family model.** Two of the three arms are
opus and so is the judge. Opaque-ID pooling — hash-derived finding ids,
deterministic ordering, arm identity held outside the judge call — is a
mitigation, not an elimination. Cross-vendor check is backlog 2.5, blocked.

---

## Bottom line

**Three things are now settled.**

1. **The pipeline finds real defects.** 22 of 77 scoreable findings anticipated
   an actual maintainer fix. Pooled across arms, 10 of 18 real fixes were
   caught. Before the judge this was entirely unknown: every prior metric
   measured change, never correctness.

2. **Stage 3 barely acts.** It changed the disposition of **4 findings out of
   77** — not four that were wrong, four it touched at all. Every claim about
   stage 3 in this document, positive or negative, rests on those four events.

3. **Stage 3 failed its only provable test.** Stage 2 wrongly rejected three
   findings that later real fixes vindicated. Stage 3 — whose job is
   adversarially checking stage 2 — rescued **none of the three**.

**One thing is not settled, and the reason is (2).** Whether stage 3 has value
is leaning negative but not closed. A stage that touches 5% of findings cannot
be evaluated, and the measurement that would close it — whether its rejections
correctly killed false positives — is precisely what this oracle cannot see
(see *Harm and benefit* below).

**The model-capability hypothesis is effectively dead.** This is the fourth
metric to find arm differences indistinguishable from chance, and under the
control the arm *ordering* reverses outright. The project's founding premise —
that adversarial verification needs opus-grade capability and sonnet can only
agree — has not survived: sonnet's stage 3 constructs genuine multi-branch
falsifications, it simply does not overturn.

---

## The pre-registered prediction, confirmed

Recorded before the judge ran: mechanism agreement should come out well below
strong-tier path correspondence, because correspondence needs only the right
file while this needs the right mechanism. The stated falsifier was that a
result *close* to correspondence would indicate the judge was rationalising
matches rather than that reviewers were that accurate.

```
strong-tier path correspondence   55/70 = 78.6%
mechanism agreement               22/77 = 28.6%    Fisher p = 1.3e-09
```

A 50-point drop. Roughly two thirds of what correspondence counted as a hit
does not survive the stricter test. The falsifier did not fire.

## The judge is not rationalising

The failure mode to watch for was a high match count with near-zero
`LOCALITY_ONLY` — a judge awarding matches where it should be distinguishing
right-function-wrong-mechanism.

```
MECHANISM       22
LOCALITY_ONLY   39      locality ratio 63.9% (two-call), 64.9% (single-call)
NONE            16
TOO_VAGUE        0
```

The judge rejected **nearly twice as many** near-misses as it awarded matches.
Rationale fields average 371 chars for `case_against` and 630 for `rationale`
against a 4,000 cap, so nothing is padded. A sample of six rationales showed
objections arguing the hard direction and sometimes nearly winning — on one
`LOCALITY_ONLY` the judge wrote that the finding "comes closest to defect 4 …
A maintainer reading this sentence is looking directly at the code defect 4
blames," then rejected it on a stated distinction. That is the opposite of
rationalising.

*Caveat:* six of 77, sampled randomly within buckets rather than adversarially.
A targeted hunt for weak objections would be a stronger test and was not done.

---

## The proven errors

Joining stage dispositions against judge verdicts, pooled, 77 findings:

```
stage 2 (b)      MECHANISM  LOCALITY_ONLY   NONE   TOO_VAGUE   total
CONFIRMED              10         15           6        0        31
NARROWED                9         16           7        0        32
REJECTED                3          5           2        0        10
INCONCLUSIVE            0          3           1        0         4

stage 3 (c)      MECHANISM  LOCALITY_ONLY   NONE   TOO_VAGUE   total
CONFIRMED              10         14           6        0        30
NARROWED                9         16           7        0        32
REJECTED                3          8           2        0        13
INCONCLUSIVE            0          1           1        0         2
```

**`REJECTED` × `MECHANISM` = 3, at both stages.** Three findings the pipeline
killed that a later real fix vindicated. These are the first *proven* errors
this project has produced — everything prior was a rate whose correctness was
unknowable. One per arm (opus-narrow 1, opus-wide 1, sonnet 1), so no arm is
implicated over the others.

**And the same three at both stages means stage 3 kept all three.** Stage 3's
three incremental rejections were all judged `LOCALITY_ONLY`, so by arithmetic
the three `MECHANISM` rejections in its row are the same three stage 2 made.
Stage 3 had three identifiable opportunities to catch a stage-2 error, with
ground truth available, and took none.

**Stage 3 damage — findings stage 2 kept, stage 3 killed, a fix vindicated:
zero.** The cell is empty. But see the opportunity-set limitation: it is empty
out of four total actions, so it means stage 3 *barely acts*, not that stage 3
is safe.

**Real defects the pipeline both found and kept:** 10 findings reached
`MECHANISM` and survived to `c_disposition = CONFIRMED`.

### The only positive signal for stage 3, and it is not evidence of benefit

All three of stage 3's independent rejections were judged `LOCALITY_ONLY` —
right area, wrong mechanism, unsupported by any fix. Everything stage 3 chose
to kill was something the oracle does not vindicate.

This is consistent with correct suppression and **cannot establish it**, for
the asymmetry below. It is also three out of three.

---

## Harm and benefit are not symmetric

**This benchmark can demonstrate harm and never benefit.**

A `NONE` verdict is three things the judge cannot distinguish: a genuine false
positive, a real defect nobody ever fixed, or a real defect whose fix the
oracle never captured. So:

| | provable? |
|---|---|
| a rejection that killed a real defect | **yes** — the fix exists |
| a rejection that correctly killed a non-defect | no — same reason false positives are invisible |

Which means the anticipation rate is a **lower bound on precision by an
unmeasurable margin**. The rendered output carries this in the string itself —
`28.6% anticipated (22/77) [true precision >= this]` — so the caveat travels
wherever the number is pasted.

No claim that staging *works* can come from this data. Only claims that it
damaged something, or did not.

---

## Arm results — reportable only with the design named

```
              two-call        single-call     recall (of 18 fixes, two-call)
sonnet        25.0% (4/16)    29.4% (5/17)    16.7% (3/18)
opus-narrow   33.3% (9/27)    25.9% (7/27)    33.3% (6/18)
opus-wide     26.5% (9/34)    24.2% (8/33)    27.8% (5/18)
overall       28.6% (22/77)   —               55.6% (10/18)
```

**Sonnet is last under one design and first under the other.** No arm
difference is significant under either (p = 0.735 for the largest gap, 1.000
for opus pooled vs sonnet, 0.443 on recall). Per-arm anticipation rates should
be treated as uninformative; if quoted at all, the design must be named and the
ordering flagged as unstable.

Note that this metric does **not** reward conservatism, unlike path
correspondence, which ranked sonnet highest precisely because it reported
fewest findings. Here sonnet reports fewest and scores worst on both axes under
the primary design. Reporting anticipation and recall as a pair is what
prevents the conservatism reward, and it worked.

## The arms find largely different defects

```
              per-arm fixes    sum   union   overlaps   best single arm
two-call         6 / 5 / 3       14     10        4            6
single-call      6 / 5 / 3       14      9        5            6
```

Per-arm counts are **identical across designs**. If the arms found the same
defects the union would be 6; if fully disjoint, 14. At 9-10 it is much closer
to disjoint.

**Pooling three arms lifts recall from 33% to 50-56% — roughly 1.6x the best
single arm.** This survived a design change that moved 14 other verdicts, and
attribution churn was 1 of 17 commonly-matched findings. It is the most robust
positive finding here, and it suggests ensembling beats improving one arm.

---

## What the control established

The single-call variant was run on all 7 cases ($2.41) to test whether the
two-call split — describing fixes with findings absent from context, then
judging against those frozen descriptions — was load-bearing.

**Stable:**
- **The anticipation rate.** 28.6% vs ~26%, roughly ±2 points.
- **The locality ratio.** 63.9% vs 64.9%, one point apart.
- **Per-arm fix counts**, identical.
- **The diversity result**, union 9-10 against a best single arm of 6.

**Not stable:**
- **The set of anticipating findings.** 22 matches vs 20, intersection 17,
  symmetric difference 8, Jaccard 0.68. About a third of the disagreement
  region flips with the design. **The rate is reproducible; the specific
  findings behind it are not.**
- **Arm ordering**, which reverses (above).

**Overall verdict churn: 14 of 78, 18%.** Transitions: MECHANISM→LOCALITY_ONLY
5, LOCALITY_ONLY→NONE 5, LOCALITY_ONLY→MECHANISM 2, plus 2 dropped.

So the two-call split is **not** load-bearing on the headline and should not be
defended as though it were. It is retained because it costs ~$1 and makes the
ordering defence structural rather than requested.

### A methodological note worth more than the result

The 2-case pilot produced three directional claims, and **the full control
reversed all three**:

| claimed at n=2 | actual at n=77 |
|---|---|
| the anticipating-finding set is stable | Jaccard 0.68 — it is not |
| the locality ratio is design-dependent (68.8% → 54.5%) | 63.9% vs 64.9% — it is not |
| `LOCALITY_ONLY`→`NONE` collapse is a general effect | all 5 instances in one case, zero in the other six |

Each had an explanation attached. Each explanation was fitted to two cases.

This is the fourth time on this project that a small-sample pattern survived
long enough to be written down — after the spend/falsification correlation, the
sub-move category, and path correspondence. The difference is that this one was
caught within hours, because a control was run. **The control cost $2.41.**

---

## Limitations

- **The anticipation rate is a lower bound on precision** by an unmeasurable
  margin. Carried in the output string, not only in prose.
- **The benchmark demonstrates harm, never benefit.** Wrong rejections are
  provable; correct ones are not.
- **Only 4 of 77 findings changed disposition between stages 2 and 3.** Every
  stage-3 claim rests on those four events. The empty damage cell reflects a
  tiny opportunity set.
- **The anticipating-finding set is design-dependent** (Jaccard 0.68) even
  though the rate is not.
- **Per-arm ordering is design-dependent and underpowered.** Treat as
  uninformative.
- **Zero `TOO_VAGUE` in 154 verdicts** across both designs. The
  vagueness-gaming defence is built and unexercised; the zero is a property of
  the findings, not of the prompt.
- **~1 in 77 findings receives no verdict at all** — one per design, in
  different cases. Each shortens a denominator in the flattering direction.
- **3 of 10 cases are excluded entirely** — `3a74a3a25bda9041` (9 findings),
  `9ba8fca704a95e70` (11), `c96a113c3301c4fa` (11). No strong-tier corrective
  signal, so nothing to judge against. **31 findings sit outside the scoreable
  base of 77.** Exclusion is not a verdict: no evidence is not no defect.
- **In-family judge.** Cross-vendor check pending (backlog 2.5).

---

## What would close the stage-3 question

Every stage-2 assessment in the cohort — **90 of 90** — already carries a
`proposed_test`: the smallest check that would confirm or refute the finding.
None has ever been run.

These measure whether a finding is *true*, not whether anyone later fixed it.
So unlike the oracle they can prove **correct** suppression, which is the half
this benchmark structurally cannot reach.

```
oracle:          18 fixes,  7 cases   agreement with maintainers
proposed tests:  90 checks, 10 cases  truth about the finding
```

Five times the base, all ten cases including the three the oracle cannot score.

### Running existing tests is worthless here. New instrumentation is not.

FRR's topotests run in CI on every pull request. Every case in this cohort is a
**merged** PR, so those tests passed on this exact code. Any proposed check of
the form "run the existing topotests" therefore tells us nothing: it already
ran and already passed.

It is uninformative in both directions. A passing test does not refute a
finding — it may not exercise that path. And it cannot confirm one, because it
already did not fail. Worse, the 18 fix-vindicated defects are **by
construction the defects CI missed**: that is what it means for a later fix to
have been required. Existing test coverage is known-insufficient for exactly
the population this benchmark scores.

The distinction that matters is **new oracle or new coverage**, not whether the
topotest harness is involved:

| check | informative? | why |
|---|---|---|
| run existing topotests unmodified | **no** | already ran in CI, already passed |
| insert an assertion, then run existing topotests | **yes** | new oracle over an existing workload — e.g. `assert(ospf->redistribute > 0)` before the new decrement, then the ospfd NSSA topotests. The suite passed before because nothing checked that invariant |
| standalone snippet, no FRR build | **yes** | e.g. four lines of C against `basename`/`strdupa`, attached to a REJECTED finding, settleable in seconds |
| authored repro | **yes**, expensive | ASan build plus specific runtime state |

### The target population is the rejections, not all 90

For the three fix-vindicated wrong rejections the answer is already known — the
fix proves it. The new information is in the rejections where **no fix exists**,
because those are where correct suppression is currently unprovable:

```
stage 2 REJECTED            10 findings
  of which fix-vindicated    3   already known wrong
  of which unresolved        7   <- running these falsifiers settles
                                    whether the rejection was right
stage 3 REJECTED            13 findings (10 inherited + 3 incremental)
```

So the scoped version of this work is **roughly 7-13 falsifiers**, not 90, and
it targets the one question this benchmark structurally cannot answer. That is
a far smaller job than executing the whole set, and it is the only route to
measuring benefit rather than harm.

*Caveat:* each test was written by the same model that reached the disposition,
so it may be shaped to confirm what that model already concluded. But a test is
checkable in a way a rationale is not — it runs or it does not, and its result
is a fact rather than an argument. Test quality needs auditing, which is a far
better position than test quality being unknowable.
