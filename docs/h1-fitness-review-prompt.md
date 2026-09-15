# Review prompt: fitness of miner + engine-runner for H1

*Amended 2026-09-15 from the original. Every change traces to a dated decision
in `miner/docs/h1-pre-registration.md`. Dispatch rule unchanged: paste the
shared preamble plus **one** role section to the corresponding coding agent.
Do not paste the other role's section. The oracle-blindness rules in each
AGENTS.md still apply, and coder-engine-runner must not receive
`docs/frr-pilot-v1-cohort.md` or any per-case defect description.*

---

## Shared preamble (paste into both)

You are reviewing this repository against a new, narrower goal that supersedes
the staged-review question in `system-design.md` §15 for the purpose of this
review. Do not evaluate the repo against its original milestones; evaluate it
against H1 below.

### Hypothesis 1 (H1)

On a curated set of C/C++ systems-software changes where roughly half later
caused a **system-level escape** — a defect that passed review and CI and
surfaced in system test, warm-restart/upgrade, scale, interop, or the field — a
pipeline of **LLM reasoning with domain-directed discovery**, given only
review-time information, classifies each change as `RISKY` or `CLEAN` with a
stated system-level reason, at precision and recall materially above (a) a
generic single-pass LLM reviewer with no domain content and (b) a zero-LLM
history baseline that flags by subsystem defect density and churn.

**H1 is narrowed to the LLM component (decision 2026-09-15).** Deterministic
analysis and a domain-invariant library are **not** part of the treatment. They
are dropped from H1's definition, not scoped out of this review — deferred
pending the H1 result, with no pilot evidence that either moves reason-match.
Do not propose them, cost them, or list them as gaps.

**"Materially above" is pre-registered as +15 points over each baseline** on
precision, recall, and reason-match, on the same frozen cases, paired,
significance by **PR-level exact permutation** — the finding-level Fisher test
was retracted for clustering. The cohort is **40 cases, 20 positive / 20
negative, matched 1:1.** At that size, +15 on precision (6 cases) can reach
p ≈ 0.03; +15 on recall or reason-match (3 cases) cannot, and is reported as
"consistent with H1, underpowered at this cohort size." Expansion to 80 is
decided after the 40-case result. **Primary comparison if only one reaches
threshold: treatment vs baseline (a) on reason-match.** Precision is reported
as conditional on the 1:1 ratio.

**Reason match** means the stated mechanism corresponds to what the later fix
actually repaired.

**Contamination standard is the per-case probe, not recency.** The recency
route is structurally unavailable (0 of 1,667 candidates have a post-cutoff
fix). Every case is probed on each arm's model. **The treatment arm must run
on a model that passes the Heartbleed false-negative guard**; opus does,
sonnet and haiku do not, Fable and Astra are untested.

H1 is per-change, not per-finding. It has three arms, two of which are
baselines. It needs failure-class labels so recall can be reported by class,
because the expected outcome is heterogeneous: ordering, config-interaction,
error-path and cross-module-invariant defects may be diff-predictable;
timing-dependent races and resource exhaustion at scale probably are not.

### What the review must produce

Work only from current source, tests, schemas and docs in this checkout. Cite
file and line for every claim. Use the repo's status vocabulary exactly; never
round up.

1. **Gap table.** For each H1 requirement listed in your role section: Present
   / Partially implemented / Implemented / Verified / Integrated / Absent, with
   the evidence path. If a doc claims a state the code does not support, say
   so and cite both.
2. **Contradictions in AGENTS.md.** List every instruction that contradicts the
   code, another instruction in the same file, or a dated decision in its own
   log. Quote each side. Propose the corrected text. Do not delete boundary
   rules to resolve a contradiction — resolve it in favour of the more recent
   dated decision, and say which.
3. **Stale narrative.** List statements in AGENTS.md, README.md or docs/ that
   describe a past state as current. For each, name the document that is
   actually live for that fact.
4. **Migration plan.** Ordered list of the smallest changes that move this repo
   from its current state to H1-ready, each with: effort in days, whether it is
   fingerprint-affecting or contract-affecting (and therefore needs a new
   protocol/schema version rather than an edit), and what it unblocks. Order by
   validity of the H1 test first, cost second.
5. **Three highest-impact changes.** Name them. For each say what the H1
   result would be worth without it.
6. **What must not change.** Boundary rules, invariants and contract elements
   that H1 work must preserve. Say why for each in one sentence. **The
   pre-registered thresholds and the 1:1 ratio are on this list.**
7. **What you did not verify.** Explicitly.

Do not write code in this pass. Do not touch the frozen pilot-v1 protocol or
the bundle wire contract. If a plan item requires either, mark it *Needs a
decision* and stop there.

**Two stale-plan traps, already sprung once.** `PLAN.md` in the miner repo
describes a 180-day correlator window (`PLAN.md:215`) and a `--require-signals`
flag (`PLAN.md:346`). Neither exists in code. Strong and medium signals are
searched to `observation_end`; only the weak file check is bounded at 90 days.
The real flag is `--require-fix-signal` and it is OR. If you find yourself
citing `PLAN.md` for anything downstream of the pipeline record, stop and cite
the code instead.

