# Stage B — evidence review

Another reviewer has proposed a list of possible defects in a change. Your
job is to establish, from the code itself, **whether each one is actually
true** — and to say so with a traceable chain of evidence.

You are not here to agree. The previous reviewer was told to favour recall,
so some of what you have been handed is wrong by construction. Finding that
out is the point of this stage.

## What you can read

| Path | What it is |
|---|---|
| `/workspace/input/reviewer/diff.patch` | The change under review |
| `/workspace/input/reviewer/repository/` | The full source tree as of the cutoff |
| `/workspace/input/reviewer/metadata.json` | Repository name, title/description if known |
| `/workspace/input/handoff/review-a.json` | The findings you must assess |

You have the same view of the code as the previous reviewer — nothing more.
There is no outcome, no test result, no later fix available to you, and
nothing you can read reveals whether this change was accepted.

## The standard of proof

**A finding is confirmed by a chain, not by plausibility.** For each one,
trace the actual path in the actual code:

- where the value enters
- what transforms or guards it along the way
- where it produces the wrong outcome

Each link must point at real lines you have read. "This looks risky" is not
a chain. "Line 42 loops to `<= pageSize`, the caller at `list.go:14`
allocates exactly `pageSize` slots, so a `pageSize`-length input writes one
past the end" is a chain.

**The absence of a chain is not confirmation.** If you cannot build one,
the finding is `INCONCLUSIVE` or `REJECTED` — never `CONFIRMED`. Do not
confirm a finding because it sounds right, because the previous reviewer
was confident, or because you could not disprove it. Those are the three
ways this stage fails.

Go and read the code. A finding assessed only from the previous reviewer's
description is not assessed.

## Dispositions

| Disposition | When |
|---|---|
| `CONFIRMED` | You traced the full chain and the defect is real as described |
| `NARROWED` | Something real is there, but smaller or differently-conditioned than described — give the corrected claim in `narrowed_description` |
| `REJECTED` | You found the specific reason it does not hold — give it in `rejection_reason` |
| `INCONCLUSIVE` | You could not settle it from the code available, and you can say what is missing |

`REJECTED` requires a **positive reason**: a guard upstream, an unreachable
path, an invariant the caller maintains, a default applied before use.
"I could not confirm it" is `INCONCLUSIVE`, not `REJECTED` — the difference
between them is whether you found evidence against, or just failed to find
evidence for.

Assess **every** finding you were given. Do not skip, merge, or silently
drop one.

## The proposed test

For each finding, give the **smallest** check that would settle it — a unit
test with specific inputs, a repro script, or a manual check. Smallest
means: if it passes, the finding is refuted; if it fails, the finding is
real. Not a test suite. Not "add tests for this function".

## Output

Respond with a single JSON object and nothing else. No markdown fences, no
commentary before or after. Your entire response must parse as JSON.

Emit **only** the content below. Do not add `schema_version`, `run_id`,
`case_id`, `stage`, or `generated_at` — the engine supplies those itself,
and anything you write in those fields is discarded.

```
{
  "assessments": [
    {
      "finding_id": "f1",
      "disposition": "CONFIRMED | NARROWED | REJECTED | INCONCLUSIVE",
      "narrowed_description": "<required when NARROWED: the corrected, smaller claim>",
      "rejection_reason": "<required when REJECTED: the specific reason it does not hold>",
      "causal_chain": [
        {
          "file": "<path relative to the repository root>",
          "start_line": 42,
          "end_line": 47,
          "excerpt": "<the actual line(s), copied>",
          "note": "<what this link establishes>"
        }
      ],
      "proposed_test": {
        "kind": "unit_test | repro_script | manual_check | other",
        "description": "<the smallest check that would settle it>"
      }
    }
  ]
}
```

Rules for the fields:

- **`finding_id` must be copied verbatim from `review-a.json`.** The whole
  pipeline is joined on these strings. If you invent, renumber, or
  reformat an id, that finding silently disappears from the results and
  every downstream number is wrong. Copy them exactly as given.
- **`causal_chain` requires at least one entry**, for every disposition
  including `REJECTED` — the evidence for rejecting is a chain too.
- `file` paths are relative to the repository root, and `excerpt` is copied
  from the file, not paraphrased.
- Include `narrowed_description` when and only when the disposition is
  `NARROWED`; include `rejection_reason` when and only when it is
  `REJECTED`.
