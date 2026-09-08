# engine-runner

Orchestration, isolation, validation, results, and scoring for a staged LLM
code-review benchmark.

The system measures the incremental engineering value of a three-stage
review pipeline over frozen public code changes, keeping prospective review
evidence physically separate from retrospective evaluation evidence so
results can be reproduced and audited without leaking answers to reviewers.

Full design: [`docs/system-design.md`](docs/system-design.md).
Agent access boundaries: [`AGENTS.md`](AGENTS.md).

## The pipeline

Three isolated reasoning stages run per case, each in a fresh environment,
communicating only through explicit, schema-validated handoff files:

| Stage | Role | Sees | Produces |
|---|---|---|---|
| `reasoner-1` | Discovery — high recall | Prospective bundle only | `review-a.json` |
| `reasoner-2` | Evidence — causal chains | Bundle + `review-a.json` (when the protocol enables the handoff) | `review-b.json` |
| `reasoner-3` | Adversarial verification | Same as reasoner-2, plus `review-b.json` | `review-c.json` |

Before any of that runs, a deterministic boundary validator checks the
case's prospective bundle for contamination — oracle-shaped files, git
metadata, outcome-carrying metadata fields, checksum mismatches. A bundle
that fails is rejected before a single token is spent, and the run is
sealed as `invalidated` with a report naming exactly which rule tripped.

Deterministic scoring runs afterwards, and only afterwards.

## Quick start

No credentials or network required — the default protocol runs entirely on
synthetic fixtures through the deterministic fixture adapter:

```bash
go run ./cmd/bench \
  -bundle fixtures/cases/happy-path/prospective \
  -case-id happy-path
```

That produces a sealed, checksummed result tree:

```
build/results/<run-id>/
├── run.json                      # protocol, status, fingerprints, per-stage summary
├── boundary-validation.json      # per-rule pass/fail for the input bundle
├── checksums.sha256              # covers every other file in the tree
├── events.jsonl
├── stages/reasoner-{1,2,3}/
│   ├── request.json              # the exact request sent to the adapter
│   ├── review-{a,b,c}.json       # the accepted output
│   └── telemetry.json            # every attempt, including failed ones
└── evaluation/
    ├── scores.json
    └── findings.json
```

Reruns of the same case produce identical fingerprints — the reproducibility
property the whole design exists to support.

### Useful flags

| Flag | Default | Purpose |
|---|---|---|
| `-bundle` | (required) | Case's `prospective/` directory |
| `-case-id` | (required) | Case identifier; also selects the fixture scenario |
| `-run-id` | generated | Run identifier |
| `-protocol` | `configs/protocols/pilot-v1.yaml` | Frozen protocol to run under |
| `-results-root` | `build/results` | Where the sealed run tree is written |
| `-workspace-root` | `~/.cache/engine-runner/runs` | Per-attempt scratch. Outside the repo on purpose: it is bind-mounted into stage containers, which may never see the engine's source |
| `-agents` | `fixtures/agents` | Vendor binding directory; use `configs/agents` for a real run |
| `-adapter-image` | — | Container image, for the `claude` / `codex` adapters |

## Two configurations: the experiment and the vendor binding

A run is defined by two files that vary independently:

- **`-protocol configs/protocols/pilot-v1.yaml`** — the *experiment*:
  prompts, handoffs, output contracts, stopping rules. This is the frozen
  cohort definition and names no vendor.
- **`-agents <dir>`** — the *vendor binding*: which adapter and model runs
  each stage, at what effort and under what budget.

So the same frozen cohort can be smoke-tested and then run for real, and
both produce the same `protocol_version` and the same `protocol_hash`:

```bash
# smoke test - no credentials, no network, no cost (the default)
bench -agents fixtures/agents  -bundle ... -case-id ...

# the real cohort
bench -agents configs/agents   -bundle ... -case-id ...
```

Both the protocol and every agent config are hashed into each run's
`fingerprints`, so a result records which experiment *and* which vendor
binding produced it.

`fixture` needs nothing. `claude` and `codex` need a credential in the
environment (`ANTHROPIC_API_KEY` or `CLAUDE_CODE_OAUTH_TOKEN`;
`OPENAI_API_KEY` or `CODEX_ACCESS_TOKEN`) plus a container image via
`-adapter-image`.

**Which credential kind you use changes the run, not just the billing.**
With `ANTHROPIC_API_KEY` the claude adapter passes `--bare`, so the stage
runs with no hooks, no LSP and no `CLAUDE.md` discovery. `CLAUDE_CODE_OAUTH_TOKEN`
cannot use `--bare` and runs with the CLI's normal defaults active — which
means a `CLAUDE.md` inside a mined repository could influence a reviewer
outside the frozen protocol. Each stage records which kind produced it as
`auth_mode` in `run.json`, so results are self-describing; treat the two as
non-comparable rather than assuming.

## Layout

```
cmd/bench/              CLI entrypoint
internal/
  adapters/             AgentAdapter contract + claude, codex, fixture
  boundaryvalidator/    Rejects contaminated bundles before any stage runs
  contextbuilder/       Allow-lists exactly what each stage may see
  orchestrator/         Protocol loading, stage sequencing, retries, validation
  runner/               Fresh per-attempt workspaces + container isolation
  results/              Sealed, checksummed result tree
  evaluation/           Deterministic scoring
configs/protocols/      Frozen protocol manifests (the experiment)
configs/agents/         Per-stage vendor binding (adapter, model, budget)
schemas/                JSON Schemas for every artifact
fixtures/               Synthetic cases, prompts, and adapter scenarios
```

## Development

```bash
go build ./... && go vet ./... && go test ./... && gofmt -l .
```

All four must be clean. Tests need no credentials, no network, and no
Docker; tests that would require Docker skip themselves when no daemon is
reachable.

## Status

Milestone 1 (deterministic vertical slice) is complete, as is the adapter
work of Milestone 2 and the boundary validator that opens Milestone 3.

The claude adapter image builds and runs (`docker/build.sh`). Known gaps
are listed in [`AGENTS.md`](AGENTS.md) — notably that no stage has yet run
*through* a container, so the runner's mounts, network policy and
credential injection are still exercised only by unit tests, and no live
vendor run has happened.
