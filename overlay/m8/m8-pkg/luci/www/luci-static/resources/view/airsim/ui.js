'use strict';
'require baseclass';

/* 外观状态（主题 / 密度 / 透明度）全在这一处，存在 localStorage，六页共用。
 * 页面只要 require 这个模块并在 render 开头调 ui.install()，就会自动套用
 * 上次选好的外观；设置页通过 set* 改，改完通知所有监听者更新按钮态。 */

var STORE = 'airsim.ui.appearance';
var DEFAULTS = { mode: 'auto', density: 'comfort', transparency: 'glass' };
var MODES = ['auto', 'light', 'dark'];
var DENSITIES = ['comfort', 'compact'];
var TRANSPARENCIES = ['glass', 'solid'];

var listeners = [];
var mq = null;
var saved = null;

function parse() {
 if (saved) return saved;
 var raw = null;
 try { raw = JSON.parse(window.localStorage.getItem(STORE) || '{}'); } catch (e) { raw = {}; }
 var out = {
  mode: MODES.indexOf(raw.mode) >= 0 ? raw.mode : DEFAULTS.mode,
  density: DENSITIES.indexOf(raw.density) >= 0 ? raw.density : DEFAULTS.density,
  transparency: TRANSPARENCIES.indexOf(raw.transparency) >= 0 ? raw.transparency : DEFAULTS.transparency
 };
 saved = out;
 return out;
}

function persist(next) {
 saved = next;
 try { window.localStorage.setItem(STORE, JSON.stringify(next)); } catch (e) { /* 无痕模式下忽略 */ }
}

function isDark(state) {
 if (state.mode === 'dark') return true;
 if (state.mode === 'light') return false;
 try { return !!(mq && mq.matches); } catch (e) { return false; }
}

function paint() {
 var state = parse();
 var dark = isDark(state);
 var html = document.documentElement;
 html.setAttribute('data-theme', dark ? 'dark' : 'light');
 html.setAttribute('data-density', state.density);
 html.setAttribute('data-transparency', state.transparency);
 // 告诉浏览器我们有深色配色，滚动条、输入控件跟着变
 try { html.style.colorScheme = dark ? 'dark' : 'light'; } catch (e) { /* 老浏览器忽略 */ }
 listeners.forEach(function(fn) { try { fn(state); } catch (e) { /* 单个监听失败不影响其它 */ } });
 return state;
}

function subscribe(fn) {
 listeners.push(fn);
 return function() { var at = listeners.indexOf(fn); if (at >= 0) listeners.splice(at, 1); };
}

function setMode(mode) {
 if (MODES.indexOf(mode) < 0) return parse();
 var next = parse(); next.mode = mode; persist(next); return paint();
}
function setDensity(density) {
 if (DENSITIES.indexOf(density) < 0) return parse();
 var next = parse(); next.density = density; persist(next); return paint();
}
function setTransparency(transparency) {
 if (TRANSPARENCIES.indexOf(transparency) < 0) return parse();
 var next = parse(); next.transparency = transparency; persist(next); return paint();
}

function install() {
 if (!mq) {
  try {
   mq = window.matchMedia('(prefers-color-scheme: dark)');
   var onSystemChange = function() { if (parse().mode === 'auto') paint(); };
   if (mq.addEventListener) mq.addEventListener('change', onSystemChange);
   else if (mq.addListener) mq.addListener(onSystemChange);
  } catch (e) { mq = null; }
 }
 return paint();
}

/* 页面统一用这个挂样式表，路径只写一次。
 * ui.css 改了要跟着改样式版本号，否则浏览器会一直吃旧缓存（这坑踩过两次）。 */
var CSSVERSION = '20261123';
function stylesheet(name) {
 return E('link', { 'rel': 'stylesheet', 'href': L.resource('view/airsim/' + (name || 'ui.css')) + '?v=' + CSSVERSION });
}

/* ---------------- 顶部菜单（我们自己接管） ----------------
 * 宽屏一行平铺、放不下就折行；窄屏（<=520）整条换成下拉。
 * 主题那边同名的 .sidebar-submenu / .mobile-nav-submenu 在 ui.css 里按页面前缀关掉了。
 */
var PAGES = [
 { id: 'overview',    title: '设备概览' },
 { id: 'connect',     title: '连接' },
 { id: 'net',         title: '联网' },
 { id: 'call',        title: '来电通知' },
 { id: 'sms',         title: '短信' },
 { id: 'maintenance', title: '维护' },
 { id: 'settings',    title: '设置' }
];

/* 链接一律交给 L.url 生成：它知道 /cgi-bin/luci 这层前缀和实际脚本名。 */
function menu(current) {
 current = current || '';
 var base = L.url('admin/services/airsim');
 var items = [];

 PAGES.forEach(function(page) {
  var active = page.id === current;
  items.push(E('a', {
   'class': 'as-menu-item' + (active ? ' is-active' : ''),
   'href': base + '/' + page.id,
   'aria-current': active ? 'page' : null
  }, page.title));
 });

 try { document.body.classList.add('as-menu-on'); } catch (e) { /* 忽略 */ }

 var picker = E('select', { 'class': 'as-input as-menu-select', 'aria-label': '切换到其它页面' },
  PAGES.map(function(page) {
   return E('option', { 'value': base + '/' + page.id, 'selected': page.id === current ? '' : null }, page.title);
  })
 );
 picker.addEventListener('change', function() {
  if (picker.value) window.location.href = picker.value;
 });

 return E('nav', { 'class': 'as-menu', 'aria-label': 'AirSIM 页面' }, [
  E('div', { 'class': 'as-menu-strip' }, items),
  E('div', { 'class': 'as-menu-narrow' }, [ picker ])
 ]);
}

