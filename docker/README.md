# Adapter container images

Images the `claude` and `codex` adapters execute a reasoning stage inside,
per [`docs/system-design.md`](../docs/system-design.md) section 9.

> **Status:** the claude image builds and runs. Verified 2026-09-08 on
> reviewbench-01: `engine-runner/adapter-claude:2.1.263`
> (`sha256:d4c96baa5ef4…`) builds all 16 steps and prints
> `2.1.263 (Claude Code)` as uid 10001 `reasoner`. That covers the three
> things previously unverified: the `ARG`-in-`COPY` substitution, the
> binary's runtime deps beyond libc under `bookworm-slim`, and the CLI
> running non-root with an empty config home.
>
> **The codex image has still never been built**, and no stage has yet run
> *through* either image — `internal/runner`'s mounts, network policy,
> resource limits and credential injection remain unexercised.

## One Dockerfile, two images

The vendors differ only in which binary lands at which name, so the
hardening rules — non-root user, CA certs, workspace layout, empty config
home, no baked arguments — are stated once in a single parameterized
`Dockerfile`. Duplicated rules in a security-relevant file drift the
moment someone hardens one copy and forgets the other.

They are still built as **two images**, deliberately. Section 9 requires
capturing the image digest as run provenance; a shared image would change
a claude run's digest whenever codex was upgraded, so the digest would
stop meaning "what ran". It would also place a second executable inside
every reasoning container for no benefit.

## Why the binary is copied, not installed

The Dockerfile `COPY`s a vendor CLI binary from the build context instead
of running an installer script. Two reasons:

1. **Pinning.** Section 12 requires pinning executable versions. Copying
   exact bytes you already have pins them harder than "whatever the
   installer served today", and the resulting image digest — captured per
   section 9 — then genuinely identifies what ran.
2. **No network at build time.** Nothing is fetched, so the build is
   reproducible offline and does not depend on installer URLs that change.

The cost is that you supply the binary. That's the intended trade.

## Building

```bash
docker/build.sh          # claude
docker/build.sh codex
```

Run it **as yourself, not under `sudo`** — it adds `sudo` to the `docker`
command internally when the socket needs it. Running the whole script as
root gives it root's `$PATH`, which can't see a vendor CLI installed under
your home; it now says so rather than reporting a bare "not on PATH".

It copies the binary into the build context, derives the tag from
`<vendor> --version`, and can be run from anywhere — it locates the
repository root itself.

The tag is derived rather than typed on purpose. It has to name the CLI
version inside the image, because the adapters record the adapter version
separately from the image digest and the two must agree — a hand-typed tag
is exactly how they stop agreeing.

Copied binaries are gitignored — they're ~215MB and ~258MB.

<details>
<summary>The equivalent by hand</summary>

```bash
cp ~/.local/share/claude/versions/2.1.263 docker/claude
docker build -f docker/Dockerfile \
  --build-arg VENDOR=claude --build-arg VENDOR_BINARY=claude \
  -t engine-runner/adapter-claude:2.1.263 docker
```

The trailing `docker` is the build context directory, not a stray word —
`docker build` fails with "requires 1 argument" when it's dropped.

A future vendor needing a different base can override it:
`--build-arg BASE_IMAGE=...`.

</details>

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
- **Not run as root.** The image creates a `reasoner` user (uid 10001).
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
- The codex image has not been built. The claude build validates the shared
  Dockerfile, but codex is statically linked and reads `CODEX_HOME`, so its
  own build and non-root run are still unverified.
- No stage has run *through* an image yet. The smoke test runs the baked
  `CMD`; it does not exercise `internal/runner`'s read-only input mount,
  output mount, network policy, resource limits, or credential injection.
- `cmd/bench` invokes `docker` directly as the calling user. On a host where
  the socket needs `sudo`, a live run fails on permission denied even though
  the image is fine. `DockerRunner.DockerPath` is injectable, but `bench`
  exposes no flag for it today; the practical fix is adding the user to the
  `docker` group, which is root-equivalent and therefore a deliberate call.
