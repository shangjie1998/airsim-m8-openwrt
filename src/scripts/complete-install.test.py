#!/usr/bin/env python3
"""Run the real setup state machine in a disposable filesystem with fake hardware."""
import gzip
import hashlib
import io
import os
from pathlib import Path
import re
import subprocess
import tarfile
import tempfile
import unittest

ROOT = Path(__file__).resolve().parent.parent


def sha(data):
    return hashlib.sha256(data).hexdigest()


class Setup(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.env = dict(os.environ, FIXTURE_ROOT=str(self.root), ENABLED='1')
        self.env['EVENT_LOG'] = str(self.root / 'events')
        self.write('/events', '')
        self.write('/proc/sys/kernel/random/boot_id', 'boot-one\n')
        self.write('/usr/lib/airsim/audio-preflight', '#!/bin/sh\nexit "${PREFLIGHT_FAIL:-0}"\n', True)
        self.write('/usr/lib/airsim/VERSION', 'fixture-version\n')
        for name in ('airsim', 'airsim-audio-setup'):
            self.write('/etc/init.d/' + name,
                       '#!/bin/sh\necho "' + name + ' $*" >> "$EVENT_LOG"\n', True)
        self.write('/mock/wget', '''#!/bin/sh
case "$*" in *127.0.0.1*) echo fixture-status; exit 0 ;; esac
echo download >> "$EVENT_LOG"
[ "${DOWNLOAD_FAIL:-0}" = 0 ] || exit 1
while [ "$1" != -O ]; do shift; done
cp "$FIRMWARE_SOURCE" "$2"
''', True)
        self.write('/mock/jsonfilter', '''#!/bin/sh
case "$*" in
 *'@.version'*) echo fixture-version ;;
 *) [ "${SERVICE_READY:-1}" = 1 ] && echo true || echo false ;;
esac
''', True)
        self.write('/mock/sleep', '#!/bin/sh\nexit 0\n', True)
        self.write('/mock/timeout', '#!/bin/sh\nshift\nexec "$@"\n', True)
        self.write('/mock/uci', '#!/bin/sh\necho "$ENABLED"\n', True)
        self.env['FIRMWARE_SOURCE'] = str(self.root / 'firmware-source')
        firmware = b'upstream fixture firmware'
        self.write('/firmware-source', firmware)
        self.write('/usr/lib/airsim/deps/agdsp.bin', firmware)
        self.state = self.root / 'mnt/mu300-disk/extra/.airsim-complete-install'
        self.ext = self.root / 'mnt/mu300-disk/extra/airsim-audio'
        files = {name: b'fixture tool\n' for name in
                 ('bin/pcmhost', 'bin/ctlset', 'bin/mu300-hfp', 'bin/mu300-voice', 'bin/mu300-audio-dsp',
                  'integration/airsim-audio-reserve', 'modules/5.4.254-gb50db5b6224c/fixture.ko',
                  'modules/5.4.254-gb50db5b6224c/KERNEL')}
        files['integration/prepare'] = b'#!/bin/sh\necho prepare >> "$EVENT_LOG"\nexit "${PREPARE_FAIL:-0}"\n'
        files['integration/stage-device.sh'] = b'''#!/bin/sh
set -eu
echo "stage $1" >> "$EVENT_LOG"
[ "${STAGE_FAIL:-0}" = 0 ] || exit 1
if [ "$1" = --stage ]; then
 cp -R "$2" "$FIXTURE_ROOT/mnt/mu300-disk/extra/airsim-audio"
 mkdir -p "$FIXTURE_ROOT/etc/airsim"
 cp "$2/integration/prepare" "$FIXTURE_ROOT/etc/airsim/prepare.local"
fi
'''
        files['manifest'] = ''.join(sha(data) + '  ./' + name + '\n' for name, data in files.items()).encode()
        bundle = io.BytesIO()
        with tarfile.open(fileobj=bundle, mode='w:gz') as tar:
            for name, data in files.items():
                item = tarfile.TarInfo('./' + name)
                item.mode = 0o755
                item.size = len(data)
                tar.addfile(item, io.BytesIO(data))
        raw = bundle.getvalue()
        self.write('/usr/lib/airsim/deps/airsim-audio-0.1.0-r16.tar.gz', raw)
        source = (ROOT / 'packaging/openwrt/audio-setup').read_text()
        source = source.replace('5af9b2dd888905b453b61273668736c24e1dd67b821d5fdb5a59c807a0134af3', sha(raw))
        source = source.replace('378ceea78dbf013e10625e71efd6fab487d727e371884ae8daf078da2828b937', sha(firmware))
        source = self.redirect(source)
        source = re.sub(r'^export PATH=.*$', 'export PATH="' + str(self.root / 'mock') + ':$PATH"', source, flags=re.M)
        self.write('/setup', source, True)
        (self.root / 'tmp').mkdir()

    def redirect(self, source):
        return re.sub(r'(?<![A-Za-z0-9_./-])/(?:usr|mnt|etc|proc|sys|lib|tmp|opt|run)(?=/)',
                      lambda m: str(self.root) + m.group(), source)

    def write(self, name, data, executable=False):
        path = self.root / name.lstrip('/')
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_bytes(data.encode() if isinstance(data, str) else data)
        if executable:
            path.chmod(0o755)
        return path

    def run_setup(self, mode='--install', **env):
        return subprocess.run(['sh', str(self.root / 'setup'), mode], env=dict(self.env, **env), capture_output=True, text=True)

    def status(self):
        return (self.state / 'status').read_text().strip()

    def next_boot(self):
        self.write('/proc/sys/kernel/random/boot_id', 'boot-two\n')
        self.write('/sys/module/audio_reserve/parameters/missing', '0\n')
        self.write('/sys/module/audio_reserve/parameters/ranges', '0xaf700000,0x300000;0xafa00000,0x600000\n')

    def test_first_install_downloads_stages_then_resumes_once_after_boot(self):
        result = self.run_setup()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.status(), 'reboot_required')
        self.assertNotIn('prepare\n', (self.root / 'events').read_text())
        self.assertEqual(self.run_setup('--resume').returncode, 0)
        self.assertEqual(self.status(), 'reboot_required')
        self.next_boot()
        result = self.run_setup('--resume')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.status(), 'ready')
        events = (self.root / 'events').read_text()
        self.assertEqual(events.count('download\n'), 1)
        self.assertIn('airsim start\n', events)
        self.assertIn('airsim-audio-setup disable\n', events)

    def test_bad_firmware_never_stages_or_claims_ready(self):
        self.write('/firmware-source', 'wrong download')
        self.write('/usr/lib/airsim/deps/agdsp.bin', 'wrong backup')
        self.assertNotEqual(self.run_setup().returncode, 0)
        self.assertEqual(self.status(), 'failed')
        self.assertFalse(self.ext.exists())
        self.assertFalse((self.root / 'lib/firmware/agdsp.bin').exists())

    def test_upstream_unavailable_uses_verified_bundled_backup(self):
        result = self.run_setup(DOWNLOAD_FAIL='1')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.status(), 'reboot_required')
        self.assertEqual((self.state / 'firmware-source').read_text().strip(), 'bundled_backup')
        self.assertEqual((self.root / 'lib/firmware/agdsp.bin').read_bytes(), b'upstream fixture firmware')
        self.next_boot()
        self.assertEqual(self.run_setup('--resume', DOWNLOAD_FAIL='1').returncode, 0)
        self.assertEqual(self.status(), 'ready')
        self.assertEqual((self.root / 'events').read_text().count('download\n'), 1)

    def test_corrupt_upstream_falls_back_without_installing_corrupt_bytes(self):
        self.write('/firmware-source', 'wrong download')
        result = self.run_setup()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((self.state / 'firmware-source').read_text().strip(), 'bundled_backup')
        self.assertEqual((self.root / 'lib/firmware/agdsp.bin').read_bytes(), b'upstream fixture firmware')

    def test_valid_upstream_does_not_depend_on_backup(self):
        self.write('/usr/lib/airsim/deps/agdsp.bin', 'wrong backup')
        self.assertEqual(self.run_setup().returncode, 0)
        self.assertEqual((self.state / 'firmware-source').read_text().strip(), 'upstream')

    def test_missing_or_linked_backup_fails_closed_when_upstream_is_down(self):
        path = self.root / 'usr/lib/airsim/deps/agdsp.bin'
        path.unlink()
        self.assertNotEqual(self.run_setup(DOWNLOAD_FAIL='1').returncode, 0)
        path.symlink_to(self.root / 'firmware-source')
        self.assertNotEqual(self.run_setup(DOWNLOAD_FAIL='1').returncode, 0)
        self.assertFalse(self.ext.exists())
        self.assertFalse((self.root / 'lib/firmware/agdsp.bin').exists())

    def test_existing_firmware_is_preserved_and_not_downloaded(self):
        self.write('/lib/firmware/agdsp.bin', 'device-owned firmware')
        self.assertEqual(self.run_setup().returncode, 0)
        self.assertEqual((self.root / 'lib/firmware/agdsp.bin').read_text(), 'device-owned firmware')
        self.assertNotIn('download', (self.root / 'events').read_text())

    def test_preflight_failure_does_not_create_state_or_touch_hardware(self):
        self.assertNotEqual(self.run_setup(PREFLIGHT_FAIL='1').returncode, 0)
        self.assertFalse(self.state.exists())
        self.assertEqual((self.root / 'events').read_text(), '')

    def test_custom_override_is_preserved(self):
        self.assertEqual(self.run_setup().returncode, 0)
        self.write('/etc/airsim/prepare.local', 'custom root-owned override')
        self.assertNotEqual(self.run_setup().returncode, 0)
        self.assertEqual((self.root / 'etc/airsim/prepare.local').read_text(), 'custom root-owned override')
        self.assertEqual(self.status(), 'failed')

    def test_failed_reservation_or_prepare_never_reports_ready(self):
        self.assertEqual(self.run_setup().returncode, 0)
        self.next_boot()
        self.write('/sys/module/audio_reserve/parameters/missing', '1\n')
        self.assertNotEqual(self.run_setup('--resume').returncode, 0)
        self.assertEqual(self.status(), 'failed')
        self.write('/sys/module/audio_reserve/parameters/missing', '0\n')
        self.assertNotEqual(self.run_setup('--resume', PREPARE_FAIL='1').returncode, 0)
        self.assertEqual(self.status(), 'failed')

    def test_existing_matched_install_is_reused_without_download_or_stage(self):
        self.assertEqual(self.run_setup().returncode, 0)
        self.next_boot()
        self.assertEqual(self.run_setup('--resume').returncode, 0)
        self.write('/events', '')
        result = self.run_setup()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.status(), 'ready')
        self.assertNotIn('download', (self.root / 'events').read_text())
        self.assertNotIn('stage ', (self.root / 'events').read_text())

    def test_lock_from_previous_boot_is_recoverable(self):
        self.assertEqual(self.run_setup().returncode, 0)
        self.write('/mnt/mu300-disk/extra/.airsim-complete-install/lock/boot', 'boot-one\n')
        self.next_boot()
        result = self.run_setup('--resume')
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(self.status(), 'ready')

    def test_resume_requires_actual_service_readiness_when_enabled(self):
        self.assertEqual(self.run_setup().returncode, 0)
        self.next_boot()
        self.assertNotEqual(self.run_setup('--resume', SERVICE_READY='0').returncode, 0)
        self.assertEqual(self.status(), 'failed')
        self.assertNotIn('airsim-audio-setup disable\n', (self.root / 'events').read_text())


