#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""闸三：线上回读（发完 Release 之后跑）。

闸一（仓库自洽）、闸二（包里常量）都只看本地。发完之后再回读一次线上，
确认「挂上去的就是刚打的那个字节序列」，防止上传被截断 / 挂错文件 / CDN 旧缓存。

只读 GitHub API，不改任何东西。

用法：
  python3 scripts/check-release.py v0.1.0.32-r22-m8 m8-pkg/airsim-m8-0.1.0.32-r22-m8.tar.gz
  M8_REPO=shangjie1998/airsim-m8-openwrt M8_PAT=/tmp/m8pat.txt python3 scripts/check-release.py ...

退出码：0 一致；1 不一致或取不到。
"""
import hashlib
import json
import os
import sys
import urllib.request
from pathlib import Path

REPO = os.environ.get('M8_REPO', 'shangjie1998/airsim-m8-openwrt')


def api(path, token):
    req = urllib.request.Request('https://api.github.com' + path,
                                 headers={'Authorization': 'token ' + token,
                                          'Accept': 'application/vnd.github+json'})
    return json.load(urllib.request.urlopen(req, timeout=30))


def main():
    if len(sys.argv) != 3:
        print(__doc__)
        return 1
    tag, local = sys.argv[1], Path(sys.argv[2])
    token = os.environ.get('M8_PAT')
    if not token:
        pat_file = Path('/tmp/m8pat.txt')
        if not pat_file.exists():
            print('缺少 token：设 M8_PAT 或放 /tmp/m8pat.txt')
            return 1
        token = pat_file.read_text().strip()

    if not local.exists():
        print('本地文件不存在：%s' % local)
        return 1
    size = local.stat().st_size
    sha = hashlib.sha256(local.read_bytes()).hexdigest()

    rel = api('/repos/%s/releases/tags/%s' % (REPO, tag), token)
    assets = [a for a in rel.get('assets', []) if a['name'] == local.name]
    if not assets:
        print('✗ Release %s 里没有附件 %s' % (tag, local.name))
        return 1
    a = assets[0]
    ok = True
    if a['size'] != size:
        print('✗ 大小不一致：线上 %s / 本地 %s' % (a['size'], size))
        ok = False
    digest = (a.get('digest') or '').replace('sha256:', '')
    if digest and digest != sha:
        print('✗ sha256 不一致：线上 %s / 本地 %s' % (digest, sha))
        ok = False
    if not digest:
        print('  提示 线上没给 digest，只核了大小与状态；本地 sha256=%s' % sha)
    if a.get('state') != 'uploaded':
        print('✗ 线上状态不是 uploaded：%s' % a.get('state'))
        ok = False

    if ok:
        print('== 闸三（线上回读）通过：%s / %s / %d 字节 ==' % (tag, local.name, size))
        return 0
    return 1


if __name__ == '__main__':
    sys.exit(main())
