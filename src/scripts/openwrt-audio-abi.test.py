#!/usr/bin/env python3
import importlib.util
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('audio_abi', Path(__file__).with_name('openwrt-audio-abi.py'))
abi = importlib.util.module_from_spec(spec)
spec.loader.exec_module(abi)


class ABIGates(unittest.TestCase):
    def fixture(self, change=None):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            refs, candidates = root / 'refs', root / 'candidates'
            refs.mkdir(); candidates.mkdir()
            reference = refs / 'reference.ko'; reference.touch()
            for n in range(24):
                (candidates / f'audio{n}.ko').touch()
            symbols = {f'symbol{n}': n for n in range(100)}
            symbols['module_layout'] = 0x12345678
            magic = '5.4-test SMP preempt mod_unload modversions aarch64'
            records = {reference: (magic, dict(symbols))}
            for p in candidates.glob('*.ko'):
                records[p] = (magic, {'module_layout': symbols['module_layout'], 'symbol5': 5, 'audio_peer': 42})
            if change:
                change(symbols, records, candidates, reference)
            with patch.object(abi, 'module', side_effect=lambda p: records[p]):
                return abi.audit(symbols, {'audio_peer': 42}, refs, candidates, '5.4-test')

    def test_matching_reference_and_peer_imports_pass(self):
        report = self.fixture()
        self.assertTrue(report['abi_pass'], report)
        self.assertFalse(report['hardware_verified'])

    def test_layout_crc_mismatch_fails(self):
        report = self.fixture(lambda syms, *_: syms.update(module_layout=0x87654321))
        self.assertFalse(report['abi_pass'])
        self.assertTrue(any('module_layout' in e for e in report['errors']))

    def test_unresolved_import_fails(self):
        def change(syms, records, candidates, _):
            records[candidates / 'audio0.ko'][1]['unknown'] = 17
        self.assertFalse(self.fixture(change)['abi_pass'])

    def test_full_vermagic_not_just_release_is_required(self):
        def change(syms, records, candidates, _):
            path = candidates / 'audio0.ko'
            _, imports = records[path]
            records[path] = ('5.4-test SMP mod_unload modversions aarch64', imports)
        self.assertFalse(self.fixture(change)['abi_pass'])

    def test_incomplete_bundle_fails(self):
        self.assertFalse(self.fixture(lambda s, r, c, f: (c / 'audio0.ko').unlink())['abi_pass'])

    def test_conflicting_reference_symbol_fails(self):
        def change(syms, records, candidates, reference):
            records[reference][1]['symbol5'] = 77
        self.assertFalse(self.fixture(change)['abi_pass'])


if __name__ == '__main__':
    unittest.main()