class Profile(Setup):
    # Run just profile cases rather than inheriting setup workflow test methods.
    def profile(self, kernel='5.4.254-gb50db5b6224c', calls='OK', config='CONFIG_FIXTURE=y\n'):
        source = (ROOT / 'packaging/openwrt/audio-preflight').read_text()
        source = source.replace('1057cb97e0d2d7f26816e8bf7841faac1153833a01d4893cdc196b75ae6356b3', sha(b'CONFIG_FIXTURE=y\n'))
        source = self.redirect(source)
        source = re.sub(r'^export PATH=.*$', 'export PATH="' + str(self.root / 'mock') + ':$PATH"', source, flags=re.M)
        self.write('/preflight', source)
        self.write('/etc/openwrt_release', 'fixture')
        self.write('/opt/mu300/lib/run-dir.sh', '')
        self.write('/proc/config.gz', gzip.compress(config.encode()))
        self.write('/proc/mounts', 'disk ' + str(self.root / 'mnt/mu300-disk') + ' ext4 rw 0 0\n')
        for name, body in {
            'id': 'echo 0', 'uname': '[ "$1" != -m ] || { echo aarch64; exit; }; echo "$KERNEL"',
            'mu300-at': 'printf "%s\\n" "$CALLS"', 'pidof': 'exit 1',
            'ubus': 'exit 1', 'jsonfilter': 'exit 1', 'timeout': 'exit 0',
        }.items():
            self.write('/mock/' + name, '#!/bin/sh\n' + body + '\n', True)
        return subprocess.run(['sh', str(self.root / 'preflight')], capture_output=True, text=True,
                              env=dict(self.env, KERNEL=kernel, CALLS=calls, PATH=str(self.root / 'mock') + ':' + os.environ['PATH']))

    def test_profile_rejects_unbuilt_kernel_configuration_and_busy_call(self):
        result = self.profile()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertNotEqual(self.profile(kernel='7.2.8').returncode, 0)
        self.assertNotEqual(self.profile(config='CONFIG_DIFFERENT=y\n').returncode, 0)
        self.assertNotEqual(self.profile(calls='+CLCC: fixture\nOK').returncode, 0)
        self.assertNotEqual(self.profile(calls='ERROR').returncode, 0)


if __name__ == '__main__':
    # Profile shares fixtures, not the inherited workflow tests.
    suite = unittest.TestSuite([unittest.defaultTestLoader.loadTestsFromTestCase(Setup),
                               Profile('test_profile_rejects_unbuilt_kernel_configuration_and_busy_call')])
    result = unittest.TextTestRunner(verbosity=2).run(suite)
    raise SystemExit(not result.wasSuccessful())
