'use strict';
'require view';
'require rpc';
'require poll';

var getSettings = rpc.declare({object:'airsim', method:'settings_get', expect:{}, reject:true});
var getStatus = rpc.declare({object:'airsim', method:'status', expect:{}, reject:true});
var saveSettings = rpc.declare({object:'airsim', method:'settings_save', params:['config'], expect:{}, reject:true, nobatch:true});
var getDiagnostics = rpc.declare({object:'airsim', method:'diagnostics', expect:{}, reject:true});
var getUpdates = rpc.declare({object:'airsim', method:'updates', params:['previews'], expect:{}, reject:true, nobatch:true});
var updateStatus = rpc.declare({object:'airsim', method:'updates_status', expect:{}, reject:true});
var updatePolicy = rpc.declare({object:'airsim', method:'updates_policy', params:['config'], expect:{}, reject:true, nobatch:true});
var installUpdate = rpc.declare({object:'airsim', method:'update_install', params:['tag'], expect:{}, reject:true, nobatch:true});
var backupMake = rpc.declare({object:'airsim', method:'backup_make', expect:{}, reject:true, nobatch:true});
var backupRestore = rpc.declare({object:'airsim', method:'backup_restore', params:['payload'], expect:{}, reject:true, nobatch:true});
var releasesURL = 'https://github.com/shangjie1998/airsim-m8-openwrt/releases';
// The Agent never hardcodes a relay host: it uses whatever the iPhone sends when
// it pairs. This is the official cloud channel that ships with the client, shown
// here so it can be seen, replaced or cleared without hunting through configs.
var DEFAULT_RELAY = 'https://airsim-push.remotepilot.site';

