#!/usr/bin/python3
"""AirSIM 蓝牙开关 —— 通用后端。

设计原则：**逻辑写在这里，机型差异只写在 /etc/config/airsim-bt 里**。
M8（展锐 WCN，vendor e5-bt 服务）和 F50 用同一份代码，只是配置不同；
F50 那边没有 vendor 服务时把 backend 写成 attach，脚本自己 btattach。

uci 配置（包 airsim-bt，section main）：
  option backend      'service'                 # service=交给现成 init 脚本；attach=自己 btattach
  option service      'e5-bt'                   # backend=service 时的 init 脚本名
  option flag_uci     'e5-bluetooth.main.enabled'   # 开/关写在这个 uci 键上（留空则不写）
  option autostart_uci 'e5-bluetooth.main.autostart' # 开机是否自动上电（可选）
  option power_on     '1'                       # 开启后是否顺便 bluetoothctl power on
  option tty          '/dev/ttyBT0'             # backend=attach 用；也作为 service 失败后的兜底
  option baud         '3000000'
  option module       'sprdbt_tty'
  option adapter      'hci0'
  option label        '蓝牙'

ubus（rpcd shim `airsim-bt`）：
  bt_get  {}            -> 状态
  bt_set  {"enable":"0|1"} -> {"ok":true,...状态}
"""
import json
import os
import subprocess
import sys
import time
from pathlib import Path

UCI_PKG = 'airsim-bt'
DEFAULTS = {
    'backend': 'service',
    'service': 'e5-bt',
    'flag_uci': 'e5-bluetooth.main.enabled',
    'autostart_uci': 'e5-bluetooth.main.autostart',
    'power_on': '1',
    'tty': '/dev/ttyBT0',
    'baud': '3000000',
    'module': 'sprdbt_tty',
    'adapter': 'hci0',
    'label': '蓝牙',
}
SYS_BT = Path('/sys/class/bluetooth')


def run(cmd, timeout=10):
    try:
        p = subprocess.run(cmd, capture_output=True, text=True, timeout=timeout)
        return p.returncode, (p.stdout or '') + (p.stderr or '')
    except Exception as exc:
        return 1, str(exc)


def cfg(key):
    rc, out = run(['uci', '-q', 'get', '%s.main.%s' % (UCI_PKG, key)], timeout=5)
    value = out.strip() if rc == 0 else ''
    return value or DEFAULTS.get(key, '')


def supported():
    rc, _ = run(['uci', '-q', 'get', '%s.main' % UCI_PKG], timeout=5)
    return rc == 0


def uci_get(key):
    if not key:
        return ''
    rc, out = run(['uci', '-q', 'get', key], timeout=5)
    return out.strip() if rc == 0 else ''


def uci_set(key, value):
    """key 是完整路径 pkg.section.option；写成 pkg.section 会把 section 改名。"""
    if not key:
        return
    pkg = key.partition('.')[0]
    run(['uci', 'set', '%s=%s' % (key, value)], timeout=5)
    run(['uci', 'commit', pkg], timeout=8)


def adapter_path():
    return SYS_BT / cfg('adapter')


def attached():
    """这一版内核的 hci0 sysfs 里没有 address/name 文件，只认目录在不在。"""
    return adapter_path().exists()


def address():
    """地址从 ioctl 读：sysfs 没有 address 节点（sprd hci uart），只能问 hciconfig。"""
    rc, out = run(['hciconfig', cfg('adapter')], timeout=6)
    for line in out.splitlines():
        if 'BD Address' in line:
            return line.split('BD Address:')[1].split()[0].strip()
    return ''


def adapter_up():
    rc, out = run(['hciconfig', cfg('adapter')], timeout=6)
    return 'UP RUNNING' in out or 'UP ' in out


def service_state():
    svc = cfg('service')
    if not svc:
        return 'none'
    rc, out = run(['/etc/init.d/%s' % svc, 'status'], timeout=6)
    text = out.strip().lower()
    if 'running' in text:
        return 'running'
    if 'active' in text:
        return 'idle'
    return 'stopped'


def bluetoothd_up():
    rc, out = run(['timeout', '4', 'bluetoothctl', 'list'], timeout=8)
    return bool((out or '').strip())


def rfkill_state():
    blocked = 0
    for node in Path('/sys/class/rfkill').glob('rfkill*'):
        try:
            if (node / 'type').read_text().strip() != 'bluetooth':
                continue
            if (node / 'soft').read_text().strip() != '0':
                blocked += 1
        except Exception:
            continue
    return blocked


def unblock():
    for node in Path('/sys/class/rfkill').glob('rfkill*'):
        try:
            if (node / 'type').read_text().strip() == 'bluetooth':
                (node / 'soft').write_text('0\n')
        except Exception:
            pass


