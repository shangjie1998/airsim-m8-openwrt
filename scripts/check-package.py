#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""闸二：打包后抽查关键常量（M8 主包）。

闸一只证明「仓库里的版本号彼此一致」，管不住「包是怎么打出来的」。
这轮就吃过「源码改了、包里还是旧常量」的亏，所以打完之后再拆开看一遍。

查什么：
  - 包内 VERSION 与仓库 VERSION 一致，且车型标识是 -m8
  - daemon / agent 二进制里能搜到当前版本串（防旧二进制混进来）
  - 七个页面 + ui.css/ui.js + 五个 rpcd 对象 + /etc/config/airsim-bt 都在
  - usr/libexec 下只许白名单脚本（调试/一次性脚本不许进包）
  - 附件名 ASCII、文件名合规

用法：
  python3 scripts/check-package.py m8-pkg/airsim-m8-0.1.0.32-r22-m8.tar.gz
退出码：0 通过；1 不合格（打包脚本据此中止）。
"""
import re
import sys
import tarfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
VER = (ROOT / 'overlay/m8/VERSION').read_text(encoding='utf-8').strip()

LIBEXEC_ALLOWED = {
    'luci/usr/libexec/rpcd/airsim',
    'luci/usr/libexec/rpcd/airsim-bt',
    'luci/usr/libexec/rpcd/airsim-call',
    'luci/usr/libexec/rpcd/airsim-sms',
    'luci/usr/libexec/rpcd/airsim-net',
    'luci/usr/libexec/airsim-net-probe',
    'luci/usr/libexec/airsim-flowfix',
}
REQUIRED = {
    'luci/www/luci-static/resources/view/airsim/ui.css',
    'luci/www/luci-static/resources/view/airsim/ui.js',
    'luci/www/luci-static/resources/view/airsim/glass.js',
    'luci/www/luci-static/resources/view/airsim/overview.js',
    'luci/www/luci-static/resources/view/airsim/connect.js',
    'luci/www/luci-static/resources/view/airsim/net.js',
    'luci/www/luci-static/resources/view/airsim/call.js',
    'luci/www/luci-static/resources/view/airsim/sms.js',
    'luci/www/luci-static/resources/view/airsim/maintenance.js',
    'luci/www/luci-static/resources/view/airsim/settings.js',
    'luci/usr/bin/airsim-netmode',
    'luci/usr/libexec/airsim-net-probe',
    'luci/usr/libexec/airsim-flowfix',
    'luci/usr/libexec/rpcd/airsim-net',
    'luci/etc/config/airsim',
    'luci/etc/config/airsim-bt',
    'luci/opt/airsim-m8/bin/airsim-bt.py',
    'luci/opt/airsim-m8/bin/sms-dash.py',
    'luci/opt/airsim-m8/bin/m8-bt-address-fix.py',
    'install.sh',
}

problems = []


def main():
    if len(sys.argv) != 2:
        print(__doc__)
        return 1
    pkg = Path(sys.argv[1])
    if not pkg.exists():
        print('包不存在：%s' % pkg)
        return 1
    if not re.fullmatch(r'airsim-m8-[0-9a-z.\-]+\.tar\.gz', pkg.name) or not pkg.name.isascii():
        problems.append('附件名不合规或含非 ASCII：%s' % pkg.name)

    with tarfile.open(pkg, 'r:gz') as tf:
        members = {m.name.lstrip('./'): m for m in tf.getmembers() if m.isfile()}
        names = set(members)

        ver = None
        for key in ('luci/opt/airsim-m8/VERSION', 'VERSION'):
            if key in members:
                ver = tf.extractfile(members[key]).read().decode('utf-8').strip()
                break
        if ver is None:
            problems.append('包里找不到 VERSION 文件')
        else:
            if ver != VER:
                problems.append('包内 VERSION=%r，仓库 VERSION=%r' % (ver, VER))
            if not ver.endswith('-m8'):
                problems.append('包内 VERSION 缺机型标识 -m8：%r' % ver)

        for binary in ('airsim-mu300d', 'airsim-agent'):
            cand = [k for k in members if k.endswith(binary)]
            if not cand:
                problems.append('包里没有二进制 %s' % binary)
                continue
            data = tf.extractfile(members[cand[0]]).read()
            if VER.encode('utf-8') not in data:
                problems.append('%s 里搜不到版本串 %r（疑似旧二进制）' % (binary, VER))

    missing = sorted(REQUIRED - names)
    if missing:
        problems.append('缺文件：%s' % '、'.join(missing))

    extra = sorted(n for n in names
                   if n.startswith('luci/usr/libexec/') and n not in LIBEXEC_ALLOWED)
    if extra:
        problems.append('usr/libexec 出现白名单外脚本：%s' % '、'.join(extra))

    if problems:
        print('== 闸二（包内常量）未通过 ==')
        for p in problems:
            print('  ✗ %s' % p)
        return 1
    print('== 闸二（包内常量）通过：%s（VERSION=%s，%d 个文件）==' % (pkg.name, VER, len(names)))
    return 0


if __name__ == '__main__':
    sys.exit(main())
