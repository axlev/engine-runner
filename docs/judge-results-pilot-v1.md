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

## Harm and benefit are not symmetric — for the oracle metric only

> **Corrected twice. Read the correction chain below before quoting anything
> in this section.**
>
> **Correction 1 — the exclusion was wrong.** This section originally claimed
> the benchmark "can demonstrate harm and never benefit", and the limitations
> section called the exclusion principled: *"Exclusion is not a verdict: no
> evidence is not no defect."* That reasoning is right for an accidental gap
> and wrong here. The pilot cohort is **7 positives and 3 designed
> negatives** — PRs chosen because no corrective commit followed them in a
> ~2-year window (backlog 1.2: *"the cohort is now fully covered — 7
> positives and 3 negatives"*). Excluding zero-signal cases discarded the
> entire control class. Correct suppression **is** measurable, on the
> negatives, without any oracle. "Never benefit" was an artifact of the
> exclusion rule, not a property of the data.
>
> **Correction 2 — the result that un-excluding produced was also wrong, and
> it was announced as the project's first significant finding.** Including
> the negatives gave 39% suppression on clean PRs against 19% on buggy ones,
> Fisher p = 0.048 (reported at the time as 0.034). **That is retracted.**
> The test treated 109 findings as independent samples, but findings cluster
> within PRs and the independent unit is the PR. A cross-vendor reviewer
> pre-registered that an exact PR-level permutation would land between 0.05
> and 0.20. It does:
>
> | test | unit | p |
> |---|---|---|
> | Fisher | finding | 0.048 — **inflated by clustering, retracted** |
> | exact permutation, one-sided | PR | 0.125 |
> | exact permutation, two-sided | PR | 0.217 |
>
> The per-PR data shows the mechanism directly. The negative class does *not*
> show consistently elevated suppression — one clean PR carries the entire
> effect, and another is suppressed *less* than four of the seven positives:
>
> | PR | findings | suppressed | rate | class |
> |---|---|---|---|---|
> | 9ba8fca7 | 11 | 8 | 0.73 | **negative** — carries the effect |
> | 3a74a3a2 | 9 | 4 | 0.44 | negative |
> | c96a113c | 12 | 1 | 0.08 | negative — *lower than 4 of 7 positives* |
> | c21d519d | 7 | 3 | 0.43 | positive |
> | 83d945a6 | 12 | 4 | 0.33 | positive |
> | de6d5d30 | 23 | 5 | 0.22 | positive |
> | fd8ee329 | 11 | 2 | 0.18 | positive |
> | deaebcc7 | 8 | 1 | 0.12 | positive |
> | fe99bcf9 | 8 | 1 | 0.12 | positive |
> | 322abe6a | 10 | 0 | 0.00 | positive |
>
> **What survives:** specificity is the right metric and is now measured. What
> does *not* survive is the claim that stage 2 discriminates between clean and
> buggy PRs — that is unsupported, not established. The project still has no
> result at p < 0.05.
>
> **What changed in the code:** `SpecificityReport.PValues()` returns the
> PR-level permutation p and the finding-level Fisher p *together*, and there
> is no accessor for the Fisher figure alone — the same inseparability
> discipline as the anticipation rate and its vagueness count. Any future
> clean-vs-buggy or arm-vs-arm comparison over findings has this clustering
> problem, and the type now makes ignoring it require deleting a test.

**For the oracle-based metric, harm and benefit remain asymmetric.**

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
- **3 of 10 cases carry no fix to judge against** — `3a74a3a25bda9041` (9
  findings), `9ba8fca704a95e70` (11), `c96a113c3301c4fa` (11). **31 findings
  sit outside the anticipation base of 77.** These are the cohort's *designed
  negatives*, not missing data, and they are excluded from anticipation and
  recall — which need a fix to compare against — while being included in
  specificity, which does not. The two denominators differ by design and the
  scorer prints both.
- **The negative class has no judge data at all.** The judge was never shown
  those cases, so specificity rests on stage dispositions alone. Specificity
  and mechanism agreement do not cover the same population, and neither can
  be read as a check on the other.
- **Per-arm survival ratios are not significant.** sonnet shows 0.33
  surviving findings per clean PR against 1.71 per buggy PR — a 5.1x ratio on
  counts of 1 and 12, PR-level p = 1.000. It is exactly the shape this
  project has twice written down too early; the scorer flags any cell under 5
  and prints the PR-level p beside every ratio.
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

---

# The falsifier run: did the pipeline throw away real bugs?

*Written to be readable without the rest of this document.*

## The goal

Stage 2's job includes **rejecting** findings — saying "stage 1 claimed this is
a bug, and it isn't." On this cohort it rejected 19 findings outright.

That rejecting is supposed to be valuable. A review tool that reports 50
possible bugs, 45 of which are imaginary, is worse than useless — somebody has
to read all 50. So a stage that correctly discards the imaginary ones is doing
real work.

But nobody had ever checked whether those rejections were **right**. If stage 2
is throwing away genuine bugs, it is actively harmful. If it is throwing away
noise, it is the most valuable part of the pipeline. Same behaviour, opposite
conclusions, and we could not tell them apart.

**The goal was to find out: of the 19 findings stage 2 rejected, how many were
real bugs it should have kept?**

## Why the oracle could not answer it

The "oracle" is our retrospective evidence: for each pull request, which later
commits fixed a bug it introduced. If a reviewer's finding matches one of those
later fixes, the finding was real — a maintainer confirmed it by fixing it.

That gives us an answer in **one direction only**:

- Finding matches a later fix → **it was a real bug.** So rejecting it was
  wrong. Proven.
- Finding matches nothing → **we learn nothing.** It could be imaginary. Or it
  could be a real bug nobody has fixed yet. Or a real bug whose fix our
  evidence failed to capture. Three very different situations that look
  identical from here.

So the oracle can prove a rejection **wrong** and can never prove one
**right**. Three of the 19 rejections are provably wrong by this route. For the
other 16 the oracle is silent, and "silent" is not "correct".

That is a hard ceiling, not a gap to be closed with more data. It is why this
document says elsewhere that the benchmark demonstrates harm and never benefit.

## What a falsifier is, and why it escapes that ceiling

When stage 2 assesses a finding it must also write a **`proposed_test`**: the
smallest check that would settle the question. Not a test suite — one check,
specific enough that if it passes the finding is refuted and if it fails the
finding is real. Every single assessment in the cohort has one, 90 of 90, and
none had ever been run.

Example, from a rejected finding:

> `grep -rn 'isis_adj_state_change(' isisd/` and check each hit that can pass
> `ISIS_ADJ_DOWN`: the finding is refuted if every such caller either returns
> immediately, re-checks the pointer for NULL, or holds no copy.

The key property: **a falsifier asks a question about the code, not about what
maintainers did later.** It does not care whether anyone ever noticed the bug.
So unlike the oracle it can come back with "this finding is genuinely wrong" —
which is exactly the answer the oracle can never give.

That is the whole reason for doing this: it is the only available route to
measuring whether the pipeline's rejections are *correct*, rather than only
whether they are *catastrophic*.

## How the check was designed, and why

Three decisions, each with a reason.

### 1. Run all 19, not just the 16 unknowns

The obvious plan is to run only the 16 rejections the oracle cannot settle,
since the other 3 are already known wrong. That plan has a fatal flaw.

Suppose all 16 come back "not a real bug." Have we learned that stage 2's
rejections were good? **No.** We might instead have learned that our method
says "not a real bug" to everything. With no case where we know the right
answer, the two possibilities are indistinguishable — and one of them means
every number is worthless.

So the 3 provably-wrong rejections were included as a **positive control**: a
question where the answer is already known, mixed in with the unknowns to test
the instrument rather than the subject.

- Method says those 3 are real bugs → **the instrument works.** Trust the
  other 16.
- Method says those 3 are not real bugs → **the instrument is broken.** The
  other 16 mean nothing, and we have learned that instead, which is still worth
  knowing.

### 2. The runner must not know which 3 are the controls

A control only works if it is hidden. Someone who knows "these three are the
ones that matter" can pass by guessing "real" on those and "not real"
elsewhere, without doing any of the work.

So the findings were exported with opaque ids (`R1247d66da2` and so on), sorted
by hash so position encodes nothing, with no oracle data attached. The
id → provenance mapping was held in a separate file the runner never reads, and
the analyst who built the export deliberately never computed the join, so even
*it* does not know which three are the controls.

The run was then done by a separate agent with no knowledge of this project —
not of the oracle, not of the judge, not of the fact that controls exist at all.

### 3. Distrust both sides

The runner was told explicitly that the finding may be wrong *and* the
rejection may be wrong, that both were written by language models, that neither
had been checked, and that its conclusion had to rest on code it had actually
read or a program it had actually run. `UNRESOLVED` was defined as a legitimate
answer, because a confident wrong answer is far more damaging here than an
honest refusal.

## What actually happened

**The toolchain blocked most of it.** Each of the 19 checks needs different
tooling, so they were classified by attempting them, not by reading them:

| tier | count | what it needs |
|---|---|---|
| static | 2 | reading and grepping only |
| standalone | 3 | `gcc` alone, no FRR build |
| needs-build | 8 | FRR compiled |
| needs-topotests | 6 | the test harness, plus root and network namespaces |
| bare suite re-run | 0 | — |

This machine has `gcc` and `make` but not `autoconf`, `automake`, `libtool`,
`bison`, `flex`, `libyang` or `pytest`. **So 5 of 19 were runnable; 13 become
runnable once FRR builds; 6 stay behind the harness.**

All 5 runnable checks were executed. **All 5 came back `NOT_REAL`** — the
rejection was correct in each case.

**And then the join, which is the part that matters.** The three positive
controls are:

```
R55fdfbdbc9   sonnet       fd8ee329   f1   -> needs-build
Rbae9075a65   opus-wide    c21d519d   f2   -> needs-topotests
Ree1ed83e7b   opus-narrow  83d945a6   f4   -> needs-topotests
```

**None of the three was among the 5 that could run.** Every control sits behind
tooling this machine does not have.

## Bottom line

**We have 5 answers and no reason to believe them yet.**

The design was sound and it did not execute. Without a single control, "all 5
rejections were correct" is indistinguishable from "our method always says
correct." That is precisely the failure the control existed to catch, so we
cannot quietly drop it and report the 5.

A second problem compounds it: **3 of the 5 that ran are in cases the oracle
excludes entirely** (`3a74a3a2`, `9ba8fca7` — no corrective signal, so nothing
to compare against ever). Only two sit in scoreable cases, and for both, judge
and falsifier agree there is nothing there. Two methods agreeing is reassuring
but not validating — they can be wrong in the same direction.

**What this changes practically:** installing FRR's build dependencies is not a
convenience, it is required. One control needs a build; two need the topotest
harness plus root. **Until at least one control runs, the falsifier approach is
unvalidated — and it is the only route this project has to measuring whether
stage 2's rejections are correct.**

Nothing here refutes the approach. It says the approach has not been tested
yet, and names exactly what testing it costs.

## Side findings worth keeping

**Two rejections were right for the wrong reasons.** The runner checked each
rejection's cited line numbers instead of accepting them, and found:

- `Rd7016fbf21`'s rejection opens by asserting the safety contract "was already
  in force." It was not: in the pre-image, `del = true` sat inside
  `else if (old_state == ISIS_ADJ_UP)`, so a non-UP→DOWN transition did not
  free. The contract genuinely changed. The verdict survives on the *other*
  legs of the argument — every caller turns out to be safe — but one of its
  stated reasons is false.
- `R1247d66da2`'s evidence chain cites a drain loop without noticing that loop's
  own behaviour (below).

This matters beyond these two cases: a rejection can be correct and still
contain false reasoning, and only reading the cited lines reveals it. Scoring
dispositions alone would have marked both as clean.

**A bug nobody in the pipeline found.** While adjudicating something else, the
runner flagged that in `bgp_evpn.c:6330-6337` — code *added* by the change under
review — the drain loop

```c
while (zebra_announce_count(...)) { pop; if (match) … else add_tail; }
```

never terminates if any FIFO entry has `za_vpn != vpn`. An infinite loop in new
code, missed by three reviewer arms and by the judge, found incidentally by a
reviewer that was not being measured. **Needs independent confirmation before
anyone acts on it** — but if it holds, it is the most valuable single output of
this exercise, and it arrived from outside the experiment rather than from
within it.

**One honest gap in the runs.** `Reec3d5a272` was settled by execution for gcc:
the 3-line enum test compiles silently under `gcc 13.3` and errors only under
`-fshort-enums`, which a grep shows is never set anywhere in the tree. But FRR
also builds under clang, clang is not installed here, and so that leg rests on
the C standard's compatibility rule rather than on a run. Recorded as a partial
result rather than a clean one.