def start_attach():
    """vendor 服务拉不起来时的兜底：自己 modprobe + rfkill 解锁 + btattach。"""
    tty = cfg('tty')
    mod = cfg('module')
    if mod:
        run(['modprobe', mod], timeout=8)
    for _ in range(20):
        if os.path.exists(tty):
            break
        time.sleep(1)
    if not os.path.exists(tty):
        return False, 'no %s' % tty
    unblock()
    run(['sh', '-c', 'setsid /usr/bin/btattach -B %s -S %s >/tmp/airsim-bt.log 2>&1 &' % (tty, cfg('baud'))], timeout=8)
    for _ in range(20):
        if attached():
            break
        time.sleep(1)
    if attached():
        run(['hciconfig', cfg('adapter'), 'up'], timeout=8)
        return True, 'attached'
    return False, 'btattach did not create %s' % cfg('adapter')


def stop_attach():
    run(['sh', '-c', 'killall btattach 2>/dev/null || true'], timeout=6)


def status(extra=None):
    st = {
        'ok': True,
        'supported': supported(),
        'label': cfg('label'),
        'backend': cfg('backend'),
        'service': cfg('service'),
        'service_state': service_state(),
        'attached': attached(),
        'up': adapter_up(),
        'adapter': cfg('adapter'),
        'address': address(),
        'bluetoothd': bluetoothd_up(),
        'rfkill_blocked': rfkill_state(),
    }
    flag = uci_get(cfg('flag_uci'))
    if flag:
        st['enabled'] = flag != '0'
    else:
        st['enabled'] = st['service_state'] in ('running', 'idle') or st['attached']
    if not st['supported']:
        st['hint'] = '这台设备没装 airsim-bt 配置，蓝牙开关不可用'
    elif st['enabled'] and st['attached'] and st['address'] and st['address'].lower().startswith('00:00:00'):
        st['hint'] = '控制器地址是 00:00:00:00:00:00，蓝牙起不来；需要补厂标地址'
    elif st['enabled'] and not st['attached']:
        st['hint'] = '已打开但控制器还没出现，稍等或重开一次'
    if extra:
        st.update(extra)
    return st


def do_get(_args=None):
    return status()


def do_set(args):
    if not supported():
        return {'ok': False, 'supported': False, 'error': 'unsupported'}
    want = str((args or {}).get('enable', '')).strip()
    if want in ('', None):
        return {'ok': False, 'error': 'enable required'}
    enable = want not in ('0', 'false', 'off', 'no')

    uci_set(cfg('flag_uci'), '1' if enable else '0')
    uci_set(cfg('autostart_uci'), '1' if enable else '1')

    svc = cfg('service')
    if cfg('backend') == 'service' and svc:
        if enable:
            run(['/etc/init.d/%s' % svc, 'enable'], timeout=8)
            run(['/etc/init.d/%s' % svc, 'start'], timeout=15)
            if not attached():
                run(['/etc/init.d/%s' % svc, 'restart'], timeout=20)
        else:
            run(['timeout', '5', 'bluetoothctl', 'power', 'off'], timeout=8)
            run(['/etc/init.d/%s' % svc, 'stop'], timeout=20)
            stop_attach()

    if enable and not attached() and cfg('backend') != 'service':
        ok, why = start_attach()
        if not ok:
            return status({'ok': False, 'error': why})

    # vendor 的 e5-bt 只负责 btattach，不负责 hciconfig up（那是它自己的 check 脚本
    # 30 秒后干的）。这里直接补一手，免得 UI 打开后还要干等半分钟。
    if enable:
        # ubus 调用有 30 s 上限，这里总共只等 8 s：没起来就让 UI 显示"正在初始化"，
        # 别把整次开关拖成超时。
        deadline = time.time() + 8
        while time.time() < deadline and not attached():
            time.sleep(1)
        if attached() and not adapter_up():
            run(['hciconfig', cfg('adapter'), 'up'], timeout=8)
        deadline = time.time() + 4
        while time.time() < deadline and not adapter_up():
            time.sleep(1)

    if enable and attached() and cfg('power_on') == '1':
        run(['timeout', '8', 'bluetoothctl', 'power', 'on'], timeout=12)

    for _ in range(8):
        if enable == attached():
            break
        time.sleep(1)
    return status()


def read_args():
    try:
        raw = sys.stdin.read()
        return json.loads(raw) if raw.strip() else {}
    except Exception:
        return {}


METHODS = {'bt_get': do_get, 'bt_set': do_set}


def main():
    name = sys.argv[1] if len(sys.argv) > 1 else ''
    fn = METHODS.get(name)
    if not fn:
        print(json.dumps({'ok': False, 'error': 'unknown method %s' % name}))
        return 2
    try:
        result = fn(read_args())
    except Exception as exc:
        result = {'ok': False, 'error': '%s: %s' % (type(exc).__name__, exc)}
    print(json.dumps(result, ensure_ascii=False))
    return 0


if __name__ == '__main__':
    sys.exit(main())
