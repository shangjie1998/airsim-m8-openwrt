#!/bin/sh
# Explicit, reversible candidate installation for the existing OpenWrt system.
# Never reboots, writes firmware, loads a driver or starts a DSP/call scene.
set -eu
export PATH="/usr/lib/airsim/bin:/usr/sbin:/usr/bin:/sbin:/bin:/opt/mu300/bin:/opt/mu300/busybox-bin"
EXT=/mnt/mu300-disk/extra/airsim-audio
SAVE=/mnt/mu300-disk/extra/.airsim-audio-recovery-backup
KREL=5.4.254-gb50db5b6224c
CONFIG=1057cb97e0d2d7f26816e8bf7841faac1153833a01d4893cdc196b75ae6356b3
die() { echo "$*" >&2; exit 1; }
idle() {
    state=$(mu300-at -t 3 AT+CLCC) || die 'Call status unavailable'
    printf '%s\n' "$state" | awk '
        /\+CLCC:/ { busy=1 }
        /^[[:space:]]*OK[[:space:]]*$/ { ok=1 }
        /ERROR/ { bad=1 }
        END { exit !(ok && !busy && !bad) }' || die 'Active or unknown call state'
    unset state
    ! pidof pcmhost aplay arecord mu300-hfp >/dev/null || die 'Audio processes are active'
}
platform() {
    [ "$(id -u)" = 0 ] || die 'Root required'
    [ -f /etc/openwrt_release ] || die 'OpenWrt required'
    [ "$(uname -m)" = aarch64 ] && [ "$(uname -r)" = "$KREL" ] || die 'Kernel mismatch'
    sum=$(gzip -dc /proc/config.gz | grep -E '^(CONFIG_|# CONFIG_)' | LC_ALL=C sort | sha256sum | cut -d' ' -f1)
    [ "$sum" = "$CONFIG" ] || die 'Kernel configuration mismatch'
    grep -q ' /mnt/mu300-disk ' /proc/mounts || die 'Persistent disk is not mounted'
    [ -r /opt/mu300/lib/run-dir.sh ] || die 'MU300 runtime library missing'
    [ -s /lib/firmware/agdsp.bin ] || die 'Existing DSP firmware missing'
    idle
}
restore() {
    [ -f "$SAVE/complete" ] || die 'No complete recovery backup'
    # Keep the previous override and boot entries exactly as they were.
    while IFS= read -r p; do rm -f "/$p"; done < "$SAVE/paths"
    tar -xpf "$SAVE/previous.tar" -C /
    if [ -d "$EXT" ]; then mv "$EXT" "$SAVE/removed-candidate"; fi
    echo 'Previous files restored. Loaded modules remain until reboot; no services started.'
}
case ${1:-} in
    --rollback)
        platform
        [ ! -d "$SAVE/removed-candidate" ] || die 'Rollback was already performed'
        /etc/init.d/airsim stop
        restore
        exit 0 ;;
    --check|--stage) ;;
    *) die 'Usage: stage-device.sh --check|--stage EXTRACTED_CANDIDATE | --rollback' ;;
esac
PKG=${2:?candidate directory required}
PKG=$(cd "$PKG" && pwd)
[ -f "$PKG/manifest" ] || die 'Candidate manifest missing'
(cd "$PKG" && sha256sum -c manifest >/dev/null) || die 'Candidate file checksum mismatch'
grep -qx "KREL=$KREL" "$PKG/modules/$KREL/KERNEL" || die 'Candidate kernel mismatch'
grep -qx "CONFIG=$CONFIG" "$PKG/modules/$KREL/KERNEL" || die 'Candidate configuration mismatch'
grep -q '"abi_pass": true' "$PKG/abi-report.json" || die 'Candidate ABI check failed'
[ ! -e "$EXT" ] && [ ! -L "$EXT" ] && [ ! -e "$SAVE" ] || die 'Existing candidate/backup requires review'
platform
for c in amixer aplay arecord timeout mu300-at; do command -v "$c" >/dev/null || die "Missing $c"; done
[ "${1}" = --stage ] || { echo 'Read-only candidate and device preflight passed'; exit 0; }
umask 077
mkdir "$SAVE"
for c in pcmhost ctlset mu300-hfp mu300-voice mu300-audio-dsp; do
    printf 'opt/mu300/bin/%s\nusr/bin/%s\n' "$c" "$c"
done > "$SAVE/paths"
printf '%s\n' etc/airsim/prepare.local etc/init.d/airsim-audio-reserve \
    etc/rc.d/S10airsim-audio-reserve etc/rc.d/S98airsim etc/modprobe.d/mu300-audio.conf >> "$SAVE/paths"
for p in /etc/rc.d/S*mu300-audio* /etc/rc.d/S*mu300-voice; do
    [ ! -e "$p" ] && [ ! -L "$p" ] || printf '%s\n' "${p#/}" >> "$SAVE/paths"
done
: > "$SAVE/present"
while IFS= read -r p; do
    [ ! -d "/$p" ] || die "Refusing directory replacement: $p"
    [ ! -e "/$p" ] && [ ! -L "/$p" ] || echo "$p" >> "$SAVE/present"
done < "$SAVE/paths"
tar -cpf "$SAVE/previous.tar" -C / -T "$SAVE/present"
touch "$SAVE/complete"
# Roll back file changes on a failed installation; leave services stopped for inspection.
trap 'rc=$?; if [ "$rc" != 0 ]; then restore; fi' EXIT
idle
/etc/init.d/airsim stop
if [ -x /etc/init.d/mu300-voice ]; then /etc/init.d/mu300-voice stop; fi
cp -a "$PKG" "$EXT"
(cd "$EXT" && sha256sum -c manifest >/dev/null)
mkdir -p /opt/mu300/bin /usr/bin /etc/airsim /etc/modprobe.d
for c in pcmhost ctlset mu300-hfp mu300-voice mu300-audio-dsp; do
    ln -sfn "$EXT/bin/$c" "/opt/mu300/bin/$c"
    ln -sfn "$EXT/bin/$c" "/usr/bin/$c"
done
cp "$EXT/integration/prepare" /etc/airsim/prepare.local
cp "$EXT/integration/airsim-audio-reserve" /etc/init.d/airsim-audio-reserve
cp "$EXT/etc/modprobe-mu300-audio.conf" /etc/modprobe.d/mu300-audio.conf
chmod 755 /etc/airsim/prepare.local /etc/init.d/airsim-audio-reserve
for p in /etc/rc.d/S*mu300-audio* /etc/rc.d/S*mu300-voice /etc/rc.d/S98airsim; do rm -f "$p"; done
ln -sfn ../init.d/airsim-audio-reserve /etc/rc.d/S10airsim-audio-reserve
trap - EXIT
echo 'Candidate staged. AirSIM autostart is temporarily disabled; no drivers loaded or DSP started.'
echo "Backup: $SAVE. Clean boot and reservation verification are required before starting AirSIM."
