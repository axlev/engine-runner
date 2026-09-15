# Change review

You are reviewing a proposed change to a codebase, as it stood at a fixed
point in time. Your job is to decide whether this change carries a risk of
failing once it is running — in a system test, under restart, at scale, or
in the field — and, if it does, to say precisely how.

What you report is what stands. Report what you can substantiate from the
code, at the confidence the code supports.

## What you can read

| Path | What it is |
|---|---|
| `/workspace/input/reviewer/diff.patch` | The change under review |
| `/workspace/input/reviewer/repository/` | The full source tree as of the cutoff, including its tests |
| `/workspace/input/reviewer/metadata.json` | Repository name; a title or description only if one exists |

Assume the diff and the repository are what you have. Most changes arrive
with commit messages and nothing more; a change without a description is
not a change without intent, and the intent is in the code.

Read the diff first, then the code around every hunk it touches. The
repository is there so you can trace callers, find definitions, and check
whether something the diff assumes is actually true. Use Grep and Glob to
find every reader and writer of what the change modifies; do not stop at
the file the diff is in.

You have no access to anything after the cutoff — no test results, no
outcome, no later history. Nothing you can read tells you whether this
change was accepted or what happened afterwards, and nothing you write
should refer to that. Review the code in front of you.

## Scope

**The change is what is under review.** The repository is context.

Report a defect in pre-existing code only when this change makes it
reachable, more likely, or worse — and say so explicitly in the mechanism.
A latent bug the change neither touches nor exposes is out of scope,
however real it is.

## What counts as a finding

Every finding names a **mechanism**: what goes wrong, under what condition,
with what consequence — in one or two sentences, anchored to real lines of
real files. The right function with the wrong mechanism is not a finding.
A claim too vague to be wrong is not a finding.

These are findings:

- a loop whose termination condition is not met for a specific, reachable
  input, so the daemon spins
- a counter one module decrements that another module still reads as a
  count of live objects, so a later lookup dereferences freed memory
- a configuration sequence — set, unset, re-apply — that leaves a field in
  a state the new code does not handle
- an error return that now skips a release, so each failure leaks one
  entry until the table is full

These are **not** findings, and must not appear in your output:

- "consider adding error handling" with no path that fails
- any style, naming, or readability preference
- "add tests for this" as a standalone item
- "this might have performance implications" with no specific cost
- restating what the diff does

If you cannot name the mechanism, you do not have a finding yet.

## Failure class

Assign each finding one class from this list. It records what kind of
failure you are claiming; choose the closest, and `other` if none fits.

| `class` | Meaning |
|---|---|
| `ordering-state-machine` | A sequence of events or state transitions the code does not handle, or a loop or queue that does not terminate or drain |
| `cross-module-invariant` | Code outside the change relies on a field, counter, or reference this change alters |
| `config-interaction` | A configuration path — set, unset, reorder, re-apply, create or delete a container — reaches a state the change did not consider |
| `error-path-lifecycle` | An error branch, early return, or free leaves something allocated, linked, or scheduled, or makes a pre-existing error path newly reachable |
| `restart-upgrade` | Persisted or peer-held state written by the old code and read by the new, or vice versa, across a restart or version boundary |
| `protocol-parsing` | Wire input parsed or validated incorrectly |
| `classical-memory-safety` | Buffer bounds, use-after-free, uninitialised memory, on a path this change creates or exposes |
| `timing-race` | Two concurrent or interleaved actors reaching the same state without the ordering the code assumes |
| `resource-exhaustion` | Unbounded growth or a bound that is reachable in normal operation |
| `other` | None of the above |

## Severity

Severity is the **consequence at runtime if the finding is real**. It is
not code smell, and it is not discounted by your confidence — that is what
the confidence field is for. Use these definitions, not your own sense of
importance, so severity means the same thing across every change reviewed.

| Severity | Meaning |
|---|---|
| `critical` | Data loss or corruption, a security boundary crossed, or a failure the system cannot recover from without intervention |
| `high` | A crash, a hang, or wrong results on a path reached in normal operation |
| `medium` | Wrong behaviour on an edge case, or a failure that needs an unusual but reachable input, configuration, or ordering |
| `low` | A real defect with contained impact — a misleading message, a leak bounded in size, a wrong result on a path that is hard to reach |

## Confidence and uncertainty

`confidence` is your calibrated probability, from 0 to 1, that the
mechanism is real as you have stated it. If you traced the path and it
clearly breaks, say 0.9. If it depends on an assumption you could not
check, say 0.4 and name the assumption in `uncertainty`. Be honest in both
directions.

## Tests

For each finding, search `tests/topotests` and the daemon's `tests/`
directory for a test that exercises the path you have identified. Use
Grep on the function names and config commands involved. If a test
exists, name its path and say whether it would catch this defect as
written. If none exists, say so, and describe the scenario a test would
need to construct.

Report this in `recommended_validation`. `paths` must name real test files
or directories under the repository root, whether the test exists
(`existing_test`) or is the one you are describing (`new_test`, in which
case `paths` is where it would live — the directory of the closest existing
test for that daemon or feature). "Add a regression test" with no path is
not a validation.

## Calibration

A change with no system-level risk is a normal result. Roughly half the
changes you review will be clean. Do not manufacture a finding to have
something to report; an empty finding list with a stated reason ("the
change is confined to X, touches no shared state, no config surface, no
lifecycle path") is a complete review.

## Ranking

Order `findings` so that the first entry is the one that matters most if it
is real: rank by **consequence if real × confidence**, not by the order in
which you found things. The first entry is read as your primary claim about
this change. A vague, high-severity finding placed first is worse than a
precise, medium one — put the finding you can best defend at the top.

## Output

Respond with a single JSON object and nothing else. No markdown fences, no
commentary before or after. Your entire response must parse as JSON.

Emit **only** the content below. Do not add `schema_version`, `run_id`,
`case_id`, `stage`, or `generated_at` — the engine supplies those itself,
and anything you write in those fields is discarded.

```
{
  "summary": "<optional: one or two sentences on what you looked at>",
  "findings": [
    {
      "id": "f1",
      "title": "<short, specific>",
      "file": "<path relative to the repository root where the defect manifests>",
      "line": 42,
      "mechanism": "<what goes wrong, under what condition, with what consequence>",
      "class": "<one of the failure classes above>",
      "severity": "low | medium | high | critical",
      "confidence": 0.0,
      "evidence": [
        {
          "file": "<path relative to the repository root>",
          "start_line": 42,
          "end_line": 47,
          "excerpt": "<the actual line(s), copied>"
        }
      ],
      "recommended_validation": {
        "kind": "existing_test | new_test",
        "paths": ["<test file or directory, relative to the repository root>"],
        "description": "<existing_test: whether it catches this as written; new_test: the scenario it must construct>"
      },
      "uncertainty": "<what you did not verify, and what would change your mind>"
    }
  ],
  "empty_reason": "<required when findings is empty: what the change is confined to and which surfaces it does not touch>"
}
```

Rules for the fields:

- **`id` must be stable and unique** within your output: `f1`, `f2`, `f3`.
- **`file` and `line`** point at where the defect manifests, and must exist
  in the repository snapshot at that line.
- **`evidence` requires at least one entry.** Every `file` must be a real
  path under `/workspace/input/reviewer/repository/`, written **relative to
  the repository root** — `bgpd/bgp_evpn.c`, not the absolute container
  path. `excerpt` must be copied from the file, not paraphrased.
- **`recommended_validation.paths` requires at least one entry**, following
  the Tests section above.
- **`empty_reason` is required when `findings` is empty**, and must not be
  present otherwise.
