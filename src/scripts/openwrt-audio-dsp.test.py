#!/usr/bin/env python3
"""Probe the real patched DSP-reply function using mock logs and a null PCM sink."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parent.parent
SOURCE = Path(os.environ.get('AIRSIM_AUDIO_SOURCE', ROOT / 'out/openwrt-audio/audio-candidate'))


class DSPReply(unittest.TestCase):
    def probe(self, after, playback_exit=0):
        source = (SOURCE / 'device/bin/mu300-audio-dsp').read_text()
        function = 'dsp_alive() {' + source.split('dsp_alive() {', 1)[1].split('\n}', 1)[0] + '\n}\ndsp_alive\n'
        with tempfile.TemporaryDirectory() as d:
            root = Path(d)
            (root / 'cards').write_text('0 sprdphone\n')
            (root / 'after').write_text(after)
            commands = {
                'dmesg': 'if [ ! -f "$STATE" ]; then touch "$STATE"; printf "%s\\n" "[1.0] existing log marker"; else cat "$AFTER"; fi\n',
                'aplay': 'cat >/dev/null\nexit "$PLAYBACK_EXIT"\n',
                'timeout': 'shift\nexec "$@"\n',
            }
            for name, body in commands.items():
                path = root / name; path.write_text('#!/bin/sh\n' + body); path.chmod(0o755)
            env = dict(os.environ, PATH=str(root) + ':' + os.environ['PATH'], STATE=str(root / 'state'),
                       AFTER=str(root / 'after'), PLAYBACK_EXIT=str(playback_exit))
            return subprocess.run(['sh'], input=function.replace('/proc/asound/cards', str(root / 'cards')),
                                  text=True, capture_output=True, env=env).returncode

    def test_fresh_reply_passes(self):
        self.assertEqual(self.probe('[1.0] existing log marker\n[2.0] aud_send_cmd out,cmd =4\n'), 0)

    def test_stale_reply_is_not_readiness(self):
        self.assertNotEqual(self.probe('[0.5] aud_send_cmd out,cmd =4\n[1.0] existing log marker\n'), 0)

    def test_evicted_marker_fails_closed(self):
        self.assertNotEqual(self.probe('[2.0] aud_send_cmd out,cmd =4\n'), 0)

    def test_reply_with_command_failure_fails(self):
        self.assertNotEqual(self.probe('[1.0] existing log marker\n[2.0] aud_send_cmd out,cmd =4\nfailed to get command\n'), 0)

    def test_pcm_probe_failure_fails(self):
        self.assertNotEqual(self.probe('[1.0] existing log marker\n[2.0] aud_send_cmd out,cmd =4\n', 1), 0)


if __name__ == '__main__':
    unittest.main()
