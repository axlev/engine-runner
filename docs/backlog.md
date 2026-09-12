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
| 1.2 | Probe the 3 replacement negatives | **Verified** | All three probed on opus: `NO RECOGNITION`, with the Heartbleed false-negative guard firing in the same batch so the nulls are reports of absence rather than refusals. The cohort is now fully covered — 7 positives and 3 negatives. |
| 1.3 | Run 10 cases | **Integrated** | **All ten completed**, $20.64, 1.8h of stage time, 19 findings. Results in [`cohort-pilot-v1-results.md`](cohort-pilot-v1-results.md); sealed trees stay in `build/` and are never committed. Stalled on OAuth session limits three times and resumed across resets, as expected for a rate-limited subscription token. |
| 1.4 | Manual inspection | **Integrated** | Done by hand across all three arms. Reasoner-3 moved **0 of 19 disposition labels** on sonnet, but **1 of 19** under the adopted label-or-claim rule (`frr-fd8ee329` f1, mechanism replaced with the verdict intact). The earlier claim that its contribution was "provably nil without any oracle" is **retracted** — see [`cohort-pilot-v1-results.md`](cohort-pilot-v1-results.md). The rubber-stamping question is also answered and the answer is no: both arms construct genuine multi-branch falsifications. |
| 1.5 | Opus arms | **Integrated** | `opus` 9/10 at $86.91, 41 findings. `opus-wide` (grep+glob, raised caps) 10/10 at $80.20, 49 stage-1 findings. Movement rates indistinguishable, composition completely different. Full writeup in [`cohort-pilot-v1-results.md`](cohort-pilot-v1-results.md). |
| 1.6 | Fable arm | **Blocked** | `configs/agents/fable.yaml` exists but its stated rationale — that "a weaker model can only re-read stage 2 and agree" — is **refuted** by the completed arms. Fix the comment before running, or the arm tests a dead premise. Cost is uncalibrated; the file's own rule is one case before a cohort. Blocked behind 2.4: a third capability point measured with a metric that cannot see improvement buys nothing. |

## Surfaced by the cohort

Items that did not exist as questions until real cases ran.

| # | Item | Status | Notes |
|---|---|---|---|
| 1.7 | **Reviewers never examine tests** | **Needs a decision** | Granting `Grep`/`Glob` made `tests/topotests` reachable — it is mounted in every bundle, 235-338 entries — and the wide arm **never went there**. Narrow `322abe6a` named the blocker three times ("could not enumerate callers under tests/topotests or tools/"); wide, with the tools, has zero references to tests and never raised the consumer-breakage finding at all. So this is a **discovery** gap, not a tooling one, and tools cannot close it. Options: direct stage 1 at in-tree consumers and test coverage in the prompt, or accept that consumer-breakage findings surface only by luck. Prompt changes are fingerprint-affecting, so this needs a new arm, not a patch. |
| 1.8 | **`max_cost_usd` kills stages it should only be bounding** | **Needs a decision** | The cap is doing two conflicting jobs. Runaway protection wants it very high; quota conservation wants it low. Quota conservation is **already handled** by `MAX_PER_BURST` in `run-cohort.sh`, which stops cleanly between cases with everything sealed — so the cap does not need to do it, and doing it costs real work. Measured damage on the narrow opus arm: `c21d519d` lost (both r2 attempts killed, $13.72 for no evidence) and `de6d5d30` silently compromised (first r2 attempt killed at $5.31, retry sealed as clean). ~$19 of pure waste plus one contaminated result caught by accident. Root cause: `opus.yaml`'s budgets are calibrated against **sonnet** measurements ($1.20/$0.49/$0.30) while opus spends 3-4x that, so nominally generous caps sat at 1-2x actual and opus ran at 88-107% of them. The bound also cannot be made soft — the vendor enforces it before the fact — so the only lever is the number. Proposal: set `max_cost_usd` and `max_wall_clock_seconds` as **circuit breakers** at ~5-10x expected per-stage spend, sized against **expected finding count** (the cohort's one reliable cost predictor — an 8-finding case costs roughly twice a 4-finding one), and leave the token and tool-call bounds as the tight detection-only signals they already are. |

| 1.9 | **Stage 3 may agree with stage 2 because it *is* stage 2** | **Needs a decision** | In every arm run so far, reasoner-3 and reasoner-2 are **the same model** — same weights, same priors, same training. Given the same evidence a model reaches the same conclusion, so stage 3 is not verifying stage 2, it is stage 2 run twice. **This is the most parsimonious remaining explanation of the project's central null**, and it is the only one never tested: capability is refuted (both arms construct genuine multi-branch falsifications — `c21d519d` f1 traces five construction paths), budget starvation is refuted and measured inverted (moved cases −1.7pp B-vs-C squeeze against unmoved +16.2pp), and metric blindness is confirmed but only partial. Existing evidence fits self-agreement well: sonnet's stage 3 cites files stage 2 never touched **and then confirms stage 2** in 15-18 of 18 verdicts — independent work converging on its own prior answer; the one replication found (`c96a113c`) was the same move by the same mechanism; and both direct contradictions (`83d945a6`, `de6d5d30` f6/f8) occurred **between arms**, where model or tooling differed, never within one. If this holds, the pipeline pays ~27% of spend for a stage structurally near-incapable of disagreeing. **No code needed** — `internal/orchestrator/agents.go:100-104` already supports per-stage `adapter`/`model`/`reasoning_level`/`budget`/`tools` overrides, `loadAgents` applies them, and `AgentConfigHashes` is keyed per stage, so a mixed arm is fingerprint-clean today. **Design:** the decisive variant is a *weaker but independent* stage 3 — sonnet reasoner-3 on the opus arm. A mixed arm otherwise changes two things at once (stage 3's model *and* its independence from stage 2), so a jump could be attributed to the new model being better at falsification; if a **weaker** independent stage 3 overturns more than a **stronger** identical one, independence is doing the work and capability is not. Fable as an independent reasoner-3 is the strong-and-independent variant. Fully independent version is reasoner-3 on `codex` — per-stage `adapter` is overridable too. **Supersedes 1.6 in value:** a full fable arm answers where fable sits on the capability curve, which is blocked behind having a working metric anyway; this answers whether staging's null is an artifact of self-verification, which is the project's actual thesis. Either outcome is publishable. |

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
