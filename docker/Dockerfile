# One adapter image, parameterized by vendor.
#
# The claude and codex images were originally two near-identical files.
# They differed only in which binary landed at which name, so the hardening
# rules below (non-root user, CA certs, workspace layout, empty config
# home, no baked arguments) were duplicated - and duplicated rules in a
# security-relevant file drift the moment someone hardens one and forgets
# the other. They are stated once here.
#
# Deliberately still TWO IMAGES, not one image containing both CLIs:
# section 9 requires capturing the image digest as run provenance, and a
# shared image would change a claude run's digest whenever codex was
# upgraded, so the digest would stop meaning "what ran". A single image
# would also put a second executable inside every reasoning container for
# no benefit.
#
# Build with docker/build.sh, which derives the image tag from the CLI's
# own --version so the tag cannot drift from the bytes inside the image:
#
#   docker/build.sh          # claude
#   docker/build.sh codex
#
# See docker/README.md for the equivalent by hand.
#
# Built and smoke-tested for claude on 2026-09-08 (2.1.263): all 16 steps
# pass, the ARG-in-COPY substitution at step 7 resolves, and the image
# prints its version as non-root reasoner. The codex build is still
# unexercised - it is statically linked and reads CODEX_HOME, so it
# exercises different parts of this file.

# Default base is glibc-based, and that is a requirement rather than a
# preference: `ldd` on claude 2.1.263 shows it dynamically linked against
# libc.so.6, librt, libpthread, libdl, libm and /lib64/ld-linux-x86-64.so.2,
# so an Alpine/musl base would produce an image that cannot run it.
#
# codex 0.153.2 is statically linked (`ldd`: "statically linked", the
# x86_64-unknown-linux-musl standalone build) and has no such constraint -
# but it still needs this base, because codex executes shell commands as
# its tool mechanism and a scratch/distroless image would leave it unable
# to read the mounted reference files at all.
#
# Overridable for a future vendor with different needs.
ARG BASE_IMAGE=debian:bookworm-slim
FROM ${BASE_IMAGE}

# ARGs used after FROM must be re-declared after it.
ARG VENDOR
ARG VENDOR_BINARY

# Required: the adapters run with network enabled so the CLI can reach its
# vendor API, and TLS verification fails without these. Nothing else is
# installed; a reasoning stage only reads files and talks to the API.
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Never run a reasoning stage as root. This is the in-image half of section
# 9's posture; the read-only input mount, network policy and resource
# limits come from internal/runner's ContainerSpec.
RUN useradd --create-home --uid 10001 --shell /bin/bash reasoner

COPY ${VENDOR_BINARY} /usr/local/bin/${VENDOR}
RUN chmod 0755 /usr/local/bin/${VENDOR}

# codex's file-reading tools run in a separate "code mode host" process. If
# it is absent the CLI does NOT fail: it warns once, and every tool call
# then fails with "failed to spawn code-mode host", so the model answers
# from the prompt alone while the run looks successful. That produced four
# confident INCONCLUSIVE verdicts on a repository the model never read.
#
# Copied in for codex and absent for claude, so the claude image's digest
# does not move. The build script supplies an empty placeholder when the
# vendor has no such helper, because COPY of a missing path fails the build.
ARG VENDOR_HELPER=README.md
COPY ${VENDOR_HELPER} /usr/local/bin/codex-code-mode-host
RUN if [ "${VENDOR}" = "codex" ]; then chmod 0755 /usr/local/bin/codex-code-mode-host; \
    else rm -f /usr/local/bin/codex-code-mode-host; fi

# A writable home that is not the workspace, so anything the CLI writes for
# itself cannot land in the stage's output mount and be mistaken for stage
# output.
ENV HOME=/home/reasoner

# CODEX_HOME is where codex reads config.toml and auth. Pointing it at an
# empty directory means no host config or persisted login can leak in and
# silently change a frozen protocol's behavior. It is inert for vendors
# that do not read it, which is why it is set unconditionally rather than
# branched on VENDOR.
ENV CODEX_HOME=/home/reasoner/.codex
RUN mkdir -p ${CODEX_HOME} && chown -R reasoner:reasoner /home/reasoner

RUN mkdir -p /workspace/input /workspace/output \
    && chown -R reasoner:reasoner /workspace

USER reasoner
WORKDIR /workspace

# No ENTRYPOINT and no baked arguments on purpose: each adapter builds the
# full invocation per stage and passes it as the container command. Baking
# flags here would move part of the frozen protocol's behavior outside the
# protocol, where it cannot be fingerprinted.
#
# ENV rather than CMD [...] because CMD's exec form does not expand ARGs.
ENV VENDOR=${VENDOR}
CMD ["sh", "-c", "$VENDOR --version"]