---

## Role section: coder-miner (paste only in miner)

You have no oracle access beyond what AGENTS.md grants. `docs/frr-pilot-v1-cohort.md`
is evaluator material; you may cite that it exists and its selection method,
not its per-case content. **`docs/h1-pre-registration.md` is the authoritative
H1 statement for this repo; read it first.**

Since the original draft of this prompt, cohort construction has landed
(`cohort-export`, exposure guard, reason-label schema, cohort manifest —
uncommitted at the time of writing, see `docs/h1-cohort-construction-report.md`).
Several items below therefore ask you to **verify what was built against the
pre-registration** rather than propose it.

### H1 requirements to assess

**Labels and controls**

- **M1. Positive label provenance.** Enumerate every corrective-evidence
  pattern in `internal/correlator` (regex families, tiers, windows — and note
  which windows actually exist in code). For each, state the plausible
  false-attribution rate for the introducing commit and how a wrong `Fixes:`
  pointer would propagate into an oracle bundle.
- **M2. Negative-control construction — verify, don't propose.** `cohort-export`
  now exists. Verify it against the pre-registration's *Cohort design*: zero
  signals at every tier including weak; 1:1 matching on stateful band,
  subsystem, and size band; first-class mode not inverted filter; exposure
  floor applied to both classes. Then assess three known limitations and say
  whether each is acceptable for a 40-case cohort or needs fixing first:
  (i) matching is greedy, not optimal; (ii) the score band is the 3-level
  category, so within-LOW spread can reach 0.30; (iii) **weak signals require a
  Git handle at correlate time, and a record correlated without one looks
  zero-signal when it should not — undetectable from the record.** For (iii),
  propose the cheapest check that a cache was correlated with the handle
  present, and where it belongs.
- **M3. Failure-class taxonomy — verify, don't propose.** `schemas/reason-label.schema.json`
  and `schemas/reason-label-categories.schema.json` now exist. Verify they are
  evaluator-only by construction, that the miner never populates them, and
  that `verdict` is distinct from the sampler's label (a disagreement is a
  finding about the sampler, not something to reconcile). Assess whether
  building the closed category list during the first ten labels, rather than
  upfront, risks a list that cannot be applied consistently to cases 11–40.
- **M4. System-level filter.** H1 wants positives whose escape was
  system-level, not lint-class. Assess whether any current signal (labels,
  files touched, fix-commit content, topotest changes in the fix) can
  approximate this, and propose the cheapest selection heuristic plus the
  human-review step it needs.

**Freeze and leakage**

- **M5.** Re-verify the review-time freeze against `docs/export-contract.md`
  §7 for the `reviewer/` bundle only: diff at cutoff, snapshot of
  `cutoff_commit`, metadata field allowlist, commit-message admission rule. For
  open risks 6, 7, 9, 10: state whether each can leak outcome information into
  `reviewer/`, or only into evaluator material.
- **M6. PR-body leakage.** Description is admitted when unchanged since
  cutoff. A body that says "fixes crash on warm reboot" at review time is
  legitimate review-time information, but a body edited after merge to describe
  the regression is not. Confirm the positive-provenance rule handles
  edited-after-merge bodies by omission, cite the code, and state what is
  unverifiable.
- **M7. Contamination — probe protocol, not a post-cutoff collect.** Specify
  the per-case probe protocol for 40 cases: which probes (A: symptom→recall,
  B: real diff→recognition, plus the Heartbleed false-negative guard and the
  fabricated-defect false-positive guard), on which models, what per-case
  status field the result populates and where it lives (evaluator-only), and
  the cost. State which candidate treatment models currently pass the
  Heartbleed guard and what running the guard on a new model costs. If a
  2026 collect is proposed, it is for **scale**, and must say so.

**Baselines**

- **M8. History baseline data.** H1's zero-LLM baseline needs per-subsystem
  defect density and churn as of each case's cutoff. State what the miner
  already computes (`internal/heuristics`, `internal/cohort`) that could
  serve, what would be new, and where the boundary sits so that baseline
  features are computed only from pre-cutoff data.

**Scale**

- **M9. Cohort size.** Cost and steps to go from the 10-case frozen pilot to
  40 verified bundles via `cohort-export` → `prospective-export` →
  `cohort-verify --bench-repo`: wall time, storage (~75 MB/bundle), the
  system-level human read from M4, and the negative-control read the
  pre-registration implies.

### AGENTS.md items specific to this repo

- The "Architecture boundaries" section contains one bullet stating the
  boundary-rule mirror was deleted on 2026-09-10 and must not be reintroduced,
  and a later bullet stating the rules "are mirrored across two repos" and must
  be hand-maintained. Reconcile in favour of the 2026-09-10 decision.
- Three bullets appear twice verbatim (fail-closed; closed schemas; frozen wire
  contract). Deduplicate.
- Add an H1 statement that points to `docs/h1-pre-registration.md` as the
  target, so an agent in this repo optimises for change-level discrimination
  with class labels, not for the original staged-review milestones. Propose
  the text.

