# H1 results

> **DRAFT SCAFFOLD.** Rendered before the arms ran; every number below is
> from fixture or partial data and means nothing yet.

Cohort 40 cases (20 positive, 20 negative). Generated from `h1-score.json` by `h1score -render`.

## Pre-registered criteria (§2)

All three must hold. Recall is reported, not criterial.

| Criterion | Threshold | Observed | Verdict |
|---|---|---|---|
| Reason match, treatment RISKY true positives | ≥ 60% MECHANISM | absent (judge pass has not run) | cannot be evaluated |
| Recommended-validation hit rate (T) | ≥ 50% | absent (no RISKY true positive with fixing paths) | cannot be evaluated |
| Case-level precision, T-G | ≥ +15 points | absent (no case scored by both arms) | cannot be evaluated |
| Case-level precision, T-H | ≥ +15 points | absent (no case scored by both arms) | cannot be evaluated |

Precision is reported conditional on the 1:1 positive/negative ratio.

**Underpowered at this cohort size:** with 20 positives, a +15-point recall or
reason-match difference cannot reach significance. Those are reported as
"consistent with H1, underpowered", never as a pass.

## Per arm

| Arm | Protocol | Cases | TP | FP | FN | TN | Precision | Recall | vs chance (Fisher, 2-sided) |
|---|---|---|---|---|---|---|---|---|---|
| T | `h1-t-v1` | 0 | 0 | 0 | 0 | 0 | absent (no case was called RISKY) | absent (no positives) | absent (no cases scored) |
| G | `h1-g-v1` | 40 | 0 | 0 | 20 | 20 | absent (no case was called RISKY) | 0.000 (0/20) | 1.0000 |
| H | `history-baseline/v1` | 0 | 0 | 0 | 0 | 0 | absent (no case was called RISKY) | absent (no positives) | absent (no cases scored) |

Arm T did not score 40 case(s): case-08467610b3eb838e, case-185dad450d340655, case-1b8adae03a329291, case-1bdbd662589b27ae, case-1d42ac76e342fe5f, case-2699efb20edbe533, case-28152325a9efbe56, case-281ffc6ace1cb130, case-2d5d8ef4a5cb1c75, case-33109a63a9cf87e8, case-384c8a19f7a29646, case-492381809029295b, case-50b1f68452ec9cb3, case-520bcfda15744771, case-6045b30e4b49983b, case-626d77ae4636bd31, case-633a3d116493c2d3, case-74a3e8b1d531640c, case-7c1ef13dfbb857a7, case-896b7e1f79c15282, case-904c6aef5524ba0e, case-96dd77c010ac73a8, case-9a6a8ab07aeba591, case-a9e6ee22c6945695, case-b1d011d5c7c060c2, case-b2de29894c47c0f6, case-b3f4e25b4a68c80a, case-b7a24f0031af12c8, case-bbb4b0dad37eccb2, case-c28b772309d357dc, case-c49c264bde360ca1, case-c782e478705d4fab, case-d13c49c44610890a, case-d63ac8dc5b36c28e, case-db44abbf39439b31, case-e536d8e5bf2d889e, case-ed5a1593c41ff315, case-f4bcd1c1e10c94b6, case-f73ff79c3d818a46, case-fccc0e0e7de3e73d.
Arm H did not score 40 case(s): case-08467610b3eb838e, case-185dad450d340655, case-1b8adae03a329291, case-1bdbd662589b27ae, case-1d42ac76e342fe5f, case-2699efb20edbe533, case-28152325a9efbe56, case-281ffc6ace1cb130, case-2d5d8ef4a5cb1c75, case-33109a63a9cf87e8, case-384c8a19f7a29646, case-492381809029295b, case-50b1f68452ec9cb3, case-520bcfda15744771, case-6045b30e4b49983b, case-626d77ae4636bd31, case-633a3d116493c2d3, case-74a3e8b1d531640c, case-7c1ef13dfbb857a7, case-896b7e1f79c15282, case-904c6aef5524ba0e, case-96dd77c010ac73a8, case-9a6a8ab07aeba591, case-a9e6ee22c6945695, case-b1d011d5c7c060c2, case-b2de29894c47c0f6, case-b3f4e25b4a68c80a, case-b7a24f0031af12c8, case-bbb4b0dad37eccb2, case-c28b772309d357dc, case-c49c264bde360ca1, case-c782e478705d4fab, case-d13c49c44610890a, case-d63ac8dc5b36c28e, case-db44abbf39439b31, case-e536d8e5bf2d889e, case-ed5a1593c41ff315, case-f4bcd1c1e10c94b6, case-f73ff79c3d818a46, case-fccc0e0e7de3e73d.

