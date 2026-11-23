/* AirSIM 玻璃元件库 —— 页面视觉的唯一真源（M8 适配）
 *
 * 为什么样式放在 JS 里而不是 ui.css：
 * ui.css 的 URL 版本号跟着固件版本走，固件不换，浏览器就一直吃旧文件，
 * 改一次样式要用户手动硬刷才看得到。放成模块里的字符串，跟页面脚本一起发，
 * 改一次生效一次。
 *
 * 三条硬规则（别破）：
 * 1. 卡面底色按主题各一套，反光只叠在上面，不参与底色调；
 * 2. 反光亮度分主题限幅：浅色 ≤.12、深色 ≤.10，且只做顶部高光带 + 一个角落柔光斑；
 * 3. 描边和阴影按主题反向：浅色黑描边淡投影，深色白描边重投影。
 *
 * 不做的三件事（性能红线）：
 * - 不用 mix-blend-mode：每张卡会变成独立合成层，滚动时逐帧重合成；
 * - 不用 mask/mask-composite 画描边圈：iOS Safari 的已知慢点，改用 inset 阴影；
 * - 不用 SVG 位移滤镜：那是逐帧重算位移图，手机发热掉电，收益只有边缘一点折射感。
 */
'use strict';
'require baseclass';

var LIGHT = [
/* ---- token：浅色 ---- */
':root{',
'--g-radius:26px;--g-radius-s:18px;--g-radius-xs:12px;',
'--g-surface:rgba(255,255,255,.62);--g-surface-strong:rgba(255,255,255,.80);',
'--g-surface-solid:#ffffff;',
'--g-tint:rgba(255,255,255,.12);',
'--g-edge:rgba(0,0,0,.10);--g-edge-top:rgba(255,255,255,.90);',
'--g-shadow:0 12px 34px -14px rgba(0,0,0,.20),0 2px 6px rgba(0,0,0,.06);',
'--g-shadow-up:0 22px 56px -18px rgba(0,0,0,.30);',
'--g-blur:24px;--g-sat:180%;',
'--g-blue:#0071e3;--g-blue-2:#0a84ff;',
'--g-track:#c7c7cc;--g-track-edge:rgba(0,0,0,.12);--g-knob:#fff;',
'--g-warn:#b42326;--g-warn-bg:rgba(255,59,48,.10);',
'--ink:#1d1d1f;--ink-2:#6e6e73;--ink-3:#8e8e93;--ink-4:#aeaeb2;',
/* 字号只有一处真源：ui.css 的 --as-fs-*。这里只做别名，不再自己定义 px。
 * 行高用无单位倍数，字号长了行高自己跟着长，不会挤在一起。 */
'--fs-title:var(--as-fs-title,2.125rem);--lh-title:1.21;--fs-h3:var(--as-fs-h3,1.375rem);--lh-h3:1.27;',
'--fs-body:var(--as-fs-body,.9375rem);--lh-body:1.47;--fs-cap:var(--as-fs-cap,.8125rem);--lh-cap:1.38;--fs-note:var(--as-fs-note,.75rem);--lh-note:1.33;',
'}'
].join('');

var DARK = [
'html[data-theme="dark"]{',
'--g-surface:rgba(28,28,30,.62);--g-surface-strong:rgba(40,40,44,.80);',
'--g-surface-solid:#141416;',
'--g-tint:rgba(255,255,255,.08);',
'--g-edge:rgba(255,255,255,.14);--g-edge-top:rgba(255,255,255,.10);',
'--g-shadow:0 18px 44px -16px rgba(0,0,0,.70),0 1px 2px rgba(0,0,0,.30);',
'--g-shadow-up:0 26px 64px -18px rgba(0,0,0,.80);',
'--g-blue:#0a84ff;--g-blue-2:#409cff;',
'--g-track:#3a3a3c;--g-track-edge:rgba(255,255,255,.08);--g-knob:#ffffff;',
'--g-warn:#ff6961;--g-warn-bg:rgba(255,69,58,.14);',
'--ink:#f5f5f7;--ink-2:#98989d;--ink-3:#6e6e73;--ink-4:#48484a;',
'}'
].join('');

