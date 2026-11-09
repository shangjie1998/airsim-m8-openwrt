#!/bin/sh
set -eu
HERE=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
[ "$(git -C "$HERE" rev-parse --show-toplevel)" = "$HERE" ] || { echo 'Requires a standalone committed repository' >&2; exit 1; }
git -C "$HERE" diff --quiet HEAD -- . || { echo 'Commit source before archiving' >&2; exit 1; }
[ -z "$(git -C "$HERE" ls-files --others --exclude-standard)" ] || { echo 'Review untracked source before archiving' >&2; exit 1; }
VERSION=$(cat "$HERE/VERSION")
sh "$HERE/scripts/fetch-sources.sh"
AGENT_TREE=$(sh "$HERE/scripts/prepare-agent.sh")
STAGE=$(mktemp -d "$HERE/out/source-release.XXXXXX")
mkdir -p "$STAGE/plugin" "$STAGE/AirSIM" "$STAGE/dependencies" "$HERE/dist"
git -C "$HERE" archive --format=tar --output="$STAGE/plugin.tar" HEAD
tar -xf "$STAGE/plugin.tar" -C "$STAGE/plugin"
cp -R "$AGENT_TREE/module" "$AGENT_TREE/LICENSE" "$AGENT_TREE/NOTICE" "$AGENT_TREE/THIRD_PARTY_NOTICES.md" "$STAGE/AirSIM/"
(cd "$STAGE/plugin"; go mod vendor)
(cd "$STAGE/AirSIM/module/module-agent"; go mod vendor)
cp "$HERE/out/release-sources/"*.tar.* "$STAGE/dependencies/"
cp "$HERE/SOURCES.md" "$STAGE/"
COPYFILE_DISABLE=1 tar -czf "$HERE/dist/airsim-mu300-$VERSION-sources.tar.gz" -C "$STAGE" plugin AirSIM dependencies SOURCES.md
printf 'Source bundle: %s\n' "$HERE/dist/airsim-mu300-$VERSION-sources.tar.gz"