## Pairwise

| Comparison | Subset | Common | Discordant | Δ precision | p (1-sided) | p (2-sided) | Δ recall | p (1-sided) | p (2-sided) |
|---|---|---|---|---|---|---|---|---|---|
| T-G | all scored cases | — | — | absent (no case scored by both arms) | | | | | |
| T-H | all scored cases | — | — | absent (no case scored by both arms) | | | | | |
| T-G | excluding fallback pairs (A9(vi)) | — | — | absent (no case scored by both arms) | | | | | |
| T-H | excluding fallback pairs (A9(vi)) | — | — | absent (no case scored by both arms) | | | | | |

Test: .

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
| `config-interaction` | absent (no positives) | 0.000 (0/20) | absent (no positives) |

Reported, not criterial. The pre-registration expects T to do worst on
`timing-race` and `resource-exhaustion`, which are deliberately not lensed.

## Strata

### Admission of title/description

| Bucket | Cases |
|---|---|
| admitted | 40 |

| Arm | Bucket | Cases | TP | FP | FN | TN | Precision | Recall |
|---|---|---|---|---|---|---|---|---|
| T | admitted | 0 | 0 | 0 | 0 | 0 | absent (no case was called RISKY) | absent (no positives) |
| G | admitted | 40 | 0 | 0 | 20 | 20 | absent (no case was called RISKY) | 0.000 (0/20) |
| H | admitted | 0 | 0 | 0 | 0 | 0 | absent (no case was called RISKY) | absent (no positives) |

### Fallback pairs (A9(vi))

A fallback pair was matched on subsystem alone, so the stateful category is unbalanced within the pair on a dimension the arms can see in the diff. §2's pairwise deltas are to be given with and without these.

| Bucket | Cases |
|---|---|
| fallback | 6 |
| fully matched | 34 |

| Arm | Bucket | Cases | TP | FP | FN | TN | Precision | Recall |
|---|---|---|---|---|---|---|---|---|
| T | fallback | 0 | 0 | 0 | 0 | 0 | absent (no case was called RISKY) | absent (no positives) |
| T | fully matched | 0 | 0 | 0 | 0 | 0 | absent (no case was called RISKY) | absent (no positives) |
| G | fallback | 6 | 0 | 0 | 3 | 3 | absent (no case was called RISKY) | 0.000 (0/3) |
| G | fully matched | 34 | 0 | 0 | 17 | 17 | absent (no case was called RISKY) | 0.000 (0/17) |
| H | fallback | 0 | 0 | 0 | 0 | 0 | absent (no case was called RISKY) | absent (no positives) |
| H | fully matched | 0 | 0 | 0 | 0 | 0 | absent (no case was called RISKY) | absent (no positives) |

### Source window (A11(iii))

| Bucket | Cases |
|---|---|
| 2024 | 16 |
| 2026H1 | 24 |

| Arm | Bucket | Cases | TP | FP | FN | TN | Precision | Recall |
|---|---|---|---|---|---|---|---|---|
| T | 2024 | 0 | 0 | 0 | 0 | 0 | absent (no case was called RISKY) | absent (no positives) |
| T | 2026H1 | 0 | 0 | 0 | 0 | 0 | absent (no case was called RISKY) | absent (no positives) |
| G | 2024 | 16 | 0 | 0 | 8 | 8 | absent (no case was called RISKY) | 0.000 (0/8) |
| G | 2026H1 | 24 | 0 | 0 | 12 | 12 | absent (no case was called RISKY) | 0.000 (0/12) |
| H | 2024 | 0 | 0 | 0 | 0 | 0 | absent (no case was called RISKY) | absent (no positives) |
| H | 2026H1 | 0 | 0 | 0 | 0 | 0 | absent (no case was called RISKY) | absent (no positives) |

