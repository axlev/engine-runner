# Backlog

Working state for `engine-runner`: what is left, in the order it should be
done, and why that order. Update the Status column in place as items move —
this file is meant to be edited, not appended to.

Status uses the closed vocabulary defined in [`../AGENTS.md`](../AGENTS.md)
("Status vocabulary"): `Absent`, `Present`, `Implemented`, `Verified`,
`Integrated`, `Shipped`, plus `Blocked` and `Needs a decision`. The terms are
ordered by strength and must not be rounded up — a thing that compiles is
`Present`, not `Implemented`; a thing whose tests were written but not run is
`Implemented`, not `Verified`; a thing verified on fixtures is not
`Integrated`. `coder-miner` uses the same vocabulary, so a status word means
the same thing in both repositories.

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
| 0.1 | Timing capture | **Shipped, Verified** (`62f4fa1`) | Stage duration is never derived — two timestamps reach `run.json` and nothing subtracts them. Also keep claude's `duration_ms` / `duration_api_ms`, currently parsed away; the API split separates model time from our overhead, which is what tells you whether a 48 MB snapshot costs in the container or at the API. Do this first so 0.2 measures something. |
| 0.2 | One FRR case, end to end, on sonnet | **Integrated** | **Completed on `case-83d945a6dd4e5785`** (+2/-0 over 76 MB / 7,544 files): **$1.99, 668s**. reasoner-1 dominates — 76% of input, 60% of cost. Engine overhead is **2.1%** (14s of 668s), so the model, not the machinery, is the cost. Extrapolates to roughly **$20 and ~2h** for a 10-case cohort on sonnet. |
| 0.3 | Recalibrate `configs/agents/*` | **Shipped, Verified** | The 400,000 input bound from 7-file fixtures **failed a stage that had produced a valid review**. Now 8,000,000 / 100,000 with the reasoning recorded in each arm file: `max_cost_usd` is the real control because it is the only bound a vendor enforces *before* the fact; token bounds are detection-only and set to catch genuine runaway, nothing tighter. Also note `max_input_tokens` sums cache reads, which are re-billed every turn, so it tracks turn count more than context size — two runs of the same case used 1.2M and 2.2M. |
| 0.4 | Per-attempt copy strategy | **Verified — no change needed** | Measured rather than assumed: a full copy of the 76 MB / 7,544-file FRR snapshot takes **0.50s**, so six copies per case cost ~3s against stages that run for minutes. Not worth optimising, and leaving it alone avoids the symlink-preservation risk a shared read-only mount would have introduced — dereferencing breaks diff/snapshot correspondence, which is why the miner rejected it too. Verified on a real bundle: all 77 symlinks arrive in the stage input as symlinks. |

## Phase 1 — the cohort

| # | Item | Status | Notes |
|---|---|---|---|
| 1.1 | Verify the reasoner-3 disposition fix | **Verified** | Verified on the real FRR case rather than the synthetic one: reasoner-2 rejected the single finding, reasoner-3 tried three angles to overturn that rejection, failed, and wrote **`REJECTED`** — giving `rejected_by_c: 1`. Before `6fcda0e` it would have written `CONFIRMED` meaning "B's verdict survived" and been scored as a real defect. |
| 1.2 | Probe the 3 replacement negatives | **Blocked** | `3a74a3a2`, `9ba8fca7`, `c96a113c` replaced the negatives after the contamination probe ran. Same quota block. Gap recorded in [`contamination-probe-pilot-v1.md`](contamination-probe-pilot-v1.md). |
| 1.3 | Run 10 cases | **Absent** | 30 stages. **Plan around OAuth session limits** — a subscription token is rate-limited, not metered, so a cohort will stall partway and need resuming across reset windows. An API key removes that failure mode entirely. |
| 1.4 | Manual inspection | **Absent** | ~40 findings; tractable by hand. This is the input to Phase 2's design, not a formality. |

## Phase 2 — the judge

Nothing here exists yet. There is no oracle ingest anywhere in the engine, and
no oracle bundle contract — only
[`prospective-bundle-contract.md`](prospective-bundle-contract.md). The miner
already emits `case-*-evaluator-only/` directories that this side cannot read.

| # | Item | Status | Notes |
|---|---|---|---|
| 2.1 | Oracle bundle contract | **Absent** | The counterpart to the prospective contract. Must state what the evaluator receives and, as that document does, why. |
| 2.2 | Oracle ingest | **Absent** | §6.5: an LLM judge **must run as a separate evaluator-only agent and must never share state or a filesystem with reasoners 1-3.** That is a second isolated adapter path, not a function call. Today's deterministic evaluator needs no such isolation only because it has no oracle access to protect. |
| 2.3 | **Matching semantics** | **Needs a decision** | "Did the reviewer find *this* defect?" — same file, same function, same mechanism? **This determines what the benchmark claims** and is the user's call, not the engine's. Specify it from Phase 1 output rather than in advance. |
| 2.4 | Precision / recall, and correct-vs-incorrect rejection | **Absent** | Today's evaluator computes reviewer-side metrics only: what stages 2 and 3 did with stage 1's findings. It cannot say whether any finding is *right*, so false-positive suppression cannot distinguish a correct rejection from a wrong one. |

## Phase 3 — reporting

| # | Item | Status | Notes |
|---|---|---|---|
| 3.1 | Cohort aggregation | **Absent** | Scores are per-run only; nothing rolls ten runs into one report. Milestone 4 requires "per-case **and cohort-level** incremental-value reports". |
| 3.2 | Join cost and duration to findings | **Absent** | `cost_usd` lives in `run.json`, findings in `evaluation/`, and nothing connects them. "Cost per incremental benefit" — one of the five required axes — is structurally absent rather than merely unimplemented. Depends on 0.1. |

## Milestone 4 exit criterion, mapped

> results distinguish review discovery, substantiation, verification,
> false-positive suppression, and cost per incremental benefit

| Axis | Status | Why |
|---|---|---|
| Discovery | `Present` | `findings_discovered` is a count. Nothing says whether a finding is correct — needs 2.4 |
| Substantiation | `Integrated` | reasoner-2 dispositions, exercised on a real case |
| Verification | `Integrated` | reasoner-3 dispositions, exercised on a real case |
| False-positive suppression | `Present` | The mechanism works and was seen rejecting on a real case, but a correct rejection is indistinguishable from a wrong one — needs 2.4 |
| Cost per incremental benefit | `Absent` | Cost and duration are recorded but never joined to findings — needs 3.2 |

Two of five axes are `Absent` or `Present` only, and both gaps close in 2.4
and 3.2. Nothing here is `Verified` at the cohort level, because no cohort has
run.

## Deliberately not in scope

- **Codex.** Roadmap, not MVP. No image built, and the adapter reports no
  usage, so its budgets are unenforced except on wall clock.
- **A post-cutoff control arm for contamination.** Closed rather than open: no
  correction in the cohort postdates the training cutoff and the whole dataset
  is 2024 PRs, so no reselection within it can help. Only a fresh 2026 collect
  would, and that is miner work and unscoped. See
  [`contamination-probe-pilot-v1.md`](contamination-probe-pilot-v1.md).