var BODY = [
/* ---- 卡片：玻璃材质 ---- */
'.as-root .as-card{position:relative;border:0;border-radius:var(--g-radius);',
'background:var(--g-surface);padding:var(--as-card-pad);min-width:0;',
'-webkit-backdrop-filter:blur(var(--g-blur)) saturate(var(--g-sat));',
'backdrop-filter:blur(var(--g-blur)) saturate(var(--g-sat));',
'box-shadow:inset 0 1px 0 var(--g-edge-top),inset 0 0 0 1px var(--g-edge),var(--g-shadow)}',
/* 镜面反光：普通渐变叠加，不用 blend、不用 mask */
'.as-root .as-card::before{content:"";position:absolute;inset:0;border-radius:inherit;pointer-events:none;',
'background:linear-gradient(180deg,var(--g-tint),transparent 42%),',
'radial-gradient(120% 80% at 8% -12%,rgba(255,255,255,.10),transparent 55%)}',

/* ---- 字体阶梯（苹果 Typography 11 档里用得到的 6 档） ---- */
'.as-root .as-header h2{font-size:var(--fs-title);line-height:var(--lh-title);font-weight:700;letter-spacing:-.015em}',
'.as-root .as-card h3,.as-root .as-card-heading{font-size:var(--fs-h3);line-height:var(--lh-h3);font-weight:600}',
'.as-root .as-eyebrow{font-size:var(--fs-note);line-height:var(--lh-note);letter-spacing:.08em;text-transform:uppercase;color:var(--ink-3)}',
'.as-root .as-eyebrow .as-sign{text-transform:none;letter-spacing:.04em}',
'.as-root .as-caption{font-size:var(--fs-cap);line-height:var(--lh-cap);color:var(--ink-2)}',
'.as-root .as-facts dt,.as-root .as-field-label{font-size:var(--fs-note);line-height:var(--lh-note);color:var(--ink-2)}',
'.as-root .as-facts dd,.as-root .as-row dd{font-size:var(--fs-body);line-height:var(--lh-body);color:var(--ink)}',
'.as-root .as-row dt{font-size:var(--fs-cap);line-height:var(--lh-cap);color:var(--ink-2)}',
'.as-root strong{font-weight:600}',

/* ---- 滑块开关：真 DOM 圆钮，不走 input[type=checkbox] ---- */
'.as-switch-row{display:flex;align-items:center;gap:12px;margin:16px 0 8px;min-height:44px;cursor:pointer;user-select:none}',
'.g-switch{position:relative;flex:0 0 auto;width:56px;height:32px;padding:0;border:0;border-radius:16px;',
'background:var(--g-track);cursor:pointer;',
'box-shadow:inset 0 0 0 1px var(--g-track-edge),inset 0 2px 6px rgba(0,0,0,.16);',
'transition:background .26s cubic-bezier(.32,.72,.28,1),box-shadow .26s cubic-bezier(.32,.72,.28,1)}',
'.g-switch[aria-checked="true"]{background:linear-gradient(180deg,var(--g-blue-2),var(--g-blue));',
'box-shadow:inset 0 0 0 1px rgba(255,255,255,.22),0 6px 18px -6px rgba(10,132,255,.55)}',
'.g-switch .g-knob{position:absolute;top:2px;left:26px;width:28px;height:28px;border-radius:14px;',
'background:var(--g-knob);box-shadow:0 2px 6px rgba(0,0,0,.32),0 0 0 .5px rgba(0,0,0,.10);',
'transition:left .26s cubic-bezier(.32,.72,.28,1),width .18s ease}',
'.g-switch[aria-checked="true"] .g-knob{left:2px}',
'.g-switch:active .g-knob{width:34px}',
'.g-switch:disabled{opacity:.5;cursor:default}',
'.g-switch:focus-visible{outline:2px solid var(--g-blue-2);outline-offset:3px}',
'.g-cap{font-size:var(--fs-note);color:var(--ink-3)}',

/* ---- 按钮 ---- */
'.as-root .as-btn{background:var(--g-surface-strong);color:var(--ink);border:0;border-radius:var(--g-radius-xs);',
'padding:10px 18px;min-height:44px;font-size:var(--fs-body);font-weight:500;cursor:pointer;',
'box-shadow:inset 0 1px 0 var(--g-edge-top),inset 0 0 0 1px var(--g-edge),0 6px 16px -10px rgba(0,0,0,.35);',
'transition:transform .16s cubic-bezier(.32,.72,.28,1),box-shadow .2s}',
'.as-root .as-btn:hover{box-shadow:inset 0 1px 0 var(--g-edge-top),inset 0 0 0 1px var(--g-edge),var(--g-shadow)}',
'.as-root .as-btn:active{transform:scale(.98)}',
'.as-root .as-btn:focus-visible{outline:2px solid var(--g-blue-2);outline-offset:2px}',

/* ---- 药丸 / 消息条 / 提示块 / 输入 ---- */
'.as-root .as-pill{font-size:var(--fs-note);font-weight:600;border-radius:999px;padding:5px 11px}',
'.as-root .as-input{padding:11px 14px;min-height:44px;font-size:var(--fs-body);border-radius:var(--g-radius-xs);',
'background:var(--g-surface-strong);color:var(--ink);border:0;',
'box-shadow:inset 0 0 0 1px var(--g-edge);transition:box-shadow .2s}',
'.as-root .as-input:focus{outline:none;box-shadow:inset 0 0 0 2px var(--g-blue-2)}',
'.as-root .as-feedback{border-radius:var(--g-radius-xs);font-size:var(--fs-cap);line-height:var(--lh-cap)}',
'.as-feedback:empty{display:none !important}',
'.as-root .as-notice{border-radius:var(--g-radius-s);font-size:var(--fs-cap);line-height:var(--lh-cap)}',
/* 这两类只有单边边框，配圆角会在两端画成弧线，看起来像"半个框"。单边边框一律不给圆角。 */
'.as-root .as-dev-item,.as-root .as-details{border-radius:0}',
'.as-root .as-dev-stack{display:block;justify-content:flex-start;align-items:stretch;gap:0;padding:12px 0;cursor:pointer}',
'.as-root .as-dev-stack .as-inline{margin-top:0}',
'.as-root .as-dev-stack .as-details{margin-top:12px}',
'.as-root .as-details .as-input+.as-input{margin-top:10px}',
'.as-root .as-details .as-actions{margin-top:12px}',
'.as-root .as-notice-warn{color:var(--g-warn);background:var(--g-warn-bg);}',

/* ---- 降级：不支持 backdrop-filter 的老设备退回不透明底，别变成灰糊 ---- */
'@supports not ((backdrop-filter:blur(1px)) or (-webkit-backdrop-filter:blur(1px))){',
'.as-root .as-card{background:var(--g-surface-solid);box-shadow:inset 0 0 0 1px var(--g-edge),var(--g-shadow)}}',

/* ---- 动效可关 ---- */
'@media (prefers-reduced-motion:reduce){',
'.as-root *,.as-root *::before,.as-root *::after{transition:none !important;animation:none !important}}'
].join('');

