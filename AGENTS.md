# AGENTS.md — engine-runner

Instructions for any coding agent working in this repository. These are
access boundaries, not style preferences: the benchmark's credibility
depends on them.

Canonical design: [`docs/system-design.md`](docs/system-design.md).

## What this repository is

`engine-runner` is the orchestration, isolation, validation, results, and
scoring layer for a staged LLM code-review benchmark. It runs three
isolated reasoning stages (discovery → evidence → adversarial
verification) over frozen public code changes, then scores the result.

The role that owns this repository is `coder-engine-runner`.

## Hard access boundaries

A coding agent working here **must not**:

1. **Read or write `/home/alex/data`.** That path holds raw mined data,
   prospective bundles, oracle bundles, and production run results. No
   coding task in this repo requires it. Use the synthetic fixtures under
   `fixtures/` instead.
2. **Use a real mined case while building the runtime layer.** Everything
   in `fixtures/` is synthetic and hand-authored for exactly this reason.
3. **Place code, prompts, schemas, or fixtures under `/home/alex/agents`.**
   That tree holds agent state (config, scratch, logs) only — never source
   code, never a git repository.
4. **Add oracle access to any adapter or reasoner.** Reasoning agents never
   see retrospective outcome data. The evaluator is the only component that
   may, and only after all reviewer stages have completed.
5. **Implement a cross-provider memory store.** Vendor polymorphism is the
   `AgentAdapter` contract and nothing more. Cross-stage knowledge moves
   only through declared handoff files (`review-a.json`, `review-b.json`).
6. **Reuse conversations, sessions, or vendor conversation IDs between
   stages.** Every stage attempt is a fresh execution.
7. **Change prompts during a frozen cohort** without creating a new
   protocol version.

## Layout rules

- Git repositories exist only under `repos/`.
- Compiled binaries, container artifacts, and sealed results live under
  `build/` (gitignored) — never committed.
- **Per-attempt workspaces are the exception: they live outside the
  repository** (`~/.cache/engine-runner/runs` by default). They are
  bind-mounted into stage containers, and `internal/runner` refuses to
  mount `/home/alex/repos` at all, so a workspace inside the repo makes
  every live run fail on the mount guard. The guard is not the thing to
  relax: a directory mounted into a reasoning container should not live in
  the tree that guard exists to protect. Results are unaffected — the
  engine writes them and never mounts them.
- Real production results live under `/home/alex/data/results`, outside
  this repository entirely.
- Source control here holds code, prompts, protocol manifests, schemas,
  docs, and small synthetic fixtures. It must never hold credentials,
  agent memory, raw mined data, oracle data, production logs, or
  unpublished benchmark results.

## Design invariants to preserve

These are the properties the whole system exists to guarantee. Changes that
weaken them need an explicit design decision, not a quiet refactor.

- **Repository content cannot steer a reviewer.** Both credential kinds
  disable CLAUDE.md auto-discovery: `--bare` for an API key, `--safe-mode` for
  an OAuth token (they are mutually exclusive - `--bare` forces an API key).
  Without it a `CLAUDE.md` inside `reviewer/repository/` is read as
  instructions from inside the bundle, outside the frozen protocol.
  Demonstrated, not assumed: a planted `CLAUDE.md` made a stage open its reply
  with "BANANA", and `--safe-mode` stopped it. Deliberately NOT solved by
  rejecting bundles that carry one - real repositories legitimately have them,
  and that rule would be the same over-broad proxy as the blanket symlink
  rejection.
- **Boundaries are enforced by software, not prompts.** The context builder
  allow-lists what each stage sees; it does not filter out what's
  forbidden. There is no filter to bypass.
- **Handoff policy is the sole authority.** A prior review output that
  happens to be available on disk is still not copied into a stage's
  context unless the protocol authorizes it.
- **Never silently retry.** Every attempt is recorded, successful or not,
  along with the retry-policy decision that produced it.
- **Everything is fingerprinted.** Protocol, prompts, schemas, adapter
  versions, and input checksums are hashed from the actual bytes used, not
  from declared version strings that could drift from disk.
- **Determinism.** The fixture adapter's behavior is a pure function of
  (scenario, stage, attempt). Container arguments are a pure function of
  their spec. Reruns of the same case produce identical fingerprints.

## Cross-repo contract with `miner`

- **Treat the prospective bundle wire contract as frozen in both directions.** Do not
  change the `reviewer/` + `control/` layout, the entry names, the pinned schema-version
  strings (`engine-manifest/v1`, `reviewer-metadata/v1`), the reviewer metadata field
  allowlist, or the checksum-manifest semantics unless a new product requirement cannot be
  implemented without it — not for tidiness, naming, or a nicer shape. If a requirement
  does force a change, bump the schema version rather than redefining an existing one, and
  say so explicitly in your report: a silently redefined `v1` is worse than a loudly
  introduced `v2`. This binds the engine as much as the miner.

