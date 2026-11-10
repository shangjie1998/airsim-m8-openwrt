'use strict';
'require view';
'require poll';
'require rpc';
'require view.airsim.ui as ui';

/* 来电通知：跟短信转发共用一条投递渠道，不碰 modem。
 * 页面自己渲染，不再依赖设备上的 vendor 共享模块（mu300.common）。 */
var rpcGet = rpc.declare({ object: 'airsim-call', method: 'call_get', expect: {}, reject: true });
var rpcStatus = rpc.declare({ object: 'airsim-call', method: 'call_status', expect: {}, reject: true });
var rpcSave = rpc.declare({ object: 'airsim-call', method: 'call_save', params: ['config'], expect: {}, reject: true, nobatch: true });
var rpcTest = rpc.declare({ object: 'airsim-call', method: 'call_test', expect: {}, reject: true, nobatch: true });
var rpcClear = rpc.declare({ object: 'airsim-call', method: 'call_clear', expect: {}, reject: true, nobatch: true });

var TITLE = '来自M8-来电';
var RESULTS = {
 idle: '暂无投递', sent: '发送成功', service_rejected: '服务返回失败结果',
 service_answer_missing: '服务响应无法确认成功', invalid_destination: '目标地址无效',
 disabled: '通知未开启', test: '测试消息'
};
var KINDS = { ringing: '来电', answered: '接通', ended: '挂断', missed: '响铃未接', test: '测试消息' };
var PRESETS = {
 bark: ['https://api.day.app/push', { device_key: 'YOUR_KEY', title: TITLE, body: '来电号码: {number}\n来电时间: {time}\n{tag}\n\n本机号码: {self}\n开机时长: {uptime}\n运营商: {operator}\n信号: {signal}', group: 'CALL' }],
 pushplus: ['https://www.pushplus.plus/send', { token: 'YOUR_TOKEN', title: TITLE, content: '来电号码: {number}\n来电时间: {time}\n{tag}\n\n本机号码: {self}\n开机时长: {uptime}\n运营商: {operator}\n信号: {signal}', template: 'txt' }],
 serverchan: ['https://sctapi.ftqq.com/YOUR_SENDKEY.send', 'title=来自M8-来电&desp=来电号码:%20{number}%0A来电时间:%20{time}%0A{tag}%0A%0A本机号码:%20{self}%0A运营商:%20{operator}%0A信号:%20{signal}'],
 telegram: ['https://api.telegram.org/botYOUR_TOKEN/sendMessage', { chat_id: 'YOUR_CHAT_ID', text: TITLE + '\n来电号码: {number}\n来电时间: {time}\n{tag}\n本机号码: {self}' }],
 wecom: ['https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=YOUR_KEY', { msgtype: 'text', text: { content: '来电 {number}\n{state} {time}' } }],
 dingtalk: ['https://oapi.dingtalk.com/robot/send?access_token=YOUR_TOKEN', { msgtype: 'text', text: { content: '来电 {number}\n{state} {time}' } }]
};
var EXPECT_BY_PRESET = { bark: 'code=200', pushplus: 'code=200', serverchan: 'code=0', telegram: 'ok=true', wecom: 'errcode=0', dingtalk: 'errcode=0' };

function result(code) { return RESULTS[code] || code || '暂无投递'; }