function css() {
	return LIGHT + DARK + BODY;
}

/* ---------------- 元件 ---------------- */

/* 滑块开关：真 DOM 圆钮。开 = 钮在左、轨道蓝；关 = 钮在右、轨道灰。
 * onToggle(next) 里拿到的就是目标状态；失败时调 switchSet 撤回。 */
function makeSwitch(on, onToggle, label) {
	var knob = E('span', { 'class': 'g-knob' });
	var btn = E('button', {
		'class': 'g-switch',
		'type': 'button',
		'role': 'switch',
		'aria-label': label || null,
		'click': function(ev) {
			ev.stopPropagation();
			if (btn.disabled) return;
			onToggle(btn.getAttribute('aria-checked') !== 'true');
		}
	}, [knob]);
	btn.setAttribute('aria-checked', on ? 'true' : 'false');
	btn._onToggle = onToggle;
	return btn;
}

function setSwitch(btn, on) {
	btn.setAttribute('aria-checked', on ? 'true' : 'false');
}

function toggleSwitch(btn) {
	if (!btn || btn.disabled) return;
	if (typeof btn._onToggle === 'function') btn._onToggle(btn.getAttribute('aria-checked') !== 'true');
}

/* LuCI 的模块必须交出一个构造器（baseclass.extend），直接返对象会报
 * "factory yields invalid constructor"。extend 出来的类同时能当单例用，
 * 所以页面里 glass.css() / glass.switch() 这么调没问题。 */
return baseclass.extend({
	css: css,
	'switch': makeSwitch,
	'switchSet': setSwitch,
	'switchToggle': toggleSwitch
});