---

## Role section: coder-engine-runner (paste only in engine-runner)

You have no oracle access. Do not read `/home/alex/data`. Do not request
`docs/frr-pilot-v1-cohort.md`. The H1 statement in the preamble is complete for
your purposes.

### H1 requirements to assess

**Change-level verdict**

- **E1.** There is no case-level output. Specify a review-verdict (or extension
  to review-a/b) schema: verdict `RISKY|CLEAN`, calibrated probability, and an
  evidence chain — affected subsystem → invariant at risk → failure mode →
  reachable runtime scenario → existing test coverage found or not found →
  missing evidence → recommended validation. State where in the pipeline it
  is produced and whether it is fingerprint-affecting (it is).
- **E2. Finding-to-verdict mapping.** `docs/cohort-pilot-v1-results.md`
  documents two spurious results caused by inheriting a metric from whichever
  field was easiest to diff. Propose the explicit mapping rule from findings
  (severity, confidence, disposition) to a case verdict, and the argument for
  why a worse stage cannot improve it.

**Arms**

- **E3. Baseline arm (a), generic reviewer.** A single-stage arm with a prompt
  containing no domain content, no invariant library, no staged handoff. State
  exactly which existing pieces (`configs/agents/*.yaml`, per-stage overrides
  in `internal/orchestrator/agents.go`, protocol manifest) produce it as a new
  protocol version without touching pilot-v1.
- **E4. Baseline arm (b), history.** A zero-LLM arm that emits a case verdict
  from pre-cutoff subsystem defect density and churn. The engine has no
  non-LLM adapter. State whether the fixture adapter, a new adapter, or an
  evaluator-side computation is the cheapest correct home, and how it stays
  inside the fingerprint chain.
- **E5. Treatment arm — this IS the treatment.** With H1 narrowed to the LLM
  component, system-aware discovery is the entire treatment. Assess
  `prompts/pilot-v1/reasoner-1.md` against H1: it optimises for classical
  mechanism findings and never directs the reviewer at in-tree consumers,
  tests (`tests/topotests` was mounted in every bundle and never read —
  backlog 1.7), configuration surfaces, or restart paths. Propose the
  discovery prompt delta and the tool grant it needs. Mark it
  fingerprint-affecting. State which model it runs on and confirm that model
  passes the Heartbleed guard.
- **E6. Stage 3.** Judge results show it changed 4 of 77 dispositions and
  rescued 0 of 3 wrongly rejected true findings. For H1, state whether
  dropping stage 3 (A→B only) changes anything the hypothesis test needs, and
  what it saves per case.

**Scoring**

- **E7. Case-level metrics.** Precision, recall and reason-match at case
  granularity, with per-arm and per-failure-class breakdown, plus a PR-level
  exact permutation test. The pre-registered thresholds are +15 points and the
  statistical ceiling at n=40 is stated in the preamble; the scorer must emit
  the "consistent, underpowered" reading when recall or reason-match hits +15
  at that size, not a bare figure. State what `internal/judge`,
  `internal/evaluation` and `internal/results` provide today and what is
  Absent. Cohort aggregation (backlog 3.1) is a dependency.
- **E8. Reason match.** `internal/judge` mechanism agreement is the right
  primitive. State what it needs to accept a case-level evidence chain instead
  of a finding, and whether the two-call ordering defence survives.
- **E9. Contamination guard at run time.** The boundary validator scans for
  outcome language; nothing detects a reviewer naming the fixing commit, the
  CVE, or quoting post-merge discussion. Propose the cheapest post-hoc scan
  over sealed `review-*.json` and its false-positive risk.
- **E10. Judge independence.** Two arms and the judge are in-family (backlog
  2.5, blocked). State the minimum that makes cross-vendor judging of case
  verdicts possible, and whether it is required before any H1 claim is
  publishable.

**Harness**

- **E11.** The `--tools Read` defence was demonstrably binding
  (`zclient.h:764`). For H1 arms, state the tool grant, and whether a
  planted-CLAUDE.md style test exists for Grep/Glob.
- **E12. Budgets.** `max_cost_usd` killed one case and silently compromised
  another. State the circuit-breaker sizing rule for H1 arms and how a
  killed-then-retried attempt is surfaced in case-level results rather than
  hidden behind `attempts: 2`.

### AGENTS.md items specific to this repo

- The "Current state" section states no real mined case has run and that the
  miner exports a flat layout the engine cannot ingest. Both are contradicted
  by the "Cross-repo contract" section and by `docs/backlog.md`. Reduce
  "Current state" to what is still true plus pointers to `docs/backlog.md`.
- Add an H1 statement and the arm/metric definitions above as the target, so
  an agent does not optimise for staged-review increment.
- Confirm the "Fixture case language" rule (FRR idiom, never FRR-derived code)
  is sufficient for H1 synthetic cases that need system-level defects, or
  propose what a synthetic restart/ordering fixture would need beyond the
  sanitizer-trace ground truth rule.
