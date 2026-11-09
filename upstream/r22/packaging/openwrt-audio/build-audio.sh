#!/bin/bash
# /audio-reference is pinned upstream + guard-reservation.patch, read-only.
# This builds into the same isolated volume as build-kernel.sh; never loads modules.
set -euo pipefail
S=/build/zte-u30air
O=/build/out-linux
A=/build/openwrt-audio
test -s "$O/Module.symvers"
test "$(cat "$O/include/config/kernel.release")" = 5.4.254-gb50db5b6224c
test "$(grep -E '^(CONFIG_|# CONFIG_)' "$O/.config" | LC_ALL=C sort | sha256sum | cut -d' ' -f1)" = 1057cb97e0d2d7f26816e8bf7841faac1153833a01d4893cdc196b75ae6356b3
if [ ! -d "$A" ]; then
    cp -a /audio-reference/kernel/5.4/audio_driver "$A"
    for p in audio-mem-fixed-region audio-mem-shm-shift mcdt-enable-clock sprd-card-dummy-on-defer \
        audio-pcm-period-timer audio-pcm-pointer-agcp-access audio-fe-dai-formats audio-sipc-stale-reply \
        audio-fe-voice-c-mcdt-params audio-ctl-bounds agdsp-access-bounded-wait headset-power-deinit-disable; do
        patch --batch --forward --fuzz=0 -d "$A" -p1 < "/audio-reference/kernel/5.4/patches/$p.patch"
    done
    cp -a /audio-reference/kernel/common/audio_reserve "$A/"
    cp /recipe/Kbuild "$A/Kbuild"
    touch "$A/.airsim-prepared"
fi
test -f "$A/.airsim-prepared"
INC="-I$A/sprd_audio/agdsp_access -I$A/sprd_audio/audiomem -I$A/sprd_audio/audiosipc"
INC="$INC -I$A/sprd/include -I$A/sprd/platform/include -I$A/sprd/platform/dmaengine_pcm"
INC="$INC -I$A/sprd/codec/sprd -I$A/sprd/dai/vbc/v4/vbc_dai"
make -C "$S" O="$O" ARCH=arm64 LLVM=1 LLVM_IAS=1 CC=clang LD=ld.lld \
    -j"${JOBS:-2}" M="$A" KCFLAGS="$INC" BSP_KERNEL_DEFCONFIG=sprd_qogirn6pro_defconfig modules
mkdir -p /evidence/audio-modules
count=0
while IFS= read -r ko; do
    llvm-strip --strip-debug -o "/evidence/audio-modules/${ko##*/}" "$ko"
    count=$((count + 1))
done < <(find "$A" -name '*.ko' -type f | LC_ALL=C sort)
test "$count" = 24
cp "$A/Module.symvers" /evidence/audio-Module.symvers
sha256sum /evidence/audio-modules/*.ko > /evidence/audio-SHA256SUMS
echo 'Built 24 candidate modules; ABI and hardware acceptance are still required.'
