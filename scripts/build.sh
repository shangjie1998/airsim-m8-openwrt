#!/bin/sh
# 合成源码树：src/（上游原版，一字不改）+ overlay/m8/（自建改动）
#
# 用法：sh scripts/build.sh [输出目录]    默认输出到仓库根的 build/
# 约定：不读 git 历史（上游只发剥了 .git 的源码包），只做目录叠加。
set -eu
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
OUT=${1:-$ROOT/build}

[ -d "$ROOT/src" ] || { echo "缺少 src/（上游原版）" >&2; exit 1; }
[ -d "$ROOT/overlay/m8" ] || { echo "缺少 overlay/m8/（自建改动）" >&2; exit 1; }

rm -rf "$OUT"
mkdir -p "$OUT"
cp -R "$ROOT/src/." "$OUT/"
cp -R "$ROOT/overlay/m8/." "$OUT/"

[ -f "$OUT/VERSION" ] || { echo "合成后没有 VERSION；检查 overlay/m8/VERSION" >&2; exit 1; }
echo "合并完成：${OUT}（VERSION=$(cat "$OUT/VERSION")）"
