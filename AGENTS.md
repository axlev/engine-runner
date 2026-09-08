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
- Compiled binaries, container artifacts, and run scratch live under
  `build/` (gitignored) — never committed.
- Real production results live under `/home/alex/data/results`, outside
  this repository entirely.
- Source control here holds code, prompts, protocol manifests, schemas,
  docs, and small synthetic fixtures. It must never hold credentials,
  agent memory, raw mined data, oracle data, production logs, or
  unpublished benchmark results.

## Design invariants to preserve

These are the properties the whole system exists to guarantee. Changes that
weaken them need an explicit design decision, not a quiet refactor.

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

## Current state

Milestone 1 (deterministic vertical slice) and the adapter work of
Milestone 2 are implemented. See `docs/system-design.md` section 15 for the
milestone definitions.

Known gaps, deliberately left open:

- No `internal/boundaryvalidator` yet — it needs a real miner-produced
  bundle to validate (Milestone 3).
- `configs/agents/*.yaml` from the documented layout is not created; its
  contract isn't specified and isn't load-bearing for the current
  milestones.
- Container execution is unit-tested at the argument-construction level but
  has never been run live (no Docker daemon access in the development
  sandbox, and no adapter container image has been built yet).
- `pilot-v1.yaml` still points at a placeholder prompt; authoring the real
  frozen reasoner prompts is Milestone 4 work.
