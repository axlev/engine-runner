# H1 results

Cohort 39 cases (19 positive, 20 negative). Generated from `h1-score.json` by `h1score -render`.

## Pre-registered criteria (§2)

All three must hold. Recall is reported, not criterial.

| Criterion | Threshold | Observed | Verdict |
|---|---|---|---|
| Reason match, treatment RISKY true positives | ≥ 60% MECHANISM | 0.562 (9/16) | does not meet |
| Recommended-validation hit rate (T) | ≥ 50% | 0.188 (3/16) | does not meet |
| Case-level precision, T-G | ≥ +15 points | -12.3 points | does not meet |
| Case-level precision, T-H | ≥ +15 points | +15.1 points | **meets** |

Precision is reported conditional on the 1:1 positive/negative ratio.

**Underpowered at this cohort size:** with 19 positives, a +15-point recall or
reason-match difference cannot reach significance. Those are reported as
"consistent with H1, underpowered", never as a pass.

## Per arm

| Arm | Protocol | Cases | TP | FP | FN | TN | Precision | Recall | vs chance (Fisher, 2-sided) |
|---|---|---|---|---|---|---|---|---|---|
| T | `h1-t-v1` | 36 | 16 | 11 | 0 | 9 | 0.593 (16/27) | 1.000 (16/16) | 0.0019 |
| G | `h1-g-v1` | 35 | 14 | 6 | 1 | 14 | 0.700 (14/20) | 0.933 (14/15) | 0.0003 |
| H | `history-baseline/v1` | 39 | 18 | 19 | 1 | 1 | 0.486 (18/37) | 0.947 (18/19) | 1.0000 |

Arm T did not score 3 case(s): case-08467610b3eb838e, case-74a3e8b1d531640c, case-e536d8e5bf2d889e.
Arm T excluded 3 run(s) voided by the post-hoc scan: case-08467610b3eb838e, case-74a3e8b1d531640c, case-e536d8e5bf2d889e.
Arm G did not score 4 case(s): case-08467610b3eb838e, case-74a3e8b1d531640c, case-c49c264bde360ca1, case-e536d8e5bf2d889e.
Arm G excluded 4 run(s) voided by the post-hoc scan: case-08467610b3eb838e, case-74a3e8b1d531640c, case-c49c264bde360ca1, case-e536d8e5bf2d889e.

## Pairwise

| Comparison | Subset | Common | Discordant | Δ precision | p (1-sided) | p (2-sided) | Δ recall | p (1-sided) | p (2-sided) |
|---|---|---|---|---|---|---|---|---|---|
| T-G | all scored cases | 35 | 6 | -0.123 | 0.9844 | 0.0625 | +0.067 | 0.5000 | 1.0000 |
| T-H | all scored cases | 36 | 11 | +0.151 | 0.0059 | 0.0117 | +0.062 | 0.5000 | 1.0000 |
| T-G | excluding fallback pairs (A9(vi)) | 28 | 5 | -0.116 | 0.9688 | 0.1250 | +0.083 | 0.5000 | 1.0000 |
| T-H | excluding fallback pairs (A9(vi)) | 29 | 9 | +0.146 | 0.0195 | 0.0391 | +0.077 | 0.5000 | 1.0000 |

Test: exact enumeration of 2^discordant arm swaps.

## Recommended-validation hit rate

Path match against the fixing commit, no judge call.

**Arm T:** 0.188 (3/16)

| Case | Hit | Matched path | Matched by |
|---|---|---|---|
| `case-185dad450d340655` | no | — |  |
| `case-1bdbd662589b27ae` | yes | `tests/topotests/bgp_link_state_bgp_fabric_srv6/test_bgp_link_state_bgp_fabric_srv6.py` | under recommended directory tests/topotests/bgp_link_state_bgp_fabric_srv6 |
| `case-1d42ac76e342fe5f` | no | — |  |
| `case-2d5d8ef4a5cb1c75` | no | — |  |
| `case-492381809029295b` | no | — |  |
| `case-50b1f68452ec9cb3` | no | — |  |
| `case-6045b30e4b49983b` | no | — |  |
| `case-633a3d116493c2d3` | no | — |  |
| `case-904c6aef5524ba0e` | no | — |  |
| `case-b1d011d5c7c060c2` | yes | `tests/topotests/isis_srv6_topo1/test_isis_srv6_topo1.py` | exact |
| `case-b3f4e25b4a68c80a` | no | — |  |
| `case-b7a24f0031af12c8` | no | — |  |
| `case-bbb4b0dad37eccb2` | no | — |  |
| `case-c28b772309d357dc` | yes | `tests/topotests/bgp_peer_group/r1/frr.conf` | under recommended directory tests/topotests/bgp_peer_group |
| `case-c49c264bde360ca1` | no | — |  |
| `case-c782e478705d4fab` | no | — |  |