return view.extend({
 load: function() { return L.resolveDefault(rpcGet(), {}); },
 render: function(data) {
  ui.install();
  var self = this;
  this.root = null;
  this.data = data || {};
  this.config = this.data.config || {};
  this.status = this.data.status || {};
  this.savedPayload = null;

  function field(id, label, node, help) {
   var kids = [E('label', { 'for': 'as-call-' + id, 'class': 'as-field-label' }, label), node];
   if (help) kids.push(E('p', { 'class': 'as-caption' }, help));
   return E('div', { 'class': 'as-setting' }, kids);
  }
  function input(id, type, extra) {
   return E('input', Object.assign({ 'id': 'as-call-' + id, 'type': type || 'text', 'class': 'as-input', 'autocomplete': 'off' }, extra || {}));
  }
  function select(id, options) {
   return E('select', { 'id': 'as-call-' + id, 'class': 'as-call-select' }, options.map(function(o) {
    return typeof o === 'string' ? E('option', { 'value': o }, o) : E('option', { 'value': o[0] }, o[1]);
   }));
  }
  function check(id, label, help) {
   var box = E('input', { 'type': 'checkbox', 'id': 'as-call-' + id });
   var kids = [E('label', { 'class': 'as-check', 'for': 'as-call-' + id }, [box, E('span', {}, label)])];
   if (help) kids.push(E('p', { 'class': 'as-caption' }, help));
   return E('div', {}, kids);
  }

  var errBox = E('div', { 'class': 'as-feedback as-feedback-error', hidden: true, 'role': 'alert' });
  var status = this.status;
  var statePill = E('span', { 'class': 'as-pill as-pill-neutral' }, '--');
  var lastLine = E('p', { 'class': 'as-caption' }, '暂无投递');
  var serviceLine = E('p', { 'class': 'as-caption' }, '—');
  var channelLine = E('p', { 'class': 'as-caption' }, '—');
  var sourceNote = E('p', { 'class': 'as-caption' }, '');
  var history = E('div', { 'class': 'as-history', 'tabindex': '0', 'aria-label': '最近投递记录' });
  var savedHint = E('p', { 'class': 'as-caption' }, '');

  var sourceSelect = select('source', [['sms', '跟随短信转发（推荐）'], ['own', '本页单独配置']]);
  var presetSelect = select('preset', [['', '自定义 / 兼容旧版'], ['json', 'JSON Webhook'], ['bark', 'Bark'], ['pushplus', 'PushPlus'], ['serverchan', 'ServerChan'], ['telegram', 'Telegram'], ['wecom', '企业微信'], ['dingtalk', '钉钉 Webhook']]);

  var urlInput = input('url', 'url', { placeholder: 'https://example.com/hook' });
  var methodSelect = select('method', ['POST', 'GET']);
  var timeoutInput = input('timeout', 'number', { min: '1', max: '120', step: '1' });
  var typeSelect = select('content_type', [['application/json', 'JSON'], ['application/x-www-form-urlencoded', 'Form URL-encoded'], ['text/plain', '纯文本']]);
  var expectInput = input('expect', 'text', { maxlength: '128', placeholder: 'code=200' });
  var headersArea = E('textarea', { 'id': 'as-call-headers', 'rows': '3', 'class': 'as-textarea', 'autocomplete': 'off', 'spellcheck': 'false' });
  var bodyArea = E('textarea', { 'id': 'as-call-body', 'rows': '6', 'class': 'as-textarea', 'spellcheck': 'false' });

  var enabled = E('input', { 'type': 'checkbox', 'id': 'as-call-enabled' });
  var notifyRinging = E('input', { 'type': 'checkbox', 'id': 'as-call-notify_ringing' });
  var notifyAnswered = E('input', { 'type': 'checkbox', 'id': 'as-call-notify_answered' });
  var notifyEnded = E('input', { 'type': 'checkbox', 'id': 'as-call-notify_ended' });
  var notifyMissed = E('input', { 'type': 'checkbox', 'id': 'as-call-notify_missed' });

  var channelBlock = E('div', { hidden: true }, [
   field('url', 'HTTPS 地址', urlInput),
   E('div', { 'class': 'as-port-fields' }, [field('method', '请求方法', methodSelect), field('timeout', '超时（秒，1–120）', timeoutInput)]),
   field('content_type', '正文格式', typeSelect),
   field('expect', '成功响应条件（可选）', expectInput, '留空时任意 HTTP 2xx 算成功；填 code=200 这类条件时，还要响应 JSON 顶层字段匹配。'),
   E('div', { 'class': 'as-setting' }, [E('label', { 'for': 'as-call-headers', 'class': 'as-field-label' }, '附加请求头（每行 Name: value）'), headersArea])
  ]);

  var saveBtn = E('button', { 'type': 'button', 'class': 'as-button as-primary' }, '保存设置');
  var testBtn = E('button', { 'type': 'button', 'class': 'as-button as-secondary' }, '发送测试通知');
  var refreshBtn = E('button', { 'type': 'button', 'class': 'as-button as-secondary' }, '刷新状态');
  var clearBtn = E('button', { 'type': 'button', 'class': 'as-button as-secondary' }, '清空投递记录');

  function error(text) {
   errBox.textContent = text || '';
   errBox.hidden = !text;
  }

  var root = E('div', { 'class': 'as-root' }, [
   ui.stylesheet(),
   E('header', { 'class': 'as-header' }, [
    E('div', {}, [E('div', { 'class': 'as-eyebrow' }, 'AirSIM · 来电通知'), E('h2', {}, '来电话，第一时间知道。'), E('p', {}, '来电、接通、挂断、漏接，按你选的渠道推出去。')]),
    statePill
   ]),
   E('div', { 'class': 'as-layout' }, [
    E('section', { 'class': 'as-card' }, [
     E('div', { 'class': 'as-card-heading' }, [E('h3', {}, '推送渠道')]),
     E('p', { 'class': 'as-caption', style: 'margin-top:8px' }, '跟随短信转发时，地址、请求方法、正文格式、超时和请求头都用短信那边的设置，这里只给通话事件准备正文模板。'),
     field('source', '渠道来源', sourceSelect),
     field('preset', '填入推送预设', presetSelect, '预设只填表单，密钥要换成自己的再保存。'),
     sourceNote,
     E('hr', {}),
     E('label', { 'class': 'as-check', 'for': 'as-call-enabled' }, [enabled, E('span', {}, '启用来电通知')]),
     E('p', { 'class': 'as-caption' }, '关掉后不再推任何通话事件，转发服务照常运行，随时能再打开。')
    ]),
    E('section', { 'class': 'as-card' }, [
     E('h3', {}, '推送哪些状态'),
     E('label', { 'class': 'as-check', 'for': 'as-call-notify_ringing' }, [notifyRinging, E('span', {}, '来电时推送 · 一响铃就发')]),
     E('label', { 'class': 'as-check', 'for': 'as-call-notify_answered' }, [notifyAnswered, E('span', {}, '接通时推送')]),
     E('label', { 'class': 'as-check', 'for': 'as-call-notify_ended' }, [notifyEnded, E('span', {}, '挂断时推送 · 带上对方号码')]),
     E('label', { 'class': 'as-check', 'for': 'as-call-notify_missed' }, [notifyMissed, E('span', {}, '响铃未接时推送')]),
     E('p', { 'class': 'as-caption', style: 'margin-top:10px' }, '设备信息每 5 分钟刷新一次；通话期间不碰 modem。')
    ]),
    E('section', { 'class': 'as-card as-wide' }, [
     E('h3', {}, '请求与正文'),
     channelBlock,
     E('div', { 'class': 'as-setting' }, [E('label', { 'for': 'as-call-body', 'class': 'as-field-label' }, '正文模板'), bodyArea]),
     E('p', { 'class': 'as-caption' }, '占位符：{number} 来电号码、{time} 来电时间、{tag} 事件标签（#CALL #CALL_IN / #CALL_ANSWER / #CALL_END / #CALL_MISSED）、{self} 本机号码、{operator} 运营商、{signal} 信号、{uptime} 开机时长、{event}、{state}、{direction}、{device}。JSON 里占位符要放在字符串内；按格式自动转义，绝不执行命令。')
    ]),
    E('section', { 'class': 'as-card' }, [
     E('h3', {}, '运行状态'),
     serviceLine, channelLine, lastLine,
     E('p', { 'class': 'as-caption' }, '转发由设备自己执行，关掉网页照样生效；只在有新通话时推，失败不自动重发。')
    ]),
    E('section', { 'class': 'as-card as-wide' }, [
     E('h3', {}, '测试与最近投递'),
     E('p', { 'class': 'as-caption', style: 'margin-top:8px' }, '测试会走真实渠道发一条固定消息（号码取设备本机号），用来确认地址和密钥对不对。'),
     E('div', { 'class': 'as-buttons' }, [saveBtn, testBtn, refreshBtn, clearBtn]),
     errBox, savedHint,
     E('hr', {}),
     E('p', { 'class': 'as-caption' }, '只保留本次开机最近 30 次投递结果，不记号码、地址或短信内容。'),
     history
    ])
   ]),
   E('footer', { 'class': 'as-footer' }, 'AirSIM for VIPME-M8 · 来电通知')
  ]);

  this.root = root;

  /* ---------- 取值 / 填表 ---------- */
  function q(id) { return root.querySelector('#as-call-' + id); }
  function fill(c) {
   ['enabled', 'notify_ringing', 'notify_answered', 'notify_ended', 'notify_missed'].forEach(function(k) {
    q(k).checked = c[k] === '1' || c[k] === 1 || (c[k] === '' && (k === 'notify_ringing' || k === 'notify_ended' || k === 'notify_missed'));
   });
   urlInput.value = c.url || '';
   methodSelect.value = c.method || 'POST';
   typeSelect.value = c.content_type || 'application/json';
   timeoutInput.value = c.timeout || 12;
   expectInput.value = c.expect || '';
   headersArea.value = c.headers || '';
   bodyArea.value = c.body || '';
   presetSelect.value = '';
   paintSource();
  }
  function payload() {
   var p = {};
   ['enabled', 'notify_ringing', 'notify_answered', 'notify_ended', 'notify_missed'].forEach(function(k) {
    p[k] = q(k).checked ? '1' : '0';
   });
   p.url = urlInput.value.trim();
   p.method = methodSelect.value.trim();
   p.content_type = typeSelect.value.trim();
   p.timeout = timeoutInput.value.trim();
   p.expect = expectInput.value.trim();
   p.headers = headersArea.value.replace(/\r/g, '');
   p.body = bodyArea.value;
   p.poll = '2';
   return p;
  }
  function validate(p, status) {
   if (!/^\d+$/.test(p.timeout) || +p.timeout < 1 || +p.timeout > 120) return '请求超时必须为 1–120 秒';
   if (p.expect && !/^[A-Za-z_][A-Za-z0-9_]{0,63}=(-?[0-9]{1,16}|[a-z]{1,16})$/.test(p.expect)) return '成功响应条件格式无效';
   if (p.body && p.content_type === 'application/json') {
    try { JSON.parse(p.body.replace(/\{[a-z_]*\}/g, 'x')); } catch (e) { return '正文模板必须是有效 JSON'; }
   }
   if (p.url && !/^https?:\/\/[^\s]+$/.test(p.url)) return '推送地址必须以 http:// 或 https:// 开头';
   if (p.enabled === '1' && !p.url && !((status && status.sms_url) || '').trim()) return '还没有推送地址：请在短信转发里填好渠道，或在本页单独填一个';
   if (p.enabled === '1' && p.notify_ringing === '0' && p.notify_answered === '0' && p.notify_ended === '0' && p.notify_missed === '0') return '至少勾选一种要推送的通话状态';
   return '';
  }

  function paintSource() {
   var own = !!urlInput.value.trim();
   channelBlock.hidden = !own;
   sourceSelect.value = own ? 'own' : 'sms';
   sourceNote.textContent = own
    ? '本页单独配置：地址、方法、正文格式、超时和请求头都以本页为准。'
    : '跟随短信转发：地址、方法、正文格式、超时和请求头都用短信那边的设置，这里只给通话事件单独准备正文模板。';
  }

  function paintStatus(s) {
   s = s || {};
   status = s;
   var on = s.enabled === '1' || s.enabled === 1;
   statePill.textContent = on ? '通知已开启' : '通知未开启';
   statePill.className = 'as-pill ' + (on ? 'as-pill-green' : 'as-pill-neutral');
   lastLine.textContent = '最近投递：' + result(s.last_result) + (s.last_code ? ' · HTTP ' + s.last_code : '');
   serviceLine.textContent = s.service_running ? '转发服务运行中，配置改动保存后立即生效。' : '转发服务未运行，请检查插件安装或服务状态。';
   channelLine.textContent = '当前渠道：' + (s.channel === 'own' ? '本页单独配置' : '跟随短信转发') + (s.sms_url ? ' · 短信渠道：' + s.sms_url : ' · 短信渠道尚未填写地址');
   history.replaceChildren();
   (s.history || []).slice(0, 30).forEach(function(item) {
    var row = E('div', { 'class': 'as-history-row' });
    [new Date(Number(item.time) * 1000).toLocaleString(), KINDS[item.kind] || item.kind || '事件', result(item.result) + (item.http_code ? ' · HTTP ' + item.http_code : '')].forEach(function(value) {
     row.appendChild(E('span', {}, value));
    });
    history.appendChild(row);
   });
   if (!history.childNodes.length) history.textContent = '暂无投递';
  }

  function markSaved() {
   var changed = JSON.stringify(payload()) !== self.savedPayload;
   savedHint.textContent = changed ? '有改动还没保存，保存后才生效。' : '已保存的配置和页面一致。';
  }

  /* ---------- 交互 ---------- */
  sourceSelect.addEventListener('change', function() { error(''); paintSource(); markSaved(); });
  presetSelect.addEventListener('change', async function() {
   var name = this.value, p = PRESETS[name];
   if (!p) return;
   var yes = await ui.dialog('填入推送预设？', '会替换当前地址、正文格式和正文模板，保存后才生效。', { okText: '确定' });
   if (!yes) return;
   urlInput.value = p[0];
   methodSelect.value = 'POST';
   typeSelect.value = typeof p[1] === 'string' ? 'application/x-www-form-urlencoded' : 'application/json';
   bodyArea.value = typeof p[1] === 'string' ? p[1] : JSON.stringify(p[1], null, 2);
   expectInput.value = EXPECT_BY_PRESET[name] || '';
   headersArea.value = '';
   paintSource();
   markSaved();
  });
  methodSelect.addEventListener('change', function() { bodyArea.disabled = methodSelect.value === 'GET'; });
  [urlInput, timeoutInput, expectInput, headersArea, bodyArea].forEach(function(el) {
   el.addEventListener('input', markSaved);
  });

  saveBtn.addEventListener('click', async function() {
   var p = payload(), problem = validate(p, status);
   if (problem) { error(problem); return; }
   var yes = await ui.dialog('保存来电通知设置？', '保存后立即生效，只处理保存之后的新通话。', { okText: '保存' });
   if (!yes) return;
   var toast = ui.toast('正在保存…', { type: 'busy', timeout: 0 });
   ui.busy(saveBtn, true); testBtn.disabled = true; error('');
   try {
    var r = await L.resolveDefault(rpcSave(JSON.stringify(p)), {});
    if (!r || !r.ok) throw Error((r && r.error) || '保存失败');
    var v = await L.resolveDefault(rpcGet(), {});
    if (v && v.config) { self.config = v.config; fill(v.config); paintStatus(v.status || {}); self.savedPayload = JSON.stringify(payload()); }
    toast.update('已保存', 'success');
    markSaved();
   } catch (e) { error(e.message || '保存失败'); toast.update('保存失败', 'error'); }
   finally { ui.busy(saveBtn, false); testBtn.disabled = false; setTimeout(function() { toast.close(); }, 3000); }
  });

  testBtn.addEventListener('click', async function() {
   if (JSON.stringify(payload()) !== self.savedPayload) { error('请先保存修改再测试'); return; }
   if (!status.enabled) { error('请先开启并保存来电通知'); return; }
   var yes = await ui.dialog('发送测试通知？', '会向当前已保存的渠道发一条固定测试消息。', { okText: '发送' });
   if (!yes) return;
   ui.busy(testBtn, true);
   try {
    var r = await L.resolveDefault(rpcTest(), {});
    var text = r && r.ok ? '测试成功' + (r.http_code ? ' · HTTP ' + r.http_code : '') : '测试失败：' + result(r && r.result);
    ui.toast(text, { type: r && r.ok ? 'success' : 'error' });
    setTimeout(function() { refreshStatus(); }, 1500);
   } finally { ui.busy(testBtn, false); }
  });

  refreshBtn.addEventListener('click', function() { refreshStatus(); });

  clearBtn.addEventListener('click', async function() {
   var yes = await ui.dialog('清空投递记录？', '只清最近投递记录，不影响转发配置。', { okText: '确定' });
   if (!yes) return;
   clearBtn.disabled = true;
   try {
    var r = await L.resolveDefault(rpcClear(), {});
    if (!r || !r.ok) throw Error((r && r.error) || '清空失败');
    await refreshStatus();
   } catch (e) { error(e.message || '清空失败'); }
   finally { clearBtn.disabled = false; }
  });

  var inflight = null;
  function refreshStatus() {
   if (inflight) return inflight;
   inflight = L.resolveDefault(rpcStatus(), {}).then(function(r) {
    if (root.isConnected && r && r.status) paintStatus(r.status);
   }).finally(function() { inflight = null; });
   return inflight;
  }
  this.refreshStatus = refreshStatus;

  if (this.data.config) {
   fill(this.config);
   paintStatus(status);
   this.savedPayload = JSON.stringify(payload());
   markSaved();
  } else {
   error(this.data.error || '读取失败，请刷新页面');
  }

  var tick = function() {
   if (document.hidden || !root.isConnected) return Promise.resolve();
   return refreshStatus();
  };
  poll.add(tick, 5);
  this._tick = tick;
  return root;
 },
 unload: function() { poll.remove(this._tick); },
 handleSaveApply: null, handleSave: null, handleReset: null
});
