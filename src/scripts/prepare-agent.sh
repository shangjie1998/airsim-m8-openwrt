#!/bin/sh
# Read only the pinned upstream commit. Never patch the user's AirSIM checkout.
set -eu
HERE=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
UPSTREAM=${AIRSIM_UPSTREAM_REPO:-$HERE/../AirSIM}
BASE=$(cat "$HERE/patches/AIRSIM_BASE")
git -C "$UPSTREAM" cat-file -e "$BASE^{commit}"
mkdir -p "$HERE/out"
TASK_SOURCE=$(mktemp -d "$HERE/out/agent-source.XXXXXX")
git -C "$UPSTREAM" archive --format=tar --output="$TASK_SOURCE/upstream.tar" "$BASE" module/module-agent LICENSE NOTICE THIRD_PARTY_NOTICES.md
tar -xf "$TASK_SOURCE/upstream.tar" -C "$TASK_SOURCE"
git -C "$TASK_SOURCE" init -q
git -C "$TASK_SOURCE" add module LICENSE NOTICE THIRD_PARTY_NOTICES.md
git -C "$TASK_SOURCE" -c user.name='MU300 build' -c user.email='build@localhost' -c commit.gpgsign=false commit -qm 'Pinned upstream Agent baseline'
git -C "$TASK_SOURCE" apply --check "$HERE/patches/AirSIM-mu300.patch"
git -C "$TASK_SOURCE" apply "$HERE/patches/AirSIM-mu300.patch"
printf '%s\n' "$TASK_SOURCE"
