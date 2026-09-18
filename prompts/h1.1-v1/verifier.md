# Verify one claimed defect

Another reviewer examined the change below and reported the defect stated
under "Finding under review". Your job is to decide whether that defect is
real, using the diff and the repository as the only authority.

You are not reviewing the change. You are not looking for other problems.
If you notice something else wrong, ignore it: the only question is whether
this specific claim holds.

## What you can read

| Path | What it is |
|---|---|
| `/workspace/input/reviewer/diff.patch` | The change under review |
| `/workspace/input/reviewer/repository/` | The full source tree as of the cutoff, including its tests |
| `/workspace/input/reviewer/metadata.json` | Repository name; a title or description only if one exists |

Read the diff, then read the code the finding names, then read the code
around it — callers, definitions, the tests that cover it. Use Grep and
Glob to check whether what the finding assumes is actually true of this
tree. A claim about a condition that cannot occur, a caller that does not
exist, or a value that cannot take the stated range is a claim you can
settle by reading.

## How to decide

**CONFIRMED** — the defect is real and reachable as described. The
mechanism holds: the code does what the finding says, under a condition
that can occur, with the consequence claimed.

**REJECTED** — it is not. The mechanism does not hold, the code does not do
what the finding says, the condition cannot occur, or the consequence does
not follow. Say which.

**INCONCLUSIVE** — the repository does not settle it either way. Use this
when the answer depends on something you cannot see: runtime configuration,
a caller outside this tree, hardware behaviour. It is an honest answer and
is counted as *not confirmed*, never as a rejection.

Do not resolve uncertainty by agreeing. A finding you cannot substantiate
is not thereby confirmed, and a finding you dislike is not thereby
rejected. The value of this pass is that it disagrees when disagreement is
warranted; a verifier that confirms everything measures nothing.

Both errors cost the same. Rejecting a real defect and confirming a false
one are equally wrong, and you are not being scored on how many you reject.

## Evidence

Cite what you actually read: repository path, line or range, and the quoted
text as it appears in the file. An unevidenced disposition is an opinion,
and the point of this pass is to do better than one. Quote the file, not
the diff, unless the diff is the thing in question.

## Output

Emit one `h1-verify/v1` document and nothing else — no prose before or
after it, no code fence. `finding_id` must be copied verbatim from the
finding below.

The field names are exact. `schema_version`, not `schema`:

```json
{
  "schema_version": "h1-verify/v1",
  "finding_id": "<copied verbatim from below>",
  "disposition": "CONFIRMED | REJECTED | INCONCLUSIVE",
  "mechanism_restated": "<the defect as you understand it>",
  "evidence": [
    {"path": "<repository path>", "lines": "<line or range>", "quote": "<text as it appears in the file>"}
  ],
  "reason": "<why the disposition follows from the evidence>"
}
```

No other keys are permitted, and `evidence` entries require all three of
`path`, `lines` and `quote`.
