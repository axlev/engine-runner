# Adapter container images

Images the `claude` and `codex` adapters execute a reasoning stage inside,
per [`docs/system-design.md`](../docs/system-design.md) section 9.

> **These images have never been built or run.** They were written from
> verified facts about the vendor binaries (linkage, install path, version)
> in an environment with no Docker daemon access. The first build is the
> real test — expect to fix something.

## Why the binary is copied, not installed

Both Dockerfiles `COPY` a vendor CLI binary from the build context instead
of running an installer script. Two reasons:

1. **Pinning.** Section 12 requires pinning executable versions. Copying
   exact bytes you already have pins them harder than "whatever the
   installer served today", and the resulting image digest — captured per
   section 9 — then genuinely identifies what ran.
2. **No network at build time.** Nothing is fetched, so the build is
   reproducible offline and does not depend on installer URLs that change.

The cost is that you supply the binary. That's the intended trade.

## Building

From the repository root:

```bash
# claude (glibc-based image - the binary is dynamically linked)
cp ~/.local/share/claude/versions/2.1.263 docker/claude/claude
docker build -t engine-runner/adapter-claude:2.1.263 docker/claude

# codex (binary is statically linked, but the image still needs a shell)
cp ~/.codex/packages/standalone/current/bin/codex docker/codex/codex
docker build -t engine-runner/adapter-codex:0.153.2 docker/codex
```

Copied binaries are gitignored — they're ~215MB and ~258MB.

Tag the image with the CLI version it contains. The adapters record the
adapter version separately from the image digest, and the two should agree.

## Running a case against one

```bash
go run ./cmd/bench \
  -protocol configs/protocols/pilot-v1.yaml \
  -adapter-image engine-runner/adapter-claude:2.1.263 \
  -bundle fixtures/cases/happy-path/prospective \
  -case-id happy-path
```

with `adapter: claude` (or `codex`) set in the protocol YAML, and a
credential in the environment:

| Adapter | Credential env var (either) |
|---|---|
| `claude` | `ANTHROPIC_API_KEY` or `CLAUDE_CODE_OAUTH_TOKEN` |
| `codex` | `OPENAI_API_KEY` or `CODEX_ACCESS_TOKEN` |

## What the images deliberately do not do

- **No baked `ENTRYPOINT` arguments.** Each adapter builds the full
  invocation per stage and passes it as the container command. Baking flags
  into the image would move part of the protocol's behavior outside the
  protocol, where it can't be fingerprinted.
- **No credentials.** They're injected per stage as scoped environment
  variables by the runner, never built into a layer.
- **Not run as root.** Both images create a `reasoner` user (uid 10001).
  This is the in-image half of section 9's posture; the read-only input
  mount, disabled-by-default network, and resource limits come from
  `internal/runner`'s `ContainerSpec`.
- **`CODEX_HOME` points at an empty directory** in the codex image, so no
  host `config.toml` or persisted login can leak in and silently alter a
  frozen protocol's behavior.

## Known gaps

- Base images are pinned by tag (`debian:bookworm-slim`), not by digest.
  Pinning by digest is the right final step for reproducibility — it just
  couldn't be resolved without a working Docker daemon.
- `codex-code-mode-host`, the companion binary in the codex standalone
  package, is not copied in. `codex exec` shouldn't need it; if a stage
  turns out to, copy it alongside and keep the versions in lockstep.
- Neither image has been built, so the layer set, the binary's runtime
  dependencies beyond libc, and whether the CLIs run cleanly as a non-root
  user with an empty home are all unverified.
