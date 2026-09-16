# H1 results

> **DRAFT SCAFFOLD.** Rendered before the arms ran; every number below is
> from fixture or partial data and means nothing yet.

Cohort 6 cases (3 positive, 3 negative). Generated from `h1-score.json` by `h1score -render`.

## Pre-registered criteria (§2)

All three must hold. Recall is reported, not criterial.

| Criterion | Threshold | Observed | Verdict |
|---|---|---|---|
| Reason match, treatment RISKY true positives | ≥ 60% MECHANISM | absent (judge pass has not run) | cannot be evaluated |
| Recommended-validation hit rate (T) | ≥ 50% | absent (no RISKY true positive with fixing paths) | cannot be evaluated |
| Case-level precision, T-G | ≥ +15 points | absent | cannot be evaluated |
| Case-level precision, T-H | ≥ +15 points | absent (no case scored by both arms) | cannot be evaluated |

Precision is reported conditional on the 1:1 positive/negative ratio.

**Underpowered at this cohort size:** with 3 positives, a +15-point recall or
reason-match difference cannot reach significance. Those are reported as
"consistent with H1, underpowered", never as a pass.

## Per arm

| Arm | Protocol | Cases | TP | FP | FN | TN | Precision | Recall | vs chance (Fisher, 2-sided) |
|---|---|---|---|---|---|---|---|---|---|
| T | `h1-t-v1` | 6 | 0 | 0 | 3 | 3 | absent (no case was called RISKY) | 0.000 (0/3) | 1.0000 |
| G | `h1-g-v1` | 6 | 0 | 0 | 3 | 3 | absent (no case was called RISKY) | 0.000 (0/3) | 1.0000 |
| H | `history-baseline/v1` | 0 | 0 | 0 | 0 | 0 | absent (no case was called RISKY) | absent (no positives) | absent (no cases scored) |

Arm H did not score 6 case(s): case-185dad450d340655, case-2ae3385ba3c3e272, case-65e6253e931e5bf1, case-81abc025b64dea3a, case-9bc74940b6b1d813, case-e536d8e5bf2d889e.

## Pairwise

| Comparison | Common | Discordant | Δ precision | p (1-sided) | p (2-sided) | Δ recall | p (1-sided) | p (2-sided) |
|---|---|---|---|---|---|---|---|---|
| T-G | 6 | 0 | absent | absent | absent | +0.000 | 1.0000 | 1.0000 |
| T-H | — | — | absent (no case scored by both arms) | | | | | |

Test: exact enumeration of 2^discordant arm swaps.

## Recommended-validation hit rate

Path match against the fixing commit, no judge call.

**Arm T:** absent (no RISKY true positive with fixing paths)

**Arm G:** absent (no RISKY true positive with fixing paths)

## Reason match (§6)

**Absent.** the reason-match judge pass has not run; fed from internal/judge in a later pass

This is the §2 criterion that cannot be computed without a paid judge pass.
Until it runs, H1 has not been evaluated, whatever the other numbers say.

## Recall by failure class (§7)

| Class | T | G | H |
|---|---|---|---|
| `config-interaction` | 0.000 (0/3) | 0.000 (0/3) | absent (no positives) |

Reported, not criterial. The pre-registration expects T to do worst on
`timing-race` and `resource-exhaustion`, which are deliberately not lensed.

## Strata

### Admission of title/description

| Bucket | Cases |
|---|---|
| admitted | 6 |

*Per-arm precision and recall within these buckets are in the scored JSON
under `arms.<arm>.by_admission_description`.*

### Fallback pairs (A9(vi))

A fallback pair was matched on subsystem alone, so the stateful category is unbalanced within the pair on a dimension the arms can see in the diff. §2's pairwise deltas are to be given with and without these.

| Bucket | Cases |
|---|---|
| fallback (subsystem only) | 2 |
| matched on category+subsystem | 4 |

*Case counts only. Per-arm precision and recall within these buckets are
not computed yet: the scorer strata only `admission.description`. Extending
it is a scorer change, not a rendering one.*

### Source window (A11(iii))

| Bucket | Cases |
|---|---|
| 2026H1 | 6 |

*Case counts only. Per-arm precision and recall within these buckets are
not computed yet: the scorer strata only `admission.description`. Extending
it is a scorer change, not a rendering one.*