**Arm G:** 0.214 (3/14)

| Case | Hit | Matched path | Matched by |
|---|---|---|---|
| `case-185dad450d340655` | no | — |  |
| `case-1bdbd662589b27ae` | yes | `tests/topotests/bgp_link_state_bgp_fabric_srv6/test_bgp_link_state_bgp_fabric_srv6.py` | exact |
| `case-1d42ac76e342fe5f` | no | — |  |
| `case-2d5d8ef4a5cb1c75` | no | — |  |
| `case-492381809029295b` | no | — |  |
| `case-50b1f68452ec9cb3` | no | — |  |
| `case-6045b30e4b49983b` | no | — |  |
| `case-633a3d116493c2d3` | no | — |  |
| `case-904c6aef5524ba0e` | no | — |  |
| `case-b1d011d5c7c060c2` | yes | `tests/topotests/isis_srv6_topo1/test_isis_srv6_topo1.py` | exact |
| `case-b3f4e25b4a68c80a` | no | — |  |
| `case-b7a24f0031af12c8` | no | — |  |
| `case-bbb4b0dad37eccb2` | no | — |  |
| `case-c28b772309d357dc` | yes | `tests/topotests/bgp_peer_group/r1/frr.conf` | under recommended directory tests/topotests/bgp_peer_group |

## Reason match (§6)

Judge model claude-opus-5 is IN-FAMILY with the treatment arm; section 6 requires that stated on every figure derived from it.

| Arm | Judged | MECHANISM | LOCALITY_ONLY | NONE | TOO_VAGUE | Reason match | LOCALITY_ONLY rate |
|---|---|---|---|---|---|---|---|
| G | 14 | 9 | 3 | 2 | 0 | 0.643 | 0.214 |
| T | 16 | 9 | 4 | 3 | 0 | 0.562 | 0.250 |

LOCALITY_ONLY is reported beside the match rate deliberately: a high MECHANISM
count with near-zero LOCALITY_ONLY is evidence the judge is agreeing too easily,
not that reviewers are right.

### Judged but voided

A voided run is dropped from every figure, so these judged findings are
excluded from the rates above. They were paid for and judged, so they are
reported rather than left as a silently smaller denominator.

| Case | Arm | Reason |
|---|---|---|
| `case-c49c264bde360ca1` | G | scan-voided (section 8): this arm's run is dropped |
| `case-74a3e8b1d531640c` | G | scan-voided (section 8): this arm's run is dropped |
| `case-74a3e8b1d531640c` | T | scan-voided (section 8): this arm's run is dropped |
| `case-08467610b3eb838e` | G | scan-voided (section 8): this arm's run is dropped |
| `case-08467610b3eb838e` | T | scan-voided (section 8): this arm's run is dropped |

## Recall by failure class (§7)

| Class | T | G | H |
|---|---|---|---|
| `classical-memory-safety` | 1.000 (1/1) | 1.000 (1/1) | 1.000 (1/1) |
| `config-interaction` | 1.000 (2/2) | 1.000 (2/2) | 1.000 (2/2) |
| `cross-module-invariant` | 1.000 (6/6) | 0.833 (5/6) | 0.833 (5/6) |
| `error-path-lifecycle` | 1.000 (1/1) | 1.000 (1/1) | 1.000 (1/1) |
| `ordering-state-machine` | absent (no positives) | absent (no positives) | 1.000 (1/1) |
| `other` | 1.000 (2/2) | 1.000 (2/2) | 1.000 (3/3) |
| `protocol-parsing` | 1.000 (3/3) | 1.000 (2/2) | 1.000 (4/4) |
| `restart-upgrade` | 1.000 (1/1) | 1.000 (1/1) | 1.000 (1/1) |

Reported, not criterial. The pre-registration expects T to do worst on
`timing-race` and `resource-exhaustion`, which are deliberately not lensed.

## Strata

### Admission of title/description

| Bucket | Cases |
|---|---|
| admitted | 40 |

| Arm | Bucket | Cases | TP | FP | FN | TN | Precision | Recall |
|---|---|---|---|---|---|---|---|---|
| T | admitted | 36 | 16 | 11 | 0 | 9 | 0.593 (16/27) | 1.000 (16/16) |
| G | admitted | 35 | 14 | 6 | 1 | 14 | 0.700 (14/20) | 0.933 (14/15) |
| H | admitted | 39 | 18 | 19 | 1 | 1 | 0.486 (18/37) | 0.947 (18/19) |

