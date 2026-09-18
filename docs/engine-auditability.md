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
| the engine's own code | `engine_commit` |

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

**`engine_commit` is read from the build, not passed in.** Every other entry
above pins an input to the engine; this one pins the engine itself, which was
the last thing a sealed run could not identify. It is taken from the binary's
VCS stamp, or - when there is none, which is the case under `go run`, and
`cmd/h1run` launches every sealed case with `go run ./cmd/bench` - from the
tree the process ran in. A tree with uncommitted changes is recorded
`<sha>-dirty`, because an uncommitted engine is exactly the provenance a
reader most needs to see, and one whose cleanliness cannot be determined is
`<sha>-unknown`. Nothing is invented: when provenance cannot be established
the field is absent.

**Prompt delivery changed mid-cohort, and the bytes did not.** Up to
`affb724` both adapters passed the prompt as the final argv element; from
`8023221` it is piped on stdin, because a prompt over the kernel's 128 KiB
per-argument limit aborted the invocation before any model call. H1's probes
1-19 were sealed under argv delivery and probe 20 and every arm run under
stdin. `prompt_hashes` is taken over the prompt bytes, which are identical
either way, so the hashes do not distinguish the two - this paragraph is the
record that they differed at all.

The H1 results document is generated (`h1score -render`), so a fact written
into it by hand is lost on the next render. Pass it instead:

```
h1score -render h1-score.json \
  -note 'probes 1-19 delivered the prompt via argv (engine <= affb724); probe 20 and all arm runs via stdin (engine >= 8023221); identical bytes'
```

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
and the resulting credential kind is recorded as `auth_mode`, beside
`safety_flags` - the customization-disabling flags the invocation actually
passed (`--bare` under an API key, `--safe-mode` under an OAuth token), so
the mechanism behind "repository content cannot steer a reviewer" is
observable in the sealed run rather than inferred from the credential kind.
Note that `cost_usd` under `oauth_token` is the CLI's estimate of equivalent
API cost, not an amount billed. Arbitrary
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

### 6. `-dry-run` does not exercise the vendor schema projection

The fixture adapter never builds a CLI invocation, so `-dry-run` never
projects a stage's `output_schema` through `vendorJSONSchema` and never sends
it anywhere. Everything a dry run does prove — gate loading, case discovery,
ordering, resume, sealing — it proves honestly; what it cannot prove is that
the resulting invocation is one the vendor will accept.

This was found the expensive way. `schemas/h1-review-a.schema.json` carries a
top-level `allOf`, which the API refuses in a tool `input_schema`:

```
400 tools.N.custom.input_schema: input_schema does not support oneOf, allOf,
or anyOf at the top level
```

Arm G's only stage and arm T's `reasoner-1` both use that schema, so every
arm run failed at the first stage while every dry run over the same cohort
passed. The projection now drops root combinators (they are constraints, and
the caller still validates the full schema), and
`TestEveryShippedSchemaProjectsWithoutRootCombinators` projects every file in
`schemas/` and fails if any root combinator survives - so this class is
caught by `go test` rather than by a paid batch.

The general point stands and is not fixed: a green dry run says nothing about
vendor acceptance. Anything that shapes the real invocation - flags, schema
projection, argument construction - is exercised only by a real call.

### 7. A vendor CLI can succeed while doing nothing

`codex exec` exits zero when its tools cannot run. Three separate
dependencies were missing in turn, and each produced a complete, sealed,
plausible result:

- `codex-code-mode-host` absent: every tool call fails with "failed to
  spawn code-mode host". One startup warning, then exit 0.
- `bubblewrap` absent: the tool sandbox cannot start. Exit 0.
- `bwrap` present but unable to create a user namespace inside the
  container: same. Exit 0.

In all three the model answered from the prompt alone and returned an
articulate INCONCLUSIVE verdict explaining that it could not read the
repository - which is what an honest verifier SHOULD say, and is exactly
what made it dangerous. Across a full pass it would have read as "the
skeptic cannot settle these questions": a fact about the image, published
as a fact about the vendor.

Nothing in the sealed record distinguishes that from a real answer. Exit
code is 0, the schema validates, usage is reported, the reasoning is
coherent. The only tell is `evidence: []` where citations were required,
and no check enforced that.

**What follows.** An adapter for a vendor CLI cannot be trusted on the
strength of it running. The flag list in particular is a claim about
another program that only that program can check: `-a never` was carried
in the codex adapter and its test REQUIRED it, while codex 0.153.2 rejects
it at argument parsing - so the adapter was green against a CLI it could
not invoke at all. A first real call is the cheapest test in the suite and
the only one that exercises this class.

The related gap is that a stage's DECLARED capability is not verified
against what it actually had. `tool_sets` records the grant; nothing
records whether a single tool call succeeded. A stage that was granted
Read, Grep and Glob and made zero successful calls is indistinguishable in
the sealed run from one that used them.

### 8. Reasoning effort defaults are per-vendor and silent

`h1verify` did not set `ReasoningLevel`, so the verifier ran at codex's
own default. That default is NONE, while the arms it judged ran
`claude-opus-5` at `high`. The run header states it plainly - "reasoning
effort: none" - but nothing in the sealed result did, and the first six
verifications came back CONFIRMED, which is what a model that is not
reasoning does with a plausible claim.

A null result on that configuration would not have meant "a
different-vendor skeptic cannot reject false positives". It would have
meant "a non-reasoning skeptic cannot" - the configuration as the finding,
wearing the hypothesis's clothes. Effort and model are now recorded in
every h1-verify report; an unset effort must never mean "whatever this
vendor happens to default to".

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
