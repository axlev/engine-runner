# Backlog

Working state for `engine-runner`: what is left, in the order it should be
done, and why that order. Update the Status column in place as items move —
this file is meant to be edited, not appended to.

Milestone definitions are in [`system-design.md`](system-design.md) §15. Known
gaps that are *decisions already taken* live in
[`../AGENTS.md`](../AGENTS.md); this file is the work queue.

## Sequencing: run the cohort before building the judge

The judge can score runs from any date, because every run seals a permanent,
checksummed tree containing all three reviews. **Cohort spend is banked, not
consumed** — a judge built later scores today's runs without re-spending.

The reverse does not hold. A judge designed before anyone has seen real
reviewer output is designed against imagined output, and its hardest decision
— the matching rule, "does this finding correspond to the real defect?" —
cannot be specified sensibly from synthetic fixtures. Ten real cases of
findings-versus-oracle turn that from a guess into an observation.

Running early also surfaces problems while re-running is still cheap. Every
defect found in this project so far came from running, not from reading.

**But one case before ten.** Budgets are calibrated against 7-file synthetic
fixtures; a real snapshot is ~7,600 files and ~48 MB. That should be
discovered on case 1, not case 7.

## Phase 0 — retire the risk on one real case

| # | Item | Status | Notes |
|---|---|---|---|
| 0.1 | Timing capture | **todo** | Stage duration is never derived — two timestamps reach `run.json` and nothing subtracts them. Also keep claude's `duration_ms` / `duration_api_ms`, currently parsed away; the API split separates model time from our overhead, which is what tells you whether a 48 MB snapshot costs in the container or at the API. Do this first so 0.2 measures something. |
| 0.2 | One FRR case, end to end, on sonnet | **todo** | Measure cost, wall clock, budget headroom, copy time. Cost is genuinely unknown: both reference points are 8-file snapshots (~$0.62 sonnet, ~$1.82 opus) and real cost scales with how much the model *explores*, not with snapshot size. |
| 0.3 | Recalibrate `configs/agents/*` | **todo** | Every current bound derives from 7-file fixtures. |
| 0.4 | Per-attempt copy strategy | **todo** | `contextbuilder` copies the snapshot file-by-file into a fresh workspace per attempt: up to 6 full-tree copies per case, ~45,600 file operations, unmeasured. May need a shared read-only mount. **Symlinks must survive as symlinks** through any change — dereferencing breaks diff/snapshot correspondence, which is why we rejected it on the miner side. |

## Phase 1 — the cohort

| # | Item | Status | Notes |
|---|---|---|---|
| 1.1 | Verify the reasoner-3 disposition fix | **blocked** | `6fcda0e` is committed but unverified: the re-run died at reasoner-2 on a vendor session limit before reasoner-3 ran. Test is `tlv-bounds` on opus showing `rejected_by_c: 1` rather than `confirmed_by_c: 2`. |
| 1.2 | Probe the 3 replacement negatives | **blocked** | `3a74a3a2`, `9ba8fca7`, `c96a113c` replaced the negatives after the contamination probe ran. Same quota block. Gap recorded in [`contamination-probe-pilot-v1.md`](contamination-probe-pilot-v1.md). |
| 1.3 | Run 10 cases | **todo** | 30 stages. **Plan around OAuth session limits** — a subscription token is rate-limited, not metered, so a cohort will stall partway and need resuming across reset windows. An API key removes that failure mode entirely. |
| 1.4 | Manual inspection | **todo** | ~40 findings; tractable by hand. This is the input to Phase 2's design, not a formality. |

## Phase 2 — the judge

Nothing here exists yet. There is no oracle ingest anywhere in the engine, and
no oracle bundle contract — only
[`prospective-bundle-contract.md`](prospective-bundle-contract.md). The miner
already emits `case-*-evaluator-only/` directories that this side cannot read.

| # | Item | Status | Notes |
|---|---|---|---|
| 2.1 | Oracle bundle contract | **todo** | The counterpart to the prospective contract. Must state what the evaluator receives and, as that document does, why. |
| 2.2 | Oracle ingest | **todo** | §6.5: an LLM judge **must run as a separate evaluator-only agent and must never share state or a filesystem with reasoners 1-3.** That is a second isolated adapter path, not a function call. Today's deterministic evaluator needs no such isolation only because it has no oracle access to protect. |
| 2.3 | **Matching semantics** | **needs a decision** | "Did the reviewer find *this* defect?" — same file, same function, same mechanism? **This determines what the benchmark claims** and is the user's call, not the engine's. Specify it from Phase 1 output rather than in advance. |
| 2.4 | Precision / recall, and correct-vs-incorrect rejection | **todo** | Today's evaluator computes reviewer-side metrics only: what stages 2 and 3 did with stage 1's findings. It cannot say whether any finding is *right*, so false-positive suppression cannot distinguish a correct rejection from a wrong one. |

## Phase 3 — reporting

| # | Item | Status | Notes |
|---|---|---|---|
| 3.1 | Cohort aggregation | **todo** | Scores are per-run only; nothing rolls ten runs into one report. Milestone 4 requires "per-case **and cohort-level** incremental-value reports". |
| 3.2 | Join cost and duration to findings | **todo** | `cost_usd` lives in `run.json`, findings in `evaluation/`, and nothing connects them. "Cost per incremental benefit" — one of the five required axes — is structurally absent rather than merely unimplemented. Depends on 0.1. |

## Milestone 4 exit criterion, mapped

> results distinguish review discovery, substantiation, verification,
> false-positive suppression, and cost per incremental benefit

| Axis | State |
|---|---|
| Discovery | Count only (`findings_discovered`). No notion of whether a finding is correct — needs 2.4 |
| Substantiation | Present: reasoner-2 dispositions |
| Verification | Present: reasoner-3 dispositions |
| False-positive suppression | Mechanically present, but cannot tell a correct rejection from a wrong one — needs 2.4 |
| Cost per incremental benefit | Absent — needs 0.1 and 3.2 |

## Deliberately not in scope

- **Codex.** Roadmap, not MVP. No image built, and the adapter reports no
  usage, so its budgets are unenforced except on wall clock.
- **A post-cutoff control arm for contamination.** Closed rather than open: no
  correction in the cohort postdates the training cutoff and the whole dataset
  is 2024 PRs, so no reselection within it can help. Only a fresh 2026 collect
  would, and that is miner work and unscoped. See
  [`contamination-probe-pilot-v1.md`](contamination-probe-pilot-v1.md).