- **You are the sole gate on admissibility, as of 2026-09-10.** The miner used to
  hand-maintain a copy of the structural rules in
  `miner/internal/prospectiveexport/bundle.go`, which was a liability: two rule tables in
  two repos, no shared memory, and no automatic consistency check, because Go forbids
  importing another module's `internal/` packages. **That copy has been deleted.** The file
  still exists but now holds only the miner's own concerns — writing and self-verifying
  `control/checksums.sha256`, refusing to materialize a tree it cannot represent as plain
  files, and blocking the exact `ground_truth.json`-style names your in-snapshot heuristic
  deliberately omits. The miner's `cohort-verify` runs `go run ./cmd/bench` against every
  bundle it produces, so your validator is exercised on real artifacts before a cohort is
  frozen.

  A rule you change therefore takes effect without a matching edit anywhere else — but
  nothing notifies the miner either. There is still no shared memory between `coder-miner`
  and `coder-engine-runner` (`docs/system-design.md` §14), so state a boundary-rule change
  prominently enough that it can be carried across by hand.

- **Admissibility is enforced in two places, not one.** `internal/boundaryvalidator` gates
  whether a bundle may run; `internal/contextbuilder` gates what is placed inside a
  container. A relaxation applied to only one of them does not work: the run passes
  validation and then fails after a container has started and money has been spent. The
  symlink rule is the worked example — both sides check containment independently.

- **The oracle-name heuristics are exempt from the freeze.** `oracleShapedSubstrings`,
  `oracleArtifactBasenames`, and `highSignalOracleSubstrings` are meant to be tuned against
  real exports, as `docs/prospective-bundle-contract.md` says. Tuning them changes no
  bundle's structure and they are warnings-only on the miner's side, so they may move on
  either side without being treated as a break. Expect `solution` and `verdict` to
  false-positive on legitimate third-party source; prefer `WaivedOracleShapedPaths` over
  narrowing a rule, and expect the miner to report such paths rather than renaming them.

- **`reviewer/repository/` is the tree of `cutoff_commit`, not `base_commit`.** This is
  deliberate: your own fixtures hold post-change content, and
  `internal/orchestrator/evidence.go` fails any citation naming a line outside the snapshot
  file, so a base-tree snapshot would fail every citation of an added line. Do not "correct"
  this to the base tree without raising it explicitly — it would invalidate every bundle the
  miner has produced.

## Fixture case language

New synthetic cases are written in **C** in the idiom of an embedded network
operating system - FRR's vocabulary: `struct stream`, `STREAM_GETC`,
`XMALLOC`/`XFREE` with an MTYPE, `zlog_err`, vty handlers. That is the code
this benchmark exists to review, and it is where the defect classes worth
measuring live: unvalidated TLV lengths, reads past a stream end, integer
truncation in a length field, use-after-free on a teardown path.

Two rules follow from it.

- **Write code in FRR's idiom, never code derived from FRR.** Invent the
  daemon and function names. Real FRR routines and their CVEs are
  well-documented and widely mirrored, so a model may recognise a memorised
  defect instead of analysing the diff - scoring well while proving nothing.
  The boundary validator cannot detect that: the leak is in the model's
  weights, not in the bundle.
- **Ground truth is a sanitizer trace, not a claim.** Build the reviewer's
  own snapshot under `-fsanitize=address,undefined` from `<case>/oracle/`
  and let the run prove both directions - that the real defect faults, and
  that the plausible-but-unreachable one does not. `fixtures/cases/tlv-bounds`
  is the worked example.

C sources are invisible to `go build ./...`, so the `//go:build ignore` tag
and the nested-`go.mod` trick used by the older Go fixtures are unnecessary.

## Working conventions

- Go 1.26. Run `go build ./... && go vet ./... && go test ./... && gofmt -l .`
  before committing; all four must be clean.
- Verify vendor CLI details against the real binary (`--help`, `--version`)
  rather than from memory or documentation summaries. This has already
  caught fabricated flags that would have shipped broken invocations.
- When something can't be verified in the current environment (no Docker
  daemon, no credentials, no container image), say so explicitly in code
  comments and commit messages rather than implying it was tested.
- Prefer stating a known gap over fabricating plausible data. `Usage` left
  at zero with a comment explaining why beats invented token counts.

## Escalation

Stop and ask before, not after:
- choosing between designs where the tradeoff is mine to make
- narrowing, disabling, or making non-fatal any existing check
- excluding a term/path/case from a detection list
- anything touching the frozen protocol or schema versions
- anything in the prospective bundle wire contract, which is frozen in both
  directions (see Cross-repo contract with `miner`)

## Reporting

- Never describe a reconstructed artifact as the original. If a demo
  required rebuilding, fabricating, or modifying input, say so in the
  same sentence as the result.
- State what you did not verify.
- A boundary-rule change is not finished when it compiles. Nothing notifies
  the miner, so report it prominently enough to be carried across by hand
  (see Cross-repo contract with `miner`).

### Status vocabulary

Use exactly these terms for the state of any work — in `docs/backlog.md`, in
commit messages, and when reporting to the user. **Do not substitute "done",
"complete", "works", or "exists"**: those round up, and each term below is a
claim with a bar attached.

