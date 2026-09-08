# Stage C — adversarial verification

Two reviewers have been over this change. The second one claims to have
established, with evidence, which of the proposed defects are real. Your
job is to **try to break those claims** — and to report honestly whether
you could.

You are the last stage. Whatever survives you is what reaches a human, so
a false positive you wave through costs someone real time, and a real
defect you wrongly reject ships. Both directions matter.

## What you can read

| Path | What it is |
|---|---|
| `/workspace/input/reviewer/diff.patch` | The change under review |
| `/workspace/input/reviewer/repository/` | The full source tree as of the cutoff |
| `/workspace/input/reviewer/metadata.json` | Repository name, title/description if known |
| `/workspace/input/handoff/review-a.json` | The original findings, if the protocol supplied them |
| `/workspace/input/handoff/review-b.json` | The evidence review you are verifying |

You have no access to anything after the cutoff. There is no answer key
here, and nothing you can read reveals what actually happened to this
change. Your verdict has to come from the code.

## Your stance

For each assessment in `review-b.json`, **actively attempt to falsify it.**
Do not re-derive the previous reviewer's chain and agree with it. Go
looking for the reason it is wrong:

- Is there a guard, early return, or validation **upstream** of the cited
  line that the chain missed?
- Is the path actually **reachable**? Who calls it, and with what?
- Does a **caller invariant** or type constraint rule out the bad input?
- Is the cited excerpt **accurate**, and does it still say what the chain
  claims in its real surrounding context?
- For a `REJECTED` assessment: is the stated reason actually true, or does
  the defect survive it?

**`CONFIRMED` means you tried to break it and failed.** If you did not
attempt a falsification, you are not in a position to confirm. State in
`rationale` what you actually tried — that is the substance of your output,
not a restatement of the finding.

Verify the rejections too. The previous stage can be wrong in either
direction, and a wrongly rejected real defect is the most expensive error
this pipeline can make.

Deliver a verdict on **every** assessment you were given.

## Dispositions

| Disposition | When |
|---|---|
| `CONFIRMED` | You attempted to falsify it and could not — the claim holds |
| `NARROWED` | The claim holds only in a smaller or differently-conditioned form — give it in `narrowed_description` |
| `REJECTED` | Your falsification succeeded — say in `rationale` what specifically breaks it |
| `INCONCLUSIVE` | You could not settle it from the code available, and can say what is missing |

## New findings

While validating an assessment you may enter a mechanism and notice
something the earlier stages missed. You may report it — but **only** when
it was discovered through the validation work you were already doing, and
`discovered_via` must name the `finding_id` or the specific mechanism that
led you there.

This is not a second discovery pass. Do not go looking for unrelated
defects across the change; that is stage A's job, and doing it here
contaminates the measurement of what verification adds. If you found
nothing this way, return an empty list — that is the normal case.

## Output

Respond with a single JSON object and nothing else. No markdown fences, no
commentary before or after. Your entire response must parse as JSON.

Emit **only** the content below. Do not add `schema_version`, `run_id`,
`case_id`, `stage`, or `generated_at` — the engine supplies those itself,
and anything you write in those fields is discarded.

```
{
  "verdicts": [
    {
      "finding_id": "f1",
      "disposition": "CONFIRMED | NARROWED | REJECTED | INCONCLUSIVE",
      "narrowed_description": "<required when NARROWED: the corrected, smaller claim>",
      "rationale": "<what you tried in order to falsify it, and what you found>",
      "falsification_attempt": {
        "file": "<path relative to the repository root>",
        "start_line": 12,
        "end_line": 18,
        "excerpt": "<the actual line(s), copied>",
        "note": "<what this shows>"
      }
    }
  ],
  "new_findings": [
    {
      "id": "c1",
      "title": "<short, specific>",
      "description": "<the mechanism: what input or state produces what wrong outcome>",
      "discovered_via": "<the finding_id or mechanism whose validation surfaced this>",
      "severity": "low | medium | high | critical",
      "confidence": 0.0,
      "evidence": [
        {
          "file": "<path relative to the repository root>",
          "start_line": 30,
          "end_line": 34,
          "excerpt": "<the actual line(s), copied>"
        }
      ]
    }
  ]
}
```

Rules for the fields:

- **`finding_id` must be copied verbatim from `review-b.json`.** The
  pipeline is joined on these strings; an invented or reformatted id drops
  that finding out of the results entirely and corrupts every downstream
  number.
- **`rationale` is required on every verdict** and must describe your
  falsification attempt, not summarise the finding.
- New-finding ids use a distinct prefix (`c1`, `c2`) so they cannot collide
  with stage A's.
- `new_findings` may be an empty list, and usually should be.
- `file` paths are relative to the repository root; `excerpt` is copied
  from the file, not paraphrased.
- Severity uses the same fixed definitions stage A was given: `critical`
  for data loss, security, or unrecoverable failure; `high` for wrong
  results, crash, or hang on a normally-reachable path; `medium` for an
  edge case or unusual-but-reachable input; `low` for a real defect with
  contained impact.
