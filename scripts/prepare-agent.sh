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
# Second, independent patch: render the SIM's own number as E.164 ("+8613...")
# instead of the bare country-code form some SIMs store.
git -C "$TASK_SOURCE" apply --check "$HERE/patches/AirSIM-e164.patch"
git -C "$TASK_SOURCE" apply "$HERE/patches/AirSIM-e164.patch"
# Third, independent patch: stop the end-grace window from vetoing hangup/answer/
# DTMF, and return a send timestamp with the SMS reply.
git -C "$TASK_SOURCE" apply --check "$HERE/patches/AirSIM-m8-sms-callfix.patch"
git -C "$TASK_SOURCE" apply "$HERE/patches/AirSIM-m8-sms-callfix.patch"
# Fourth, independent patch: poll the call state once a second while a call is up.
# No event-driven URC is active on this build, so a hang-up is otherwise only seen
# by the next poll and the end-of-call state lags by a couple of seconds.
git -C "$TASK_SOURCE" apply --check "$HERE/patches/AirSIM-m8-fast-hangup.patch"
git -C "$TASK_SOURCE" apply "$HERE/patches/AirSIM-m8-fast-hangup.patch"
# The upstream .gitattributes marks the Agent go.mod/go.sum export-ignore, so
# `git archive` leaves them out and a module-mode build cannot resolve deps.
git -C "$UPSTREAM" show "$BASE:module/module-agent/go.mod" > "$TASK_SOURCE/module/module-agent/go.mod"
git -C "$UPSTREAM" show "$BASE:module/module-agent/go.sum" > "$TASK_SOURCE/module/module-agent/go.sum"
printf '%s\n' "$TASK_SOURCE"