/* 字段封装：视觉标签和真实 <label for> 一次生成。
 * node 必须有 id，没有就按顺序补一个；有 help 时挂成 aria-describedby。 */
var fieldSeq = 0;
var dialogSeq = 0;
function field(labelText, node, help) {
 if (!node) return null;
 if (!node.id) { fieldSeq += 1; node.id = 'as-f-' + fieldSeq; }
 var label = E('label', { 'for': node.id, 'class': 'as-field-label' }, labelText);
 var kids = [label, node];
 if (help) {
  var helpId = node.id + '-help';
  node.setAttribute('aria-describedby', helpId);
  kids.push(E('p', { 'class': 'as-caption', 'id': helpId }, help));
 }
 return E('div', { 'class': 'as-setting' }, kids);
}

/* 确认框：替代 vendor 模块的 confirmBox，样式走 ui.css 的 .as-modal。
 * 返回 Promise<boolean>，取消 / 点遮罩 / Esc 都为 false。 */
function dialog(title, message, opts) {
 opts = opts || {};
 return new Promise(function(resolve) {
  var settled = false;
  var titleId = 'as-dlg-title-' + (dialogSeq += 1);
  var ok = E('button', { 'type': 'button', 'class': 'as-button' + (opts.danger ? ' as-danger' : '') }, opts.okText || '确定');
  var cancel = E('button', { 'type': 'button', 'class': 'as-button' }, opts.cancelText || '取消');
  var mask = E('div', { 'class': 'as-modal-mask' }, [
   E('div', { 'class': 'as-modal', 'role': 'dialog', 'aria-modal': 'true', 'aria-labelledby': titleId }, [
    E('h3', { 'id': titleId }, title),
    E('p', { 'class': 'as-copy as-modal-copy' }, message || ''),
    E('div', { 'class': 'as-modal-actions' }, opts.okOnly ? [ok] : [cancel, ok])
   ])
  ]);
  function close(value) {
   if (settled) return;
   settled = true;
   document.removeEventListener('keydown', onKey, true);
   if (mask.parentNode) mask.parentNode.removeChild(mask);
   resolve(value);
  }
  function onKey(ev) {
   if (ev.key === 'Escape') { ev.stopPropagation(); close(false); }
   else if (ev.key === 'Enter') { ev.stopPropagation(); close(true); }
  }
  ok.addEventListener('click', function() { close(true); });
  if (!opts.okOnly) cancel.addEventListener('click', function() { close(false); });
  mask.addEventListener('click', function(ev) { if (ev.target === mask) close(false); });
  document.addEventListener('keydown', onKey, true);
  document.body.appendChild(mask);
  setTimeout(function() { try { ok.focus(); } catch (e) { /* 忽略 */ } }, 30);
 });
}

/* 轻提示：返回句柄，可 update 文案或 close 掉 */
function toast(text, opts) {
 opts = opts || {};
 var timer = null;
 var el = E('div', { 'class': 'as-toast', 'role': 'status' }, text);
 function arm(ms) {
  if (timer) clearTimeout(timer);
  if (ms) timer = setTimeout(function() { close(); }, ms);
 }
 function close() {
  if (timer) { clearTimeout(timer); timer = null; }
  if (el.parentNode) el.parentNode.removeChild(el);
 }
 el.setAttribute('data-kind', opts.type || 'info');
 document.body.appendChild(el);
 arm(opts.timeout === undefined ? 2600 : opts.timeout);
 return {
  update: function(next, kind) { el.textContent = next; if (kind) el.setAttribute('data-kind', kind); arm(opts.timeout === undefined ? 2600 : opts.timeout); },
  close: close,
  el: el
 };
}

/* 按钮忙碌态：只切 disabled 与半透明，不改文案 */
function busy(button, flag) {
 if (!button) return;
 button.disabled = !!flag;
 if (flag) button.classList.add('is-busy');
 else button.classList.remove('is-busy');
}

/* 时间戳戳：秒级 epoch → MM-DD HH:MM */
function stampEpoch(epoch) {
 var n = Number(epoch || 0);
 if (!n) return '';
 var d = new Date(n * 1000), p2 = function(x) { return (x < 10 ? '0' : '') + x; };
 return p2(d.getMonth() + 1) + '-' + p2(d.getDate()) + ' ' + p2(d.getHours()) + ':' + p2(d.getMinutes());
}

return baseclass.extend({
 install: install,
 paint: paint,
 subscribe: subscribe,
 stylesheet: stylesheet,
 menu: menu,
 dialog: dialog,
 field: field,
 toast: toast,
 busy: busy,
 getMode: function() { return parse().mode; },
 getDensity: function() { return parse().density; },
 getTransparency: function() { return parse().transparency; },
 setMode: setMode,
 setDensity: setDensity,
 setTransparency: setTransparency,
 isDark: function() { return isDark(parse()); },
 stampEpoch: stampEpoch
});
