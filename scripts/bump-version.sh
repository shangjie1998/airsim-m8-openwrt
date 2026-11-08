#!/bin/sh
# Bump the M8 release counter for the next packaged change.
#
# Version contract (matches upstream where it matters):
#   0.1.0(<n>)-r22-m8
#     ^^^^^            upstream base version, unchanged
#            ^^^^      upstream revision, unchanged
#                  ^^  device name, unchanged
#         ^^^         monotonic local change counter, +1 per packaged change
#
# Usage: sh scripts/bump-version.sh          # increment by one and print
#        sh scripts/bump-version.sh 7        # set the counter explicitly
set -eu
HERE=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
PKG=${M8_PKG_BUILD:-$HERE/../new-chat-6/m8-pkg/build}
VFILE=$HERE/VERSION
CUR=$(cat "$VFILE")

BASE=$(printf '%s' "$CUR" | sed -n 's/^\(0\.1\.0\([0-9][0-9]*\)-r22-m8\)$/\1/p')
if [ -z "$BASE" ]; then
	echo "VERSION '$CUR' is not in the 0.1.0(<n>)-r22-m8 form" >&2
	exit 1
fi
N=$(printf '%s' "$CUR" | sed -n 's/^0\.1\.0(\([0-9][0-9]*\))-r22-m8$/\1/p')
if [ "${1:-}" != "" ]; then
	case "$1" in
	''|*[!0-9]*) echo "counter must be a non-negative integer" >&2; exit 1 ;;
	esac
	NEXT=$1
else
	NEXT=$((N + 1))
fi

NEW="0.1.0($NEXT)-r22-m8"
printf '%s\n' "$NEW" > "$VFILE"
if [ -d "$PKG" ]; then
	printf '%s\n' "$NEW" > "$PKG/VERSION"
fi
printf '%s\n' "$NEW"
printf 'package: airsim-m8-%s.tar.gz\n' "$NEW" >&2
