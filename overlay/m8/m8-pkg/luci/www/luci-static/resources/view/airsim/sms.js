'use strict';
'require view';
'require rpc';
'require poll';
'require view.airsim.ui as ui';

// Use the firmware's two-SIM inbox and its per-card submit path, never the Agent's
// text-mode SMS API: the Agent relays AT through mmcli and only ever sees the
// primary card, so a message on the second SIM would simply not exist for it.
var listSMS = rpc.declare({ object: 'airsim-sms', method: 'sms_list', params: ['page', 'card'], expect: {}, reject: true, nobatch: true });
var showSMS = rpc.declare({ object: 'airsim-sms', method: 'sms_show', params: ['id'], expect: {}, reject: true, nobatch: true });
var sendSMS = rpc.declare({ object: 'airsim-sms', method: 'sms_send', params: ['num', 'text', 'card'], expect: {}, reject: true, nobatch: true });
var getJob = rpc.declare({ object: 'airsim-sms', method: 'sms_job', expect: {}, reject: true, nobatch: true });
var getSims = rpc.declare({ object: 'airsim-sms', method: 'sms_sims', expect: {}, reject: true, nobatch: true });
var markAllRead = rpc.declare({ object: 'airsim-sms', method: 'sms_read_all', expect: {}, reject: true, nobatch: true });
var clearBatch = rpc.declare({ object: 'airsim-sms', method: 'sms_clear_batch', params: ['limit'], expect: {}, reject: true, nobatch: true });
var getStatus = rpc.declare({ object: 'airsim', method: 'status', expect: {} });