### Fallback pairs (A9(vi))

A fallback pair was matched on subsystem alone, so the stateful category is unbalanced within the pair on a dimension the arms can see in the diff. §2's pairwise deltas are to be given with and without these.

| Bucket | Cases |
|---|---|
| fallback | 8 |
| fully matched | 32 |

| Arm | Bucket | Cases | TP | FP | FN | TN | Precision | Recall |
|---|---|---|---|---|---|---|---|---|
| T | fallback | 7 | 3 | 2 | 0 | 2 | 0.600 (3/5) | 1.000 (3/3) |
| T | fully matched | 29 | 13 | 9 | 0 | 7 | 0.591 (13/22) | 1.000 (13/13) |
| G | fallback | 7 | 3 | 1 | 0 | 3 | 0.750 (3/4) | 1.000 (3/3) |
| G | fully matched | 28 | 11 | 5 | 1 | 11 | 0.688 (11/16) | 0.917 (11/12) |
| H | fallback | 8 | 4 | 4 | 0 | 0 | 0.500 (4/8) | 1.000 (4/4) |
| H | fully matched | 31 | 14 | 15 | 1 | 1 | 0.483 (14/29) | 0.933 (14/15) |

### Source window (A11(iii))

| Bucket | Cases |
|---|---|
| 2024 | 25 |
| 2026H1 | 15 |

| Arm | Bucket | Cases | TP | FP | FN | TN | Precision | Recall |
|---|---|---|---|---|---|---|---|---|
| T | 2024 | 24 | 9 | 7 | 0 | 8 | 0.562 (9/16) | 1.000 (9/9) |
| T | 2026H1 | 12 | 7 | 4 | 0 | 1 | 0.636 (7/11) | 1.000 (7/7) |
| G | 2024 | 23 | 8 | 5 | 0 | 10 | 0.615 (8/13) | 1.000 (8/8) |
| G | 2026H1 | 12 | 6 | 1 | 1 | 4 | 0.857 (6/7) | 0.857 (6/7) |
| H | 2024 | 24 | 9 | 14 | 0 | 1 | 0.391 (9/23) | 1.000 (9/9) |
| H | 2026H1 | 15 | 9 | 5 | 1 | 0 | 0.643 (9/14) | 0.900 (9/10) |

### Fix before/after model cutoff (A11(vi))

Recorded as a covariate per positive; the model cutoff is May 2026.

| Bucket | Cases |
|---|---|
| fix after cutoff | 26 |
| fix before cutoff | 14 |

| Arm | Bucket | Cases | TP | FP | FN | TN | Precision | Recall |
|---|---|---|---|---|---|---|---|---|
| T | after cutoff | 25 | 5 | 11 | 0 | 9 | 0.312 (5/16) | 1.000 (5/5) |
| T | before cutoff | 11 | 11 | 0 | 0 | 0 | 1.000 (11/11) | 1.000 (11/11) |
| G | after cutoff | 25 | 4 | 6 | 1 | 14 | 0.400 (4/10) | 0.800 (4/5) |
| G | before cutoff | 10 | 10 | 0 | 0 | 0 | 1.000 (10/10) | 1.000 (10/10) |
| H | after cutoff | 26 | 5 | 19 | 1 | 1 | 0.208 (5/24) | 0.833 (5/6) |
| H | before cutoff | 13 | 13 | 0 | 0 | 0 | 1.000 (13/13) | 1.000 (13/13) |

### Subsystem (A9(v))

| Bucket | Cases |
|---|---|
| bfdd | 2 |
| bgpd | 16 |
| isisd | 4 |
| lib | 6 |
| ospfd | 2 |
| zebra | 10 |

