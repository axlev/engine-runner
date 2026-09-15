# Brief: the arm T discovery prompt

*Design brief for migration item #3, the treatment discovery prompt. Read
before any prompt text is written. Canonical pre-registration:
`miner/docs/h1-preregistration.md` (§4 arms, §5 verdict, §6 scoring, §7
classes, §8 contamination).*

## 1. What this prompt is

**It is the hypothesis.** H1 §1 says an A→B pipeline *with a system-aware
discovery prompt* beats a generic reviewer. The current `reasoner-1.md` is
domain-free — the engine-runner fitness review established that reusing it
verbatim gives you arm G, not arm T. So until this prompt exists, the
treatment does not exist, and a weak version of it makes H1 fail by
construction rather than by evidence.

Everything below follows from one principle: **the difference between T and G
must be exactly the thing H1 claims matters, and nothing else.**

## 2. The delta principle, and a confound to remove first

T and G share: the model (`claude-opus-5`), the tool grant (Read, Grep, Glob),
the output schema, the finding shape, the calibration instruction, and the
verdict mapping in §5. T differs from G in **two** declared ways:

1. The discovery prompt directs the reviewer at system-level failure modes.
2. T has a substantiation stage; G does not.

The second is accepted by design — §1 defines the treatment as the pipeline,
not the prompt alone. But it means a T-over-G result cannot be attributed to
the prompt versus the staging. **State this as a limitation in the H1 results;
do not let it be discovered later.** If the result is worth decomposing, a
fourth arm (generic prompt + substantiation) is the ~$100 follow-up.

