#!/usr/bin/env python3
"""Run the recovery preparation shell with fake hardware commands and filesystem."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parent.parent


class Prepare(unittest.TestCase):
    def run_prepare(self, response='OK', at_exit=0, reserve_exit=0, start_exit=0, ready=False):
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            binary = root / 'bin'; binary.mkdir()
            for name in ('mu300-at', 'mu300-voice', 'mu300-audio-dsp', 'pcmhost', 'ctlset', 'amixer', 'aplay', 'arecord'):
                p = binary / name
                if name == 'mu300-at':
                    body = 'printf "%s\\n" "$RESPONSE"\nexit "$AT_EXIT"\n'
                elif name == 'mu300-audio-dsp':
                    body = 'echo "$1" >> "$TRACE"\ncase $1 in reserve) exit "$RESERVE_EXIT";; start) exit "$START_EXIT";; esac\n'
                else:
                    body = 'exit 0\n'
                p.write_text('#!/bin/sh\n' + body); p.chmod(0o755)
            # macOS need not have the GNU timeout utility; invocation is recorded by the fake DSP.
            (binary / 'timeout').write_text('#!/bin/sh\nshift\nexec "$@"\n')
            (binary / 'timeout').chmod(0o755)
            for p in ('opt/mu300/lib/run-dir.sh', 'lib/firmware/agdsp.bin'):
                target = root / p; target.parent.mkdir(parents=True, exist_ok=True); target.write_text('fixture')
            (root / 'run').mkdir()
            marker = root / 'run/airsim-dsp-prepared'
            if ready:
                marker.touch()
            text = (ROOT / 'packaging/openwrt-audio/prepare').read_text()
            text = text.replace('unset MU300_AUDIO_ANY_BUILD', f'export PATH="{binary}:$PATH"\nunset MU300_AUDIO_ANY_BUILD')
            for prefix in ('/opt/mu300/lib/', '/lib/firmware/', '/etc/init.d/', '/run/'):
                text = text.replace(prefix, str(root) + prefix)
            env = dict(os.environ, RESPONSE=response, AT_EXIT=str(at_exit), RESERVE_EXIT=str(reserve_exit),
                       START_EXIT=str(start_exit), TRACE=str(root / 'trace'))
            result = subprocess.run(['sh'], input=text, text=True, capture_output=True, env=env)
            trace = (root / 'trace').read_text().splitlines()
            return result.returncode, trace, marker.exists()

    def test_idle_starts_only_after_reservation(self):
        self.assertEqual(self.run_prepare(), (0, ['drivers', 'reserve', 'start'], True))

    def test_active_unknown_and_error_status_never_start(self):
        for response, status in [('OK', 1), ('ERROR\nOK', 0), ('+CLCC: redacted\nOK', 0), ('', 0)]:
            with self.subTest(response=response, status=status):
                code, trace, ready = self.run_prepare(response, status)
                self.assertNotEqual(code, 0); self.assertEqual(trace, ['drivers']); self.assertFalse(ready)

    def test_failed_reservation_never_starts(self):
        code, trace, ready = self.run_prepare(reserve_exit=1)
        self.assertNotEqual(code, 0); self.assertEqual(trace, ['drivers', 'reserve']); self.assertFalse(ready)

    def test_failed_dsp_start_never_marks_ready(self):
        code, trace, ready = self.run_prepare(start_exit=1)
        self.assertNotEqual(code, 0); self.assertFalse(ready)

    def test_same_boot_still_verifies_reservation_without_restarting(self):
        self.assertEqual(self.run_prepare(ready=True), (0, ['drivers', 'reserve'], True))


if __name__ == '__main__':
    unittest.main()