| Arm | Bucket | Cases | TP | FP | FN | TN | Precision | Recall |
|---|---|---|---|---|---|---|---|---|
| T | bfdd | 2 | 1 | 1 | 0 | 0 | 0.500 (1/2) | 1.000 (1/1) |
| T | bgpd | 13 | 5 | 5 | 0 | 3 | 0.500 (5/10) | 1.000 (5/5) |
| T | isisd | 4 | 2 | 1 | 0 | 1 | 0.667 (2/3) | 1.000 (2/2) |
| T | lib | 6 | 3 | 1 | 0 | 2 | 0.750 (3/4) | 1.000 (3/3) |
| T | ospfd | 2 | 1 | 1 | 0 | 0 | 0.500 (1/2) | 1.000 (1/1) |
| T | zebra | 9 | 4 | 2 | 0 | 3 | 0.667 (4/6) | 1.000 (4/4) |
| G | bfdd | 2 | 0 | 0 | 1 | 1 | absent (no case was called RISKY) | 0.000 (0/1) |
| G | bgpd | 13 | 5 | 5 | 0 | 3 | 0.500 (5/10) | 1.000 (5/5) |
| G | isisd | 4 | 2 | 0 | 0 | 2 | 1.000 (2/2) | 1.000 (2/2) |
| G | lib | 6 | 3 | 0 | 0 | 3 | 1.000 (3/3) | 1.000 (3/3) |
| G | ospfd | 2 | 1 | 1 | 0 | 0 | 0.500 (1/2) | 1.000 (1/1) |
| G | zebra | 8 | 3 | 0 | 0 | 5 | 1.000 (3/3) | 1.000 (3/3) |
| H | bfdd | 2 | 0 | 0 | 1 | 1 | absent (no case was called RISKY) | 0.000 (0/1) |
| H | bgpd | 16 | 8 | 8 | 0 | 0 | 0.500 (8/16) | 1.000 (8/8) |
| H | isisd | 4 | 2 | 2 | 0 | 0 | 0.500 (2/4) | 1.000 (2/2) |
| H | lib | 6 | 3 | 3 | 0 | 0 | 0.500 (3/6) | 1.000 (3/3) |
| H | ospfd | 2 | 1 | 1 | 0 | 0 | 0.500 (1/2) | 1.000 (1/1) |
| H | zebra | 9 | 4 | 5 | 0 | 0 | 0.444 (4/9) | 1.000 (4/4) |

## Excluded cases

**Voided by the contamination probe (A11), excluded from every arm:** case-33109a63a9cf87e8.

The model already knew how these changes were fixed, so no arm's answer on
them means anything.

**Arm T, voided by the §8 post-hoc scan:** case-08467610b3eb838e, case-74a3e8b1d531640c, case-e536d8e5bf2d889e.

**Arm G, voided by the §8 post-hoc scan:** case-08467610b3eb838e, case-74a3e8b1d531640c, case-c49c264bde360ca1, case-e536d8e5bf2d889e.

## Cost

Figures are the CLI's ESTIMATE of equivalent API cost under a subscription token, not amounts billed.

## Provenance

| Artifact | Value |
|---|---|
| engine commit | `f66ae87` |
| cohort `records_sha256` | `b2279b9cab3e522c7c02cf0da9e480f6ca4090afe0d20f5a18a0d6712cd9d96f` |
| input `cohort_manifest_sha256` | `3524745a98563086b9744d3060a4a7d38a99336a76578f749df67689c88c4c37` |
| input `fixing_paths_dir` | `/home/alex/data/FRR2026H1/evaluator/results/inputs/fixing-paths` |
| input `history_dir` | `/home/alex/repos/miner/output/frr-h1-cohort-evaluator-inputs/history` |
| input `judge_dir` | `/home/alex/data/FRR2026H1/evaluator/results/judge` |
| input `labels_sha256` | `02ce750fddb2320c8f1d38f1fe3db4c9dc052430d12366a4418794663a4fcffe` |
| input `probe_verdicts_dir` | `/home/alex/repos/miner/output/frr-h1-cohort-evaluator-inputs/probe-verdicts` |
| input `probes_dir` | `/home/alex/data/FRR2026H1/evaluator/probes-out` |
| input `runs_root` | `build/results` |
| input `scans_dir` | `/home/alex/data/FRR2026H1/evaluator/results/scan` |

- Prompt delivery: probes 1–19 argv (engine ≤ affb724); probe 20 and all arm runs stdin (engine ≥ 8023221); identical bytes, prompt hashes unchanged.
- Vendor schema projection drops root-level allOf/anyOf/oneOf from h1-review-a; the model was not sent the 'empty findings requires empty_reason' conditional; it is enforced on the response (78ed775).
- max_tool_calls in the arm file did not bind on any stage: the adapter reports no tool counts.
- Containers ran without --memory/--cpus (ResourceLimits unpopulated); same for both arms.
- Engine commits across sealed runs: T f66ae87×35 + 78ed775×4; G f66ae87×39; deltas are a test and -bench-bin plumbing, no stage behaviour change.
- Cost from sealed run.json: T $168.52 (mean $4.32, max $10.22), G $88.26 (mean $2.26); total $256.78; ~$14 additional spent on attempts killed mid-case by the harness supervisor that sealed nothing (actual ≈ $271). 0 stalls, 0 cap kills, 2/117 stage retries; 78/78 boundary validation pass.
- Reason-match denominators exclude scan-voided runs (T 16, G 14); h1judge's own summary over all judged runs (T 18: 11 MECHANISM; G 17: 11) is reported for transparency and is not the criterion.

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

