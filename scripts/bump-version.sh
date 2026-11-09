#!/bin/sh
# M8 版本号推进：0.1.0.<n>-r<NN>-m8
#
#   0.1.0      上游基础版本，不动
#   <n>        本地改动序号，每打一个包 +1
#   r<NN>      上游 revision，从当前 VERSION 读取，不写死（上游升级后仍可用）
#   -m8        机型标识
#
# 版本只写在 overlay/m8/VERSION 一处真源，其余位置由本脚本同步：
#   overlay/m8/m8-pkg/VERSION   打包骨架
#   <外部暂存区>/VERSION         打包暂存
#
# 用法：sh scripts/bump-version.sh        # 序号 +1
#       sh scripts/bump-version.sh 7      # 指定序号
set -eu
HERE=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
PKG=${M8_PKG_BUILD:-$HERE/../new-chat-6/m8-airsim/m8-pkg/build}
VFILE=$HERE/overlay/m8/VERSION
CUR=$(cat "$VFILE")

# 只允许在研内核线（M8 现行：6.18.54-m8-probe12）。冻结线一律拒绝。
case "$CUR" in
*-m8) ;;
*) echo "VERSION '$CUR' 不属于 -m8 在研线，拒绝推进" >&2; exit 1 ;;
esac

N=$(printf '%s' "$CUR" | sed -n 's/^0\.1\.0\.\([0-9][0-9]*\)-r[0-9][0-9]*-m8$/\1/p')
REV=$(printf '%s' "$CUR" | sed -n 's/^0\.1\.0\.[0-9][0-9]*-r\([0-9][0-9]*\)-m8$/\1/p')
if [ -z "$N" ] || [ -z "$REV" ]; then
	echo "VERSION '$CUR' 不符合 0.1.0.<n>-r<NN>-m8" >&2
	exit 1
fi

if [ "${1:-}" != "" ]; then
	case "$1" in
	''|*[!0-9]*) echo "序号必须是非负整数" >&2; exit 1 ;;
	esac
	NEXT=$1
else
	NEXT=$((N + 1))
fi

NEW="0.1.0.$NEXT-r$REV-m8"
printf '%s\n' "$NEW" > "$VFILE"
[ -f "$HERE/overlay/m8/m8-pkg/VERSION" ] && printf '%s\n' "$NEW" > "$HERE/overlay/m8/m8-pkg/VERSION"
[ -d "$PKG" ] && printf '%s\n' "$NEW" > "$PKG/VERSION"
printf '%s\n' "$NEW"
printf 'package: airsim-m8-%s.tar.gz\n' "$NEW" >&2
