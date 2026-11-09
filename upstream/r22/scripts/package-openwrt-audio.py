#!/usr/bin/env python3
"""Package a locally audited recovery candidate; never install or publish it."""
import argparse
import gzip
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import tarfile

ROOT = Path(__file__).resolve().parent.parent
RECIPE = ROOT / 'packaging/openwrt-audio'
SPEC = importlib.util.spec_from_file_location('audio_abi', ROOT / 'scripts/openwrt-audio-abi.py')
ABI = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(ABI)


def digest(data):
    return hashlib.sha256(data).hexdigest()


def archive(path, files):
    """Only explicit regular files, root ownership, fixed time; no host metadata."""
    with path.open('wb') as raw, gzip.GzipFile(filename='', mode='wb', fileobj=raw, mtime=0) as gz:
        with tarfile.open(fileobj=gz, mode='w', format=tarfile.GNU_FORMAT) as tar:
            for name, (data, executable) in sorted(files.items()):
                entry = tarfile.TarInfo('./' + name)
                entry.mode = 0o755 if executable else 0o644
                entry.uid = entry.gid = entry.mtime = 0
                entry.uname = entry.gname = 'root'
                entry.size = len(data)
                tar.addfile(entry, io.BytesIO(data))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--evidence', type=Path, required=True)
    parser.add_argument('--references', type=Path, required=True)
    parser.add_argument('--audio-source', type=Path, required=True)
    parser.add_argument('--legacy-userspace', type=Path, required=True)
    parser.add_argument('--out', type=Path, required=True)
    args = parser.parse_args()
    pins = json.loads((RECIPE / 'sources.json').read_text())
    modules = args.evidence / 'audio-modules'
    report = ABI.audit(ABI.symvers([args.evidence / 'openwrt-Module.symvers']),
                       ABI.symvers([args.evidence / 'audio-Module.symvers']),
                       args.references, modules, pins['target_kernel_release'])
    if not report['abi_pass']:
        raise SystemExit(json.dumps(report))
    legacy = args.legacy_userspace.read_bytes()
    if digest(legacy) != pins['legacy_userspace_archive']['sha256']:
        raise SystemExit('Legacy userspace archive hash mismatch')
    files = {}

    def add(name, data, executable=False):
        files[name] = (data, executable)

    with tarfile.open(fileobj=io.BytesIO(legacy), mode='r:gz') as tar:
        for name in ('bin/pcmhost', 'bin/ctlset', 'bin/mu300-hfp', 'licence/LICENSE', 'licence/COPYING'):
            item = tar.getmember('./' + name)
            if not item.isfile():
                raise SystemExit('Unexpected legacy archive entry')
            data = tar.extractfile(item).read()
            if name.startswith('bin/') and (data[:6] != b'\x7fELF\x02\x01' or data[18:20] != b'\xb7\x00'):
                raise SystemExit('Userspace tool is not AArch64 ELF64 LE')
            add(name, data, name.startswith('bin/'))
    for name in ('mu300-voice', 'mu300-audio-dsp'):
        add('bin/' + name, (args.audio_source / 'device/bin' / name).read_bytes(), True)
    add('licence/current-audio-LICENSE', (args.audio_source / 'LICENSE').read_bytes())
    add('etc/modprobe-mu300-audio.conf', (args.audio_source / 'device/etc/modprobe-mu300-audio.conf').read_bytes())
    add('integration/prepare', (RECIPE / 'prepare').read_bytes(), True)
    add('integration/airsim-audio-reserve', (RECIPE / 'airsim-audio-reserve').read_bytes(), True)
    add('integration/stage-device.sh', (RECIPE / 'stage-device.sh').read_bytes(), True)
    target = 'modules/' + pins['target_kernel_release'] + '/'
    for path in sorted(modules.glob('*.ko')):
        add(target + path.name, path.read_bytes())
    add(target + 'KERNEL', ('KEY=5.4\nKREL=' + pins['target_kernel_release'] + '\nCONFIG=' +
                           pins['target_config_sha256'] + '\nTAG=v2026.10.30.1\n').encode())
    report['module_sha256'] = {path.name: digest(path.read_bytes()) for path in sorted(modules.glob('*.ko'))}
    add('abi-report.json', (json.dumps(report, indent=2) + '\n').encode())
    add('sources.json', (RECIPE / 'sources.json').read_bytes())
    add('guard-reservation.patch', (RECIPE / 'guard-reservation.patch').read_bytes())
    add('startup-check.patch', (RECIPE / 'startup-check.patch').read_bytes())
    add('name', b'airsim-audio\n')
    add('VERSION', b'0.1.0-r16\n')
    add('NOTICE', b'Independent AirSIM integration; not an official MU300 Linux/Audio release.\n'
                  b'MU300 audio: copyright 2026 dikeckaan and contributors.\n'
                  b'Unisoc driver sources and modifications: GPL-2.0. Tools/scripts: MIT.\n'
                  b'ABI checked; short U30 Air calls confirmed; long calls unverified. No proprietary firmware included.\n')
    manifest = ''.join(f'{digest(data)}  ./{name}\n' for name, (data, _) in sorted(files.items()))
    add('manifest', manifest.encode())
    args.out.mkdir(parents=True, exist_ok=True)
    result = args.out / 'airsim-audio-0.1.0-r16.tar.gz'
    archive(result, files)
    # Corresponding audio source and integration recipes, with explicit roots:
    # never sweep the workspace, device backups, signing keys or build outputs.
    source_files = {}
    for directory in ('kernel/5.4', 'kernel/common', 'src', 'device'):
        for path in sorted((args.audio_source / directory).rglob('*')):
            if not path.is_file() or path.is_symlink() or '__pycache__' in path.parts:
                continue
            if path.suffix in ('.ko', '.o', '.out', '.pyc', '.pem'):
                raise SystemExit('Unexpected generated/private file in audio source')
            name = 'mu300-audio/' + path.relative_to(args.audio_source).as_posix()
            source_files[name] = (path.read_bytes(), bool(path.stat().st_mode & 0o111))
    for name in ('LICENSE', 'kernel/COPYING', 'kernel/README.md'):
        path = args.audio_source / name
        if path.is_file():
            source_files['mu300-audio/' + name] = (path.read_bytes(), False)
    for path in sorted(RECIPE.iterdir()):
        if path.is_file():
            source_files['integration/packaging/openwrt-audio/' + path.name] = (path.read_bytes(), path.suffix == '.sh')
    for name in ('package-openwrt-audio.py', 'openwrt-audio-abi.py', 'openwrt-audio-abi.test.py',
                 'openwrt-audio.test.py', 'openwrt-audio-prepare.test.py', 'openwrt-audio-dsp.test.py',
                 'openwrt-audio-stage.test.sh'):
        source_files['integration/scripts/' + name] = ((ROOT / 'scripts' / name).read_bytes(), True)
    source_result = args.out / 'airsim-audio-0.1.0-r16-sources.tar.gz'
    archive(source_result, source_files)
    (args.out / 'SHA256SUMS').write_text(''.join(f'{digest(p.read_bytes())}  {p.name}\n' for p in (result, source_result)))
    print(result)
    print(source_result)
    print('Candidate only: no firmware, device state, kernel Image or automatic installation.')


if __name__ == '__main__':
    main()
