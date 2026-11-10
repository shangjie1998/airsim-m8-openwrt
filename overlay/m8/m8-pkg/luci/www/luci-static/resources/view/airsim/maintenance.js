'use strict';
'require view';
'require poll';
'require rpc';
'require view.airsim.ui as ui';

var getStatus = rpc.declare({ object: 'airsim', method: 'status', expect: {}, reject: true });
var getDiagnostics = rpc.declare({ object: 'airsim', method: 'diagnostics', expect: {}, reject: true });
var getUpdates = rpc.declare({ object: 'airsim', method: 'updates', params: ['previews'], expect: {}, reject: true, nobatch: true });
var updateStatus = rpc.declare({ object: 'airsim', method: 'updates_status', expect: {}, reject: true });
var updatePolicy = rpc.declare({ object: 'airsim', method: 'updates_policy', params: ['config'], expect: {}, reject: true, nobatch: true });
var installUpdate = rpc.declare({ object: 'airsim', method: 'update_install', params: ['tag'], expect: {}, reject: true, nobatch: true });
var backupMake = rpc.declare({ object: 'airsim', method: 'backup_make', expect: {}, reject: true, nobatch: true });
var backupRestore = rpc.declare({ object: 'airsim', method: 'backup_restore', params: ['payload'], expect: {}, reject: true, nobatch: true });

var releasesURL = 'https://github.com/shangjie1998/airsim-m8-openwrt/releases';

function base64ToBlob(b64, type) {
 var bin = atob(b64.replace(/\s+/g, '')), arr = new Uint8Array(bin.length);
 for (var i = 0; i < bin.length; i++) arr[i] = bin.charCodeAt(i);
 return new Blob([arr], { type: type });
}

function readBase64(file) {
 return new Promise(function(resolve, reject) {
  var reader = new FileReader();
  reader.onload = function() { var s = String(reader.result || ''); var i = s.indexOf(','); resolve(i >= 0 ? s.slice(i + 1) : s); };
  reader.onerror = function() { reject(Error('读取文件失败')); };
  reader.readAsDataURL(file);
 });
}

function noticeBox() {
 return E('div', { 'class': 'as-feedback', hidden: true, 'role': 'status', 'aria-live': 'polite' }, [E('strong', {}, ''), E('p', {}, '')]);
}

function setNotice(box, title, text, kind) {
 if (!text) { box.hidden = true; return; }
 box.hidden = false;
 box.className = 'as-feedback as-feedback-' + (kind || 'info');
 box.firstChild.textContent = title || '';
 box.lastChild.textContent = text;
}

