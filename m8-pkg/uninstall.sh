#!/bin/sh
# Removes the AirSIM bubble completely: service, files, DSP routes, LuCI menu.
# The device keeps its modem, kernel, partitions, native e5 voice stack and every
# firmware-provided file — including the vendor /usr/libexec/rpcd/mu300dash,
# which we never wrote to in the first place.
set -eu
say() { printf '%s\n' "$*"; }

say "== stopping service"
if [ -x /etc/init.d/airsim-m8 ]; then
	/etc/init.d/airsim-m8 stop 2>/dev/null || true
	/etc/init.d/airsim-m8 disable 2>/dev/null || true
fi
rm -f /etc/init.d/airsim-m8
rm -f /etc/rc.d/S95airsim-m8 /etc/rc.d/K10airsim-m8

say "== clearing DSP voice routes"
amixer -q -c 0 cset "name=S_VOICE_CAP_C SWITCH" off 2>/dev/null || true
amixer -q -c 0 cset "name=S_VOICE_PCM_P SWITCH" off 2>/dev/null || true

say "== removing shims and pool"
rm -f /usr/bin/mu300-at /opt/mu300/bin/mu300-sms
rm -f /usr/lib/airsim/airsim-agent /usr/lib/airsim/airsim-mu300d
rmdir /usr/lib/airsim 2>/dev/null || true
rmdir /opt/mu300/bin 2>/dev/null || true
rmdir /opt/mu300 2>/dev/null || true
rm -rf /etc/mu300

say "== removing LuCI menu (ours only)"
rm -f /usr/libexec/rpcd/airsim
rm -f /usr/libexec/rpcd/airsim-sms
rm -f /usr/share/luci/menu.d/luci-app-airsim.json
rm -f /usr/share/rpcd/acl.d/luci-app-airsim.json
rm -f /www/luci-static/resources/view/airsim/overview.js \
	/www/luci-static/resources/view/airsim/manage.js \
	/www/luci-static/resources/view/airsim/sms.js \
	/www/luci-static/resources/view/airsim/overview.css
rmdir /www/luci-static/resources/view/airsim 2>/dev/null || true
rm -f /etc/config/airsim
/etc/init.d/rpcd restart >/dev/null 2>&1 || true

say "== firmware file guard"
if [ -f /usr/libexec/rpcd/mu300dash ]; then
	say "vendor mu300dash: PRESENT (untouched) $(md5sum /usr/libexec/rpcd/mu300dash 2>/dev/null | awk '{print $1}')"
else
	say "vendor mu300dash: not on this firmware"
fi

say "== removing /opt/airsim-m8"
rm -rf /opt/airsim-m8

say "== native voice service"
if pgrep -f e5-voice-native >/dev/null 2>&1; then
	say "e5-voice-native still running (untouched)"
else
	say "WARNING: e5-voice-native is not running; reboot restores the native stack"
fi
say "uninstall finished"