### Fix before/after model cutoff (A11(vi))

Recorded as a covariate per positive; the model cutoff is May 2026.

| Bucket | Cases |
|---|---|
| fix after cutoff | 4 |
| fix before cutoff | 2 |

*Case counts only. Per-arm precision and recall within these buckets are
not computed yet: the scorer strata only `admission.description`. Extending
it is a scorer change, not a rendering one.*

### Subsystem (A9(v))

| Bucket | Cases |
|---|---|
| bgpd | 6 |

*Case counts only. Per-arm precision and recall within these buckets are
not computed yet: the scorer strata only `admission.description`. Extending
it is a scorer change, not a rendering one.*

## Excluded cases

None.

## Cost

Figures are the CLI's ESTIMATE of equivalent API cost under a subscription token, not amounts billed.

## Provenance

| Artifact | Value |
|---|---|
| engine commit | `c6a8d14` |
| cohort `records_sha256` | `provisional-d645e854` |
| input `cohort_manifest_sha256` | `b5c3ea8cb853e62fe88d91de20cd768c81038ded5e7cb342966b4378046b2b61` |
| input `labels_sha256` | `8caaa39034c89cb4ccd7bd4bb4ddc57fa7f0fe455e69b34985728e089275574a` |
| input `runs_root` | `/tmp/claude-1000/-home-alex-repos-engine-runner/9749265c-8488-4ebc-b293-c4c4410eb27c/scratchpad/batch` |

### Rules as applied

- `arm_ids`: map[G:h1-g-v1 T:h1-t-v1]
- `exact_limit_discordant`: 20
- `history_absent`: a case with no history-baseline/v1 file is absent for H, not CLEAN
- `hit_match`: a recommended path equals a fixing path, or a fixing path lies under a recommended directory; over RISKY true positives with an h1-fixing-paths file
- `pairwise_null`: per case, the two arms' verdicts are exchangeable; reference distribution swaps them on the discordant cases
- `pairwise_test`: paired-exchangeable-verdicts, exact <=20 discordant, MC 200k seeded above (A8)
- `per_arm_vs_chance`: case-level Fisher exact, two-sided, one per arm, on the verdict x label table under fixed margins (A8); not criterial
- `precision`: TP / (TP + FP) over labelled cases the arm scored; RISKY from evaluation/verdict.json
- `preregistered_threshold`: 15
- `probe_unresolved`: A11: a case whose probe scan was clean but which no evaluator has read makes the result provisional; a clean mechanical scan is not an acquittal
- `probe_voided`: A11: a case the contamination probe voided is dropped from EVERY arm before counting
- `recall`: TP / (TP + FN); reported, not criterial (pre-registration s2)
- `voided_runs`: excluded from the arm and counted; from contamination-scan/v1 void=true

## Registered limitations

These are pre-registered, not discovered after the fact. They apply whatever
the numbers above show.

**Staging confound (brief §2).** T has a substantiation stage and G does not,
so T differs from G in two declared ways: the discovery prompt's five domain
lenses, and the second stage. A T-over-G result is therefore prompt *plus*
staging, and cannot be attributed to the prompt alone. A fourth arm (generic
prompt + substantiation) would decompose it and is deferred.

**Arm H is a floor, not a competitor (A10).** Under subsystem matching a
positive and its negative share a subsystem and near-identical windows, so H
gives both the same verdict by construction: its precision is pinned near the
base rate and its recall near 1.0. T−H answers "does the treatment beat
knowing the subsystem". The §2 pairwise criterion is carried by T−G.

**Fallback pairs (A9(vi)).** A positive with no counted-clean negative in its
(category, subsystem) cell is matched on subsystem alone. Within such a pair
the stateful category is unbalanced on a dimension the arms can see in the
diff — every HIGH bgpd positive is a fallback pair by construction. Pairwise
deltas are to be reported with and without them.

**In-family judge (§6).** Reason match is adjudicated by Opus, the same model
family as the treatment arm. This is stated on every figure derived from it.
Cross-vendor re-judging is desirable and not required for this
pre-registration.

**Cost figures are estimates.** The credential is a subscription token, so
`cost_usd` is the CLI's estimate of equivalent API cost, not an amount billed.

