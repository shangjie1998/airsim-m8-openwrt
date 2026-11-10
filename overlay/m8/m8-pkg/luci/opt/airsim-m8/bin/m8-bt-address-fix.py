#!/usr/bin/env python3
"""【仅 VIPME-M8 用，F50 不要装】给缺厂标的 BT PSKEY 补一个本地管理地址（02:..）。

这台机器出厂 btmac 是全 0，vendor 的 /usr/libexec/m8-bluetooth-address 又要去读
/mnt/e5-data/misc/bluedroid/bt_config.conf（本机根本没有），于是 e5-bt 起不来、
hciconfig 报 Address not available (99)。

只在 pskey 里的地址为全 0 / 全 ff / 组播时动手；先备份再改，改完 fsync。
地址取 br-lan MAC 最后一段 +1，保证同一台机器每次算出来一样。
"""
import os
import shutil
import sys
from pathlib import Path

PSKEY = Path('/lib/firmware/sprd/marlin3lite_pskey.bin')
OFF = 20


def read_mac(iface='br-lan'):
    return Path('/sys/class/net/%s/address' % iface).read_text().strip()


def wanted():
    mac = read_mac().split(':')
    last = (int(mac[-1], 16) + 1) & 0xff
    return '%s:%s:%s:%s:%s:%02x' % (mac[0], mac[1], mac[2], mac[3], mac[4], last)


def valid(addr):
    b = bytes.fromhex(addr.replace(':', ''))
    return b not in (bytes(6), b'\xff' * 6) and not (b[0] & 1)


def main():
    data = bytearray(PSKEY.read_bytes())
    current = ':'.join('%02x' % v for v in reversed(data[OFF:OFF + 6]))
    if valid(current):
        print('address already set: %s' % current)
        return 0
    addr = wanted()
    if not valid(addr):
        print('derived address invalid: %s' % addr)
        return 1
    backup = PSKEY.with_suffix('.bin.bak-%s' % current.replace(':', ''))
    if not backup.exists():
        shutil.copy2(PSKEY, backup)
    data[OFF:OFF + 6] = bytes.fromhex(addr.replace(':', ''))[::-1]
    tmp = PSKEY.with_suffix('.tmp')
    tmp.write_bytes(bytes(data))
    tmp.chmod(PSKEY.stat().st_mode & 0o777)
    tmp.replace(PSKEY)
    fd = os.open(PSKEY.parent, os.O_RDONLY)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)
    print('address repaired: %s -> %s (backup %s)' % (current, addr, backup))
    return 0


if __name__ == '__main__':
    sys.exit(main())
