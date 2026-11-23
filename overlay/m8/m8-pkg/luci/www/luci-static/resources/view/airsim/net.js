'use strict';
'require view';
'require rpc';
'require view.airsim.ui as ui';
'require view.airsim.glass as glass';

/* 联网（上网通道切换 + Wi-Fi 管理）
 *
 * 这页只跟 airsim-net 打交道，真正的门在设备上的 /usr/bin/airsim-netmode：
 * 模式判断只看 uci 三个值（network.wan.disabled / wireless.ap0.disabled / wireless.wwan0.disabled），
 * 页面不另存状态，每次刷新都回读设备。
 *
 * 两个别改坏的约定：
 * 1. 切模式要 8~30 秒才稳（wpa_supplicant 重启 + 认证 + DHCP），点完不能立刻刷状态，得轮询。
 * 2. 密码只在「连接」那一下发出去，读回来永远没有密码 —— 后端不给。 */

var netScreen    = rpc.declare({ object: 'airsim-net', method: 'screen', expect: {} });
var netProbe     = rpc.declare({ object: 'airsim-net', method: 'probe', expect: {} });
var netStatus    = rpc.declare({ object: 'airsim-net', method: 'status', expect: {} });
var netModeSet   = rpc.declare({ object: 'airsim-net', method: 'mode_set', params: ['mode'], expect: {}, nobatch: true });
var netScan      = rpc.declare({ object: 'airsim-net', method: 'wifi_scan', expect: {} });
var netSaved     = rpc.declare({ object: 'airsim-net', method: 'wifi_saved', expect: {} });
var netConnect   = rpc.declare({ object: 'airsim-net', method: 'wifi_connect', params: ['ssid', 'key', 'encryption'], expect: {}, reject: true, nobatch: true });
var netForget    = rpc.declare({ object: 'airsim-net', method: 'wifi_forget', expect: {}, reject: true, nobatch: true });
var netAdminSt   = rpc.declare({ object: 'airsim-net', method: 'admin_status', expect: {} });
var netAdminSet  = rpc.declare({ object: 'airsim-net', method: 'admin_set', params: ['enable'], expect: {}, reject: true, nobatch: true });

/* 只有两种状态，一个开关就够：开 = 连外部 Wi-Fi，关 = 用流量并开热点。
 * 单射频机器上「连 Wi-Fi」和「开热点」本来就互斥，拆成三档反而让人猜哪档对。 */
var MODE_TEXT = {
 wifi: '连外部 Wi-Fi 上网，蜂窝数据断开、热点关闭。',
 ap:   '用流量上网，同时开热点，手机电脑可以连本机的 Wi-Fi。',
 cell: '用流量上网，热点关着。'
};

function pill(text, kind) {
 return E('span', { 'class': 'as-pill' + (kind ? ' as-pill-' + kind : '') }, text);
}

/* 后端已经把 iwinfo 那层 \xe5\x88\x98 还原成真 UTF-8 了，这里只做兜底：
 * 万一还有哪条漏网（比如某些固件的 iw 也会转义），显示前再过一遍，
 * 转成 %E5%88%98 交给 decodeURIComponent，解不开就原样返回，绝不吞字符。*/
function showSsid(s) {
 var str = s == null ? '' : String(s);
 if (!/\\x[0-9a-fA-F]{2}/.test(str)) return str;
 try {
  return decodeURIComponent(str.replace(/\\x([0-9a-fA-F]{2})/g, '%$1'));
 } catch (e) {
  return str;
 }
}

/* 信号分档照苹果那套来：-50 极好 / -60 很好 / -70 一般 / -80 差 / -90 以下别用。
 * 列表只留 >= -80 的，弱的收进一个折叠，点开才看——不静默吞掉，也不让一堆
 * -90 的噪音占满屏幕让人挑花眼。iOS 的 Wi-Fi 列表是强信号在前，这里照排。*/
var WEAK_RSSI = -80;
function rssiOf(ap) {
 var m = /(-?\d+(?:\.\d+)?)\s*dBm/.exec((ap && ap.signal) || '') || /(-?\d+(?:\.\d+)?)/.exec((ap && ap.signal) || '');
 return m ? parseFloat(m[1]) : 0;
}

