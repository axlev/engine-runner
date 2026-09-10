# Training-data contamination in the FRR pilot-v1 cohort

**Question.** The cohort's ten cases were merged in 2024. FRR is a large, public,
widely-mirrored project, and the reviewer models are trained to roughly May 2026. So
every case predates the training cutoff by 19–28 months. If a reviewer recognises a
memorised defect instead of analysing the diff, it scores well and measures nothing —
and **no part of the engine can detect that**, because the leak is in the model's
weights rather than in the bundle. The boundary validator inspects artifacts; it has no
visibility here.

## Why the obvious fix is unavailable

The clean design is a *control arm* — a set of cohort cases the model cannot have seen,
scored against the ones it might have. (Distinct from the two probe guards below; this
is a property of the cohort, not of the test.) It is not It is not
reachable from this dataset, and the reason is structural rather than an oversight in
selection:

- Retrospective evidence needs **time**. A positive requires a defect to be found and
  corrected; a negative is only convincing after a long quiet window.
- Non-contamination needs **recency** — the case must postdate training.

These pull in opposite directions. The usable window is the gap between the training
cutoff (~2026-05) and the dataset build (2026-08-21): about **3.5 months**. Inside it,
positives would need a defect introduced *and* corrected within that span — selecting
only shallow, fast-found bugs — and negatives would rest on a 3.5-month silence rather
than the two-year window the current negatives use.

Confirmed against the data: **0 of 1,667 candidates** have a cutoff after 2026-05-01.
Choosing 2024 was correct for the benchmark's primary purpose; it simply forecloses the
control-arm design.

So contamination here can be **bounded and documented, not eliminated**.

## Method

Two probes, run as plain adapter calls **outside** the pipeline — no bundle, no stages,
no results written — against all three arms. `--tools Read` only, and no filesystem is
mounted, so the model cannot search for an answer and turn a recall probe into a lookup.

**Probe A — recall from a symptom description.** The defect is described in prose; the
model is asked what it knows. Tests retrieval from a hint.

**Probe B — recognition from the real diff.** The actual `reviewer/diff.patch` is shown,
including blob hashes, and the model is asked whether it recognises *this specific
change* and knows its outcome. It is told explicitly **not** to analyse the code for
bugs, since the question is what it remembers, not what it can deduce.

Probe B is the stronger test. **Recognition is a lower bar than recall** — one can
recognise a face one could not describe — so failing the easier test is better evidence
of absence.

## Two guards, guarding opposite failures

The word "control" is doing too much work in most write-ups of this kind, so the two
guards are named separately here. A probe can fail in two opposite directions and each
needs its own check:

**Fabricated-defect guard — against FALSE POSITIVES.** Three defects invented for this
test, in real FRR daemons (`ripd`, `pimd`, `staticd`), describing bugs that never
existed. If a model produces a confident recollection of one of these, then its
recollections are confabulation and every substantive answer it gave elsewhere is
worthless. Probe A only.

**Heartbleed guard — against FALSE NEGATIVES.** The real OpenSSL fix for CVE-2014-0160,
a change any model that memorises anything has memorised. It is **not fabricated and not
part of the cohort**. If a model answers `NO RECOGNITION` to this, then its `NO
RECOGNITION` on the FRR cases is meaningless — it is refusing rather than reporting
absence. Probe B only.

This second guard was learned the hard way. A first version of Probe A instructed *"do
not guess a PR number; a wrong number is worse than NO RECALL"*, and returned refusals
on **all ten prompts, real and fabricated alike**, because refusal had been made the safe
answer. That run had zero discriminating power. Without a false-negative guard it would
have read as a clean result.

**Not a guard: the cohort's own negatives.** Cases 15082, 17190 and 15227 are real FRR
PRs where the miner found no corrective evidence. They are 3 of the 10 cohort cases, not
instruments of this test. They are reported on their own row only because a claim that
one of them "was later found defective" would itself be confabulation, and so doubles as
weak evidence about the probe.