**The confound that must be removed before anything runs:** the engine-runner
migration plan's item #4 proposes G as *"`reasoner-1.md` copied verbatim."*
`reasoner-1.md` instructs the reviewer to favour recall — the reasoner-2
prompt says so explicitly ("the previous reviewer was told to favour recall,
so some of what you have been handed is wrong by construction"). A
recall-favouring G against a calibrated T would hand T a precision advantage
that has nothing to do with system awareness. **G must be a fresh prompt with
the same calibration instruction as T** — which is what §4 already says:
"prompt modelled on a general-purpose PR reviewer." Item #4 becomes ~1 day,
not half.

## 3. What the prompt directs the reviewer at

Five lenses. Each maps onto one of the five §7 failure classes where the
pre-registration expects T's recall to be highest. The prompt should make the
reviewer *ask each question* about the change, and cite the repository lines
that answer it.

| lens | question the reviewer must answer | §7 class | pilot evidence it would have caught |
|---|---|---|---|
| **State and ordering** | For every state transition, list, or queue the change touches: what are the legal orderings, and does the change create a path that violates one? For every loop the diff adds or alters: state its termination condition and the input that violates it. | `ordering-state-machine` | the `bgp_evpn.c` drain loop that never terminates when a FIFO entry doesn't match — found by an agent whose task was "state each loop's termination condition," missed by three configured reviewers |
| **Invariants across modules** | What does code *outside* this diff assume about the fields, counters, or refcounts this change touches? Grep for every reader and writer of each. Does the change break an assumption a distant caller holds? | `cross-module-invariant` | the `ospf->redistribute` counter — the wide arm grepped all nine writers and was right; the narrow arm couldn't and was wrong |
| **Configuration surfaces** | Which CLI commands, YANG paths, or config knobs reach this code? Can a config sequence — set, unset, reorder, re-apply — put it in a state the diff didn't consider? Include VRF creation and deletion. | `config-interaction` | the netns path handling that changes meaning under empty vs `"."` input on VRF delete |
| **Error and lifecycle paths** | For every early return, error branch, and free: what is left allocated, linked, or scheduled? Is the pre-existing error path now reachable from a new place? | `error-path-lifecycle` | the ASLA leak — pre-existing, but the change made it newly reachable from flex-algo config |
| **Restart and upgrade** | If the daemon restarts, is warm-restarted, or upgrades across this change: what persisted state does the new code read, and what does the old code have written? Does anything assume both sides run the same version? | `restart-upgrade` | none in the pilot — this is the class with the least evidence and the most product importance |

The pilot evidence column is for the prompt's *author*, not the prompt. It
comes from the 2024 pilot cohort, which is disjoint from H1's 2026 cohort, and
from general FRR architecture. **The prompt itself must contain no PR number,
no commit hash, no CVE, and no description of a specific historical defect** —
§8's post-hoc scan would flag it, and it would be right to.

`timing-race` and `resource-exhaustion` are not lensed. §7 pre-states that T
will do worst there; the prompt should not pretend otherwise by adding a
thin sixth question. If those classes matter for the product, they need
different instruments (execution, load), not a prompt.

## 4. In-tree tests — the direction the pilot proved is needed

The pilot mounted `tests/topotests` in every bundle (235–338 entries) and the
reviewers never read it: the narrow arm said it couldn't enumerate the
directory, the wide arm had Grep and Glob and never went there. Backlog 1.7.

The prompt must make tests a required step, not an available one:

> For each finding, search `tests/topotests` and the daemon's `tests/`
> directory for a test that exercises the path you have identified. Use
> Grep on the function names and config commands involved. If a test
> exists, name its path and say whether it would catch this defect as
> written. If none exists, say so, and describe the scenario a test would
> need to construct.

This is also what makes §2's **recommended-validation hit rate** scoreable.
That criterion matches by *path or explicit name* against the fixing commit,
with no judge call. "Add a regression test" scores nothing. "Extend
`tests/topotests/ospf_nssa_topo1/` to reconfigure the area as non-NSSA and
check `ospf_asbr_status`" can match. The prompt must require the specific
form.

## 5. Output contract

Driven by how §5 and §6 score, not by what reads well.

**Each finding carries, all required:**

- `file` and `line` — the diff line or the repository line where the defect
  manifests. §6 checks citations against the snapshot.
- `mechanism` — the failure mode in one or two sentences: *what* goes wrong,
  *under what condition*, *with what consequence*. This is what the judge
  compares to the fixing diff. `LOCALITY_ONLY` (right function, wrong
  mechanism) and `TOO_VAGUE` earn nothing. The pilot judge rejected 39
  near-misses against 22 matches; specificity is the whole game.
- `class` — one of the §7 enum values, the reviewer's own assignment. Reported
  against the evaluator's label; not scored, but it's how "recall by class"
  gets a per-arm view.
- `severity` — §5 gates RISKY on ≥ `medium`. The prompt must define the levels
  in terms of *consequence at runtime*, not code smell.
- `confidence` — used by G's verdict rule (≥ 0.6) and for the continuous
  score.
- `evidence` — the repository lines forming the chain, as today.
- `recommended_validation` — the §4 field above: existing test path, or the
  scenario a new test needs, in path-matchable form.

**Ranking.** §6 judges the *highest-ranked* finding. The prompt must have the
reviewer rank, and rank by *consequence if real × confidence*, not by order of
discovery. A vague high-severity finding ranked first is a `TOO_VAGUE` on the
one finding that counts.

## 6. Calibration — shared by T and G, verbatim

§5 makes a case RISKY if *any* finding survives at ≥ `medium`. On the pilot,
arms B and C produced ~3 surviving findings per **clean** PR. Under §5 those
PRs are all RISKY, and precision on the 20 negatives collapses. The prompt has
to make "nothing system-level here" a legitimate, expected outcome:

> A change with no system-level risk is a normal result. Roughly half the
> changes you review will be clean. Do not manufacture a finding to have
> something to report; an empty finding list with a stated reason ("the
> change is confined to X, touches no shared state, no config surface, no
> lifecycle path") is a complete review.

**This paragraph goes into G's prompt word for word.** If T is calibrated
and G is not, the precision delta is calibration, not system awareness.

## 7. What the prompt must not do

- **Not ask what the model remembers.** No "have you seen this change," no
  "what happened to this code later." §8 voids a case for any arm whose
  review names the fixing SHA, PR number, or CVE, or lifts post-merge
  phrasing. The prompt must not invite it.
- **Not assume a PR description exists.** Pre-registration §12 A2: on the
  pilot, 1 of 10 bundles carried a description. Until the metadata/v2
  decision lands, the reviewer has the diff, the snapshot, and commit
  messages. Write for that.
- **Not reward volume.** See §6 above. Every finding at ≥ `medium` on a clean
  PR is a precision hit.
- **Not direct at `timing-race` or `resource-exhaustion`** with a token lens.
  See §3.
- **Not contain FRR-derived code.** The AGENTS.md fixture rule (FRR idiom,
  never FRR-derived) is written for fixtures, but the reasoning — a prompt
  that quotes real FRR code is a prompt tuned on the population it reviews —
  applies here too.

## 8. The substantiation stage changes too

§4 defines T as "→ substantiation → recommended validation." The validation
output comes *after* substantiation, so stage B's prompt (`reasoner-2.md` or
its `h1` successor) has to emit `recommended_validation` for each surviving
finding, in the same path-matchable form, and may refine what discovery
proposed once it has built the causal chain. That is a second prompt change
and a schema change, and it overlaps migration item #1 (verdict schema). Do
not build #3 assuming #1's schema is fixed; build them together or sequence
#1 first.

## 9. Fingerprint and files

- New protocol `h1-t-v1`: discovery prompt, substantiation prompt, schemas.
  Two stages declared, `review-a` handed to stage 2. Requires migration #2.
- New protocol `h1-g-v1`: one discovery prompt, one stage.
- Both hash their own prompts and schemas; `pilot-v1` untouched.
- Arm file with `tools: [Read, Grep, Glob]` and the raised caps from the wide
  arm. Same file for T and G — the arm is the binding, the protocol is the
  experiment.
- §8: the planted-`CLAUDE.md` steering test re-runs once under this grant
  before T starts. It has not yet been run behaviourally for Grep/Glob
  (fitness review E11: `Absent`).

## 10. Decisions and open items

**Decided 2026-09-15:**

1. **The staging confound is accepted as stated in §2.** A fourth arm
   (generic prompt + substantiation) is deferred to after the H1 result, not
   added now. The results section states the confound.
2. **G is a fresh prompt, not `reasoner-1.md` verbatim.** Same calibration
   paragraph as T, word for word; differs only in the absence of the five
   lenses. Migration item #4 is ~1 day.

3. **The reviewer sees the §7 class list.** The enum goes into the prompt so
   the `class` field is consistent. The evaluator's labels are assigned
   before any arm runs, so nothing leaks.
4. **The lens text is written by the owner.** `engine-coder` builds the
   prompt scaffold — structure, output contract (§5), calibration paragraph
   (§6), the in-tree tests step (§4), the prohibitions (§7), and the stage-B
   changes (§8) — with §3's five lenses left as a marked slot. The owner
   fills the slot. The scaffold is what makes T and G comparable; the lenses
   are what makes T a treatment, and they are FRR daemon knowledge.
