# Adapter image for internal/adapters/codex.
#
# The codex binary is statically linked (verified with ldd against
# codex-cli 0.153.2: "statically linked"; codex doctor reports the
# x86_64-unknown-linux-musl standalone build), so unlike the claude image
# this one has no libc constraint and could in principle run on scratch.
# It does not, for one reason: codex executes shell commands as its tool
# mechanism, so the model needs a real shell and basic file utilities to
# read the mounted reference files at all. A distroless/scratch base would
# leave it unable to do the one thing this stage exists for.
#
# As with the claude image, the binary is COPYed from the build context
# rather than fetched by an installer, to pin exact bytes (section 12) and
# to keep the build network-free.
#
# Build (from the repository root):
#
#   cp ~/.codex/packages/standalone/current/bin/codex docker/codex/codex
#   docker build -t engine-runner/adapter-codex:0.153.2 docker/codex
#
# NOTE: this image has never been built or run - written from verified
# facts about the binary in an environment with no Docker daemon access.
# Treat the first build as the real test.

FROM debian:bookworm-slim

# ca-certificates for TLS to the OpenAI API. Everything else here is
# already in the base image (sh, coreutils) and is what the model uses to
# read files under /workspace/input.
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*

RUN useradd --create-home --uid 10001 --shell /bin/bash reasoner

ARG CODEX_BINARY=codex
COPY ${CODEX_BINARY} /usr/local/bin/codex
RUN chmod 0755 /usr/local/bin/codex

# Optional companion binary shipped alongside codex in the standalone
# package. It backs "code mode" and is not needed by `codex exec`, so it is
# not copied here. If a stage ever turns out to need it, copy it in the same
# way and keep the two versions in lockstep:
#   COPY codex-code-mode-host /usr/local/bin/codex-code-mode-host

# CODEX_HOME is where codex reads config.toml and auth. Point it at a
# writable directory inside the image's home, deliberately empty: the
# adapter injects credentials as an environment variable per stage
# (section 9 - "credentials injected as scoped secrets"), and an empty
# CODEX_HOME means no host config.toml or persisted login can leak in and
# silently change a frozen protocol's behavior.
ENV HOME=/home/reasoner
ENV CODEX_HOME=/home/reasoner/.codex
RUN mkdir -p ${CODEX_HOME} && chown -R reasoner:reasoner /home/reasoner

RUN mkdir -p /workspace/input /workspace/output \
    && chown -R reasoner:reasoner /workspace

USER reasoner
WORKDIR /workspace

# No baked arguments: internal/adapters/codex builds the full `codex exec`
# invocation per stage (--skip-git-repo-check, --ephemeral, --sandbox
# read-only, -a never, --json, -o, -m, -c model_reasoning_effort) and passes
# it as the container command.
CMD ["codex", "--version"]
