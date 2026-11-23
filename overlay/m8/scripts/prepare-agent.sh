#!/bin/sh
# 从钉住的上游提交导出 Agent 源码，按 manifest.tsv 顺序打补丁。
# 只读上游 checkout，绝不改用户的 AirSIM 工作区。
set -eu
HERE=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
UPSTREAM=${AIRSIM_UPSTREAM_REPO:-$HERE/../../../AirSIM}
BASE=$(cat "$HERE/patches/AIRSIM_BASE")
MANIFEST="$HERE/patches/manifest.tsv"

[ -f "$MANIFEST" ] || { echo "缺少 patches/manifest.tsv" >&2; exit 1; }
git -C "$UPSTREAM" cat-file -e "$BASE^{commit}"

mkdir -p "$HERE/out"
TASK_SOURCE=$(mktemp -d "$HERE/out/agent-source.XXXXXX")
git -C "$UPSTREAM" archive --format=tar --output="$TASK_SOURCE/upstream.tar" "$BASE" module/module-agent LICENSE NOTICE THIRD_PARTY_NOTICES.md
tar -xf "$TASK_SOURCE/upstream.tar" -C "$TASK_SOURCE"
git -C "$TASK_SOURCE" init -q
git -C "$TASK_SOURCE" add module LICENSE NOTICE THIRD_PARTY_NOTICES.md
git -C "$TASK_SOURCE" -c user.name='MU300 build' -c user.email='build@localhost' -c commit.gpgsign=false commit -qm 'Pinned upstream Agent baseline'

# 顺序由 manifest 决定；active 必须打，打不上立即失败，不许静默跳过。
while IFS='	' read -r name status _markers; do
	case "$name" in ''|\#*) continue ;; esac
	if [ "$status" = "merged" ]; then
		printf 'skip merged: %s\n' "$name" >&2
		continue
	fi
	patch="$HERE/patches/$name"
	[ -f "$patch" ] || { echo "manifest 登记了但补丁不存在：$name" >&2; exit 1; }
	git -C "$TASK_SOURCE" apply --check "$patch" || { echo "补丁打不上：$name" >&2; exit 1; }
	git -C "$TASK_SOURCE" apply "$patch"
	printf 'applied %s\n' "$name" >&2
done < "$MANIFEST"

# 上游 .gitattributes 把 Agent 的 go.mod/go.sum 标成 export-ignore，git archive 会漏，
# 模块模式构建解析不了依赖，这里补回来。
git -C "$UPSTREAM" show "$BASE:module/module-agent/go.mod" > "$TASK_SOURCE/module/module-agent/go.mod"
git -C "$UPSTREAM" show "$BASE:module/module-agent/go.sum" > "$TASK_SOURCE/module/module-agent/go.sum"
printf '%s\n' "$TASK_SOURCE"
