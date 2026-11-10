'use strict';
'require view';
'require poll';
'require rpc';
'require view.airsim.ui as ui';

var getSettings = rpc.declare({ object: 'airsim', method: 'settings_get', expect: {}, reject: true });
var getStatus = rpc.declare({ object: 'airsim', method: 'status', expect: {}, reject: true });
var saveSettings = rpc.declare({ object: 'airsim', method: 'settings_save', params: ['config'], expect: {}, reject: true, nobatch: true });

// Agent 不硬编码中继地址：默认用 iPhone 配对时下发的那条，这里只是让它可见、可改、可清。
var DEFAULT_RELAY = 'https://airsim-push.remotepilot.site';

function validate(s) {
 if (s.mode !== 'local' && s.mode !== 'cloud') return '请选择连接模式。';
 if (!/^[a-zA-Z0-9_.-]{1,15}$/.test(s.interface)) return 'LAN 接口名称无效。';
 var used = [7580, 7581, 8575, 8576];
 for (var port of [s.pair_port, s.control_port, s.pcm_port]) {
  if (!Number.isInteger(port) || port < 1024 || port > 65535 || used.indexOf(port) >= 0) return '端口须为 1024–65535，互不重复且避开内部端口 7580/7581/8575/8576。';
  used.push(port);
 }
 if (s.relay_url) {
  try {
   var u = new URL(s.relay_url);
   if (s.relay_url.length > 512 || u.protocol !== 'https:' || !u.hostname || u.username || u.password || /[?#\s]/.test(s.relay_url)) return 'Relay 只接受不含账号、查询参数或片段的 HTTPS 地址。';
  } catch (_) { return 'Relay 地址格式不正确。'; }
 }
 return '';
}

function relayDescription(s) {
 var r = s.relay || {};
 if (!s.running || !s.agent || !s.agent.reachable) return '服务未就绪，无法确认 Relay 状态';
 if (s.mode === 'local' || r.management_blocked === 'local_mode') return '局域网模式 · 云端通信已禁用';
 if (r.management_blocked === 'relay_pairing_required') return 'Relay 已变更 · 请在客户端设置相同地址并重新配对';
 if (r.management_blocked) return '管理策略阻止云端连接，请检查设置';
 if (r.cloud_enabled === false) return '配对配置未启用云端 · 请在客户端启用并重新配对';
 if (r.last_error) return 'Relay 最近请求异常 · 不代表局域网通话不可用';
 if (r.command_connected) return '云端命令通道已连接 · 通话音频仍需实际验证';
 return r.configured ? 'Relay 已配置 · 尚未确认实时命令通道连接' : '尚未配置云端配对资料';
}

return view.extend({
 load: function() {
  return Promise.all([getSettings(), getStatus().catch(function() { return {}; })]);
 },
 render: function(data) {
  ui.install();
  var config = data[0], state = data[1] || {}, pending = false, fields = {};
  if (!config || !config.ok || !config.settings) return E('p', { 'role': 'alert' }, (config && config.error) || '无法读取连接设置，请检查插件安装；不会用默认值覆盖已有设置。');
  var settings = config.settings;

  var note = E('div', { 'class': 'as-feedback', hidden: true, 'role': 'status', 'aria-live': 'polite' }, [E('strong', {}, ''), E('p', {}, '')]);
  function message(text, kind) {
   if (!text) { note.hidden = true; return; }
   note.hidden = false;
   note.className = 'as-feedback as-feedback-' + (kind || 'info');
   note.firstChild.textContent = kind === 'error' ? '没有保存成功' : '连接设置';
   note.lastChild.textContent = text;
  }

  var relay = E('p', { 'class': 'as-copy' }, '正在读取…');
  var runtime = E('p', { 'class': 'as-caption' }, '');
  var saveBtn = E('button', { 'type': 'button', 'class': 'as-button as-primary', click: save }, '保存设置');

  function editable() { return state.service_alive === false && state.running === false; }
  function refresh(s) {
   state = s || state;
   relay.textContent = relayDescription(state);
   runtime.textContent = state.running
    ? '当前运行：配对 ' + state.pairing_port + ' / 控制 ' + state.control_port + ' / PCM ' + state.pcm_port
    : '服务已停止或状态不可用，保存的设置将在下次启动时生效。';
   saveBtn.disabled = pending || !editable();
  }

  function input(key, label, type, help) {
   fields[key] = E('input', { 'id': 'as-conn-' + key, 'type': type || 'text', 'class': 'as-input', 'value': String(settings[key]), 'autocomplete': 'off', 'maxlength': key === 'relay_url' ? 512 : 32 });
   var kids = [E('label', { 'for': 'as-conn-' + key, 'class': 'as-field-label' }, label), fields[key]];
   if (help) kids.push(E('p', { 'class': 'as-caption' }, help));
   return E('div', { 'class': 'as-setting' }, kids);
  }

  fields.mode = E('select', { 'id': 'as-conn-mode', 'class': 'as-call-select' }, [
   E('option', { 'value': 'local' }, '仅局域网'),
   E('option', { 'value': 'cloud' }, '云端 + 局域网')
  ]);
  fields.mode.value = settings.mode;

  async function save() {
   if (pending) return;
   if (!editable()) { message('请先回概览页关闭电话服务，确认停止后再保存。', 'error'); return; }
   var next = {
    mode: fields.mode.value,
    relay_url: fields.relay_url.value.trim().replace(/\/+$/, ''),
    interface: fields.interface.value.trim(),
    pair_port: Number(fields.pair_port.value),
    control_port: Number(fields.control_port.value),
    pcm_port: Number(fields.pcm_port.value)
   };
   var problem = validate(next);
   if (problem) { message(problem, 'error'); return; }
   pending = true; ui.busy(saveBtn, true);
   message('正在保存设置…', 'pending');
   try {
    var result = await saveSettings(JSON.stringify(next));
    if (!result.ok || result.error) throw Error(result.error || '没收到保存确认，请重新读取核对。');
    message(result.message || '设置已保存，回概览页启动服务后生效。', 'success');
   } catch (e) {
    message(e.message || '保存结果未知，请重新打开此页核对，不自动重试。', 'error');
   } finally {
    pending = false; ui.busy(saveBtn, false); refresh(state);
   }
  }

  var modeCard = E('section', { 'class': 'as-card' }, [
   E('div', { 'class': 'as-card-heading' }, [E('h3', {}, '连接模式')]),
   E('p', { 'class': 'as-copy', style: 'margin-top:8px' }, '局域网模式只在本网内跑；云端模式保留局域网通路，另外注册 Relay 做远程命令与推送。'),
   E('label', { 'for': 'as-conn-mode', 'class': 'as-field-label' }, '当前模式'),
   fields.mode,
   E('p', { 'class': 'as-caption' }, '改模式后需要回概览页重启服务；客户端那边也要按新地址重新配对。')
  ]);

  var relayCard = E('section', { 'class': 'as-card' }, [
   E('h3', {}, 'Relay 地址'),
   input('relay_url', 'HTTPS 地址', 'url', '留空即跟随客户端配对时下发的地址。官方通道默认 ' + DEFAULT_RELAY + '；改成自己的地址后，客户端必须用同一地址重新配对。'),
   E('div', { 'class': 'as-buttons' }, [
    E('button', { 'type': 'button', 'class': 'as-button as-secondary', click: function() { fields.relay_url.value = DEFAULT_RELAY; } }, '填入官方默认'),
    E('button', { 'type': 'button', 'class': 'as-button as-secondary', click: function() { fields.relay_url.value = ''; } }, '改为跟随客户端')
   ]),
   E('p', { 'class': 'as-relay-line' }, '页面设置：' + (settings.relay_url || '未指定 · 跟随客户端配对地址')),
   E('p', { 'class': 'as-caption' }, '“已配置”不等于公网可达；命令通道连上也不代表双向语音已验收。')
  ]);

  var portCard = E('section', { 'class': 'as-card' }, [
   E('h3', {}, '端口与接口'),
   input('interface', '可信 LAN 接口', 'text', '通常为 br-lan。只绑定该接口的私有 IPv4，不改路由、防火墙或设备网络。'),
   E('div', { 'class': 'as-port-fields' }, [
    input('pair_port', '配对端口', 'number'),
    input('control_port', '控制端口', 'number'),
    input('pcm_port', 'PCM 端口', 'number')
   ]),
   E('p', { 'class': 'as-caption' }, '改端口会通过 Bonjour 重新公布，客户端需要重新发现，必要时重新配对。内部端口保持仅回环访问。')
  ]);

  var saveCard = E('section', { 'class': 'as-card' }, [
   E('h3', {}, '保存'),
   E('p', { 'class': 'as-copy', style: 'margin-top:8px' }, '保存前必须在概览页把电话服务停掉，否则新设置不会落盘。'),
   E('div', { 'class': 'as-buttons' }, [saveBtn]),
   note
  ]);

  var rootEl = E('div', { 'class': 'as-root' }, [
   ui.stylesheet(),
   E('header', { 'class': 'as-header' }, [
    E('div', {}, [E('div', { 'class': 'as-eyebrow' }, 'AirSIM · 连接'), E('h2', {}, '连接方式，自己定。'), E('p', {}, '只管 VIPME-M8 这一侧，不动 AirSIM 客户端。')]),
    E('span', { 'class': 'as-version' }, 'v' + config.version)
   ]),
   E('div', { 'class': 'as-layout' }, [
    modeCard,
    relayCard,
    portCard,
    E('section', { 'class': 'as-card' }, [E('h3', {}, '运行状态'), relay, runtime]),
    saveCard
   ]),
   E('footer', { 'class': 'as-footer' }, 'AirSIM for VIPME-M8 · 连接')
  ]);
  refresh(state);
  return rootEl;
 },
 handleSaveApply: null, handleSave: null, handleReset: null
});