/* 下面两组行结构首屏渲染和动作后的局部刷新共用，抽成纯函数，避免两处各写一份
 * 导致状态口径不一致。 */
function statusLinesFor(st) {
 var mode = (st && st.mode) || '';
 return [
  E('div', { 'class': 'as-row' }, [E('dt', {}, '当前状态'), E('dd', {}, mode === 'wifi' ? 'Wi-Fi 上网' : mode === 'wifi-pending' ? 'Wi-Fi 待配置（热点已关）' : mode === 'ap' ? '流量 + 热点' : mode === 'cell' ? '流量（热点关）' : '状态没读出来')]),
  E('div', { 'class': 'as-row' }, [E('dt', {}, '出口'), E('dd', {}, (st.route_dev || '—') + (st.sta_ip ? ' · ' + st.sta_ip : ''))]),
  E('div', { 'class': 'as-row' }, [E('dt', {}, '已连 Wi-Fi'), E('dd', {}, showSsid(st.sta_ssid) || '（未连接）')])
 ];
}

function dctRowsFor(dct) {
 var dctLive = dct && dct.live === true;
 return [
  E('div', { 'class': 'as-row' }, [E('dt', {}, '放行状态'), E('dd', {}, dctLive ? '配对、控制、语音端口与设备发现都已放行' : '规则未生效，手机可能搜不到或连不上')]),
  E('div', { 'class': 'as-row' }, [E('dt', {}, '直连端口'), E('dd', {}, (dct.ports || '8580 8581 8582') + '（TCP）')]),
  E('div', { 'class': 'as-row' }, [E('dt', {}, '设备发现'), E('dd', {}, '5353（UDP）')]),
  E('div', { 'class': 'as-row' }, [E('dt', {}, '当前地址'), E('dd', {}, dct.host || '—')]),
  E('div', { 'class': 'as-row' }, [E('dt', {}, '无线口'), E('dd', {}, dct.devices || '—')])
 ];
}

