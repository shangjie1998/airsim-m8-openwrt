#!/bin/sh
# 账本一致性断言：不通过就别打包。
#   1) overlay 里每个文件都必须在 docs/M8-CHANGE-LEDGER.md 里出现
#   2) 账本里引用的 overlay 文件必须真实存在
#   3) overlay/m8/VERSION 与 m8-pkg/VERSION 一致
#   4) 产物名只允许 [a-z0-9.-]
set -eu
HERE=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
OVERLAY=$HERE/overlay/m8
LEDGER=$OVERLAY/docs/M8-CHANGE-LEDGER.md
FAIL=0

[ -f "$LEDGER" ] || { echo "缺少账本：$LEDGER" >&2; exit 1; }

echo "== 1) overlay 文件是否都已记账 =="
MISSING=0
for f in $(cd "$OVERLAY" && find . -type f | sed 's|^\./||' | sort); do
	case "$f" in
	.*|*/.*) continue ;;
	docs/M8-CHANGE-LEDGER.md|docs/M8-FEATURE-GOVERNANCE.md|VERSION|m8-pkg/VERSION) continue ;;
	esac
	if ! grep -qF "$(basename "$f")" "$LEDGER"; then
		echo "  未记账：$f"; MISSING=$((MISSING + 1))
	fi
done
[ "$MISSING" -eq 0 ] && echo "  全部已记账" || FAIL=1

echo "== 2) 账本引用的文件是否存在 =="
GONE=0
for f in $(grep -o '`[A-Za-z0-9_./-]*\.\(go\|js\|css\|py\|sh\|patch\|json\|init\|config\|md\)`' "$LEDGER" | tr -d '`' | sort -u); do
	case "$f" in
	*/*) ;;
	*) continue ;;
	esac
	[ -e "$OVERLAY/$f" ] || [ -e "$HERE/$f" ] || { echo "  账本里有、树里没有：$f"; GONE=$((GONE + 1)); }
done
[ "$GONE" -eq 0 ] && echo "  引用都在" || FAIL=1

echo "== 3) 版本真源一致 =="
V=$(cat "$OVERLAY/VERSION")
PV=$(cat "$OVERLAY/m8-pkg/VERSION" 2>/dev/null || echo "缺失")
if [ "$V" = "$PV" ]; then
	echo "  ${V}（两处一致）"
else
	echo "  VERSION=$V 但 m8-pkg/VERSION=$PV"; FAIL=1
fi
if ! printf '%s' "$V" | grep -q '^[a-z0-9][a-z0-9.-]*$'; then
	echo "  版本串含非法字符：$V"; FAIL=1
fi

echo "== 4) 产物名规则 =="
case "airsim-m8-$V.tar.gz" in
*'('*|*')'*|*' '*) echo "  产物名含括号或空格"; FAIL=1 ;;
*) echo "  airsim-m8-$V.tar.gz 合规" ;;
esac

[ "$FAIL" -eq 0 ] && echo "== 通过 ==" || { echo "== 未通过，不许打包 ==" >&2; exit 1; }
