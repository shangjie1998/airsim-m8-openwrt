#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""闸零：补丁体检（只读，不改仓库里的 src/）。

做的事：
1. 按 manifest.tsv 顺序，在「基线提交的干净导出树」上逐个 `git apply --check`；
   任一打不上 → 退出 1，并指出是哪个补丁。
2. 真打一遍，检查每个补丁登记的关键标记串是否仍存在于结果里
   （防「上游改词、补丁照打但功能静默失效」）。
3. 报告基线与上游仓库 HEAD 的关系（本地 pin 落后几个提交是提示，不是失败）。

用法：
    python3 overlay/m8/scripts/check-patches.py
    AIRSIM_UPSTREAM_REPO=/path/to/AirSIM python3 overlay/m8/scripts/check-patches.py

退出码：0 全过；1 有必须修的（打不上 / 标记缺失）。
"""
import os
import shutil
import subprocess
import sys
import tempfile
from pathlib import Path

HERE = Path(__file__).resolve().parents[1]          # overlay/m8
PATCHES = HERE / 'patches'
ROOT = HERE.parents[1]                              # 仓库根
UPSTREAM = Path(os.environ.get('AIRSIM_UPSTREAM_REPO') or (ROOT / '..' / 'AirSIM').resolve())

problems = []
notes = []


def run(cmd, cwd=None, check=True):
    p = subprocess.run(cmd, cwd=cwd, text=True, capture_output=True)
    if check and p.returncode != 0:
        raise RuntimeError('%s\n%s\n%s' % (' '.join(cmd), p.stdout.strip(), p.stderr.strip()))
    return p


def read_manifest():
    rows = []
    for line in (PATCHES / 'manifest.tsv').read_text(encoding='utf-8').splitlines():
        line = line.strip()
        if not line or line.startswith('#'):
            continue
        cols = line.split('\t')
        name, status = cols[0].strip(), cols[1].strip()
        markers = [m.strip() for m in (cols[2] if len(cols) > 2 else '').split(',') if m.strip()]
        if status not in ('active', 'merged'):
            problems.append('%s：状态 %r 非法（只允许 active / merged）' % (name, status))
            continue
        rows.append({'name': name, 'status': status, 'markers': markers})
    return rows


def main():
    if not (PATCHES / 'AIRSIM_BASE').exists():
        print('缺少 patches/AIRSIM_BASE（基线提交号）')
        return 1
    base = (PATCHES / 'AIRSIM_BASE').read_text(encoding='utf-8').strip()
    if not UPSTREAM.is_dir():
        print('找不到上游仓库：%s（用 AIRSIM_UPSTREAM_REPO 指定）' % UPSTREAM)
        return 1
    run(['git', '-C', str(UPSTREAM), 'cat-file', '-e', base + '^{commit}'])

    head = run(['git', '-C', str(UPSTREAM), 'rev-parse', 'HEAD']).stdout.strip()
    if head != base:
        behind = run(['git', '-C', str(UPSTREAM), 'rev-list', '--count', base + '..HEAD']).stdout.strip()
        notes.append('本地基线 %s 落后上游 HEAD %s 共 %s 个提交（提示，不阻断）' % (base[:8], head[:8], behind))

    rows = read_manifest()
    if not rows:
        print('manifest.tsv 里没有任何补丁')
        return 1

    tmp = Path(tempfile.mkdtemp(prefix='m8patch-'))
    try:
        arch = tmp / 'upstream.tar'
        run(['git', '-C', str(UPSTREAM), 'archive', '--format=tar', '--output=' + str(arch), base, 'module/module-agent'])
        run(['tar', '-xf', str(arch)], cwd=str(tmp))

        # 干净导出树里没有 go.mod / go.sum（.gitattributes 标了 export-ignore），补回来只为了
        # 让 git apply 有个完整工作树；内容不参与判定。
        for f in ('go.mod', 'go.sum'):
            src = run(['git', '-C', str(UPSTREAM), 'show', '%s:module/module-agent/%s' % (base, f), ], check=False)
            if src.returncode == 0:
                (tmp / 'module/module-agent' / f).write_text(src.stdout, encoding='utf-8')

        run(['git', 'init', '-q'], cwd=str(tmp))
        run(['git', 'add', '-A'], cwd=str(tmp))
        run(['git', '-c', 'user.name=MU300 build', '-c', 'user.email=build@localhost',
             '-c', 'commit.gpgsign=false', 'commit', '-qm', 'baseline'], cwd=str(tmp))

        for row in rows:
            name = row['name']
            path = PATCHES / name
            if not path.exists():
                problems.append('%s：manifest 登记了但文件不存在' % name)
                continue
            if row['status'] == 'merged':
                notes.append('%s：merged，跳过打补丁（只校验标记串）' % name)
            else:
                chk = run(['git', 'apply', '--check', str(path)], cwd=str(tmp), check=False)
                if chk.returncode != 0:
                    problems.append('%s：打不上\n%s' % (name, chk.stderr.strip()[:400]))
                    continue
                run(['git', 'apply', str(path)], cwd=str(tmp))

            missing = []
            for m in row['markers']:
                out = run(['grep', '-rqF', m, str(tmp / 'module')], check=False)
                if out.returncode != 0:
                    missing.append(m)
            if missing:
                problems.append('%s：打完源码里找不到标记 %s' % (name, '、'.join(missing)))
            else:
                print('  ok  %s（%s）' % (name, row['status']))
    finally:
        shutil.rmtree(tmp, ignore_errors=True)

    for n in notes:
        print('  提示 %s' % n)
    if problems:
        print('\n== 补丁体检未通过 ==')
        for p in problems:
            print('  ✗ %s' % p)
        return 1
    print('\n== 补丁体检通过（%d 个）==' % len(rows))
    return 0


if __name__ == '__main__':
    sys.exit(main())