### Fix before/after model cutoff (A11(vi))

Recorded as a covariate per positive; the model cutoff is May 2026.

| Bucket | Cases |
|---|---|
| fix after cutoff | 30 |
| fix before cutoff | 10 |

| Arm | Bucket | Cases | TP | FP | FN | TN | Precision | Recall |
|---|---|---|---|---|---|---|---|---|
| T | after cutoff | 0 | 0 | 0 | 0 | 0 | absent (no case was called RISKY) | absent (no positives) |
| T | before cutoff | 0 | 0 | 0 | 0 | 0 | absent (no case was called RISKY) | absent (no positives) |
| G | after cutoff | 30 | 0 | 0 | 10 | 20 | absent (no case was called RISKY) | 0.000 (0/10) |
| G | before cutoff | 10 | 0 | 0 | 10 | 0 | absent (no case was called RISKY) | 0.000 (0/10) |
| H | after cutoff | 0 | 0 | 0 | 0 | 0 | absent (no case was called RISKY) | absent (no positives) |
| H | before cutoff | 0 | 0 | 0 | 0 | 0 | absent (no case was called RISKY) | absent (no positives) |

### Subsystem (A9(v))

| Bucket | Cases |
|---|---|
| bgpd | 20 |
| ospfd | 20 |

| Arm | Bucket | Cases | TP | FP | FN | TN | Precision | Recall |
|---|---|---|---|---|---|---|---|---|
| T | bgpd | 0 | 0 | 0 | 0 | 0 | absent (no case was called RISKY) | absent (no positives) |
| T | ospfd | 0 | 0 | 0 | 0 | 0 | absent (no case was called RISKY) | absent (no positives) |
| G | bgpd | 20 | 0 | 0 | 10 | 10 | absent (no case was called RISKY) | 0.000 (0/10) |
| G | ospfd | 20 | 0 | 0 | 10 | 10 | absent (no case was called RISKY) | 0.000 (0/10) |
| H | bgpd | 0 | 0 | 0 | 0 | 0 | absent (no case was called RISKY) | absent (no positives) |
| H | ospfd | 0 | 0 | 0 | 0 | 0 | absent (no case was called RISKY) | absent (no positives) |

## Excluded cases

None.

## Cost

Figures are the CLI's ESTIMATE of equivalent API cost under a subscription token, not amounts billed.

## Provenance

| Artifact | Value |
|---|---|
| engine commit | `f494ad6` |
| cohort `records_sha256` | `b2279b9c` |
| input `cohort_manifest_sha256` | `9b0d8e6524d634101eb765ba77066b86958ae242ea166c3cea6caa7f276bccb0` |
| input `labels_sha256` | `1cf01f86a3dbb723588d0856326521ab0aefd410492e0c94812f273c3f792703` |
| input `runs_root` | `/tmp/claude-1000/-home-alex-repos-engine-runner/9749265c-8488-4ebc-b293-c4c4410eb27c/scratchpad/dryrun40` |

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

**The judge reads one finding per arm, not all of them (§6).** The judged
pool carries each arm's HIGHEST-RANKED finding only — under T stage B's first
surviving assessment, under G `findings[0]`. This differs from the pilot's
judge, which pooled every finding a reviewer produced, and the difference is
deliberate: §6 scores "the case's highest-ranked finding" and the treatment
brief §5 makes ranking a scored output for exactly this reason. It is the
stricter test — a reviewer earns nothing for burying the right answer at
position nine, and ranking badly costs the same as not finding the defect at
all. Reason-match figures here are therefore NOT comparable with the pilot's
anticipation rate, which used the looser pool.

**In-family judge (§6).** Reason match is adjudicated by Opus, the same model
family as the treatment arm. This is stated on every figure derived from it.
Cross-vendor re-judging is desirable and not required for this
pre-registration.

**Cost figures are estimates.** The credential is a subscription token, so
`cost_usd` is the CLI's estimate of equivalent API cost, not an amount billed.

