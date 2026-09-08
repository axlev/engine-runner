# Adapter image for internal/adapters/claude.
#
# Base is glibc-based on purpose: the claude binary is dynamically linked
# (verified with ldd against claude 2.1.263 - it needs libc.so.6, librt,
# libpthread, libdl, libm and /lib64/ld-linux-x86-64.so.2). An Alpine/musl
# base will not run it.
#
# The binary is COPYed in from the build context rather than downloaded by
# an installer script. That is deliberate: docs/system-design.md section 12
# requires pinning executable versions, and copying exact bytes you already
# have pins them far more tightly than "whatever the installer served
# today". It also means this image builds with no network access.
#
# Build (from the repository root):
#
#   cp ~/.local/share/claude/versions/2.1.263 docker/claude/claude
#   docker build -t engine-runner/adapter-claude:2.1.263 docker/claude
#
# Then run a case with:
#
#   go run ./cmd/bench -adapter-image engine-runner/adapter-claude:2.1.263 ...
#
# NOTE: this image has never been built or run. It was written from verified
# facts about the binary (linkage, path, version) in an environment with no
# Docker daemon access. Treat the first build as the real test.

FROM debian:bookworm-slim

# ca-certificates is required: the adapter runs with network enabled so the
# CLI can reach the Anthropic API, and TLS verification fails without it.
# Nothing else is installed - the reasoning stage only needs to read files
# and talk to the API.
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Never run the reasoning stage as root. The container also gets a
# read-only input mount and resource limits from the runner (section 9);
# this is the in-image half of that posture.
RUN useradd --create-home --uid 10001 --shell /bin/bash reasoner

ARG CLAUDE_BINARY=claude
COPY ${CLAUDE_BINARY} /usr/local/bin/claude
RUN chmod 0755 /usr/local/bin/claude

# The CLI writes config and cache under $HOME. Give it a writable home that
# is not the workspace, so nothing it writes for itself can land in the
# stage's output mount and be mistaken for stage output.
ENV HOME=/home/reasoner

# Mount points the adapter binds: input read-only, output writable.
RUN mkdir -p /workspace/input /workspace/output \
    && chown -R reasoner:reasoner /workspace

USER reasoner
WORKDIR /workspace

# No ENTRYPOINT and no baked arguments on purpose: internal/adapters/claude
# builds the full argument list per stage (--bare, -p, --output-format json,
# --tools Read, --model, --effort, --max-budget-usd) and passes it as the
# container command. Baking flags here would put part of the frozen
# protocol's behavior outside the protocol, where it could not be
# fingerprinted.
CMD ["claude", "--version"]