## Known limits of this method

- **A negative proves less than a positive.** Failure to recognise on demand does not
  rule out being primed when the code is actually under review; those are different
  retrieval paths.
- **The sensitivity floor is unmeasured.** Heartbleed is among the most-discussed commits
  in software history. The false-negative guard shows the probe detects *strong*
  memorisation. It does not establish that the probe would detect *faint* memorisation of
  a moderately-discussed routing-daemon PR. A mid-fame guard would measure that floor;
  none was available.
- **Probe A's asymmetry.** The real symptoms are paraphrased from actual PR descriptions
  while the fabricated defects were invented for this test, so authentic phrasing is a
  possible confound for any real-vs-fabricated difference.
- **Single sample per cell.** Model output is stochastic and nothing here is repeated.

## Results

63 calls: 10 cohort cases × 3 arms × 2 probes, plus both guards. 2026-09-10.

### The false-negative guard decides what is readable

| Arm | Heartbleed guard (false-negative check) | Are that arm's nulls interpretable? |
|---|---|---|
| haiku | **NO RECOGNITION** | **No** |
| sonnet | **NO RECOGNITION** | **No** |
| opus | Full recognition — OpenSSL, `ssl/d1_both.c`, the Heartbleed fix | **Yes** |

Only **opus** demonstrates the probe can detect memorisation at all. haiku and sonnet
answered `NO RECOGNITION` to one of the most-documented commits in software history, so
their uniform nulls on the FRR cases carry no information — they cannot be distinguished
from an instrument that says no to everything.

**This is not a finding about haiku or sonnet.** They are *unmeasured*, not implicated.
Smaller models retain fewer such specifics, so their real exposure is plausibly lower
than opus's — but this method does not show it either way. Reporting their nulls as
clearance would have been wrong, and without the false-negative guard that is exactly
what would have happened.

### opus — the one arm where the answer means something

| Probe | Group | Substantive answers |
|---|---|---|
| A (symptom → recall) | 7 real defects | 7 / 7 |
| A | 3 fabricated defects *(false-positive guard)* | 0 / 3 |
| B (real diff → recognition) | 7 positives | **0 / 7** |
| B | 3 cohort negatives *(not a guard)* | **0 / 3** |
| B | Heartbleed *(false-negative guard)* | 1 / 1 |

Probe A looks alarming and is not. Across all seven real cases opus produced **no PR
number and no commit hash**, self-labelled every answer "partial recall" or "low
confidence", and when pushed for specifics on PR 16194 named `bfdd/bfd.c` — **the wrong
daemon**, since that case is in `bgpd`. Meanwhile it declined all three fabricated
defects.

That pattern is **knowledge of FRR's codebase, not memory of these incidents**: it can
reason about real code paths and has nothing to say about invented ones. Codebase
familiarity is legitimate reviewer expertise — a skilled human reviewer brings the same
thing — and is not contamination.

Probe B is the stronger test and it is clean: shown the actual diffs, including blob
hashes, opus recognised **none of the ten**, while recognising Heartbleed from the same
prompt shape with commit hash, author and date.

### Verdict

**No evidence of incident-level memorisation of these ten PRs, on the only arm where the
test is valid.** The risk raised against this cohort is materially reduced, though not
eliminated — see the limits above, particularly that the sensitivity floor is unmeasured
and that failure to recognise on demand does not rule out priming during review.

**Residual risk, stated plainly:** the cohort is not proven clean. It is proven not to be
*obviously* contaminated on opus, and unmeasured on haiku and sonnet. That is the honest
ceiling of what can be established without a post-cutoff control arm, which this dataset
cannot supply.

### Reproducing

Harness, raw answers and the classifier are under the session scratchpad
(`probeA/matrix.sh`, `probeA/raw/`). Probes run as plain adapter calls outside the
pipeline — no bundle, no stages, no results written — with `--tools Read` and no mounted
filesystem, so no lookup is possible. Total cost of the full matrix: roughly $6.