return view.extend({
 load: function() { return getStatus().catch(function() { return {}; }); },
 render: function(data) {
  ui.install();
  var state = data || {};
  var page = 1, pages = 1, loading = false, reading = false, checking = false;
  var clearing = false, marking = false;
  var sending = false, running = false, jobKnown = false, watching = false, uncertain = false;
  var lastList = 0, signature = '', draft = null, selected = '';
  var filterCard = '', sendCard = '0', sendCardInit = false, simState = null;
  var list = E('div', { 'class': 'as-sms-list', 'aria-label': '短信列表' });
  var count = E('span', { 'class': 'as-pill as-pill-neutral' }, '读取中');
  var listHint = E('p', { 'class': 'as-caption', role: 'status' }, '正在读取设备短信…');
  var feedback = E('p', { 'class': 'as-feedback', role: 'status', 'aria-live': 'polite', hidden: true });
  var fullTitle = E('strong'), fullBody = E('p', { 'class': 'as-sms-body' });
  var full = E('div', { 'class': 'as-sms-full', hidden: true }, [fullTitle, fullBody]);
  function button(label, action, style) { return E('button', { type: 'button', 'class': 'as-button ' + (style || 'as-secondary'), click: action }, label); }
  function message(kind, value) { feedback.hidden = false; feedback.className = 'as-feedback as-feedback-' + kind; feedback.textContent = value; }
  function valid() {
   return /^\+?[0-9]{1,20}$/.test(number.value.trim()) && body.value.trim().length > 0 && body.value.length <= 70 && !/[\uD800-\uDFFF\x00-\x08\x0B\x0C\x0E-\x1F\x7F]/.test(body.value);
  }
  function update() {
   var locked = sending || running || uncertain || !!draft;
   number.disabled = locked; body.disabled = locked;
   sendCardSelect.disabled = locked;
   filterSelect.disabled = loading;
   send.disabled = locked || checking || !jobKnown || !valid() || !sendCardPresent();
   confirm.disabled = sending || running || checking || uncertain || !jobKnown || !draft;
   cancel.disabled = sending;
   ack.hidden = !uncertain; ack.disabled = sending || running || !jobKnown;
   counter.textContent = body.value.length + ' / 70 字';
   previous.disabled = loading || page <= 1; next.disabled = loading || page >= pages;
   refresh.disabled = loading;
   markReadAll.disabled = loading || marking || clearing;
   clearAll.disabled = loading || marking || clearing;
  }
  async function refreshList() {
   if (loading) return;
   loading = true; update();
   try {
    var result = await listSMS(String(page), filterCard);
    if (!result || result.error || !Array.isArray(result.msgs)) throw new Error('unavailable');
    pages = Math.max(1, Number(result.pages) || 1);
    if (page > pages) { page = pages; signature = ''; return; }
    count.textContent = (Number(result.unread) || 0) + ' 条未读';
    var scope = filterCard === '' ? '全部 SIM' : 'SIM' + (Number(filterCard) + 1);
    listHint.textContent = scope + ' · 共 ' + (Number(result.total) || 0) + ' 条 · 第 ' + page + ' / ' + pages + ' 页 · 自动刷新';
    var current = JSON.stringify(result.msgs);
    if (current !== signature) {
     list.replaceChildren();
     result.msgs.forEach(function(m) {
      if (!/^[0-9]{1,12}$/.test(String(m.id))) return;
      var read = button(m.status === 'unread' ? '查看全文 · 标记已读' : '查看全文', function() { return openMessage(String(m.id), String(m.peer || '未知号码')); }, 'as-text-button');
      var sim = String(m.sim || (m.card === '0' ? 'SIM1' : m.card === '1' ? 'SIM2' : '来源未知'));
      list.appendChild(E('article', { 'class': 'as-sms-item' }, [
       E('div', { 'class': 'as-sms-meta' }, [E('strong', {}, String(m.peer || '未知号码')),
        E('span', { 'class': 'as-sim-tag' }, sim), E('span', {}, m.dir === 'mo' ? '已发送' : m.status === 'unread' ? '未读' : '已读')]),
       E('p', { 'class': 'as-caption' }, String(m.time || '时间未知')),
       E('p', { 'class': 'as-sms-preview' }, String(m.preview || '（无预览）')), read
      ]));
     });
     if (!result.msgs.length) list.appendChild(E('p', { 'class': 'as-caption' }, filterCard === '' ? '暂无短信。新短信由设备原生服务自动接收。' : '这张卡下暂无短信。'));
     signature = current;
    }
   } catch (_) { listHint.textContent = '短信读取失败。请确认 AirSIM 短信服务 airsim-sms 已运行，并稍后刷新。'; }
   finally { loading = false; lastList = Date.now(); update(); }
  }
  async function openMessage(id, peer) {
   if (reading) return;
   reading = true; selected = id; full.hidden = false; fullTitle.textContent = peer; fullBody.textContent = '正在读取全文…';
   try {
    var result = await showSMS(id);
    if (!result || !result.ok || typeof result.text !== 'string') throw new Error('unavailable');
    var boundary = result.text.indexOf('\n---\n');
    if (boundary < 0) throw new Error('format');
    if (selected === id) fullBody.textContent = result.text.slice(boundary + 5);
    await refreshList();
   } catch (_) { if (selected === id) fullBody.textContent = '未能读取全文，请重试。'; }
   finally { reading = false; }
  }
  async function checkJob() {
   if (checking || sending) return;
   checking = true; update();
   try {
    var result = await getJob();
    if (!result || result.error && !result.done || !(result.none || result.running || result.done)) throw new Error('unavailable');
    jobKnown = true; running = !!result.running;
    if (running) {
     watching = true;
     message('pending', '设备正在发送短信（已等待 ' + (Number(result.elapsed) || 0) + ' 秒）。请勿重复发送。');
    } else if (watching) {
     watching = false;
     if (result.done && result.ok) message('success', '设备发送任务已提交运营商，不代表对方已收到。请核对已发送记录；草稿已保留。');
     else { uncertain = true; message('error', '未取得成功提交回执。请先核对已发送记录和对方收件情况，不要直接重发。'); }
     await refreshList();
    }
   } catch (_) {
    jobKnown = false;
    if (watching || running) uncertain = true;
    message('error', '无法确认设备发送任务状态，已暂停发送。网络恢复后自动查询，不会自动重发。');
   } finally { checking = false; update(); }
  }
  // 发送卡槽没插卡就不让发；sims 还没读到时不拦。
  function sendCardPresent() {
   if (sendCard !== '0' && sendCard !== '1') return false;
   if (!simState || !Array.isArray(simState.sims)) return true;
   var sim = simState.sims[Number(sendCard)];
   return !!(sim && sim.present);
  }
  function cardName(value) {
   if (value === '0') return 'SIM1';
   if (value === '1') return 'SIM2';
   return '所选卡';
  }
  var filterSelect = E('select', { id: 'as-sms-filter', 'class': 'as-call-select', change: function() {
   filterCard = filterSelect.value; page = 1; signature = ''; return refreshList();
  } }, [
   E('option', { value: '' }, '全部 SIM'), E('option', { value: '0' }, 'SIM1'), E('option', { value: '1' }, 'SIM2')
  ]);
  var sendCardSelect = E('select', { id: 'as-sms-card', 'class': 'as-call-select', change: function() {
   sendCard = sendCardSelect.value; sendCardInit = true; update();
  } }, [
   E('option', { value: '0' }, 'SIM1'), E('option', { value: '1' }, 'SIM2')
  ]);
  var simsInfo = E('p', { 'class': 'as-caption' }, '正在读取两张 SIM 卡…');
  async function loadSims() {
   try {
    var r = await getSims();
    if (!r || r.error || !Array.isArray(r.sims)) return;
    simState = r;
    var present = r.sims.filter(function(s) { return s && s.present; });
    simsInfo.textContent = present.length
     ? '在位卡：' + present.map(function(s) {
        return s.name + ' ' + (s.number || '号码未知') + (s.operator ? '（' + s.operator + '）' : '');
       }).join(' · ')
     : '未检测到在位 SIM 卡。';
    // 发送卡只列两张实卡：默认落在当前数据卡那张，那张空着就退到另一张。
    if (!sendCardInit) {
     var prefer = String(r.current) === '1' ? '1' : '0';
     var prefSim = r.sims[Number(prefer)];
     if (!prefSim || !prefSim.present) {
      var other = prefer === '0' ? '1' : '0';
      var otherSim = r.sims[Number(other)];
      if (otherSim && otherSim.present) prefer = other;
     }
     sendCard = prefer; sendCardSelect.value = prefer; sendCardInit = true;
    }
    [0, 1].forEach(function(i) {
     var s = r.sims[i];
     var sopt = sendCardSelect.querySelectorAll('option')[i];
     if (sopt) {
      sopt.disabled = !s || !s.present;
      sopt.textContent = s && s.present ? (s.name + (s.number ? ' ' + s.number : '')) : 'SIM' + (i + 1) + '（卡槽空）';
     }
     var fopt = filterSelect.querySelectorAll('option')[i + 1];
     if (fopt) fopt.disabled = !s || !s.present;
    });
   } catch (_) { simsInfo.textContent = '两张 SIM 卡信息读取失败，发送卡请选 SIM1 / SIM2。'; }
  }
  function prepare() {
   if (!sendCardPresent()) { message('error', '所选的 ' + cardName(sendCard) + ' 卡槽是空的，请换一张在位的卡再发送。'); return; }
   if (send.disabled || !valid()) return;
   draft = { number: number.value.trim(), text: body.value, card: sendCard };
   confirmationText.textContent = '确认发送至 ' + draft.number + '？将从 ' + cardName(draft.card) + ' 发出，可能产生短信费用。';
   confirmationBody.textContent = draft.text;
   confirmation.hidden = false; update(); confirm.focus();
  }
  async function submit() {
   if (confirm.disabled || !draft || sending) return;
   sending = true; update(); message('pending', '正在提交短信，请勿重复操作…');
   try {
    var result = await sendSMS(draft.number, draft.text, draft.card);
    if (result && result.ok && result.started) {
     running = true; watching = true;
     message('pending', '短信已排入设备发送任务，正在等待基带确认。');
    } else if (result && result.busy) {
     running = true; watching = true;
     message('info', '设备已有发送任务，本次短信未提交。请等待任务结束后重新确认。');
    } else {
     uncertain = true;
     message('error', '短信提交未获确认。草稿已保留，请先核对记录和收件情况，再决定是否重试。');
    }
   } catch (_) {
    uncertain = true; jobKnown = false; watching = true;
    message('error', '提交时连接中断，发送结果待确认。不会自动重发，请先核对已发送记录。');
   } finally { sending = false; draft = null; confirmation.hidden = true; update(); }
  }
  var number = E('input', { id: 'as-sms-number', type: 'tel', inputmode: 'tel', autocomplete: 'off', maxlength: 21, 'class': 'as-dial-input', placeholder: '收件号码', input: update });
  var body = E('textarea', { id: 'as-sms-text', rows: 3, maxlength: 70, 'class': 'as-sms-input', placeholder: '输入短信内容', 'aria-describedby': 'as-sms-help', input: update });
  var counter = E('span', { 'class': 'as-caption' });
  var send = button('发送短信', prepare, 'as-primary');
  var confirm = button('确认发送', submit, 'as-primary');
  var cancel = button('返回编辑', function() { if (sending) return; draft = null; confirmation.hidden = true; update(); });
  var ack = button('已核对结果，继续编辑', function() { if (ack.disabled) return; uncertain = false; feedback.hidden = true; update(); });
  var confirmationText = E('p'), confirmationBody = E('p', { 'class': 'as-sms-body' });
  var confirmation = E('div', { 'class': 'as-feedback as-feedback-info', hidden: true }, [confirmationText, confirmationBody, E('div', { 'class': 'as-buttons' }, [confirm, cancel])]);
  var previous = button('上一页', function() { if (previous.disabled) return; page--; signature = ''; return refreshList(); });
  var next = button('下一页', function() { if (next.disabled) return; page++; signature = ''; return refreshList(); });
  var refresh = button('刷新短信', refreshList, 'as-text-button');
  var markReadAll = button('全部已读', function() { return markEverything(); }, 'as-secondary');
  var clearAll = button('清空短信', function() { return clearEverything(); }, 'as-danger');
  var opsNote = E('p', { 'class': 'as-feedback', role: 'status', 'aria-live': 'polite', hidden: true });
  function opMessage(kind, text) {
   if (!text) { opsNote.hidden = true; return; }
   opsNote.hidden = false;
   opsNote.className = 'as-feedback as-feedback-' + kind;
   opsNote.textContent = text;
  }
  // 本地已读标记，不碰 SIM 卡：设备端只把这几条 id 记下来，列表里就不再显示未读。
  async function markEverything() {
   if (marking || clearing || loading) return;
   marking = true; update(); opMessage('pending', '正在把未读短信标成已读…');
   try {
    var result = await markAllRead();
    if (!result || result.error) throw new Error((result && result.error) || '标记失败');
    opMessage('success', '已标记 ' + (Number(result.marked) || 0) + ' 条为已读。');
    signature = ''; await refreshList();
   } catch (e) { opMessage('error', '标记已读失败：' + (e.message || '请稍后重试') + '。'); }
   finally { marking = false; update(); }
  }
  // 清空是逐条真删（固件 e5-sms 只有单条删除），所以分批做、边删边报进度，
  // 避免一次请求耗时太长被 20 秒的 rpc 超时掐断。
  async function clearEverything() {
   if (clearing || marking || loading) return;
   var go = await ui.dialog('清空全部短信？', '会把两张卡里收、发的短信从设备上删掉，删了之后找不回来。只清列表里的未读标记不受影响。', { danger: true, okText: '确认清空' });
   if (!go) return;
   clearing = true; update();
   var deleted = 0, round = 0;
   opMessage('pending', '正在清空短信…');
   try {
    while (round++ < 60) {
     var result = await clearBatch('4');
     if (!result || result.error) throw new Error((result && result.error) || '删除失败');
     deleted += Number(result.deleted) || 0;
     var remaining = Number(result.remaining) || 0;
     opMessage('pending', '已删 ' + deleted + ' 条，还剩 ' + remaining + ' 条…');
     if (!remaining) break;
    }
    opMessage('success', '已清空短信，共删除 ' + deleted + ' 条。');
    page = 1; signature = ''; await refreshList();
   } catch (e) {
    opMessage('error', '清空中断了：' + (e.message || '请稍后重试') + '。已删 ' + deleted + ' 条，可以再点一次接着清。');
    signature = ''; await refreshList();
   } finally { clearing = false; update(); }
  }
  var close = button('收起全文', function() { selected = ''; full.hidden = true; fullBody.textContent = ''; }, 'as-text-button');
  full.appendChild(close);
  var card = E('section', { 'class': 'as-card as-sms', 'aria-label': '短信' }, [
   E('div', { 'class': 'as-card-heading' }, [E('h3', {}, '短信'), count]),
   E('p', { 'class': 'as-copy' }, '查看与发送 VIPME-M8 两张 SIM 卡的短信。'),
   simsInfo,
   E('div', { 'class': 'as-sms-toolbar' }, [E('label', { 'for': 'as-sms-filter', 'class': 'as-field-label' }, '查看卡'), filterSelect, refresh]),
   E('div', { 'class': 'as-buttons as-sms-ops' }, [markReadAll, clearAll]),
   opsNote,
   listHint, list, E('div', { 'class': 'as-buttons as-sms-pages' }, [previous, next]), full,
   E('div', { 'class': 'as-sms-compose' }, [
    E('h4', {}, '新短信'),
    E('label', { 'for': 'as-sms-card', 'class': 'as-field-label' }, '发送卡'), sendCardSelect,
    E('label', { 'for': 'as-sms-number', 'class': 'as-field-label' }, '收件号码'), number,
    E('label', { 'for': 'as-sms-text', 'class': 'as-field-label' }, '短信内容'), body, counter,
    E('p', { id: 'as-sms-help', 'class': 'as-caption' }, '本页每条最多 70 字，暂不支持 emoji 和长短信拆分。短信只在本设备管理，不转发至 AirSIM 客户端。'),
    E('div', { 'class': 'as-buttons' }, [send]), confirmation, feedback, ack
   ])
  ]);
  var rootEl = E('div', { 'class': 'as-root' }, [
   ui.stylesheet(),
   E('header', { 'class': 'as-header' }, [
    E('div', {}, [E('div', { 'class': 'as-eyebrow' }, 'AirSIM · 短信'), E('h2', {}, '短信，两张卡一页管。'), E('p', {}, '查看、标记已读、清空与发送 VIPME-M8 两张 SIM 卡的短信。')]),
    E('span', { 'class': 'as-version' }, state && state.version ? 'v' + state.version : '')
   ]),
   E('div', { 'class': 'as-layout' }, [card]),
   E('footer', { 'class': 'as-footer' }, 'AirSIM for VIPME-M8 · 短信')
  ]);
  update(); loadSims(); refreshList(); checkJob();
  poll.add(function() {
   return Promise.all([checkJob(), Date.now() - lastList >= 10000 ? refreshList() : Promise.resolve()]);
  }, 2);
  return rootEl;
 }
});
