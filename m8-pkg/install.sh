#!/bin/sh
# AirSIM / M8 installer. Adds a self-contained bubble under /opt plus one procd
# service. The only things outside /opt are: the procd script, three tiny shims
# (an AT relay, an SMS sender, a SMS-pool mirror), a pool directory the Agent
# reads, and a LuCI menu made of files we own.
#
# SAFETY: this installer never writes a path it does not own. In particular the
# firmware ships /usr/libexec/rpcd/mu300dash (sysinfo / sim_get / sim_set /
# sim_radio_*); our SMS backend is named airsim-sms instead, so firmware files
# stay byte-identical.
set -eu
HERE=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)

say() { printf '%s\n' "$*"; }
die() { printf 'ERROR: %s\n' "$*" >&2; exit 1; }

[ "$(id -u)" = 0 ] || die "run as root"
[ -f "$HERE/airsim-mu300d" ] || die "airsim-mu300d missing next to this script"
[ -f "$HERE/airsim-agent" ] || die "airsim-agent missing next to this script"

# Refuse to install where the call audio cannot work at all: no sound card or no
# per-call voice capture endpoint means the package would look fine and stay mute.
[ -r /proc/asound/pcm ] || die "/proc/asound/pcm missing (no ALSA)"
grep -q 'FE_ST_VOICE_C' /proc/asound/pcm || die 'FE_ST_VOICE_C not exposed by this sound card'
grep -q 'FE_ST_VOICE_PCM_P' /proc/asound/pcm || die 'FE_ST_VOICE_PCM_P not exposed by this sound card'

# The Agent talks to the modem through ModemManager on this device.
command -v mmcli >/dev/null 2>&1 || die "mmcli (ModemManager) not found"

# The bubble lives on one filesystem; make sure there is room before copying.
AVAIL=$(df -k /opt | awk 'NR==2 {print $4}')
[ "${AVAIL:-0}" -gt 20000 ] || die "less than 20 MB free on /opt"

say "== installing AirSIM bubble"
mkdir -p /opt/airsim-m8/bin /opt/airsim-m8/data /etc/mu300/sms-pool/msg
chmod 700 /opt/airsim-m8/data
if /etc/init.d/airsim-m8 status >/dev/null 2>&1; then
	/etc/init.d/airsim-m8 stop || true
fi
# A running binary cannot be replaced in place, so install to a sibling name
# first and rename over it.
for bin in airsim-mu300d airsim-agent; do
	cp "$HERE/$bin" "/opt/airsim-m8/bin/.$bin.new"
	chmod 755 "/opt/airsim-m8/bin/.$bin.new"
	mv -f "/opt/airsim-m8/bin/.$bin.new" "/opt/airsim-m8/bin/$bin"
done
cp "$HERE/sms-pool-sync.py" /opt/airsim-m8/bin/sms-pool-sync.py
chmod 755 /opt/airsim-m8/bin/sms-pool-sync.py

# Shims: the Agent hardcodes the vendor helper paths. mu300-at did not exist
# before us; mu300-sms lives under /opt, never in firmware paths.
mkdir -p /opt/mu300/bin
cp "$HERE/mu300-at" /usr/bin/mu300-at
cp "$HERE/mu300-sms" /opt/mu300/bin/mu300-sms
chmod 755 /usr/bin/mu300-at /opt/mu300/bin/mu300-sms
mkdir -p /usr/lib/airsim
ln -sf /opt/airsim-m8/bin/airsim-agent /usr/lib/airsim/airsim-agent
ln -sf /opt/airsim-m8/bin/airsim-mu300d /usr/lib/airsim/airsim-mu300d

say "== installing LuCI menu (AirSIM overview / manage / SMS)"
mkdir -p /usr/libexec/rpcd /usr/share/luci/menu.d /usr/share/rpcd/acl.d \
	/www/luci-static/resources/view/airsim /etc/config
cp "$HERE/luci/usr/libexec/rpcd/airsim" /usr/libexec/rpcd/airsim
cp "$HERE/luci/usr/libexec/rpcd/airsim-sms" /usr/libexec/rpcd/airsim-sms
chmod 755 /usr/libexec/rpcd/airsim /usr/libexec/rpcd/airsim-sms
cp "$HERE/luci/usr/share/luci/menu.d/luci-app-airsim.json" /usr/share/luci/menu.d/luci-app-airsim.json
cp "$HERE/luci/usr/share/rpcd/acl.d/luci-app-airsim.json" /usr/share/rpcd/acl.d/luci-app-airsim.json
chmod 644 /usr/share/luci/menu.d/luci-app-airsim.json /usr/share/rpcd/acl.d/luci-app-airsim.json
for f in overview.js manage.js sms.js overview.css; do
	cp "$HERE/luci/www/luci-static/resources/view/airsim/$f" "/www/luci-static/resources/view/airsim/$f"
	chmod 644 "/www/luci-static/resources/view/airsim/$f"
done
cp "$HERE/luci/opt/airsim-m8/bin/sms-dash.py" /opt/airsim-m8/bin/sms-dash.py
chmod 755 /opt/airsim-m8/bin/sms-dash.py
cp "$HERE/luci/etc/config/airsim" /etc/config/airsim
/etc/init.d/rpcd restart >/dev/null 2>&1 || true

cp "$HERE/airsim-m8.init" /etc/init.d/airsim-m8
chmod 755 /etc/init.d/airsim-m8
cp "$HERE/uninstall.sh" /opt/airsim-m8/uninstall.sh
chmod 755 /opt/airsim-m8/uninstall.sh
VERSION=$(cat "$HERE/VERSION" 2>/dev/null || echo unknown)
printf '%s\n' "airsim-m8 $VERSION, installed $(date '+%Y-%m-%d %H:%M:%S')" > /opt/airsim-m8/MANIFEST

# A stray copy started by hand would keep the PCM/Agent ports and make procd
# respawn forever on "address already in use".
for pat in airsim-mu300d airsim-agent; do
	if pgrep -f "$pat" >/dev/null 2>&1; then
		for p in $(pgrep -f "$pat"); do kill "$p" 2>/dev/null || true; done
		sleep 2
	fi
done

/etc/init.d/airsim-m8 enable
/etc/init.d/airsim-m8 start
sleep 3

say "== firmware file guard (firmware must stay untouched)"
if [ -f /usr/libexec/rpcd/mu300dash ]; then
	say "vendor mu300dash: PRESENT $(md5sum /usr/libexec/rpcd/mu300dash 2>/dev/null | awk '{print $1}')"
else
	say "vendor mu300dash: not on this firmware (nothing overwritten)"
fi

say "== service state"
/etc/init.d/airsim-m8 status || true
LAN=$(ip -4 addr show br-lan 2>/dev/null | awk '/inet /{sub(/\/.*/,"",$2); print $2; exit}')
[ -n "${LAN:-}" ] || LAN=192.168.9.1
say "== daemon status"
curl -s --max-time 4 http://127.0.0.1:7581/status | tr ',' '\n' | grep -E 'audio_available|"Capture"|"Playback"|"running"' || say "(status not reachable yet)"
say "== agent health"
curl -s --max-time 4 http://127.0.0.1:8575/api/health || say "(Agent not reachable yet)"
say "== ubus objects"
ubus list 2>/dev/null | grep -E '^(airsim|airsim-sms)$' || true
say ""
say "Installed. Remove everything with: sh /opt/airsim-m8/uninstall.sh"
say "PCM endpoint for AirSIM clients: tcp $LAN:8582"
