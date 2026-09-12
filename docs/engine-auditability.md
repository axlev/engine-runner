# What a sealed run does and does not pin down

Design principle 5 says "every prompt, model, budget, handoff rule, adapter
version, schema, and stopping rule is versioned". This document is the
standing set-difference against that claim: every input that can change a
stage's output, checked against what a sealed run actually records.

It outlives any one cohort. The gaps below were found while auditing the
pilot-v1 arms, but none of them are about those arms.

The ceiling gap — that the served model can move under a byte-identical
fingerprint — is recorded in `cohort-pilot-v1-results.md` instead, because it
bounds every comparison in that document specifically. It is restated at the
end here for completeness.

## Recorded, and hashed from the file on disk

| input | where it is recorded |
|---|---|
| protocol (prompts, handoffs, stopping rules) | `protocol_hash` |
| arm config — adapter, model, effort, budgets, tool set | `agent_config_hashes` |
| stage prompts | `prompt_hashes` |
| output schemas | `schema_versions` |
| input bundle | `input_checksums`, carried from the bundle's own manifest |
| resolved tool grant per stage | `tool_sets`, and each stage's `request.json` |
| CLI version inside the image | `adapter_versions` |
| image content digest | `container_digests` |
| credential kind | `auth_mode`, per stage record |

The last four were added in `381aee2`. Before that the tool grant was
hardcoded in adapter code, `adapter_versions` read `"claude/"` because the
field was never populated, and `container_digests` was absent.

Two of those are worth a note on *why* they are recorded the way they are.

**`adapter_versions` queries inside the image, not the host.** The adapter's
`Version()` originally read whatever `claude` was on `PATH`. On the machine
where this was found the host CLI was 2.1.269 and the pinned image carried
2.1.263, so populating the field from the host would have attributed six
patch releases of behaviour to the wrong code — in a field an auditor has no
reason to distrust. An empty string at least announces itself as missing; a
plausible wrong version does not.

**`container_digests` records the digest, not the tag.** A tag can be
repointed at new content, so two runs naming `adapter-claude:2.1.263` may
have executed different code. The digest is the only answer to what ran.

## Gaps

In descending order of how much they should worry a reader.

### 1. `-adapter-image` has no declared source

`cmd/bench/main.go:59`, default `""`. The image arrives as a command-line
flag and appears in no hashed config. The digest is now recorded, so you can
see *which* image ran — but nothing declares which image *should* have run.
A run against the wrong image is therefore auditable after the fact and
undetectable in advance, which is asymmetric with the arm config, where the
declared value and the recorded value can be compared.

Candidate fix: name the image in the arm file, so it falls under
`agent_config_hashes` like everything else that determines a stage's
behaviour.

### 2. Host performance is an input, via wall-clock

`max_wall_clock_seconds` is enforced by the orchestrator's context, not by
the vendor — neither CLI has a timeout flag. So a slow host can time out a
stage that a fast host would complete, and nothing records host load,
hardware, or contention.

This is live rather than theoretical: `opus-c21d519d` reasoner-2 ran at 96%
of its 900s bound on attempt 1, and `opus-de6d5d30` reasoner-2 at 93%. Either
would have failed on a machine under load, and the sealed record would
attribute the failure to the stage.

### 3. `runner.ResourceLimits` is declared and never populated

`grep` finds no `ResourceLimits{...}` outside tests, and `cmd/bench` builds
the adapter with `claude.New(image, nil)`, so `Adapter.Limits` is the zero
value and containers run unbounded.

Harmless today precisely because it never varies. Latent because the day
someone sets it, it becomes an input that changes stage behaviour and nothing
records it.

### 4. Sourced environment reaches the docker client

`build/cohort/run-cohort.sh:21` sources `~/.config/engine-runner.env` with
`set -a`, exporting everything in it.

This is narrower than it looks. Only two variables are read by the engine —
`ANTHROPIC_API_KEY` and `CLAUDE_CODE_OAUTH_TOKEN` (`credentials.go:19-20`) —
and the resulting credential kind is recorded as `auth_mode`. Arbitrary
environment does **not** reach the container: the adapter passes only the
credential variable plus `ENGINE_ATTEMPT`.

The residual is that exported variables still configure the *docker client*.
`DOCKER_HOST` would target a different daemon, and nothing in the run records
which daemon executed it.

### 5. Prompt-cache state decides budget outcomes

Cache hits and misses change a stage's cost and token counts materially
without changing its output. That would be uninteresting except that budgets
are enforced on cost, and `max_cost_usd` is the bound the vendor applies
before the fact — so cache state can decide whether a stage is killed.

Related and worth recording somewhere a reader will find it:
**`max_cost_usd` is not a hard ceiling.** A probe against the pinned image
recorded $0.005638 against a $0.002 cap. The CLI stops shortly after
exceeding rather than at, which is why sealed costs read 106% and 107% of cap
rather than exactly 100%.

## The ceiling: the served model can move

`claude-opus-5` is a name resolved server-side, and the review container runs
with unrestricted egress (`NetworkPolicy: NetworkEnabled`; system-design §16
leaves controlled egress an explicitly deferred decision). Two runs with
byte-identical fingerprints can therefore execute different model weights.

Nothing in this design can close that. It is the ceiling on reproducibility
for the whole project, and the consequence for reading results is concrete: a
re-run is not a replication, and arms that ran on different days differ by an
unrecorded variable. It belongs in every comparison's caveats rather than
being rediscovered each time.
