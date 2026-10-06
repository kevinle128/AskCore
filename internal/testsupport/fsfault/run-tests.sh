#!/usr/bin/env bash
set -euo pipefail
# Run from any directory. The source mount is read-only and the container is removed.
fixture=$(cd "$(dirname "$0")" && pwd)
workspace=$(cd "$fixture/../../.." && pwd)
source_path=${FSFAULT_SOURCE:-$workspace}
engine=("${FSFAULT_ENGINE:-podman}")
if [[ -n "${FSFAULT_CONNECTION:-}" ]]; then
    engine+=(--connection "$FSFAULT_CONNECTION")
fi
image=${FSFAULT_IMAGE:-askcore-fsync-fault:local}
container="askcore-fsync-fault-${UID}-$$"
pattern=${1:-^TestAuthCommandSignalsDuringLocalDurableCommit$}
if (( $# )); then shift; fi
cleanup() { "${engine[@]}" rm -f "$container" >/dev/null 2>&1 || true; }
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
"${engine[@]}" build -t "$image" "$fixture"
"${engine[@]}" run --rm --init --name "$container" \
    --device /dev/fuse --cap-add SYS_ADMIN \
    --security-opt label=disable \
    -e GOTOOLCHAIN=local \
    -e GOCACHE=/var/cache/askcore-fsync/build \
    -e GOMODCACHE=/var/cache/askcore-fsync/mod \
    -v askcore-fsync-fault-cache:/var/cache/askcore-fsync \
    -v "$source_path:/workspace:ro" "$image" \
    go test -p 1 -mod=readonly -tags fsfault ./cmd/tui -run "$pattern" -count=1 -v -timeout=5m "$@"