function validateSettings(s) {
 if (s.mode !== 'local' && s.mode !== 'cloud') return '请选择连接模式。';
 if (!/^[a-zA-Z0-9_.-]{1,15}$/.test(s.interface)) return 'LAN 接口名称无效。';
 var used = [7580,7581,8575,8576];
 for (var port of [s.pair_port,s.control_port,s.pcm_port]) {
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
 load: function(){ return Promise.all([getSettings(),getStatus().catch(function(){return {};}),updateStatus().catch(function(){return {};})]); },
 render: function(data) {
  var config = data[0], state = data[1] || {}, pending = false, fields = {}, buttons = {};
  if (!config || !config.ok || !config.settings) return E('p', {'role':'alert'}, config && config.error || '无法读取管理设置，请检查插件安装；不会用默认值覆盖已有设置。');
  var settings = config.settings;
  var feedback = E('p', {'class':'as-management-feedback','role':'status','aria-live':'polite'});
  var relay = E('p', {'class':'as-copy'}), runtime = E('p', {'class':'as-caption'});
  var updateResult = E('p', {'role':'status','aria-live':'polite','class':'as-copy'}, '正在读取自动检查结果…');
  var updateInfo = data[2] || {}, updateKnown = !!updateInfo.ok, latest = '', confirmedTag = '';
  var jobResult = E('p', {'role':'status','aria-live':'polite','class':'as-copy'});
  var automatic = E('input', {type:'checkbox',id:'as-auto-update'});
  automatic.checked = updateInfo.policy ? !!updateInfo.policy.automatic : true;
  var updateLink = E('a', {href:releasesURL,target:'_blank',rel:'noopener noreferrer','class':'as-text-button'}, '打开项目 Release');
  var previews = E('input', {type:'checkbox',checked:true,id:'as-previews'}); previews.checked = true;
  if (updateInfo.policy) previews.checked = !!updateInfo.policy.previews;
  function message(text, error) { feedback.textContent = text; feedback.className = 'as-management-feedback' + (error ? ' is-error' : ''); }
  function editable(){ return state.service_alive === false && state.running === false; }
  function refresh(s) {
   state = s;
   relay.textContent = relayDescription(s);
   runtime.textContent = s.running ? '当前运行：配对 ' + s.pairing_port + ' / 控制 ' + s.control_port + ' / PCM ' + s.pcm_port : '服务已停止或状态不可用。保存的设置将在下次启动时使用。';
   buttons.save.disabled = pending || !!updateInfo.busy || !editable();
   buttons.diagnostics.disabled = pending;
   buttons.updates.disabled = pending;
   buttons.install.disabled = pending || !updateKnown || !!updateInfo.busy || !editable() || !latest;
   buttons.confirm.disabled = buttons.install.disabled || confirmedTag !== latest;
   automatic.disabled = previews.disabled = pending || !!updateInfo.busy;
   // Never replace editable fields during polling: an unfinished draft is local.
  }
  function input(key, label, type, help) {
   fields[key] = E('input', {id:'as-setting-'+key,type:type||'text','class':'as-dial-input',value:String(settings[key]),autocomplete:'off',maxlength:key==='relay_url'?512:32});
   return E('div', {'class':'as-setting'}, [E('label', {'for':'as-setting-'+key,'class':'as-field-label'}, label),fields[key],E('p', {'class':'as-caption'},help||'')]);
  }
  fields.mode = E('select',{id:'as-setting-mode','class':'as-call-select'},[
   E('option',{value:'local'},'仅局域网'),E('option',{value:'cloud'},'云端 + 局域网')
  ]); fields.mode.value = settings.mode;
  function values() {
   return {mode:fields.mode.value,relay_url:fields.relay_url.value.trim().replace(/\/+$/,''),interface:fields.interface.value.trim(),pair_port:Number(fields.pair_port.value),control_port:Number(fields.control_port.value),pcm_port:Number(fields.pcm_port.value)};
  }
  async function save() {
   if (pending) return;
   if (!editable()) { message('请先回到概览页关闭服务，确认停止后再保存。', true); return; }
   var next = values(), error = validateSettings(next);
   if (error) { message(error,true); return; }
   pending = true; refresh(state); message('正在保存设置…');
   try {
    var result = await saveSettings(JSON.stringify(next));
    if (!result.ok || result.error) throw Error(result.error || '未收到保存确认，请重新读取核对。');
    message(result.message || '设置已保存，请回到概览页启动服务。');
   } catch(e) { message(e.message || '保存结果未知，请重新打开此页核对，不自动重试。',true); }
   finally { pending = false; refresh(state); }
  }
  async function diagnostics() {
   if (pending) return;
   pending = true; refresh(state); message('正在生成脱敏状态报告…');
   try {
    var result = await getDiagnostics();
    if (!result.ok || !result.report || result.error) throw Error(result.error || '诊断生成失败');
    var blob = new Blob([JSON.stringify(result.report,null,2)+'\n'],{type:'application/json;charset=utf-8'});
    var url = URL.createObjectURL(blob);
    var link = E('a',{href:url,download:'airsim-mu300-diagnostics.json'});
    document.body.appendChild(link); link.click(); link.remove();
    setTimeout(function(){URL.revokeObjectURL(url);},1000);
    message('脱敏报告已交给浏览器下载；仅含状态白名单，不含原始日志。');
   } catch(e) { message(e.message || '导出失败，请重试。',true); }
   finally { pending = false; refresh(state); }
  }
  async function updates() {
   if (pending) return;
   pending = true; latest = ''; refresh(state); updateResult.textContent = '正在检查项目 GitHub Release…';
   updateLink.href = releasesURL;
   try {
    var result = await getUpdates(!!previews.checked);
    if (!result.ok || result.error) throw Error(result.error || '检查失败');
    showRelease(result);
   } catch(e) { updateResult.textContent = e.message || '检查失败，请稍后重试。'; }
   finally { pending = false; refresh(state); }
  }
  function showRelease(result) {
   var valid = /^v\d+\.\d+\.\d+-r\d+(-m8)?$/.test(result.latest || '');
   latest = result.status === 'available' && valid ? result.latest : '';
   var messages = {available:'发现新版本',current:'当前已是此频道最新版本',ahead:'当前构建领先于公开 Release',unknown_version:'当前开发版本无法自动比较',no_release:'M8 为整包发布，此处不做在线安装；请从「打开项目 Release」下载后手动安装'};
   updateResult.textContent = (messages[result.status] || '版本状态未知') + (valid ? ' · '+result.latest : '') + (result.prerelease ? '（开发预览）' : '') + (result.checked_at ? ' · 检查于 '+new Date(result.checked_at).toLocaleString() : '');
   updateLink.href = valid ? releasesURL+'/tag/'+result.latest : releasesURL;
  }
  function showUpdateStatus(info) {
   updateKnown = !!info.ok; updateInfo = info;
   if (!updateKnown) { latest = ''; jobResult.textContent = '更新状态暂不可用，等待重新连接；不会自动重试安装。'; return; }
   var cache = info.cache || {};
   if (cache.previews === !!previews.checked && cache.result) showRelease(cache.result);
   else { latest = ''; updateResult.textContent = cache.error || '尚无当前频道的检查结果，可点击检查更新。'; }
   var job = info.job || {};
   jobResult.textContent = job.message || '';
   if (job.phase === 'succeeded' && job.target === latest) latest = '';
  }
  async function savePolicy() {
   if (pending) return;
   pending = true; latest = ''; refresh(state);
   try {
    var info = await updatePolicy(JSON.stringify({automatic:!!automatic.checked,previews:!!previews.checked}));
    if (!info.ok || info.error) throw Error(info.error || '保存更新偏好失败');
    showUpdateStatus(info);
   } catch(e) { updateKnown = false; jobResult.textContent = e.message || '保存失败'; }
   finally { pending = false; refresh(state); }
  }
  automatic.addEventListener('change', savePolicy);
  previews.addEventListener('change', savePolicy);
  var confirmationText = E('p');
  var confirmation = E('div', {'class':'as-feedback as-feedback-info',hidden:true}, []);
  buttons.install = E('button',{type:'button','class':'as-button as-primary',click:function(){
   if (buttons.install.disabled) return;
   confirmedTag = latest;
   confirmationText.textContent = '将更新到 '+confirmedTag+'。保留设置与配对资料；更新期间请勿断电，完成后需手动启动电话服务。';
   confirmation.hidden = false; refresh(state);
  }},'下载并更新');
  buttons.confirm = E('button',{type:'button','class':'as-button as-primary',click:async function(){
   if (buttons.confirm.disabled || pending) return;
   pending = true; refresh(state); confirmation.hidden = true;
   try {
    var result = await installUpdate(confirmedTag);
    if (!result.ok || result.error) throw Error(result.error || '升级未确认，请等待状态刷新');
    updateInfo.busy = true; jobResult.textContent = result.job.message;
   } catch(e) { updateKnown = false; jobResult.textContent = e.message || '连接中断，请等待状态刷新，勿重复安装。'; }
   finally { pending = false; confirmedTag = ''; refresh(state); }
  }},'确认更新');
  var cancelUpdate = E('button',{type:'button','class':'as-button as-secondary',click:function(){confirmation.hidden=true;confirmedTag='';refresh(state);}},'取消');
  confirmation.appendChild(confirmationText);
  confirmation.appendChild(E('div',{'class':'as-buttons'},[buttons.confirm,cancelUpdate]));
  buttons.save = E('button',{type:'button','class':'as-button as-primary',click:save},'保存设置');
  buttons.diagnostics = E('button',{type:'button','class':'as-button as-secondary',click:diagnostics},'导出脱敏诊断');

  // --- 备份与恢复 ---------------------------------------------------------
  var backupBusy = false, restoreFile = null, localShortId = '';
  var backupNote = E('p',{'class':'as-management-feedback','role':'status','aria-live':'polite'});
  function backupMessage(text, error) {
   backupNote.textContent = text || '';
   backupNote.className = 'as-management-feedback' + (error ? ' is-error' : '');
  }
  var fileInput = E('input',{type:'file',accept:'.tar.gz,.tgz,application/gzip,application/x-gzip',hidden:true});
  var pickedName = E('p',{'class':'as-caption'},'尚未选择文件。');
  var btnRestoreConfirm = E('button',{type:'button','class':'as-button as-primary',click:async function(){
   if (backupBusy || !restoreFile) return;
   restorePanel.hidden = true;
   backupBusy = true; syncBackupButtons(); backupMessage('正在导入备份并重启服务…');
   try {
    var payload = await readBase64(restoreFile);
    var result = await backupRestore(payload);
    if (!result || result.ok === false) throw Error((result && result.error) || '恢复未确认，请刷新后核对状态');
    backupMessage(result.message || '备份已恢复，服务正在重启。');
    restoreFile = null; pickedName.textContent = '尚未选择文件。';
   } catch(e) { backupMessage(e.message || '恢复失败，请重试。', true); }
   finally { backupBusy = false; syncBackupButtons(); }
  }},'确认覆盖');
  var btnRestoreCancel = E('button',{type:'button','class':'as-button as-secondary',click:function(){ restorePanel.hidden = true; }},'取消');
  var restorePanel = E('div',{'class':'as-feedback as-feedback-error',hidden:true},[
   E('strong',{},'确认用这份备份覆盖本机数据？'),
   E('p',{},'会覆盖当前插件配置、配对凭据与运行状态，并重启电话服务。覆盖前会把现在的状态另存到 /tmp/airsim-before-restore-*。'),
   E('div',{'class':'as-buttons'},[btnRestoreConfirm,btnRestoreCancel])
  ]);
  var btnRestore = E('button',{type:'button','class':'as-button as-primary',click:function(){
   if (!restoreFile || backupBusy) return;
   restorePanel.hidden = false;
   backupMessage('已读取文件，请确认覆盖。');
  }},'恢复这份备份');
  var btnBackup = E('button',{type:'button','class':'as-button as-primary',click:async function(){
   if (backupBusy) return;
   backupBusy = true; syncBackupButtons(); backupMessage('正在打包配置与配对资料…');
   try {
    var result = await backupMake();
    if (!result || result.ok === false || !result.data) throw Error((result && result.error) || '打包失败');
    localShortId = result.short_id || '';
    var blob = base64ToBlob(result.data, 'application/gzip');
    var url = URL.createObjectURL(blob);
    var link = E('a',{href:url,download:result.filename || 'airsim-m8-backup.tar.gz'});
    document.body.appendChild(link); link.click(); link.remove();
    setTimeout(function(){ URL.revokeObjectURL(url); }, 1500);
    backupMessage('已交给浏览器保存（' + (result.size ? Math.round(result.size/1024) + ' KB' : '大小未知') + '，本机标识 ' + (result.short_id || '--') + '）。恢复时校验的就是这个标识。');
   } catch(e) { backupMessage(e.message || '备份失败，请重试。', true); }
   finally { backupBusy = false; syncBackupButtons(); }
   function base64ToBlob(b64, type) {
    var bin = atob(b64.replace(/\s+/g,'')), arr = new Uint8Array(bin.length);
    for (var i = 0; i < bin.length; i++) arr[i] = bin.charCodeAt(i);
    return new Blob([arr], {type:type});
   }
  }},'备份到电脑');
  function readBase64(file) {
   return new Promise(function(resolve, reject){
    var reader = new FileReader();
    reader.onload = function(){ var s = String(reader.result || ''); var i = s.indexOf(','); resolve(i >= 0 ? s.slice(i+1) : s); };
    reader.onerror = function(){ reject(Error('读取文件失败')); };
    reader.readAsDataURL(file);
   });
  }
  function syncBackupButtons() {
   btnBackup.disabled = backupBusy;
   btnRestore.disabled = backupBusy || !restoreFile;
   btnRestoreConfirm.disabled = backupBusy || !restoreFile;
   pickButton.disabled = backupBusy;
  }
  var pickButton = E('button',{type:'button','class':'as-button as-secondary',click:function(){ if (!backupBusy) fileInput.click(); }},'选择备份文件');
  var dropZone = E('div',{'class':'as-backup-drop',tabindex:'0'},[
   E('strong',{},'把备份文件拖到这里'),
   E('span',{},'或点「选择备份文件」。只接受本插件导出、且属于这台机器的 .tar.gz')
  ]);
  dropZone.addEventListener('dragover', function(ev){ ev.preventDefault(); dropZone.className = 'as-backup-drop is-active'; });
  dropZone.addEventListener('dragleave', function(){ dropZone.className = 'as-backup-drop'; });
  dropZone.addEventListener('drop', function(ev){
   ev.preventDefault(); dropZone.className = 'as-backup-drop';
   var files = (ev.dataTransfer && ev.dataTransfer.files) || [];
   if (files.length) takeFile(files[0]);
  });
  dropZone.addEventListener('click', function(){ if (!backupBusy) fileInput.click(); });
  dropZone.addEventListener('keydown', function(ev){ if (ev.key === 'Enter' || ev.key === ' ') { ev.preventDefault(); if (!backupBusy) fileInput.click(); } });
  fileInput.addEventListener('change', function(){ if (fileInput.files && fileInput.files.length) takeFile(fileInput.files[0]); });
  function takeFile(file) {
   if (backupBusy) return;
   if (!/\.(tar\.gz|tgz|gz)$/i.test(file.name || '')) { backupMessage('这不是 .tar.gz 备份包，请重新选择。', true); return; }
   if (file.size > 8388608) { backupMessage('文件超过 8 MB，不像是本插件的备份。', true); return; }
   restoreFile = file;
   pickedName.textContent = '已选择：' + file.name + '（' + Math.max(1, Math.round(file.size/1024)) + ' KB）';
   restorePanel.hidden = true;
   backupMessage('文件已读入，点「恢复这份备份」后还会再确认一次。');
   syncBackupButtons();
  }
  syncBackupButtons();
  buttons.updates = E('button',{type:'button','class':'as-button as-primary',click:updates},'检查更新');
  var root = E('div',{'class':'as-root'},[
   E('link',{rel:'stylesheet',href:L.resource('view/airsim/overview.css')}),
   E('header',{'class':'as-header'},[E('div',{},[E('div',{'class':'as-eyebrow'},'AirSIM · 管理与维护'),E('h2',{},'按你的方式，保持连接。'),E('p',{},'仅管理 VIPME-M8 插件，不改变 AirSIM 客户端。')]),E('span',{'class':'as-version'},'v'+config.version)]),
   E('a',{href:L.url('admin/services/airsim/overview'),'class':'as-text-button'},'返回设备概览'),
   E('div',{'class':'as-management-layout'},[
    E('section',{'class':'as-card'},[
     E('h3',{},'管理设置'),
     E('label',{'for':'as-setting-mode','class':'as-field-label'},'连接模式'),fields.mode,
     E('p',{'class':'as-caption'},'局域网模式禁用 Relay 注册、心跳和云端命令；云端模式保留局域网通路，但仍须有效的客户端云端配对。'),
     input('relay_url','Relay HTTPS 地址','url','官方云通道默认 ' + DEFAULT_RELAY + '。留空即跟随客户端配对时下发的地址；改成自己的地址后，客户端必须用同一地址重新配对。'),
     E('div',{'class':'as-buttons'},[
      E('button',{'type':'button','class':'as-button as-secondary',click:function(){ if (fields.relay_url) fields.relay_url.value = DEFAULT_RELAY; }},'填入官方默认'),
      E('button',{'type':'button','class':'as-button as-secondary',click:function(){ if (fields.relay_url) fields.relay_url.value = ''; }},'改为跟随客户端')
     ]),
     input('interface','可信 LAN 接口','text','通常为 br-lan。仅绑定该接口的私有 IPv4；不修改路由、防火墙或设备网络。'),
     E('div',{'class':'as-port-fields'},[input('pair_port','配对端口','number'),input('control_port','控制端口','number'),input('pcm_port','PCM 端口','number')]),
     E('p',{'class':'as-caption'},'先在概览页关闭服务，再保存并手动启动。端口变化会通过 Bonjour 公布；客户端需重新发现，必要时重新配对。内部端口保持仅回环访问。'),
     buttons.save,feedback
    ]),
    E('div',{'class':'as-management-stack'},[
     E('section',{'class':'as-card'},[E('h3',{},'Relay 与运行状态'),E('p',{'class':'as-copy'},'页面设置：'+(settings.relay_url || '未指定 · 跟随客户端配对地址')+'　官方默认：'+DEFAULT_RELAY),relay,runtime,E('p',{'class':'as-caption'},'“已配置”不等于公网可达；命令通道连接也不代表双向语音已验收。')]),
     E('section',{'class':'as-card'},[E('h3',{},'维护工具'),E('p',{'class':'as-copy'},'导出可分享的状态报告。'),E('p',{'class':'as-caption'},'不读取短信正文，不导出号码、IP、Relay 地址、标识、配对码、密钥、原始日志或音频；不会自动上传。'),buttons.diagnostics,
      E('hr',{}),E('h3',{},'插件更新'),E('label',{'class':'as-check-label','for':'as-auto-update'},[automatic,' 自动检查更新（每 6 小时）']),E('label',{'class':'as-check-label','for':'as-previews'},[previews,' 包含开发预览版']),E('p',{'class':'as-caption'},'自动检查只获取公开版本信息，不自动安装。先在概览关闭电话服务，再更新；安装前验证签名、设备与通话/短信空闲状态。'),buttons.updates,updateResult,buttons.install,confirmation,jobResult,updateLink]),
     E('section',{'class':'as-card'},[E('h3',{},'备份与恢复'),
      E('p',{'class':'as-copy'},'把插件配置、配对凭据和运行状态打成一个 .tar.gz 存到电脑；换固件、刷机、折腾完再导回来。'),
      E('p',{'class':'as-caption'},'备份包按本机硬件指纹标记，恢复时先校验指纹，别人的备份会被直接拒绝，不会把别的设备的配对凭据装进来。日志、音频、短信正文都不进包。'),
      btnBackup,
      backupNote,
      E('hr',{}),
      dropZone,fileInput,pickButton,pickedName,btnRestore,restorePanel,
      E('p',{'class':'as-caption','style':'margin-top:10px'},'恢复会覆盖当前配置与配对资料并重启服务，覆盖前自动把现在的状态存到 /tmp/airsim-before-restore-*。')
     ]),
     E('section',{'class':'as-card'},[E('h3',{},'版本说明'),E('p',{'class':'as-copy'},'当前安装：'+config.version),E('ul',{'class':'as-instructions'},[
      E('li',{},'r17：插件与匹配音频组件一体安装，缺失固件自动校验下载，必要时重启后继续准备。'),E('li',{},'r16：修复麦克风突发帧丢弃与上行卡顿。'),E('li',{},'r15 已撤回；保留插件兼容性、依赖和签名校验改进。'),E('li',{},'r14：自动检查更新、签名校验升级、任务进度与失败提示。'),E('li',{},'保留 r13 的连接模式、Relay、端口、脱敏诊断和版本说明。'),E('li',{},'包含 VIPME-M8 振铃 mode 3 解析修复；已有通话和短信功能保留。'),E('li',{},'U30 Air 短时局域网双向声音已人工确认；长通话、多路及其他硬件尚未验收。')])])
    ])
   ])
  ]);
  showUpdateStatus(updateInfo); refresh(state);
  poll.add(function(){ if (pending) return Promise.resolve(); return Promise.all([getStatus().catch(function(){return {};}),updateStatus().catch(function(){return {};})]).then(function(r){showUpdateStatus(r[1]);refresh(r[0]);}); },5);
  return root;
 },
 handleSaveApply:null,handleSave:null,handleReset:null
});
