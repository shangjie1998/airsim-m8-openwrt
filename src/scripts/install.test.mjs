import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import {spawnSync} from 'node:child_process';
import {fileURLToPath} from 'node:url';
const file=fileURLToPath(new URL('./install.sh',import.meta.url));
const src=readFileSync(file,'utf8');
test('installer shell syntax, help, invalid arguments are safe on development host',()=>{
 assert.equal(spawnSync('sh',['-n',file]).status,0);
 assert.equal(spawnSync('sh',[file,'--help']).status,0);
 assert.notEqual(spawnSync('sh',[file,'--force']).status,0);
});
test('fixed version and public-key fingerprint; no untrusted install or curl-pipe-shell',()=>{
 assert.ok(src.includes('RELEASE='+readFileSync(new URL('../VERSION',import.meta.url),'utf8').trim()));
 assert.match(src,/KEY_SHA=[a-f0-9]{64}/);
 assert.equal(src.includes('--allow-untrusted'),false);
 assert.match(src,/apk --keys-dir .* verify/);
 assert.ok(src.indexOf("idle\necho") < src.indexOf('mktemp'));
 assert.ok(src.indexOf('APK signature verification failed') < src.indexOf('apk --no-network add'));
 assert.match(src,/refusing to replace it/);
 assert.equal(/ATD|ATA|CMGS|CMGD|(^|[;\n])\s*(reboot|rm -rf)\b/.test(src),false);
});
// Exercise the actual idle() shell function, including jsonfilter's nonzero
// exit for absent keys. No device/filesystem/network commands are executed.
const idle=src.slice(src.indexOf('idle() {'),src.indexOf('\nidle\necho'));
function check({calls='OK',sms='none',bridge='off',service='valid',nativeSMS=1}={}){
 const fixture=`set -eu
fail() { echo "$*" >&2; exit 1; }
mu300_at() { printf '%s\\n' '${calls}'; }
pidof() { [ "$1" != mu300-sms ] || return ${nativeSMS}; return 0; } # independent update monitor is always running
wget() { echo status; }
ubus() { if [ "$1" = list ]; then echo mu300dash; elif [ "$2" = service ]; then echo '${service}'; else echo '${sms}'; fi; }
jsonfilter() {
 read -r v || true
 case "$*:$v" in
  '-e @:valid') echo '{}' ;;
  '-e @:invalid') return 1 ;;
  *instances.*.running:valid) echo '${bridge==='off'?'false':'true'}' ;;
  *audio.active:status|*call_active:status) echo '${bridge==='busy'?'true':'false'}' ;;
  *running:running) echo 1 ;;
  *running:*) return 1 ;;
  *:none|*:done) echo 1 ;;
  *) return 1 ;;
 esac
}
${idle.replaceAll('mu300-at', 'mu300_at')}
idle
echo passed`;
 return spawnSync('sh',['-c',fixture],{encoding:'utf8'});
}
test('idle permits absent running key for native none/done job',()=>{
 assert.equal(check().status,0);
 assert.equal(check({sms:'done',bridge:'idle'}).status,0);
});
test('idle rejects calls, media, running SMS, unknown SMS and invalid CLCC',()=>{
 for(const data of [{calls:'+CLCC: fixture\nOK'},{calls:'ERROR'},{sms:'running'},{sms:'unknown'},{bridge:'busy'},{service:'invalid'},{nativeSMS:0},{nativeSMS:127}])assert.notEqual(check(data).status,0);
});

// Run the real platform preflight with shell fixtures for the bundled audio
// kernel/configuration. No real hardware or system configuration is accessed.
const platform=src.slice(src.indexOf('[ "$(id -u)"'),src.indexOf('idle() {'));
function checkPlatform({arch='aarch64',apk='apk-tools 3.0.5',missing='',kernel='5.4.254-gb50db5b6224c',config='1057cb97e0d2d7f26816e8bf7841faac1153833a01d4893cdc196b75ae6356b3'}={}) {
 return spawnSync('sh',['-c',`set -eu
fail() { echo "$*" >&2; exit 1; }
id() { echo 0; }
uname() { if [ "$1" = -m ]; then echo '${arch}'; else echo '${kernel}'; fi; }
gzip() { echo CONFIG_FIXTURE=y; }
sha256sum() { echo '${config}  -'; }
cat() { return 1; }
command() { [ "$2" != '${missing}' ]; }
apk() { echo '${apk}'; }
${platform}
echo passed`],{encoding:'utf8'});
}
test('complete installer requires matching audio kernel/configuration and basic capabilities',()=>{
 assert.equal(checkPlatform().status,0);
 assert.notEqual(checkPlatform({arch:'x86_64'}).status,0);
 assert.notEqual(checkPlatform({kernel:'7.2.8'}).status,0);
 assert.notEqual(checkPlatform({config:'different'}).status,0);
 assert.notEqual(checkPlatform({missing:'mu300-at'}).status,0);
 assert.notEqual(checkPlatform({apk:'apk-tools 2.14.0'}).status,0);
});
