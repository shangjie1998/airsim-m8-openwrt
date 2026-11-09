# Current-kernel OpenWrt audio recovery (r16 preview)

> [!WARNING]
> **安装前必须按系统版本选包：R16 仅适用于 1031 之后的新系统版本；R12 仅适用于 1031 之前的旧系统版本。不得跨版本线混装，也不要仅因 R16 更新就升级旧系统。**
> 这里的 1031 指 mu300-linux 系统版本线，不是插件下载或安装日期。无法确认所属版本线时，先核对系统发布说明再安装；音频模块还必须通过内核、配置与 ABI 检查。

This is an independent integration, not an official MU300 Linux/Audio release.
It keeps the installed OpenWrt system and kernel. `sources.json` pins recovered
ZTE kernel sources, the current system's build recipes, the preserved audio
repository and the old userspace archive. Original licences and notices apply.

The legacy audio bundle is **not** installed: its configuration differs from
the running kernel despite the same release string. No `MU300_AUDIO_ANY_BUILD`
bypass is accepted by the patched loader. No proprietary firmware is bundled.

`build-kernel.sh` runs inside a fresh Linux volume with clang/LLVM 12. It applies
the system release's kernel patches and checks the normalized configuration hash
before and after olddefconfig. Its Image is a build artifact, not a flash target.
`build-audio.sh` then builds the 23 old driver modules plus audio_reserve against
the reconstructed tree and symbol table. Neither script operates a device.

Container mounts:

- `/build`: dedicated `airsim-openwrt-audio-dev1` volume (not the Android volume).
- `/inputs`: directory containing the pinned `zte-b50db5b.tar.gz`.
- `/linux-reference`: extracted and hash-verified current system recipe.
- `/audio-reference`: isolated audio commit with `guard-reservation.patch` applied.
- `/recipe`: this directory, read-only.
- `/evidence`: generated build evidence. Configuration is reconstructed from the
  pinned system's stock config and Linux fragment, not copied from device state.

Run `bash /recipe/build-kernel.sh prepare`, then `compile`, then
`bash /recipe/build-audio.sh`. Check the exported symbol CRCs against the official
current-system modules, including module_layout, before any installation.
Apply `guard-reservation.patch` then `startup-check.patch` to the isolated audio
commit. The second patch only changes userspace startup validation: bounded
probes, no kernel-log clearing, and a missing DSP reply is a startup failure.
`python3 scripts/openwrt-audio.test.py` tests the patched candidate under
`out/openwrt-audio/audio-candidate` (override with `AIRSIM_AUDIO_SOURCE`).

The reservation patch waits for ownership of every page before DMA cache
maintenance, refuses to report ready after a cache-maintenance error, bounds
range parsing, and refuses to reclaim memory while an unreserved legacy audio_mem
driver is already loaded. Activation consequently requires a clean boot with
reservation before audio driver loading. Do not load it into the current legacy
driver session. r15 remains withdrawn.

`scripts/package-openwrt-audio.py` re-audits all module imports before making a
deterministic candidate and corresponding audio-source archive. Its five inputs
are the evidence directory, official reference-module directory, isolated patched
audio source, hash-pinned v1.0.5 userspace archive, and output directory. The
source archive contains already-patched audio sources plus the integration recipe;
do not apply guard-reservation.patch to those patched sources a second time.
Kernel and system recipe inputs are pinned by URL and hash in sources.json.
The three legacy static tools' source is unchanged between v1.0.5 and the pinned
audio commit. Only the newly built 24 kernel modules enter the candidate.

For the explicitly authorized recovery, transfer the candidate over the selected
USB connection, verify its host-computed SHA-256, and unpack it in a private
temporary directory. Run `integration/stage-device.sh --check DIRECTORY` first.
`--stage DIRECTORY` saves all replaced files/links in a root-only backup, stops
AirSIM, installs the candidate under `extra/airsim-audio`, and temporarily removes
AirSIM's boot entry. It enables only the early memory reservation service. It
never reboots, loads drivers, starts the DSP, downloads firmware or makes calls.

After an arranged clean reboot, require the reservation's missing count to be 0,
the exact expected ranges, and no unreserved legacy audio_mem. Then initialize
the DSP through the guarded prepare script and start AirSIM explicitly. Restore
the previous AirSIM boot entry only after verification. Real bidirectional calls
are dialed and judged by the user; byte counts do not establish audibility.
`--rollback` restores the saved override, command links and boot entries, leaving
services stopped. Loaded permanent modules require another arranged reboot.

The staging/rollback test runs only in a disposable container:
`docker run --rm -v "$PWD:/repo:ro" node:22-alpine sh /repo/scripts/openwrt-audio-stage.test.sh`.
It covers read-only preflight, failed-copy rollback, successful staging, exact
old-link/override restoration and firmware preservation. Five preparation tests
cover unknown/active calls, failed reservation/start and same-boot readiness.
On 2026-11-01, the rebuilt modules were activated on U30 Air running
v2026.10.30.1 without replacing the system/kernel. The user confirmed clear
bidirectional iPhone AirSIM LAN audio with stutter absent or significantly
improved after the bridge queue fix. Three media sessions closed and both PCM
devices returned to closed, with no new kernel panic detected. This is short-call
evidence, not 30-minute, other-hardware or public Relay acceptance.
See ../../docs/RELEASE_v0.1.0-r16.md for installation and remaining limits.
