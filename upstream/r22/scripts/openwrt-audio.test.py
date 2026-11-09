#!/usr/bin/env python3
"""Run recovery guard tests against the isolated, patched upstream source."""
import os
from pathlib import Path
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
SOURCE = Path(os.environ.get('AIRSIM_AUDIO_SOURCE', ROOT / 'out/openwrt-audio/audio-candidate'))


class RecoveryGuards(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.script = (SOURCE / 'device/bin/mu300-audio-dsp').read_text()

    def reserve(self, loaded, missing=None, ranges='0xaf700000,0x300000;0xafa00000,0x600000'):
        body = self.script.split('reserve() {', 1)[1].split('\n}\n', 1)[0]
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            if missing is not None:
                (root / 'reservation').mkdir()
                (root / 'reservation/missing').write_text(missing)
                (root / 'reservation/ranges').write_text(ranges)
            (root / 'audio_reserve.ko').touch()
            result = subprocess.run(['sh', '-c', f'''
RESERVE="$1/reservation"
A="$1"
DDR32=0xaf700000,0x300000
DSPBIN=0xafa00000,0x600000
loaded() {{ return {0 if loaded else 1}; }}
insmod() {{ echo UNEXPECTED_LOAD; return 83; }}
reserve() {{{body}
}}
reserve
''', 'test', directory], text=True, capture_output=True)
            return result

    def test_loaded_legacy_driver_cannot_reclaim_memory(self):
        for missing in (None, '1', '', 'invalid'):
            result = self.reserve(True, missing)
            self.assertNotEqual(result.returncode, 0)
            self.assertNotIn('UNEXPECTED_LOAD', result.stdout)

    def test_complete_reservation_is_reused(self):
        for loaded in (False, True):
            result = self.reserve(loaded, '0')
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertNotIn('UNEXPECTED_LOAD', result.stdout)

    def test_other_reserved_ranges_do_not_authorize_audio(self):
        for ranges in ('', '0x100000,0x300000'):
            self.assertNotEqual(self.reserve(False, '0', ranges).returncode, 0)

    def test_partial_reservation_fails_without_global_cache_write(self):
        result = self.reserve(False, '1')
        self.assertNotEqual(result.returncode, 0)
        self.assertNotIn('UNEXPECTED_LOAD', result.stdout)
        body = self.script.split('reserve() {', 1)[1].split('\n}\n', 1)[0]
        self.assertNotIn('/proc/sys/vm/drop_caches', body)

    def test_legacy_bypass_cannot_override_config_mismatch(self):
        body = self.script.split('set_fits() {', 1)[1].split('\n}\n', 1)[0]
        with tempfile.TemporaryDirectory() as directory:
            (Path(directory) / 'KERNEL').write_text('CONFIG=expected\n')
            for actual, code in (('wrong', 1), ('expected', 0)):
                result = subprocess.run(['sh', '-c', f'''
MU300_AUDIO_ANY_BUILD=1
config_sum() {{ echo {actual}; }}
set_fits() {{{body}
}}
set_fits "$1"
''', 'test', directory], capture_output=True)
                self.assertEqual(result.returncode, code)

    def test_cache_clean_never_maps_unowned_pages_and_propagates_errors(self):
        source = (SOURCE / 'kernel/common/audio_reserve/audio_reserve.c').read_text()
        body = source[source.index('static bool clean(void)'):source.index('static void report(void)')]
        harness = r'''
#include <stdbool.h>
#include <stddef.h>
#include <assert.h>
#include <stdint.h>
typedef unsigned long dma_addr_t;
struct device { unsigned long *dma_mask; unsigned long coherent_dma_mask; };
struct platform_device { struct device dev; };
static struct platform_device device;
static unsigned long missing_pages;
static struct { unsigned long start, end; bool kernel_reserved; } rg[2];
static int nr=2, maps, unregisters, fail_register, fail_mask, fail_map;
#define IS_ERR(p) ((p)==NULL)
#define DMA_BIT_MASK(n) (~0ULL)
#define DMA_BIDIRECTIONAL 0
#define PAGE_SHIFT 12
#define pr_warn(...) ((void)0)
static struct platform_device *platform_device_register_simple(const char *n, int i, void *v, int c) { return fail_register ? NULL : &device; }
static int dma_set_mask_and_coherent(struct device *d, unsigned long long m) { return fail_mask; }
static void *pfn_to_page(unsigned long p) { return (void *)(uintptr_t)p; }
static dma_addr_t dma_map_page(struct device *d, void *p, int o, size_t s, int direction) { maps++; return maps==fail_map ? 0 : 1; }
static bool dma_mapping_error(struct device *d, dma_addr_t a) { return !a; }
static void dma_unmap_page(struct device *d, dma_addr_t a, size_t s, int direction) {}
static void platform_device_unregister(struct platform_device *p) { unregisters++; }
'''
        main = r'''
int main(void) {
 rg[0].start=1; rg[0].end=2; rg[1].start=2; rg[1].end=3;
 missing_pages=1; assert(!clean()); assert(maps==0 && unregisters==0);
 missing_pages=0; fail_register=1; assert(!clean()); assert(maps==0);
 fail_register=0; fail_mask=1; assert(!clean()); assert(maps==0 && unregisters==1);
 fail_mask=0; fail_map=2; assert(!clean()); assert(maps==2 && unregisters==2);
 maps=0; fail_map=0; assert(clean()); assert(maps==2 && unregisters==3);
 maps=0; rg[0].kernel_reserved=true; assert(clean()); assert(maps==1);
 return 0;
}
'''
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory)
            (path / 'test.c').write_text(harness + body + main)
            subprocess.run(['cc', '-std=c11', str(path / 'test.c'), '-o', str(path / 'test')], check=True)
            subprocess.run([str(path / 'test')], check=True)


if __name__ == '__main__':
    unittest.main()
