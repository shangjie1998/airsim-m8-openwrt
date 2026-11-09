#!/usr/bin/env python3
"""Exercise cache/mirror recovery without any network or device access."""
import hashlib
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parent.parent


class FirmwareCache(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix='firmware cache ')
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.repo = self.root / 'upstream'
        self.repo.mkdir()
        self.git = shutil.which('git')
        self.data = b'public upstream fixture'
        (self.repo / 'agdsp.bin').write_bytes(self.data)
        def git(*args):
            return subprocess.check_output([self.git, '-C', str(self.repo), *args], stderr=subprocess.DEVNULL).decode().strip()
        git('init', '-q')
        git('add', 'agdsp.bin')
        git('-c', 'user.name=Fixture', '-c', 'user.email=fixture@example.invalid',
            '-c', 'commit.gpgsign=false', 'commit', '-qm', 'fixture')
        commit = git('rev-parse', 'HEAD')
        self.cache = self.root / 'cache'
        self.cache.mkdir()
        source = (ROOT / 'scripts/fetch-firmware.sh').read_text()
        source = source.replace('8a3fbd2cd0556796d0ffef5be05b4c21a743f868', commit)
        source = source.replace('378ceea78dbf013e10625e71efd6fab487d727e371884ae8daf078da2828b937', hashlib.sha256(self.data).hexdigest())
        self.script = self.root / 'fetch.sh'
        self.script.write_text(source)
        mock = self.root / 'mock'
        mock.mkdir()
        for name, script in {
            'curl': '#!/bin/sh\necho network-refused >> "$EVENTS"\nexit 1\n',
            'git': '#!/bin/sh\ncase "$*" in *clone*|*fetch*) echo network-refused >> "$EVENTS"; exit 1;; esac\necho local-git >> "$EVENTS"\nexec "$REAL_GIT" "$@"\n',
        }.items():
            p = mock / name
            p.write_text(script)
            p.chmod(0o755)
        self.events = self.root / 'events'
        self.events.write_text('')
        self.env = dict(os.environ, AIRSIM_FIRMWARE_CACHE=str(self.cache),
                        AIRSIM_FIRMWARE_REPO=str(self.repo), REAL_GIT=self.git,
                        EVENTS=str(self.events), PATH=str(mock) + ':' + os.environ['PATH'])

    def run_fetch(self):
        return subprocess.run(['sh', str(self.script)], env=self.env, capture_output=True, text=True)

    def test_preserved_mirror_recovers_missing_cache_offline(self):
        r = self.run_fetch()
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual(Path(r.stdout.strip()).read_bytes(), self.data)
        self.assertNotIn('network', self.events.read_text())
        self.assertTrue((self.cache / 'PROVENANCE').exists())

    def test_verified_cache_does_not_need_git_or_network(self):
        (self.cache / 'agdsp.bin').write_bytes(self.data)
        shutil.rmtree(self.repo)
        self.assertEqual(self.run_fetch().returncode, 0)
        self.assertEqual(self.events.read_text(), '')

    def test_corrupt_cache_is_replaced_only_from_verified_mirror(self):
        (self.cache / 'agdsp.bin').write_bytes(b'broken')
        r = self.run_fetch()
        self.assertEqual(r.returncode, 0, r.stderr)
        self.assertEqual((self.cache / 'agdsp.bin').read_bytes(), self.data)
        self.assertNotIn('network', self.events.read_text())

    def test_no_valid_source_does_not_replace_cache_with_bad_data(self):
        (self.cache / 'agdsp.bin').write_bytes(b'preserved invalid cache')
        shutil.rmtree(self.repo)
        self.assertNotEqual(self.run_fetch().returncode, 0)
        self.assertEqual((self.cache / 'agdsp.bin').read_bytes(), b'preserved invalid cache')
        self.assertEqual(list(self.cache.glob('.fetch.*')), [])


if __name__ == '__main__':
    unittest.main(verbosity=2)
