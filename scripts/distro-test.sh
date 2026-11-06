#!/usr/bin/env bash
# Cross-distribution test in disposable containers (Docker or Podman).
# Usage: scripts/distro-test.sh [image ...]
# Nothing runs on the host system except building; containers are removed afterwards.
set -uo pipefail
cd "$(dirname "$0")/.."
RT=${CONTAINER_RT:-$(command -v docker || command -v podman)}
[ -n "$RT" ] || { echo "need docker or podman"; exit 2; }
IMAGES=("$@")
[ ${#IMAGES[@]} -gt 0 ] || IMAGES=(archlinux:latest ubuntu:24.04 debian:12 fedora:42 opensuse/tumbleweed:latest)

D=$(mktemp -d); trap 'rm -rf "$D"' EXIT
export CGO_ENABLED=0
go build -trimpath -ldflags "-s -w" -o "$D/turbo" ./cmd/turbo
(cd scripts/ptysmoke && go build -trimpath -o "$D/ptysmoke" .) || echo "warning: pty driver not built; TUI checks will be skipped"
mkdir -p "$D/tests"
for p in $(go list ./internal/...); do
  go test -c -o "$D/tests/$(basename "$p").test" "$p" 2>/dev/null || true
done
cp scripts/container-smoke.sh "$D/"
chmod -R a+rX "$D"

rc=0
for img in "${IMAGES[@]}"; do
  for user in "0:0" "1000:1000"; do
    echo "################ $img as uid ${user%%:*}"
    "$RT" run --rm --pull=never --user "$user" --cap-drop=ALL --security-opt no-new-privileges \
      -e HOME=/tmp/home -e BIN=/dist/turbo -e TESTS=/dist/tests -e PTY=/dist/ptysmoke \
      -v "$D":/dist:ro "$img" sh -c 'mkdir -p /tmp/home/bin && sh /dist/container-smoke.sh' || rc=1
  done
done
exit $rc
