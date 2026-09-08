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
| `-adapter-image` | — | Container image, for the `claude` / `codex` adapters |

## Swapping providers

Changing which vendor runs the stages is a configuration change only — edit
`adapter` in the protocol YAML:

```yaml
adapter: fixture   # or: claude, codex
```

`fixture` needs nothing. `claude` and `codex` need a credential in the
environment (`ANTHROPIC_API_KEY` or `CLAUDE_CODE_OAUTH_TOKEN`;
`OPENAI_API_KEY` or `CODEX_ACCESS_TOKEN`) plus a container image via
`-adapter-image`.

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
configs/protocols/      Frozen protocol manifests
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
work of Milestone 2 and the boundary validator that opens Milestone 3. Known gaps are listed in [`AGENTS.md`](AGENTS.md) —
notably that container execution has been unit-tested at the
argument-construction level but never run live, and that no adapter
container image has been built yet.