return view.extend({
 load: function() {
  return Promise.all([getStatus().catch(function() { return {}; }), updateStatus().catch(function() { return {}; })]);
 },
 render: function(data) {
  ui.install();
  var state = data[0] || {}, updateInfo = data[1] || {};
  var pending = false, busyFlag = false, restoreFile = null;

  /* ---------------- 备份与恢复 ---------------- */
  var backupNote = noticeBox();
  var fileInput = E('input', { 'type': 'file', 'accept': '.tar.gz,.tgz,application/gzip,application/x-gzip', hidden: true });
  var pickedName = E('p', { 'class': 'as-caption' }, '尚未选择文件。');
  var pickButton = E('button', { 'type': 'button', 'class': 'as-button as-secondary' }, '选择备份文件');
  var restoreConfirm = E('button', { 'type': 'button', 'class': 'as-button as-danger' }, '确认覆盖');
  var restoreCancel = E('button', { 'type': 'button', 'class': 'as-button as-secondary' }, '取消');
  var restorePanel = E('div', { 'class': 'as-feedback as-feedback-error', hidden: true }, [
   E('strong', {}, '确认用这份备份覆盖本机数据？'),
   E('p', {}, '会覆盖当前插件配置、配对凭据与运行状态，并重启电话服务。覆盖前会把现在的状态另存到 /tmp/airsim-before-restore-*。'),
   E('div', { 'class': 'as-buttons' }, [restoreConfirm, restoreCancel])
  ]);
  var restoreBtn = E('button', { 'type': 'button', 'class': 'as-button as-primary' }, '恢复这份备份');
  var backupBtn = E('button', { 'type': 'button', 'class': 'as-button as-primary' }, '备份到电脑');
  var dropZone = E('div', { 'class': 'as-backup-drop', 'tabindex': '0' }, [
   E('strong', {}, '把备份文件拖到这里'),
   E('span', {}, '或点「选择备份文件」。只接受本插件导出、且属于这台机器的 .tar.gz')
  ]);

  function backupMessage(text, kind) { setNotice(backupNote, kind === 'error' ? '这一步没成' : '备份与恢复', text, kind); }
  function syncBackupButtons() {
   backupBtn.disabled = busyFlag;
   restoreBtn.disabled = busyFlag || !restoreFile;
   restoreConfirm.disabled = busyFlag || !restoreFile;
   pickButton.disabled = busyFlag;
  }

  function takeFile(file) {
   if (busyFlag) return;
   if (!/\.(tar\.gz|tgz|gz)$/i.test(file.name || '')) { backupMessage('这不是 .tar.gz 备份包，请重新选择。', 'error'); return; }
   if (file.size > 8388608) { backupMessage('文件超过 8 MB，不像是本插件的备份。', 'error'); return; }
   restoreFile = file;
   pickedName.textContent = '已选择：' + file.name + '（' + Math.max(1, Math.round(file.size / 1024)) + ' KB）';
   restorePanel.hidden = true;
   backupMessage('文件已读入，点「恢复这份备份」后还会再确认一次。', 'info');
   syncBackupButtons();
  }

  dropZone.addEventListener('dragover', function(ev) { ev.preventDefault(); dropZone.className = 'as-backup-drop is-active'; });
  dropZone.addEventListener('dragleave', function() { dropZone.className = 'as-backup-drop'; });
  dropZone.addEventListener('drop', function(ev) {
   ev.preventDefault(); dropZone.className = 'as-backup-drop';
   var files = (ev.dataTransfer && ev.dataTransfer.files) || [];
   if (files.length) takeFile(files[0]);
  });
  dropZone.addEventListener('click', function() { if (!busyFlag) fileInput.click(); });
  dropZone.addEventListener('keydown', function(ev) { if (ev.key === 'Enter' || ev.key === ' ') { ev.preventDefault(); if (!busyFlag) fileInput.click(); } });
  pickButton.addEventListener('click', function() { if (!busyFlag) fileInput.click(); });
  fileInput.addEventListener('change', function() { if (fileInput.files && fileInput.files.length) takeFile(fileInput.files[0]); });

  backupBtn.addEventListener('click', async function() {
   if (busyFlag) return;
   busyFlag = true; syncBackupButtons(); backupMessage('正在打包配置与配对资料…', 'pending');
   try {
    var result = await backupMake();
    if (!result || result.ok === false || !result.data) throw Error((result && result.error) || '打包失败');
    var blob = base64ToBlob(result.data, 'application/gzip');
    var url = URL.createObjectURL(blob);
    var link = E('a', { href: url, download: result.filename || 'airsim-m8-backup.tar.gz' });
    document.body.appendChild(link); link.click(); link.remove();
    setTimeout(function() { URL.revokeObjectURL(url); }, 1500);
    backupMessage('已交给浏览器保存（' + (result.size ? Math.round(result.size / 1024) + ' KB' : '大小未知') + '，本机标识 ' + (result.short_id || '--') + '）。恢复时校验的就是这个标识。', 'success');
   } catch (e) { backupMessage(e.message || '备份失败，请重试。', 'error'); }
   finally { busyFlag = false; syncBackupButtons(); }
  });

  restoreBtn.addEventListener('click', function() {
   if (!restoreFile || busyFlag) return;
   restorePanel.hidden = false;
   backupMessage('已读取文件，请确认覆盖。', 'info');
  });
  restoreCancel.addEventListener('click', function() { restorePanel.hidden = true; });
  restoreConfirm.addEventListener('click', async function() {
   if (busyFlag || !restoreFile) return;
   restorePanel.hidden = true;
   busyFlag = true; syncBackupButtons(); backupMessage('正在导入备份并重启服务…', 'pending');
   try {
    var payload = await readBase64(restoreFile);
    var result = await backupRestore(payload);
    if (!result || result.ok === false) throw Error((result && result.error) || '恢复没确认，请刷新后核对状态');
    backupMessage(result.message || '备份已恢复，服务正在重启。', 'success');
    restoreFile = null; pickedName.textContent = '尚未选择文件。';
   } catch (e) { backupMessage(e.message || '恢复失败，请重试。', 'error'); }
   finally { busyFlag = false; syncBackupButtons(); }
  });
  syncBackupButtons();

  /* ---------------- 诊断 ---------------- */
  var diagNote = noticeBox();
  var diagBtn = E('button', { 'type': 'button', 'class': 'as-button as-secondary', click: async function() {
   if (pending) return;
   pending = true; ui.busy(diagBtn, true); setNotice(diagNote, '诊断', '正在生成脱敏状态报告…', 'pending');
   try {
    var result = await getDiagnostics();
    if (!result.ok || !result.report || result.error) throw Error(result.error || '诊断生成失败');
    var blob = new Blob([JSON.stringify(result.report, null, 2) + '\n'], { type: 'application/json;charset=utf-8' });
    var url = URL.createObjectURL(blob);
    var link = E('a', { href: url, download: 'airsim-mu300-diagnostics.json' });
    document.body.appendChild(link); link.click(); link.remove();
    setTimeout(function() { URL.revokeObjectURL(url); }, 1000);
    setNotice(diagNote, '诊断', '脱敏报告已交给浏览器下载，仅含状态白名单，不含原始日志。', 'success');
   } catch (e) { setNotice(diagNote, '诊断', e.message || '导出失败，请重试。', 'error'); }
   finally { pending = false; ui.busy(diagBtn, false); }
  } }, '导出脱敏诊断');

  /* ---------------- 插件更新 ---------------- */
  var updateNote = noticeBox();
  var jobNote = E('p', { 'class': 'as-caption' }, '');
  var updateResult = E('p', { 'class': 'as-copy' }, '正在读取自动检查结果…');
  var confirmedTag = '', latest = '', updateKnown = !!updateInfo.ok;
  var automatic = E('input', { 'type': 'checkbox', 'id': 'as-auto-update' });
  automatic.checked = updateInfo.policy ? !!updateInfo.policy.automatic : true;
  var previews = E('input', { 'type': 'checkbox', 'checked': true, 'id': 'as-previews' });
  if (updateInfo.policy) previews.checked = !!updateInfo.policy.previews;
  var updateLink = E('a', { 'href': releasesURL, 'target': '_blank', 'rel': 'noopener noreferrer', 'class': 'as-text-button' }, '打开项目 Release');
  var installBtn = E('button', { 'type': 'button', 'class': 'as-button as-primary' }, '下载并更新');
  var confirmBtn = E('button', { 'type': 'button', 'class': 'as-button as-primary' }, '确认更新');
  var cancelUpdate = E('button', { 'type': 'button', 'class': 'as-button as-secondary' }, '取消');
  var installPanel = E('div', { 'class': 'as-feedback as-feedback-info', hidden: true }, [
   E('p', {}, ''),
   E('div', { 'class': 'as-buttons' }, [confirmBtn, cancelUpdate])
  ]);

  function editable() { return state.service_alive === false && state.running === false; }
  function syncUpdateButtons() {
   installBtn.disabled = pending || !updateKnown || !!updateInfo.busy || !editable() || !latest;
   confirmBtn.disabled = installBtn.disabled || confirmedTag !== latest;
   automatic.disabled = previews.disabled = pending || !!updateInfo.busy;
  }

  function showRelease(result) {
   var valid = /^v\d+\.\d+\.\d+-r\d+(-m8)?$/.test(result.latest || '');
   latest = result.status === 'available' && valid ? result.latest : '';
   var messages = {
    available: '发现新版本', current: '当前已是此频道最新版本', ahead: '当前构建领先于公开 Release',
    unknown_version: '当前开发版本无法自动比较',
    no_release: 'M8 为整包发布，此处不做在线安装；请从「打开项目 Release」下载后手动安装'
   };
   updateResult.textContent = (messages[result.status] || '版本状态未知') + (valid ? ' · ' + result.latest : '') + (result.prerelease ? '（开发预览）' : '') + (result.checked_at ? ' · 检查于 ' + new Date(result.checked_at).toLocaleString() : '');
   updateLink.href = valid ? releasesURL + '/tag/' + result.latest : releasesURL;
   syncUpdateButtons();
  }

  function showUpdateStatus(info) {
   updateKnown = !!info.ok; updateInfo = info;
   if (!updateKnown) { latest = ''; jobNote.textContent = '更新状态暂不可用，等待重新连接；不会自动重试安装。'; syncUpdateButtons(); return; }
   var cache = info.cache || {};
   if (cache.previews === !!previews.checked && cache.result) showRelease(cache.result);
   else { latest = ''; updateResult.textContent = cache.error || '尚无当前频道的检查结果，可点击检查更新。'; }
   var job = info.job || {};
   jobNote.textContent = job.message || '';
   if (job.phase === 'succeeded' && job.target === latest) latest = '';
   syncUpdateButtons();
  }

  async function savePolicy() {
   if (pending) return;
   pending = true; latest = ''; syncUpdateButtons();
   try {
    var info = await updatePolicy(JSON.stringify({ automatic: !!automatic.checked, previews: !!previews.checked }));
    if (!info.ok || info.error) throw Error(info.error || '保存更新偏好失败');
    showUpdateStatus(info);
   } catch (e) { updateKnown = false; jobNote.textContent = e.message || '保存失败'; }
   finally { pending = false; syncUpdateButtons(); }
  }
  automatic.addEventListener('change', savePolicy);
  previews.addEventListener('change', savePolicy);

  var checkBtn = E('button', { 'type': 'button', 'class': 'as-button as-primary', click: async function() {
   if (pending) return;
   pending = true; latest = ''; syncUpdateButtons(); updateResult.textContent = '正在检查项目 GitHub Release…';
   updateLink.href = releasesURL;
   try {
    var result = await getUpdates(!!previews.checked);
    if (!result.ok || result.error) throw Error(result.error || '检查失败');
    showRelease(result);
   } catch (e) { updateResult.textContent = e.message || '检查失败，请稍后重试。'; }
   finally { pending = false; syncUpdateButtons(); }
  } }, '检查更新');

  installBtn.addEventListener('click', function() {
   if (installBtn.disabled) return;
   confirmedTag = latest;
   installPanel.hidden = false;
   installPanel.firstChild.textContent = '将更新到 ' + confirmedTag + '。保留设置与配对资料；更新期间请勿断电，完成后需手动启动电话服务。';
   syncUpdateButtons();
  });
  cancelUpdate.addEventListener('click', function() { installPanel.hidden = true; confirmedTag = ''; syncUpdateButtons(); });
  confirmBtn.addEventListener('click', async function() {
   if (confirmBtn.disabled || pending) return;
   pending = true; syncUpdateButtons(); installPanel.hidden = true;
   try {
    var result = await installUpdate(confirmedTag);
    if (!result.ok || result.error) throw Error(result.error || '升级没确认，请等状态刷新');
    updateInfo.busy = true; jobNote.textContent = result.job.message;
   } catch (e) { updateKnown = false; jobNote.textContent = e.message || '连接中断，请等状态刷新，别重复安装。'; }
   finally { pending = false; confirmedTag = ''; syncUpdateButtons(); }
  });

  showUpdateStatus(updateInfo);

  var backupCard = E('section', { 'class': 'as-card as-wide' }, [
   E('h3', {}, '备份与恢复'),
   E('p', { 'class': 'as-copy', style: 'margin-top:8px' }, '把插件配置、配对凭据和运行状态打成一个 .tar.gz 存到电脑；换固件、刷机、折腾完再导回来。'),
   E('p', { 'class': 'as-caption' }, '备份包按本机硬件指纹标记，恢复时先校验指纹，别人的备份会被直接拒绝。日志、音频、短信正文都不进包。'),
   E('div', { 'class': 'as-buttons' }, [backupBtn]),
   backupNote,
   E('hr', {}),
   dropZone, fileInput, pickButton, pickedName,
   E('div', { 'class': 'as-buttons' }, [restoreBtn]),
   restorePanel,
   E('p', { 'class': 'as-caption', style: 'margin-top:10px' }, '恢复会覆盖当前配置与配对资料并重启服务，覆盖前自动把现在的状态存到 /tmp/airsim-before-restore-*。')
  ]);

  var updateCard = E('section', { 'class': 'as-card as-wide' }, [
   E('h3', {}, '插件更新'),
   E('label', { 'class': 'as-check', 'for': 'as-auto-update' }, [automatic, E('span', {}, '自动检查更新（每 6 小时）')]),
   E('label', { 'class': 'as-check', 'for': 'as-previews' }, [previews, E('span', {}, '包含开发预览版')]),
   E('p', { 'class': 'as-caption' }, '自动检查只取公开版本信息，不自动安装。要先在概览页关掉电话服务再更新。'),
   E('div', { 'class': 'as-buttons' }, [checkBtn, installBtn]),
   updateResult, installPanel, jobNote, updateLink,
   E('p', { 'class': 'as-caption' }, 'M8 是整包发布：网页上只显示版本与链接，下载安装走「打开项目 Release」手动刷。')
  ]);

  var diagCard = E('section', { 'class': 'as-card' }, [
   E('h3', {}, '诊断导出'),
   E('p', { 'class': 'as-copy', style: 'margin-top:8px' }, '导出一份可分享的状态报告，出问题时直接发给维护的人。'),
   E('p', { 'class': 'as-caption' }, '不读取短信正文，不导出号码、IP、Relay 地址、标识、配对码、密钥、原始日志或音频；不会自动上传。'),
   E('div', { 'class': 'as-buttons' }, [diagBtn]),
   diagNote
  ]);

  var versionCard = E('section', { 'class': 'as-card' }, [
   E('h3', {}, '版本说明'),
   E('p', { 'class': 'as-copy', style: 'margin-top:8px' }, '当前安装：' + (state.version || '—')),
   E('ul', { 'class': 'as-instructions' }, [
    E('li', {}, 'r17：插件与匹配音频组件一体安装，缺失固件自动校验下载。'),
    E('li', {}, 'r16：修复麦克风突发帧丢弃与上行卡顿。'),
    E('li', {}, 'r15 已撤回；保留兼容性、依赖和签名校验改进。'),
    E('li', {}, 'r14：自动检查更新、签名校验升级、任务进度与失败提示。'),
    E('li', {}, '保留 r13 的连接模式、Relay、端口、脱敏诊断。'),
    E('li', {}, '含 VIPME-M8 振铃 mode 3 解析修复；通话与短信功能保留。'),
    E('li', {}, 'U30 Air 短时局域网双向声音已人工确认；长通话、多路及其他硬件尚未验收。')
   ])
  ]);

  poll.add(function() { if (pending || busyFlag) return Promise.resolve(); return Promise.all([getStatus().catch(function() { return {}; }), updateStatus().catch(function() { return {}; })]).then(function(r) { state = r[0] || state; showUpdateStatus(r[1] || {}); }); }, 5);

  return E('div', { 'class': 'as-root' }, [
   ui.stylesheet(),
   E('header', { 'class': 'as-header' }, [
    E('div', {}, [E('div', { 'class': 'as-eyebrow' }, 'AirSIM · 维护'), E('h2', {}, '备份、更新、诊断。'), E('p', {}, '折腾完想回退的时候，这里能救回来。')]),
    E('span', { 'class': 'as-version' }, 'v' + (state.version || '—'))
   ]),
   E('div', { 'class': 'as-layout' }, [backupCard, updateCard, diagCard, versionCard]),
   E('footer', { 'class': 'as-footer' }, 'AirSIM for VIPME-M8 · 维护')
  ]);
 },
 handleSaveApply: null, handleSave: null, handleReset: null
});
