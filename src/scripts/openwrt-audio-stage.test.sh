#!/bin/sh
# Runs ONLY inside a disposable container. No host/device directories are writable.
# docker run --rm -v "$PWD:/repo:ro" node:22-alpine sh /repo/scripts/openwrt-audio-stage.test.sh
set -eu
[ -e /.dockerenv ] && [ -d /repo/packaging/openwrt-audio ] || exit 2
mkdir -p /fixture/bin /fixture/pkg /opt/mu300/lib /opt/mu300/bin /lib/firmware \
    /etc/airsim /etc/rc.d /etc/init.d /mnt/mu300-disk/extra
printf 'CONFIG_TEST=y\n' | gzip > /fixture/config.gz
printf '/dev/test /mnt/mu300-disk ext4 rw 0 0\n' > /fixture/mounts
printf 'OpenWrt fixture\n' > /etc/openwrt_release
printf 'library\n' > /opt/mu300/lib/run-dir.sh
printf 'existing firmware\n' > /lib/firmware/agdsp.bin
printf 'old override\n' > /etc/airsim/prepare.local
ln -s ../init.d/airsim /etc/rc.d/S98airsim
ln -s /old/missing/mu300-voice /usr/bin/mu300-voice
printf '#!/bin/sh\nexit 0\n' > /etc/init.d/airsim
chmod +x /etc/init.d/airsim
cat > /fixture/bin/uname <<'SH'
#!/bin/sh
case $1 in -m) echo aarch64;; -r) echo 5.4.254-gb50db5b6224c;; esac
SH
printf '#!/bin/sh\necho OK\n' > /fixture/bin/mu300-at
for cmd in amixer aplay arecord; do printf '#!/bin/sh\nexit 0\n' > /fixture/bin/$cmd; done
printf '#!/bin/sh\n[ "${FAIL_COPY:-0}" = 0 ] || exit 1\nexec /bin/busybox cp "$@"\n' > /fixture/bin/cp
chmod +x /fixture/bin/*
config=$(gzip -dc /fixture/config.gz | sha256sum | cut -d' ' -f1)
sed -e 's@/proc/config.gz@/fixture/config.gz@g' -e 's@/proc/mounts@/fixture/mounts@g' \
    -e "s/^CONFIG=.*/CONFIG=$config/" \
    -e '/^export PATH=/a export PATH="/fixture/bin:$PATH"' \
    /repo/packaging/openwrt-audio/stage-device.sh > /fixture/stage.sh
P=/fixture/pkg
K=$P/modules/5.4.254-gb50db5b6224c
mkdir -p "$P/bin" "$P/integration" "$P/etc" "$K"
for cmd in pcmhost ctlset mu300-hfp mu300-voice mu300-audio-dsp; do
    printf '#!/bin/sh\nexit 0\n' > "$P/bin/$cmd"
done
cp /repo/packaging/openwrt-audio/prepare /repo/packaging/openwrt-audio/airsim-audio-reserve "$P/integration/"
printf 'fixture\n' > "$P/etc/modprobe-mu300-audio.conf"
printf 'KREL=5.4.254-gb50db5b6224c\nCONFIG=%s\n' "$config" > "$K/KERNEL"
printf '{"abi_pass": true}\n' > "$P/abi-report.json"
(cd "$P"; find . -type f -exec sha256sum {} \;) > /fixture/manifest
cp /fixture/manifest "$P/manifest"
sh /fixture/stage.sh --check "$P"
[ "$(cat /etc/airsim/prepare.local)" = 'old override' ]
[ -L /etc/rc.d/S98airsim ]
# A copy failure after the backup must restore all old links and files.
if FAIL_COPY=1 sh /fixture/stage.sh --stage "$P"; then exit 1; fi
[ "$(cat /etc/airsim/prepare.local)" = 'old override' ]
[ "$(readlink /usr/bin/mu300-voice)" = /old/missing/mu300-voice ]
[ -L /etc/rc.d/S98airsim ]
rm -rf /mnt/mu300-disk/extra/.airsim-audio-recovery-backup
sh /fixture/stage.sh --stage "$P"
[ ! -e /etc/rc.d/S98airsim ] && [ ! -L /etc/rc.d/S98airsim ]
[ -L /etc/rc.d/S10airsim-audio-reserve ]
[ "$(readlink /usr/bin/mu300-voice)" = /mnt/mu300-disk/extra/airsim-audio/bin/mu300-voice ]
sh /fixture/stage.sh --rollback
[ "$(cat /etc/airsim/prepare.local)" = 'old override' ]
[ "$(readlink /usr/bin/mu300-voice)" = /old/missing/mu300-voice ]
[ -L /etc/rc.d/S98airsim ]
[ ! -L /etc/rc.d/S10airsim-audio-reserve ]
[ "$(cat /lib/firmware/agdsp.bin)" = 'existing firmware' ]
echo 'PASS: read-only preflight, failed-copy recovery, staging, rollback, firmware preservation'
