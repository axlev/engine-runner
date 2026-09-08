#!/usr/bin/env bash
#
# Build one adapter image.
#
#   docker/build.sh            # claude (default)
#   docker/build.sh codex
#
# The flags this wraps are easy to get wrong by hand - the trailing context
# directory in particular reads like a stray word and is the argument
# `docker build` fails on when it is dropped.
#
# The image tag is derived from `<vendor> --version` rather than typed.
# docker/README.md requires the tag to name the CLI version inside the
# image, because the adapters record the adapter version separately from
# the image digest and the two must agree; a hand-typed tag is precisely
# how they stop agreeing.

set -euo pipefail

VENDOR="${1:-claude}"
case "$VENDOR" in
    claude | codex) ;;
    *)
        echo "build.sh: unknown vendor ${VENDOR@Q} (expected: claude, codex)" >&2
        exit 2
        ;;
esac

# Run from the repository root regardless of where this was invoked, so the
# build context path is stable.
cd "$(dirname "$(readlink -f "$0")")/.."

# Running the whole script under sudo is the natural thing to try, and it
# fails confusingly: $PATH becomes root's, the vendor CLI is installed under
# the invoking user's home, and the only symptom is "not on PATH". Only the
# docker command needs escalating, and that is handled below.
if [ -n "${SUDO_USER:-}" ]; then
    echo "build.sh: run this as $SUDO_USER, without sudo:" >&2
    echo "    docker/build.sh ${VENDOR}" >&2
    echo "  Only the docker command needs root, and the script adds sudo to" >&2
    echo "  that command itself. Under sudo the whole script gets root's PATH," >&2
    echo "  which cannot see $VENDOR in ~$SUDO_USER." >&2
    exit 2
fi

if ! command -v "$VENDOR" >/dev/null 2>&1; then
    echo "build.sh: $VENDOR is not on PATH as $(id -un), so there is nothing" >&2
    echo "  to copy into the image. The image contains the exact binary you" >&2
    echo "  already have (docker/README.md: 'Why the binary is copied, not" >&2
    echo "  installed'), so install or PATH-expose $VENDOR first." >&2
    exit 1
fi

# Resolve through the PATH entry to the real binary: for claude that is a
# symlink into a versioned directory, for codex a symlink into a versioned
# release package. Following it beats hardcoding either path, both of which
# move on upgrade.
SOURCE="$(readlink -f "$(command -v "$VENDOR")")"

# Both CLIs print a semver but wrap it differently ("2.1.263 (Claude Code)"
# vs "codex-cli 0.153.2"), so take the first semver in the line rather than
# a per-vendor field number.
VERSION="$("$VENDOR" --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1)"
if [ -z "$VERSION" ]; then
    echo "build.sh: could not parse a version from \`$VENDOR --version\`" >&2
    exit 1
fi

IMAGE="engine-runner/adapter-${VENDOR}:${VERSION}"

# Docker can only read files inside the build context, so the binary has to
# be copied in rather than referenced where it lives. These copies are
# gitignored - they are ~215MB and ~258MB.
echo "build.sh: copying $SOURCE -> docker/$VENDOR"
cp "$SOURCE" "docker/$VENDOR"
if ! cmp -s "$SOURCE" "docker/$VENDOR"; then
    echo "build.sh: copy does not match source; aborting rather than baking a truncated binary" >&2
    exit 1
fi

# The docker group is root-equivalent, so not being in it is a defensible
# state to be in rather than a misconfiguration to fix. Detect instead of
# assuming, and let sudo prompt on the terminal.
DOCKER=(docker)
if ! docker info >/dev/null 2>&1; then
    echo "build.sh: docker socket not reachable as $(id -un); using sudo"
    DOCKER=(sudo docker)
fi

echo "build.sh: building $IMAGE"
"${DOCKER[@]}" build \
    -f docker/Dockerfile \
    --build-arg "VENDOR=${VENDOR}" \
    --build-arg "VENDOR_BINARY=${VENDOR}" \
    -t "$IMAGE" \
    docker

# Section 9 captures the image digest as run provenance, so print it here:
# it is the identity that actually pins what ran, whereas the tag can be
# moved onto different bytes later.
echo
echo "build.sh: built $IMAGE"
"${DOCKER[@]}" image inspect "$IMAGE" --format 'build.sh: image id {{.Id}}' 2>/dev/null || true
echo
echo "Smoke-test the image alone (should print the CLI version, as user reasoner):"
echo "  docker run --rm $IMAGE"
echo
echo "Then one case through it (spends real money; needs a credential in the environment):"
echo "  go run ./cmd/bench -agents configs/agents -adapter-image $IMAGE \\"
echo "    -bundle fixtures/cases/happy-path/prospective -case-id happy-path"
