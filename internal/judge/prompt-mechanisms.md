# Describe what each fix repaired

You are reading patches that maintainers of a software project landed to
correct a defect. Each patch was explicitly linked by a maintainer to an
earlier change — by a `Fixes:` trailer, a revert, or a regression report.

For each fix you are given, describe **what was actually wrong with the
code**: the failure mode, the condition under which it went wrong, and the
consequence. Then say what the patch changed to repair it.

Write about the defect, not about the diff. "Adds a NULL check at line 412"
describes the patch; "the early-init path could reach `handle_request()`
before `alloc_buf()` ran, so `conn->buf` was dereferenced while still NULL,
crashing the daemon on the first request after a config reload" describes the
defect. The second is what is wanted.

Be specific enough that someone who has not seen the patch could recognise
the same defect if they encountered it described in other words. Name the
function, the variable, and the triggering condition where the patch makes
them clear. Do not speculate beyond what the patch shows: if the triggering
condition is not evident from the diff and its commit message, say that
rather than inventing one.

## Why this is a separate task

Your description will later be compared against what code reviewers said
about the original change, to judge whether any reviewer anticipated this
defect.

**You are not being shown those reviews, and that is deliberate.** If you
could see them, it would be easy to describe the fix in language that echoes
whatever a reviewer happened to write, and the later comparison would then be
measuring that echo rather than a real agreement. Describing the defect on
its own terms first is what makes the comparison mean anything.

For the same reason, do not try to guess what a reviewer might have said, and
do not hedge your description to make it easier to match. Describe the defect
as you understand it from the patch.

## If a patch is unclear

Some patches are large, mechanical, or mix an unrelated cleanup with the
fix. If you cannot identify a specific defect being repaired, say so plainly
in the mechanism field rather than manufacturing one. "This patch is a
mechanical rename across 40 files with no identifiable defect repair" is a
useful and honest answer. An invented mechanism is worse than none, because
everything downstream will compare against it as though it were real.

Return a single JSON document satisfying the supplied schema, with exactly
one entry per fix you were given.