| Term | Bar |
|---|---|
| `Absent` | Not built. Nothing to point at. |
| `Present` | Code exists and compiles. Says nothing about whether it is right. |
| `Implemented` | Built to its intended behaviour, with tests that exercise that behaviour. |
| `Verified` | The stated checks were **actually run successfully in this session**. Not "should pass" and not "passed last week". |
| `Integrated` | Exercised end to end against real inputs, not fixtures. |
| `Shipped` | Committed and pushed to the branch the user approved. |
| `Blocked` | Cannot proceed, with the blocker named. |
| `Needs a decision` | Waiting on the user, because the tradeoff is theirs. |

They compose: "Implemented and Verified" is ordinary; "Implemented, not
Verified" is the honest state of anything whose test has not been run since it
was written. `6fcda0e` shipped in exactly that state and said so.

The terms are ordered by strength and **must not be rounded up**. A thing that
compiles is `Present`, not `Implemented`. A thing whose tests you wrote but did
not run is `Implemented`, not `Verified`. A thing verified on fixtures is not
`Integrated`.

Adopted from `coder-miner`, which uses the same vocabulary, so a status word
means the same thing in both repositories.
## Verification artifacts

Do not delete a run tree in the same command that produces it. Clean up
at the start of the next run, or in a separate command after the output
has been read. If a result needs investigating, the tree must still exist.


## Current state

How to read a sealed run — what each artifact says and how to trace a finding
through all three stages — is in
[`docs/reading-results.md`](docs/reading-results.md).

The work queue lives in [`docs/backlog.md`](docs/backlog.md) — what is left, in
what order, and why that order. Update its Status column in place rather than
tracking work here. This section records gaps that are settled decisions; the
backlog records what is still to be done.


Milestone 1 (deterministic vertical slice) and the adapter work of
Milestone 2 are implemented. See `docs/system-design.md` section 15 for the
milestone definitions.

Known gaps, deliberately left open:

- No real mined case has been run yet. `internal/boundaryvalidator` exists
  and gates every run, but it has only ever been exercised against
  synthetic bundles and deliberately contaminated copies of them — a real
  miner export will likely surface rules that need tightening or relaxing.
- The miner currently exports `miner/prospective-case/v1` — a flat root
  with a patch-and-SHA source model — which this engine cannot ingest. The
  decision was that the miner adapts to the engine;
  [`docs/prospective-bundle-contract.md`](docs/prospective-bundle-contract.md)
  is the normative target it must hit, and the migration delta is listed
  there. Making those miner changes is a `coder-miner` job, not this
  role's: the same context that holds the validator's heuristics should
  not also write the export they judge, or it will teach to the test.
- The claude adapter image exists and works:
  `engine-runner/adapter-claude:2.1.263` was built and smoke-tested on
  2026-09-08 and prints its version as non-root `reasoner`. The codex image
  has not been built.
- A live stage HAS now run: on 2026-09-08 reasoner-1 completed a real
  64-second claude call through the container, with mounts, network egress
  and credential injection all working. It then failed writing its result
  (the output-dir ownership bug, since fixed), and the run was interrupted
  before reaching reasoner-2. So: one stage proven end to end, no complete
  three-stage run yet, and resource limits still unexercised.
- The full three-stage pipeline HAS now run live and completed, on
  `configs/agents/haiku.yaml`, against the synthetic bundle: both handoffs
  correct, no contamination, $0.19-$0.20 per run. `configs/agents/sonnet.yaml`
  and `opus.yaml` exist but have never been run, and their budgets are
  informed estimates from the haiku numbers, not measurements.
- An agent set is ONE FILE PER ARM (`-agents configs/agents/opus.yaml`), not
  a directory of per-stage files. Shared binding at the top, per-stage
  budgets below.
- `cmd/bench` shells out to `docker` as the calling user. Where the socket
  requires `sudo`, a live run fails on permission denied regardless of the
  image. `DockerRunner.DockerPath` is injectable but `bench` exposes no flag
  for it.
- **The pilot cohort's cases all predate the reviewer models' training cutoff**, and no
  post-cutoff control arm is reachable from the dataset (0 of 1,667 candidates qualify).
  Contamination was probed rather than assumed:
  [`docs/contamination-probe-pilot-v1.md`](docs/contamination-probe-pilot-v1.md) records
  the method and results. Short version - no evidence of incident-level memorisation on
  opus, which is the only arm where the probe is valid; haiku and sonnet are unmeasured
  because they fail its false-negative guard. Read that document before treating any
  cohort result as clean, and before designing a similar probe: it also records how a
  first attempt produced ten identical refusals that would have read as a clean result.
  The door is now closed rather than merely unexplored: no corrective commit in the
  cohort postdates the cutoff, and the whole dataset is 2024 PRs, so no reselection
  within it can produce a contamination-safe cohort. Only a fresh collect over 2026 PRs
  would.
- Budget bounds are only partly preventable, because neither vendor CLI has
  a timeout or token flag. `max_wall_clock_seconds` is enforced by the
  orchestrator and `max_cost_usd` by claude alone; the token and tool-call
  bounds are checked after the fact against reported usage. `codex exec`
  has no budget flag at all and the codex adapter reports no usage, so a
  codex stage is currently unbounded except on wall clock - a run warns
  when a declared bound could not be checked.

