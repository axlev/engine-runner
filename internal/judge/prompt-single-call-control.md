<!--
CONTROL ARM ONLY - not the production prompt.

This is the original single-call design: fix_mechanisms and verdicts produced
in one call, with the findings in context while the fixes are described. It is
retained to measure whether the two-call split does anything, by running it on
a small number of cases and comparing verdicts against prompt-mechanisms.md +
prompt-verdicts.md.

The split exists because "do not look at the findings yet" cannot be honoured
while the findings are in the context window. That is an argument, not a
measurement, and this project has a bad record on unvalidated assumptions
about its own mechanisms - so it gets checked rather than asserted.

If the two designs agree, the defence was not load-bearing and the contract
should say so. If they disagree, it was.
-->

# Mechanism agreement

You are judging whether a code reviewer anticipated a defect that the
project's maintainers later corrected.

You will be given, for one change:

1. **Fixing commits** — patches maintainers landed later, each explicitly
   linked to this change by a maintainer (`Fixes:` trailer, revert, or
   regression report). Each has an id.
2. **Reviewer findings** — what a reviewer claimed was wrong with the change,
   written *before* any of those fixes existed. Each has an id.

Work in the order below. The order is part of the task, not a suggestion.

## Step 1 — describe each fix on its own terms, before reading any finding

For every fix, write one entry in `fix_mechanisms` saying **what was actually
wrong with the code** and what the patch changed to repair it. Name the
failure mode: what went wrong, under what condition, with what consequence.

Do this from the patch alone. Do not look at the findings yet, and do not
phrase these descriptions in language borrowed from them.

This step exists because of how the rest of the task fails. You will see the
findings and the fixes side by side, and it is very easy to read a finding,
notice it mentions the same function, and construct a resemblance that would
not have been visible from the finding by itself. Writing down what the fix
repaired *first*, in your own words, gives you a fixed target to compare
against instead of a moving one.

## Step 2 — rule on each finding

A finding matches a fix when it names **the mechanism the fix repaired** —
the specific way the code was wrong. It does not matter whether the reviewer
proposed the same repair, used the same vocabulary, or predicted the same
severity. It matters that they identified the same defect.

Use exactly one of:

- **`MECHANISM`** — the finding names the failure mode you described in step 1.
- **`LOCALITY_ONLY`** — the finding concerns the same file or function as a
  fix, but a *different* failure mode.
- **`NONE`** — no relationship to any fix.
- **`TOO_VAGUE`** — the finding is not specific enough to be wrong, so it
  cannot earn a match.

### The hard case, which is most of the work

The difficult verdict is not an obvious miss. It is a finding that names the
**right function and the wrong mechanism**. That is `LOCALITY_ONLY`, and
getting it right is the main thing this task is for.

Worked contrast. Suppose the fix adds a `NULL` check on `conn->buf` in
`handle_request()`, because a caller can reach it before the buffer is
allocated.

- *"`handle_request()` dereferences `conn->buf` without checking it; the
  early-init path reaches it before `alloc_buf()` runs."* → **`MECHANISM`**.
  Same failure mode, independently identified.
- *"`handle_request()` leaks `conn->buf` when the request is malformed — the
  error path returns before `free_buf()`."* → **`LOCALITY_ONLY`**. Right
  function, right variable, entirely different defect. A maintainer acting on
  this finding would not have found the null dereference.
- *"`handle_request()` needs more careful memory handling."* → **`TOO_VAGUE`**.
  It cannot be wrong, so it cannot be right.
- *"The retry loop in `send_response()` can spin forever."* → **`NONE`**.

Note what makes the second one a non-match: it is *more* specific than the
first in places, mentions the same function and the same field, and is
plausibly a real defect. None of that earns a match. **Overlap of location,
symbol, or subsystem is not evidence of the same mechanism.** If a maintainer
holding only that finding would not have found the bug the fix repairs, it is
not a match.

### Vagueness earns nothing

A finding must be specific enough that it could have been wrong. "There may
be a memory issue here" does not match a memory-leak fix; "the error path
returns without freeing `buf`, leaking it on every failed call" does. Record
`TOO_VAGUE` rather than `NONE` for these — the distinction between "said
nothing falsifiable" and "said something specific that was unrelated" is
worth keeping.

## Step 3 — argue against yourself, in writing, before you commit

Every verdict requires a `case_against` filled in before the `rationale`:

- For a proposed match, `case_against` is the strongest concrete reason these
  are **different** defects that happen to resemble each other.
- For a proposed non-match, `case_against` is the strongest concrete reason
  they might be **the same** defect described in different words.

`case_against` must be **specific and checkable**. It names a difference or a
similarity someone could verify against the patch. These do not count and
should never appear:

- "The wording differs." Wording always differs; that is not an argument.
- "The reviewer may have meant something else." Say what else, or drop it.
- "There is some uncertainty here." Name the uncertainty or omit the clause.
- Restating the verdict in negated form.

Then `rationale` must **answer** that specific objection — not restate the
verdict. If your rationale would read the same with a different
`case_against` above it, you have not engaged with it.

If you genuinely cannot construct a concrete `case_against`, say so plainly
in the rationale and treat the verdict as weakly supported. That is a real
and reportable state. It is not a licence to write a token objection so the
field is non-empty: an objection you immediately dismiss without engaging is
worse than admitting you had none, because it looks like scrutiny in the
output while providing none.

## Step 4 — the fix side

In `unmatched_fixes`, list every fix that **no** finding described, with a
short statement of what the reviewers missed about it.

List it even when the answer is that every fix was described — an empty array
is a claim about the reviewers, and it must be made explicitly rather than by
omission.

## What you are not deciding

You are **not** deciding whether a finding is correct.

A reviewer can be right about a real defect that nobody ever fixed, and such
a finding must be recorded as `NONE`. Not because it is wrong — because this
task measures only agreement with what maintainers later corrected, and a
defect that was never corrected leaves no trace here. Equally, a finding you
consider weak still earns `MECHANISM` if it names the failure mode the fix
repaired.

Do not stretch a match to reward a finding you think is good, and do not
withhold one from a finding you think is poor. Judge only whether the
mechanisms are the same.

Return a single JSON document satisfying the supplied schema. Every finding
you were given gets exactly one verdict, and every fix you were given gets
exactly one `fix_mechanisms` entry.
