#!/usr/bin/env python3
"""Audit rebuilt OpenWrt audio imports and reference module CRCs, without loading."""
import argparse
import json
from pathlib import Path
import struct


def module(path):
    data = path.read_bytes()
    if data[:6] != b'\x7fELF\x02\x01' or struct.unpack_from('<H', data, 18)[0] != 183:
        raise ValueError(f'{path.name}: expected AArch64 ELF64 LE')
    offset = struct.unpack_from('<Q', data, 40)[0]
    size, count, names_index = struct.unpack_from('<HHH', data, 58)
    if size != 64 or names_index >= count or offset + count * size > len(data):
        raise ValueError('invalid section table')
    headers = [struct.unpack_from('<IIQQQQIIQQ', data, offset + i * size) for i in range(count)]
    h = headers[names_index]
    names = data[h[4]:h[4] + h[5]]
    sections = {}
    for h in headers:
        if h[1] == 8:
            continue
        if h[4] + h[5] > len(data):
            raise ValueError('section outside file')
        name = names[h[0]:].split(b'\0', 1)[0].decode('ascii')
        sections[name] = data[h[4]:h[4] + h[5]]
    versions = sections.get('__versions', b'')
    if not versions or len(versions) % 64:
        raise ValueError(f'{path.name}: missing version records')
    imports = {}
    for offset in range(0, len(versions), 64):
        crc = struct.unpack_from('<Q', versions, offset)[0]
        name = versions[offset + 8:offset + 64].split(b'\0', 1)[0].decode('ascii')
        imports[name] = crc
    vermagic = [v[9:].decode() for v in sections.get('.modinfo', b'').split(b'\0') if v.startswith(b'vermagic=')]
    if len(vermagic) != 1 or 'module_layout' not in imports:
        raise ValueError(f'{path.name}: incomplete module metadata')
    return vermagic[0], imports


def symvers(paths):
    symbols = {}
    for path in paths:
        for line in path.read_text().splitlines():
            fields = line.split()
            if len(fields) < 4:
                raise ValueError('malformed symbol table')
            crc = int(fields[0], 16)
            if fields[1] in symbols and symbols[fields[1]] != crc:
                raise ValueError(f'conflicting symbol: {fields[1]}')
            symbols[fields[1]] = crc
    return symbols


def audit(kernel, peers, references, candidates, release):
    errors, ref_count, checked = [], 0, set()
    reference_magic = set()
    for path in sorted(references.rglob('*.ko')):
        magic, imports = module(path)
        reference_magic.add(magic)
        ref_count += 1
        for name, crc in imports.items():
            if name in kernel:
                checked.add(name)
                if kernel[name] != crc:
                    errors.append(f'reference CRC differs: {path.name}:{name}')
    if not ref_count or len(checked) < 100 or 'module_layout' not in checked:
        errors.append('insufficient reference-module coverage')
    all_symbols = dict(kernel)
    for name, crc in peers.items():
        if name in all_symbols and all_symbols[name] != crc:
            errors.append(f'peer/kernel CRC conflict: {name}')
        all_symbols[name] = crc
    paths = sorted(candidates.glob('*.ko'))
    if len(paths) != 24:
        errors.append(f'expected 24 candidate modules, found {len(paths)}')
    total = 0
    for path in paths:
        magic, imports = module(path)
        if magic not in reference_magic or magic.split()[0] != release:
            errors.append(f'vermagic differs: {path.name}')
        for name, crc in imports.items():
            total += 1
            if all_symbols.get(name) != crc:
                errors.append(f'candidate unresolved/mismatched import: {path.name}:{name}')
    return dict(abi_pass=not errors, reference_module_count=ref_count,
                reference_unique_crc_checked=len(checked), candidate_module_count=len(paths),
                candidate_imports_checked=total, errors=errors, hardware_verified=False)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--kernel-symvers', type=Path, required=True)
    parser.add_argument('--audio-symvers', type=Path, required=True)
    parser.add_argument('--references', type=Path, required=True)
    parser.add_argument('--candidates', type=Path, required=True)
    parser.add_argument('--release', default='5.4.254-gb50db5b6224c')
    parser.add_argument('--report', type=Path, required=True)
    args = parser.parse_args()
    report = audit(symvers([args.kernel_symvers]), symvers([args.audio_symvers]),
                   args.references, args.candidates, args.release)
    args.report.write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps(report))
    raise SystemExit(0 if report['abi_pass'] else 1)


if __name__ == '__main__':
    main()
