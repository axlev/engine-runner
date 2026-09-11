# Reading a run's results

What each file in a sealed run says, and how to trace one finding through all
three stages. Written for someone inspecting a cohort by hand — which is
backlog item 1.4, and currently the only way to judge whether the pipeline
earned its cost, because no oracle-based scoring exists yet.

## The tree

```
build/results/<run-id>/
├── run.json                  identity, status, fingerprints, per-stage summary
├── boundary-validation.json  per-rule pass/fail for the input bundle
├── checksums.sha256          covers every other file here
├── events.jsonl
├── stages/reasoner-{1,2,3}/
│   ├── prompt.md             the EXACT prompt that stage was given
│   ├── request.json          the exact request sent to the adapter
│   ├── review-{a,b,c}.json   the accepted output
│   └── telemetry.json        every attempt, including failed ones
└── evaluation/
    ├── scores.json           disposition counts
    └── findings.json         one row per finding, with its final status
```

## Which PR is this?

Deliberately not answerable from the result tree. A run id is `frr-` plus the
first eight characters of an opaque case id, and the case id is a SHA-256 of
repository, PR number and cutoff — so nothing in a sealed result resolves to a
PR number or an outcome. That is the point: the tree can be handled freely
without leaking which change is under review.

The mapping lives on the evaluator side only, in the cohort document's PR↔case
table (`tmp/frr-pilot-v1-cohort.md` for the FRR pilot).

## Start here: the whole cohort at a glance

```bash
for d in build/results/frr-*/; do python3 -c "
import json
r='$d'
d=json.load(open(r+'run.json')); s=json.load(open(r+'evaluation/scores.json'))
print('{:<14} {:<10} \${:<5.2f} found={} | B c{} n{} r{} i{} | C c{} n{} r{} i{} new={}'.format(
 d['run_id'], d['status'], sum(x['usage'].get('cost_usd',0) for x in d['stages']),
 s['findings_discovered'],
 s['confirmed_by_b'], s['narrowed_by_b'], s['rejected_by_b'], s['inconclusive_by_b'],
 s['confirmed_by_c'], s['narrowed_by_c'], s['rejected_by_c'], s['inconclusive_by_c'],
 s['new_findings_by_c']))"; done
```

`c`/`n`/`r`/`i` are confirmed, narrowed, rejected, inconclusive.

## One case, finding by finding

```bash
R=build/results/frr-83d945a6
jq -r '.findings[] | "[\(.final_status)] sev=\(.severity) conf=\(.confidence) B=\(.b_disposition) C=\(.c_disposition)\n    \(.title)"' $R/evaluation/findings.json
```

`final_status` prefers stage C's disposition, falling back to B's.

## What each stage actually says

The counts are a summary; the reasoning is the substance. Read it in order.

**reasoner-1 — discovery.** What was claimed, and on what evidence.

```bash
jq -r '.findings[] | "\(.id) [\(.severity) conf=\(.confidence)] \(.title)\n  \(.description)\n  evidence: \(.evidence[] | "\(.file):\(.start_line)-\(.end_line)")"' $R/stages/reasoner-1/review-a.json
```

**reasoner-2 — evidence.** Assessments carry `causal_chain` (the steps from
cited line to wrong outcome), plus `narrowed_description` when narrowed,
`rejection_reason` when rejected, and `proposed_test` — the smallest falsifier.

```bash
jq -r '.assessments[] | "\(.finding_id) -> \(.disposition)\n  \(.rejection_reason // .narrowed_description // "")\n  chain: \(.causal_chain[] | "\(.file):\(.start_line)")"' $R/stages/reasoner-2/review-b.json
```

**reasoner-3 — adversarial verification.** `rationale` must say what
falsification was actually attempted; `falsification_attempt` cites what it
checked. `new_findings` are things it noticed while verifying — permitted only
when `discovered_via` names the assessment that led there.

```bash
jq -r '.verdicts[] | "\(.finding_id) -> \(.disposition)\n  \(.rationale)"' $R/stages/reasoner-3/review-c.json
jq -r '.new_findings[]? | "NEW \(.title)\n  via: \(.discovered_via)"' $R/stages/reasoner-3/review-c.json
```

## What to look for

The question this inspection answers is **whether stages 2 and 3 earned their
cost** — roughly 40% of it. The single-pass baseline is already in every run:
`review-a.json` is what one reviewer produces in one pass, and `review-c.json`
is the same review after evidence and adversarial verification. The increment
is A → C.

- Did anything **change** between A and C, or did all three stages agree?
- Where a finding was `NARROWED` or `REJECTED`, does the causal chain give a
  real reason, and is that reason true of the code?
- Where C says `CONFIRMED`, does `rationale` describe a falsification that was
  actually attempted, or is it a restatement of the finding?
- Do `new_findings` name a `discovered_via` that justifies them, or is stage 3
  doing a second discovery pass it was told not to do?

Three stages agreeing with no new reasoning is the pipeline not earning its
cost. Dispositions changing for stated, checkable reasons is it working.

## Run-to-run variance is large

`case-83d945a6dd4e5785` was run twice on sonnet under the same protocol. The
first run produced **1 finding**, rejected by both later stages. The cohort run
produced **3 findings** — one narrowed, two confirmed. Same bundle, same arm,
same prompts.

So a single run is a sample, not a measurement, and differences between two
cases may be noise rather than signal. Read the cohort for whether the A→C
increment *exists and is reasoned*, not for precise counts. Anything that turns
on a difference of one or two findings needs repeats before it can be believed.

## What this cannot tell you

**Whether any finding is correct.** `internal/evaluation` computes only
reviewer-side metrics — what stages 2 and 3 did with stage 1's findings. It
never reads the oracle, so a correct rejection and a wrong one are
indistinguishable in these files. Ground truth exists per case in the miner's
`case-*-evaluator-only/correlated-report.json`, and nothing in this engine
reads it yet (backlog Phase 2).

So this pass judges whether the increment looks *real*, not whether it is
*right*.

## Diagnosing a failed run

`status` is `failed` or `invalidated` rather than `completed`:

```bash
jq -r '.status, .failure_reason' $R/run.json
jq -r '.attempts[] | "attempt \(.attempt) exit=\(.result.exit_code) \(.error // "ok")"' $R/stages/reasoner-1/telemetry.json
jq -r '.violations[]? | "\(.rule) \(.path): \(.detail)"' $R/boundary-validation.json
```

`telemetry.json` keeps every attempt including failed ones, with the adapter's
stderr folded into the error — a failed run is still a run worth reading.