return view.extend({
 load: function() {
  /* 一次调用拿齐四张卡片的数据：原来分四次，每次都要过一遍 rpcd 再起一个 shell，
   * 这颗 SoC 上光进程开销就是 60 ms 一级。 */
  return L.resolveDefault(netScreen(), {});
 },

 render: function(d) {
  ui.install();
  var probe = (d && d.probe) || {};
  var st = (d && d.status) || {};
  var saved = (d && d.saved) || {};
  var mode = st.mode || '';

  /* 下面这些是动作成功后要局部刷新的节点引用；扫描列表 listBox 和手动输入框
   * manualSsid/manualKey 特意不进这张表 —— 刷新状态时原地保留，不重建。 */
  var root, factsBox, feedTitle, feedSub, feedPill, savedBox,
      admTitle, admSub, admPill, admSwitchLabel, dctBox, dctTitle, dctPill;

  /* 能力受限的提示：这不是错误，是机器本身的边界，写清楚免得用户以为功能坏了 */
  var limits = [];
  var dualBand = (probe.band_count || 1) >= 2;
  if (probe.single_radio) limits.push('这台机器只有一个射频，Wi-Fi 与热点不能同时开：连外部 Wi-Fi 时热点会断，手机断网几秒到十几秒。');
  if (dualBand) {
   limits.push('这台机器 2.4G 与 5G 双频都能连，附近两个频段的热点都会扫出来。');
  } else if (probe.band === '5g') {
   limits.push('当前无线只在 5G 工作，扫描只列 ' + (probe.channels || '36 40 44 48') + ' 信道上的热点；2.4G 的热点扫得到也连不上，所以不列。');
  }
  if (probe.noscan === true) limits.push('热点固定 36 信道、不做信道外扫描，这是出厂设置，不要改。');

  factsBox = E('dl', { 'class': 'as-facts' });
  statusLinesFor(st).forEach(function(n) { factsBox.appendChild(n); });

  /* ---- 卡 1：上网方式（一个滑块两态）----
   * 为什么不用 input[type=checkbox]：主题给复选框挂了伪元素渐变和勾，
   * 我们用 !important 也压不干净（真机上看到的是"蓝方块里一个勾"）。
   * 换成 button + 圆点，样式全在自己手里，主题插不进来。 */
  var feed = E('p', { 'class': 'as-feedback', 'role': 'status', 'aria-live': 'polite' }, '');
  var uplinkOn = mode === 'wifi' || mode === 'wifi-pending';
  var uplinkSwitch = glass.switch(uplinkOn, function(next) {
   uplinkSwitch.disabled = true;
   feed.setAttribute('class', 'as-feedback as-feedback-pending');
   feed.textContent = next
    ? '正在连外部 Wi-Fi…先连上再断流量，最慢半分钟，期间别关页面。'
    : '正在关 Wi-Fi、开流量和热点…最慢半分钟，期间别关页面。';
   netModeSet(next ? 'wifi' : 'ap').then(function(res) {
    if (res && res.ok) {
     feed.setAttribute('class', 'as-feedback as-feedback-success');
     /* 还没配置过外部 Wi-Fi 时，这一步只是把热点关掉、腾出射频，并没有真的切到 Wi-Fi。
      * 直接刷新页面，开关会跟着"待配置"状态弹回去，看着像没生效；这里就地更新，
      * 再把人直接领到下面的扫描卡。 */
     if (next && !(saved && saved.ssid)) {
      uplinkSwitch.disabled = false;
      glass.switchSet(uplinkSwitch, true);
      feed.textContent = '热点已关，射频腾出来了。下面点「扫描」，选一个 Wi-Fi 输密码，连上后会自动切到 Wi-Fi 上网。';
      var wifiCard = document.getElementById('as-wifi-card');
      if (wifiCard && wifiCard.scrollIntoView) wifiCard.scrollIntoView({ behavior: 'smooth', block: 'start' });
      return;
     }
     feed.textContent = (next ? '已切到 Wi-Fi 上网。' : '已切成流量 + 热点。') + (res.message ? ' ' + res.message : '');
     window.setTimeout(function() { refreshState(); }, 1500);
    } else {
     uplinkSwitch.disabled = false;
     glass.switchSet(uplinkSwitch, !next);
     feed.setAttribute('class', 'as-feedback as-feedback-error');
     feed.textContent = '没切成：' + ((res && (res.message || res.error)) || '设备没有返回原因');
    }
   }).catch(function(err) {
    uplinkSwitch.disabled = false;
    glass.switchSet(uplinkSwitch, !next);
    feed.setAttribute('class', 'as-feedback as-feedback-error');
    feed.textContent = '切换请求失败：' + (err && err.message ? err.message : err) + '（多半是半分钟内没连上 Wi-Fi，已退回原状态）';
   });
  }, 'Wi-Fi 上网');

  /* ---- 卡 2：Wi-Fi 列表 ---- */
  var listBox = E('div', { 'class': 'as-devs' }, [E('p', { 'class': 'as-caption' }, '还没扫描。')]);
  var scanBtn = E('button', { 'class': 'as-btn', 'type': 'button' }, '扫描');
  var manualSsid = E('input', { 'id': 'as-net-manual-ssid', 'class': 'as-input', 'type': 'text', 'placeholder': '隐藏网络：直接输 Wi-Fi 名称' });
  var manualKey = E('input', { 'id': 'as-net-manual-key', 'class': 'as-input', 'type': 'password', 'placeholder': '密码（开放网络留空）' });

  function connect(ssid, encryption, key) {
   feed.removeAttribute('class');
   feed.textContent = '正在连 ' + ssid + '…最多等 30 秒。';
   feed.className = 'as-feedback as-feedback-pending';
   netConnect(ssid, key || '', encryption || 'psk2').then(function(res) {
    if (res && res.ok) {
     feed.className = 'as-feedback as-feedback-success';
     feed.textContent = '已连上 ' + ssid + '。';
     window.setTimeout(function() { refreshState(); }, 1500);
    } else {
     feed.className = 'as-feedback as-feedback-error';
     feed.textContent = '连不上：' + ((res && res.message) || '密码错或信号太弱') + '（失败会自动退回原来的设置）';
    }
   }).catch(function(err) {
    feed.className = 'as-feedback as-feedback-error';
    feed.textContent = '连接请求失败：' + err;
   });
  }

  var apSeq = 0;
  function apRow(ap) {
   apSeq += 1;
   /* 这个热点就是已保存的那个、而且密码还存着 —— 就不用再让人输一遍。
    * 只认同一个 SSID；开放网络本来就不用密码，不算这条。 */
   var known = !!(saved && saved.ssid && ap.ssid === saved.ssid && saved.has_key
                  && ap.encryption && ap.encryption !== 'NONE');
   var keyInput = E('input', { 'id': 'as-net-key-' + apSeq, 'class': 'as-input', 'type': 'password', 'placeholder': '这个 Wi-Fi 的密码' });
   var panel = known
    ? E('div', { 'class': 'as-details', 'style': 'display:none' }, [
       E('p', { 'class': 'as-caption' }, '这个 Wi-Fi 之前连过，密码已保存，直接连就行。'),
       E('div', { 'class': 'as-actions' }, [
        E('button', { 'class': 'as-btn', 'type': 'button', 'click': function() {
         connect(ap.ssid, ap.encryption === 'NONE' ? 'none' : 'psk2', '');
        } }, '直接连接')
       ])
      ])
    : E('div', { 'class': 'as-details', 'style': 'display:none' }, [
       ui.field(showSsid(ap.ssid) + ' 的密码', keyInput),
       E('div', { 'class': 'as-actions' }, [
        E('button', { 'class': 'as-btn', 'type': 'button', 'click': function() {
         var k = (keyInput.value || '').trim();
         if (!k && ap.encryption !== 'NONE') { keyInput.focus(); return; }
         connect(ap.ssid, ap.encryption === 'NONE' ? 'none' : 'psk2', k);
        } }, '连接')
       ])
      ]);
   var open = function() {
    var visible = panel.style.display !== 'none';
    panel.style.display = visible ? 'none' : '';
    if (!visible && !known && ap.encryption !== 'NONE') keyInput.focus();
   };
   var locked = ap.encryption && ap.encryption !== 'NONE';
   return E('div', { 'class': 'as-dev-item as-dev-stack', 'click': function(ev) { if (ev.target === keyInput || ev.target.tagName === 'BUTTON') return; open(); } }, [
    E('div', { 'class': 'as-inline', style: 'justify-content:space-between' }, [
     E('div', {}, [
      E('strong', {}, showSsid(ap.ssid)),
      E('div', { 'class': 'as-caption' }, (ap.band || '') + ' · 信道 ' + ap.channel + ' · ' + ap.signal + (locked ? ' · 加密' : ' · 开放') + (known ? ' · 密码已保存' : ''))
     ]),
     known ? pill('已保存', 'green')
           : (ap.supported === false ? pill('不支持', 'red') : (locked ? pill('加密', 'neutral') : pill('开放', 'green')))
    ]),
    panel
   ]);
  }

  function paintScan(res) {
   listBox.innerHTML = '';
   if (!res || !res.ok) {
    listBox.appendChild(E('p', { 'class': 'as-caption' }, (res && res.error) || '扫描失败。'));
    return;
   }
   var all = res.aps || [];
   var strong = all.filter(function(ap) { return rssiOf(ap) >= WEAK_RSSI; })
    .sort(function(a, b) { return rssiOf(b) - rssiOf(a); });   /* 强的在前，跟 iOS 一样 */

   /* 低于 -80 dBm 的直接不出现：连上也是卡，列出来只会让人多犹豫一次。 */
   if (!strong.length) {
    listBox.appendChild(E('p', { 'class': 'as-caption' },
     all.length ? '附近热点的信号都低于 ' + WEAK_RSSI + ' dBm，连上也用不了，挪近点再扫。'
                : '这个频段上没扫到可连的热点。'));
   } else {
    strong.forEach(function(ap) { listBox.appendChild(apRow(ap)); });
   }

   if (res.filtered) {
    listBox.appendChild(E('p', { 'class': 'as-caption' }, '另有 ' + res.filtered + ' 个在 2.4G 或其它信道上，本机连不上，没列出。'));
   }
  }

  scanBtn.addEventListener('click', function() {
   scanBtn.disabled = true;
   listBox.innerHTML = '<p class="as-caption">扫描中…</p>';
   netScan().then(paintScan).catch(function(err) {
    listBox.innerHTML = '';
    listBox.appendChild(E('p', { 'class': 'as-caption' }, '扫描请求失败：' + err));
   }).then(function() { scanBtn.disabled = false; });
  });

  /* ---- 卡 3：已保存的 Wi-Fi ---- */
  function buildSavedChildren(sv) {
   if (sv && sv.ssid) {
    return [
     E('div', {}, [
      E('div', { 'class': 'as-inline', style: 'justify-content:space-between' }, [
       E('div', {}, [
        E('strong', {}, showSsid(sv.ssid)),
        E('div', { 'class': 'as-caption' }, (sv.encryption === 'psk2' ? 'WPA2' : sv.encryption === 'sae' ? 'WPA3' : '开放') + (sv.has_key ? ' · 密码已保存' : ''))
       ]),
       pill('当前', 'green')
      ]),
      E('div', { 'class': 'as-actions' }, [
       E('button', { 'class': 'as-btn', 'type': 'button', 'click': function() {
        /* 空密码交给后端：它会复用已经存下的那份 key（只认同一个 SSID）。 */
        connect(sv.ssid, sv.encryption || 'psk2', '');
       } }, '连接'),
       E('button', { 'class': 'as-btn', 'type': 'button', 'click': function() {
        if (!window.confirm('删除保存的「' + showSsid(sv.ssid) + '」？删完要重新输密码。')) return;
        netForget().then(function(res) {
         feed.className = 'as-feedback ' + (res && res.ok ? 'as-feedback-success' : 'as-feedback-error');
         feed.textContent = (res && (res.message || res.error)) || '';
         if (res && res.ok) window.setTimeout(function() { refreshState(); }, 1200);
        });
       } }, '删除')
      ]),
      E('p', { 'class': 'as-caption' }, '正在用这个 Wi-Fi 上网时不能删，先切回「手机流量」再删 —— 不然删完就没网了。')
     ])
    ];
   }
   return [ E('p', { 'class': 'as-caption' }, '还没有保存的 Wi-Fi。连上一次就会记住，下次开机自动连。') ];
  }
  savedBox = E('div', {});
  buildSavedChildren(saved).forEach(function(n) { savedBox.appendChild(n); });

  /* ---- 卡 4：管理页面访问（安全开关）----
   * 这一页权限等同 root，能不能从 Wi-Fi 那边打开它是个安全决定，所以单列一张卡，
   * 默认关、说清楚代价。规则由后端绑在 wlan0 上：蜂窝口永远不放。 */
  var adm = (d && d.admin) || {};
  var admOn = adm.enabled === true;
  var admLive = adm.live === true;
  var dct = (d && d.direct) || {};
  var dctLive = dct.live === true;
  var admFeed = E('p', { 'class': 'as-feedback', 'role': 'status', 'aria-live': 'polite' }, '');
  admFeed.style.display = 'none';   // 没有话说的时候不要留一个空框
  var admSwitch = glass.switch(admOn, function(nextOn) {
   var next = nextOn ? 1 : 0;
   admSwitch.disabled = true;
   admFeed.style.display = '';
   admFeed.className = 'as-feedback as-feedback-pending';
   admFeed.textContent = next ? '正在打开…' : '正在关掉…';
   netAdminSet(next ? '1' : '0').then(function(res) {
    if (res && res.ok) {
     admFeed.style.display = '';
     admFeed.className = 'as-feedback as-feedback-success';
     admFeed.textContent = (res.message || '') || (next ? '已允许。' : '已禁止。');
     window.setTimeout(function() { refreshState(); }, 1500);
    } else {
     admSwitch.disabled = false;
     glass.switchSet(admSwitch, !nextOn);
     admFeed.style.display = '';
    admFeed.className = 'as-feedback as-feedback-error';
     admFeed.textContent = '没设成：' + ((res && (res.error || res.message)) || '设备没有返回原因');
    }
   }).catch(function(err) {
    admSwitch.disabled = false;
    glass.switchSet(admSwitch, !nextOn);
    admFeed.style.display = '';
    admFeed.className = 'as-feedback as-feedback-error';
    admFeed.textContent = '请求失败：' + err;
   });
  }, '允许从 Wi-Fi 打开管理页');

  /* ---- 手机连接自检（只读）----
   * 配对 / 控制 / 语音端口与 mDNS 发现是否真的进了防火墙表，一眼能看到；
   * 数据来自后端 direct_status，不在这里改任何规则。 */
  dctBox = E('dl', { 'class': 'as-facts' });
  dctRowsFor(dct).forEach(function(n) { dctBox.appendChild(n); });

  /* ---- 局部刷新：动作成功后只回读并重绘可变节点，不整页 reload ----
   * 扫描列表 listBox、手动输入框 manualSsid/manualKey 不进 paintState，原地保留。
   * 单射频下连 Wi-Fi 要等认证 + DHCP，回读到的就是当下真实态；拉取失败保留现状。 */
  function paintState(nd) {
   var nst = (nd && nd.status) || {};
   var nsv = (nd && nd.saved) || {};
   var nadm = (nd && nd.admin) || {};
   var ndct = (nd && nd.direct) || {};
   var nmode = nst.mode || '';
   var nUplink = nmode === 'wifi' || nmode === 'wifi-pending';
   saved = nsv;

   factsBox.innerHTML = '';
   statusLinesFor(nst).forEach(function(n) { factsBox.appendChild(n); });
   feedTitle.textContent = nUplink ? '正在用 Wi-Fi 上网' : (nmode === 'ap' ? '正在用流量上网，热点开着' : '正在用流量上网');
   feedSub.textContent = nUplink ? '蜂窝数据已断，热点已关' : '热点开着，手机电脑可以连本机的 Wi-Fi';
   feedPill.textContent = nUplink ? 'Wi-Fi' : '流量';
   feedPill.className = 'as-pill ' + (nUplink ? 'as-pill-green' : 'as-pill-red');
   glass.switchSet(uplinkSwitch, nUplink);

   savedBox.innerHTML = '';
   buildSavedChildren(nsv).forEach(function(n) { savedBox.appendChild(n); });

   var nAdmOn = nadm.enabled === true;
   var nAdmLive = nadm.live === true;
   admTitle.textContent = nAdmOn ? 'Wi-Fi 那边可以打开管理页' : '只允许 USB 口打开管理页';
   admSub.textContent = '只放行 80/443 · 绑定 ' + (nadm.device || 'wlan0') + ' 无线口';
   admPill.textContent = nAdmOn ? (nAdmLive ? '已生效' : '没生效') : '已关';
   admPill.className = 'as-pill ' + (nAdmOn ? (nAdmLive ? 'as-pill-green' : 'as-pill-red') : 'as-pill-neutral');
   admSwitchLabel.textContent = nAdmOn ? '允许（从 Wi-Fi 打开）' : '禁止（只走 USB 口）';
   glass.switchSet(admSwitch, nAdmOn);

   var nDctLive = ndct.live === true;
   dctTitle.textContent = nDctLive ? '手机连本机的通路已放行' : '直连规则没生效';
   dctPill.textContent = nDctLive ? '已生效' : '未生效';
   dctPill.className = 'as-pill ' + (nDctLive ? 'as-pill-green' : 'as-pill-red');
   dctBox.innerHTML = '';
   dctRowsFor(ndct).forEach(function(n) { dctBox.appendChild(n); });

   /* 动作期间开关被置 busy，原来靠整页 reload 复位；局部刷新不重建 DOM，这里手动解开。 */
   uplinkSwitch.disabled = false;
   admSwitch.disabled = false;
  }

  var _refreshInflight = null;
  function refreshState() {
   if (_refreshInflight) return _refreshInflight;
   _refreshInflight = L.resolveDefault(netScreen(), {}).then(function(nd) {
    if (root && root.isConnected && nd) paintState(nd);
   }).catch(function() { /* 拉取失败保留现状，不刷错屏 */ }).finally(function() { _refreshInflight = null; });
   return _refreshInflight;
  }

  root = E('div', { 'class': 'as-root' }, [
   (ui.menu ? ui.menu('net') : ''),
   ui.stylesheet(),
   E('style', {}, glass.css()),
   E('header', { 'class': 'as-header' }, [
    E('div', {}, [
     E('div', { 'class': 'as-eyebrow' }, 'AirSIM · 联网'),
     E('h2', {}, '上网走哪条路。'),
     E('p', {}, '打开就上网走外部 Wi-Fi，关掉就上网走流量并开热点。USB 管理地址从头到尾不变。')
    ]),
    E('span', { 'class': 'as-version' }, dualBand ? 'Wi-Fi 双频' : (probe.band_label ? 'Wi-Fi ' + probe.band_label : ''))
   ]),
   E('div', { 'class': 'as-layout' }, [
    E('section', { 'class': 'as-card' }, [
     E('h3', {}, '上网方式'),
     E('div', { 'class': 'as-inline', style: 'justify-content:space-between' }, [
      E('div', {}, [
       feedTitle = E('strong', {}, uplinkOn ? '正在用 Wi-Fi 上网' : (mode === 'ap' ? '正在用流量上网，热点开着' : '正在用流量上网')),
       feedSub = E('div', { 'class': 'as-caption' }, uplinkOn ? '蜂窝数据已断，热点已关' : '热点开着，手机电脑可以连本机的 Wi-Fi')
      ]),
      feedPill = pill(uplinkOn ? 'Wi-Fi' : '流量', uplinkOn ? 'green' : 'red')
     ]),
     E('div', { 'class': 'as-switch-row', 'click': function(ev) {
      if (ev.target === uplinkSwitch || uplinkSwitch.contains(ev.target)) return;
      glass.switchToggle(uplinkSwitch);
     } }, [
      E('span', { 'class': 'g-cap' }, '开'),
      uplinkSwitch,
      E('span', { 'class': 'g-cap' }, '关'),
      E('span', { 'class': 'as-caption', style: 'margin-left:4px' }, 'Wi-Fi 上网')
     ]),
     feed,
     factsBox,
     E('p', { 'class': 'as-caption' }, '开：连外部 Wi-Fi 上网，流量和热点都关。关：用流量上网，并把热点开起来。顺序是固定的 —— 连 Wi-Fi 时先确认拿到地址再断流量，反过来会先丢网；失败会自动退回切换前的状态。')
    ]),

    E('section', { 'class': 'as-card', 'id': 'as-wifi-card' }, [
     E('h3', {}, '连接 Wi-Fi'),
     E('p', { 'class': 'as-caption' }, dualBand ? '附近 2.4G 与 5G 的热点都会扫出来。热点开着时不做扫描——先把上面的开关拨到「Wi-Fi 上网」，热点关掉再扫。' : ('只扫这台机器能连的信道：' + (probe.band === '5g' ? '5G ' : '') + (probe.channels || '36 40 44 48') + ' 这几个。2.4G 和其它信道扫得到也连不上，所以不列。热点开着时不做扫描——先把上面的开关拨到「Wi-Fi 上网」，热点关掉再扫。')),
     E('div', { 'class': 'as-actions' }, [scanBtn]),
     listBox,
     E('div', { 'class': 'as-details' }, [
      E('p', { 'class': 'as-caption' }, '隐藏网络 / 手动输入'),
      ui.field('Wi-Fi 名称', manualSsid),
      ui.field('密码（开放网络留空）', manualKey),
      E('div', { 'class': 'as-actions' }, [
       E('button', { 'class': 'as-btn', 'type': 'button', 'click': function() {
        var s = (manualSsid.value || '').trim();
        var k = (manualKey.value || '').trim();
        if (!s) { manualSsid.focus(); return; }
        if (!k) {
         /* 空密码不要瞎猜成「开放网络」—— 只有已保存的那个才允许空着走，
          * 别的网络直接让人补密码，别把 uci 里存着的 psk2 配置改成 none。 */
         if (!(saved && saved.ssid === s && saved.has_key)) {
          manualKey.focus();
          feed.className = 'as-feedback as-feedback-error';
          feed.textContent = '这个网络需要密码，请填写。';
          return;
         }
         connect(s, saved.encryption || 'psk2', '');
         return;
        }
        connect(s, 'psk2', k);
       } }, '连接这个网络')
      ])
     ])
    ]),

    E('section', { 'class': 'as-card' }, [
     E('h3', {}, '已保存的 Wi-Fi'),
     savedBox
    ]),

    E('section', { 'class': 'as-card' }, [
     E('h3', {}, '管理页面访问'),
     E('div', { 'class': 'as-inline', style: 'justify-content:space-between' }, [
      E('div', {}, [
       admTitle = E('strong', {}, admOn ? 'Wi-Fi 那边可以打开管理页' : '只允许 USB 口打开管理页'),
       admSub = E('div', { 'class': 'as-caption' }, '只放行 80/443 · 绑定 ' + (adm.device || 'wlan0') + ' 无线口')
      ]),
      admPill = pill(admOn ? (admLive ? '已生效' : '没生效') : '已关', admOn ? (admLive ? 'green' : 'red') : 'neutral')
     ]),
     E('div', { 'class': 'as-switch-row', 'click': function(ev) {
      if (ev.target === admSwitch || admSwitch.contains(ev.target)) return;
      glass.switchToggle(admSwitch);
     } }, [
      admSwitch,
      admSwitchLabel = E('span', {}, admOn ? '允许（从 Wi-Fi 打开）' : '禁止（只走 USB 口）')
     ]),
     admFeed,
     E('div', { 'class': 'as-notice as-notice-warn' }, '这一页权限等同 root：能改联网、能装包、能看短信。打开后，同一个上游 Wi-Fi 里的任何设备都能看到登录框并一直试密码；80 端口是明文，共享网络里有人抓包就看得见登录动作。只在自己家的 Wi-Fi 下打开，用完关掉。'),
     E('p', { 'class': 'as-caption' }, '规则只绑无线口，切到「手机流量」后蜂窝公网进不来 —— 就算忘了关，也不会把管理页暴露在运营商网络上。USB 口的管理入口不受这个开关影响。')
    ]),

    E('section', { 'class': 'as-card' }, [
     E('h3', {}, '手机连接自检'),
     E('div', { 'class': 'as-inline', style: 'justify-content:space-between' }, [
      E('div', {}, [
       dctTitle = E('strong', {}, dctLive ? '手机连本机的通路已放行' : '直连规则没生效'),
       E('div', { 'class': 'as-caption' }, '配对、控制、语音与设备发现走下面这些端口')
      ]),
      dctPill = pill(dctLive ? '已生效' : '未生效', dctLive ? 'green' : 'red')
     ]),
     dctBox,
     E('p', { 'class': 'as-caption' }, '这些规则开机自动建好，不用手动配防火墙。电话 / 配对 / 语音走 8580-8582，设备发现走 5353；若显示未生效，来回切一次上网方式，或重启 AirSIM 服务，会把规则补建起来。')
    ]),

    E('section', { 'class': 'as-card' }, [
     E('h3', {}, '这台机器的限制'),
     E('div', { 'class': 'as-notice as-notice-warn' }, limits.length ? limits.join(' ') : '没有额外限制。'),
     E('p', { 'class': 'as-caption' }, 'Wi-Fi 上网时 USB 电脑只能管理本机，不能通过本机上网；要供网就把开关关掉（流量 + 热点）。')
    ])
   ]),
   E('footer', { 'class': 'as-footer' }, 'AirSIM for VIPME-M8 · 联网')
  ]);
  return root;
 },

 handleSaveApply: null,
 handleSave: null,
 handleReset: null
});
