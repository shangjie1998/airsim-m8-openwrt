# U30 Air Android 形态（UFI-TOOLS 插件载体）设计附录

状态：附录草案，从属于主设计，未在真机验证  
日期：2026-10-26  
主设计：[U30AIR_AIRSIM_DESIGN.md](U30AIR_AIRSIM_DESIGN.md)（设计基线，2026-10-22）

## 0. 从属声明

本文**不是**并行方案。主设计定义协议、组件、状态机、音频管线和验收；本文只回答一件事：

> 主设计 8.2 的「Android AirSIM APK + Root 配套模块」，在已经装好 UFI-TOOLS 的机器上，用插件替代 APK 的管理页职责，需要哪些设计约束？

凡是本文没写的，一律以主设计为准。冲突时主设计胜出。

## 1. 与主设计的差异

| 维度 | 主设计（8.2） | 本文（UFI-TOOLS 载体） |
| --- | --- | --- |
| 管理页 | `AirSIM-U30.apk` 内置 LAN Web 页 | 注入 UFI-TOOLS 页面的插件卡片 + Agent 自带 `/ui` |
| 安装入口 | APK 内的「Root 模块安装与健康检查客户端」 | 插件按钮 → UFI 的 `/api/root_shell` |
| Root 载荷 | `airsim-u30-root-module` | 同一模块，打包为 Magisk / KernelSU 形态 |
| 进程自启 | Root 模块的 init 启动项 | 模块 `service.sh`（不依赖 UFI 保活脚本） |
| 其余全部 | — | **不变**：Agent 路由、PCM 协议、`DeviceBackend`、配对、Relay、更新原则 |

替换的只有「谁来显示和点按钮」。**APK 仍然是对没有 UFI-TOOLS 的机器的默认形态**，插件是有 UFI-TOOLS 时的等价 UI。

## 2. 沿用的主设计条款

| 主设计章节 | 本文如何沿用 |
| --- | --- |
| 2.1 设备 API | Agent 路由与 `call_id` 校验原样保留，插件不新增业务路由 |
| 2.2 媒体协议 | `AIRSIMPCM1` / 8 kHz mono / 20 ms / 320 B 不变；插件完全不接触 PCM |
| 2.3 身份与所有权 | `call_uuid` / `generation` / owner claim 不变 |
| 4.2 AGDSP 与 PCM | 三端点、重采样方向、持续写静音、禁止重复 prepare 全部不变 |
| 4.3 本机 PCM 服务 | `127.0.0.1:7580`，仅 loopback，不变 |
| 5 Agent 改造 | `DeviceBackend` 接口不变；新增 profile 取值见第 3 节 |
| 6 配对 | Bonjour + 120 s 窗口 + 六位码参与派生，不变；配对码从 LuCI 改为插件卡片显示 |
| 9 更新与回滚 | Ed25519 manifest、host 白名单、平台指纹、暂存 rename、健康检查、失败回滚，全部沿用 |
| 12 验收 | 逐条沿用；本文第 8 节只加 UFI 特有的四条 |

## 3. 命名与取值（补齐主设计 5 节的 Android 分支）

| 变量 | OpenWrt（主设计） | Android（本文） |
| --- | --- | --- |
| `AIRSIM_RUNTIME_PROFILE` | `u30air-openwrt` | `u30air-android` |
| `AIRSIM_VOICE_BACKEND` | `u30air` | `u30air`（不变） |
| `AIRSIM_U30_PCM_ADDRESS` | `127.0.0.1:7580` | 同上 |
| `AIRSIM_DATA_DIR` | `/var/lib/airsim` | `/data/local/airsim` |
| `Platform` | `u30air-arm64-openwrt-25.12` | `u30air-arm64-android-<build>` |
| 模块 id | `airsim-u30-core` | `airsim-u30`（Magisk） |

profile 属性（主设计 5 节表格）在 Android 下取相同值：`RequiresModem=true`、`DirectModule=true`、`CallAudio=true`、`AndroidTelecom=false`、`NativeContacts=false`、`WRTLite=false`。

## 4. 插件执行契约（Android 载体的唯一新增约束）

主设计没有这一节，因为 LuCI 是正规的 OpenWrt 组件；UFI-TOOLS 插件不是，所以必须写清边界。

UFI-TOOLS v4.1.3 的插件是**注入到 head 的 HTML/JS 文本**：

- 存储：`POST /api/set_custom_head`，整块替换，后端硬限 5 MB；
- 结构：`<!-- [KANO_PLUGIN_START] name -->` / `<!-- [KANO_PLUGIN_END] name -->` 分段；
- 加载：页面用 `DOMParser` 解析后**只把 `style`/`link`/`meta` 克隆进 head，把每个 `script` 追加到 head**，body 不插入；
- 停用：片段含 `[kano_disabled]` 时前端忽略；
- 签名：市场清单只有 MD5，**没有签名**；
- 权限：与页面同权限；页面的 `fetch` 会自动补 `kano-t`/`kano-sign`，但**不补 `authorization`**。

由此得到三条不可协商的约束：

1. **插件不承载任何数据面**。5 MB 与「无进程」决定了它连二进制都装不下，更不可能持有 AT 或 ALSA。
2. **插件等价 root**。页面任意 JS 都能对 `root_shell` 发请求，因此插件只能从自建源分发、哈希钉住、禁止公共商店自动更新。
3. **插件不保存凭证**。控制 token 留在 Agent 自己的页面里。

## 5. 组件与端口

```text
UFI-TOOLS Web   :2333  LAN       插件卡片（只读状态 + 安装入口）
airsim-agent    :8575  LAN 私有  主设计 2.1 全部路由 + /ui + /api/public/status
airsim-u30-bridge :7580 loopback  主设计 4.2 媒体管线与 PCM 握手
airsim-u30-ctl          模块内    install / start / stop / health / probe
```

Android 形态**不引入 8576 installerd**：那是 AVF Debian 的救援服务，这里由签名模块更新替代（主设计 9 节原则不变）。

## 6. 插件 ↔ Agent 通道

**A（默认）iframe**：卡片里嵌 `http://设备LAN地址:8575/ui`，Agent 自己出页面，控制 token 只进该页面的 `sessionStorage`。跨端口不需要 CORS。

**B（卡片数据）只读状态**：`GET /api/public/status`，字段白名单 `runtime_profile / cellular_state / call_state / relay_connected / version`，`Access-Control-Allow-Origin` 只回显 `http://设备LAN地址:2333`，无凭证、无号码、无短信正文。

禁止：

- 把 `KANO_TOKEN` 或 Agent 控制 token 写进 UFI 的 `localStorage` / `shared_prefs`；
- 用 `/api/proxy` 访问本机 8575/7580（该接口对回环与私网一律 403，也不要去改）；
- 在插件里内联 Relay secret、HMAC 密钥、配对密钥。

## 7. AT 与音频后端（2026-10-26 实测）

这两项是主设计 Phase 0 在 OpenWrt 上验证过、但 Android 上没有对应结论的部分，现已在本机实测，结论见下。

### 7.1 AT（2026-10-26 实测，结论已定）

Android 13 上没有 `mu300-atd`，但存在**两条互补通道**：

| 用途 | 通道 | 实测结果 |
| --- | --- | --- |
| 命令（一问一答） | `sendat` → binder `service call vendor.sprd.hardware.log.ILogControl/default 1 s16 "miscserver" s16 "sendAt <slot> <at>"` | `AT` / `AT+CSQ` / `AT+CGMR` / `AT+CEREG?` / `AT+CPIN?` / `AT+CLCC` 全部有响应 |
| URC（异步事件） | 直接读 `/dev/stty_nr0..19`（major 493，`radio:system 660`） | `+CESQ`、`+COPS`、`+SPACTSTATSINFO` 持续上报 |

Android 14+ 的 `sendat` 改走 `vendor.sprd.hardware.tool.IToolControl/default` 事务 3（设备上留有 `send_at.go` 源码），本机是 13，走旧接口。

因此 `ATTransport` 拆成两个实现：`SendAtTransport`（命令）+ `TtyUrcTransport`（URC）。**不需要和 UFI 抢同一条通道**——UFI 只发命令，我们只读 URC；需要仲裁的只是「谁在发命令」，用 `flock` 串行化即可。

未定：`stty_nr*` 上是否出现 `RING` / `+CLIP` / `+CIEV`。这需要一通真实来电，尚未测。

### 7.2 音频（2026-10-26 实测，候选收窄且有硬阻断）

本机**没有任何 ALSA 声卡**：`/proc/asound/cards` 返回 `--- no soundcards ---`，`/dev/snd` 只有 `timer`，`ro.vendor.ag.mem` 指向的 `/dev/audio_dsp_mem` 不存在。

所以主设计 4.2 的「ALSA 端点 + 重采样」在 Android 上不成立，直接 ALSA 这条候选**排除**。音频由 SPRD HIDL HAL（`libsprdaudiohal@7.0.so`，`ro.hardware.audio.primary=whale`）承载，只剩两条路：

| 候选 | 状态 |
| --- | --- |
| AudioRecord / AudioTrack（`VOICE_CALL` / `VOICE_DOWNLINK`） | 下行可读，需 root 或 `CAPTURE_AUDIO_OUTPUT`；**上行注入没有公开 API** |
| Vendor HAL 隐藏接口 / AGDSP 节点 | 需逆向 `libsprdaudiohal`，当前未暴露设备节点 |

这是 v0.1 的最大风险：**下行可解，上行（把 iPhone 的语音送进对端）在标准 Android 上没有路径**。主设计 8.2 把音频驱动列为发布前置，现在这条前置要从「需要匹配模块」升级为「需要先证明存在可行路径」。拿到结论前，不应承诺 Android 形态能打电话。

## 8. 安装、回滚与验收

### 8.1 安装

插件按钮把**固定 URL + 固定 SHA-256** 的引导脚本交给 `/api/root_shell`（接口超时上限 100 s）：

```sh
curl -fsSL --max-time 30 "$AIRSIM_BOOTSTRAP_URL" -o /data/local/tmp/airsim-u30-install.sh \
  && echo "$AIRSIM_BOOTSTRAP_SHA256  /data/local/tmp/airsim-u30-install.sh" | sha256sum -c - \
  && sh /data/local/tmp/airsim-u30-install.sh --channel stable
```

引导脚本只做：下载模块包 → 按主设计 9 节校验 Ed25519 manifest 与 SHA-256、文件白名单 → `magisk --install-module` 或 KernelSU 等效命令 → 重启服务 → 健康检查。不接受任意参数。

### 8.2 回滚

沿用主设计 9 节：双槽、`rename` 激活、健康检查通过才删旧（`/api/health` + AT 自检 + PCM 首帧），失败自动回退，日志不含号码、短信正文、IMEI、token。

### 8.3 额外验收（在主设计 12 节之外）

- UFI-TOOLS 升级、插件清空、UFI 进程被杀后，AirSIM 仍能接听与通话；
- UFI 的 AT 终端与 AirSIM 并发操作时，不丢 URC、不掉 `CLCC`；
- 插件被禁用（`[kano_disabled]`）时，native 通话路径不受影响；
- 模块卸载后无残留进程、端口、`service.sh`。

## 9. 与主设计阶段对齐

| 主设计阶段 | 本文对应工作 |
| --- | --- |
| Phase 0（音频事实） | OpenWrt 结论不可直接搬到 Android；需补一轮 Android 探针（第 7 节） |
| Phase 1（bridge MVP） | 同主设计；Android 只替换 `AudioBackend` 实现 |
| Phase 2（Agent profile） | 新增 `u30air-android` 取值，其余不变 |
| Phase 3（VoWLAN 与 UI） | LuCI 与插件是同一职责的两种实现，接口一致 |
| Phase 4（发布） | 主设计发 `.apk` 包；Android 发签名模块包 |
| Phase 5（Android APK/Root） | 本文即 Phase 5 的一条实现路径；APK 形态保留给无 UFI 的机器 |

## 10. 待真机验证

已结（2026-10-26 实测，见附录 D）：

- ~~`sendat` 能否订阅 URC~~ → 命令走 `sendat`/binder，URC 走 `/dev/stty_nr*`，两条独立；
- ~~`/dev/smd*` / `atd` socket~~ → 不存在；可用的是 `/dev/stty_nr*`（493）与 `sendat`；
- ~~AGDSP 节点~~ → 无 ALSA 声卡，`/dev/audio_dsp_mem` 不存在，音频走 SPRD HAL。

仍然未定：

1. `/dev/stty_nr*` 上是否出现 `RING` / `+CLIP` / `+CIEV`——需要一通真实来电；
2. 上行语音注入是否存在可行路径（当前判断：标准 Android 无路径）；
3. `root_shell` 的长期稳定性（依赖「高级功能」的 socat socket，且该 socket 是 0777）；
4. 本机无 Magisk/KernelSU（`/data/adb` 为空），模块化安装与 `service.sh` 自启方案需重做；
5. 2333 与 8575 在同一 `wlan0` 地址下的防火墙冲突。

---

## 附录 A：插件骨架（注入用）

```html
<!-- [KANO_PLUGIN_START] airsim -->
<script>
(() => {
  if (window.__AIRSIM_PLUGIN__) return;
  window.__AIRSIM_PLUGIN__ = true;

  var AGENT_PORT = 8575;
  var agentBase = function () { return 'http://' + location.hostname + ':' + AGENT_PORT; };
  var token = function () {
    if (typeof KANO_TOKEN !== 'undefined' && KANO_TOKEN) return KANO_TOKEN;
    return localStorage.getItem('kano_sms_token');
  };

  function mount() {
    if (document.getElementById('airsim-card')) return;
    var card = document.createElement('div');
    card.id = 'airsim-card';
    card.style.cssText = 'position:fixed;right:16px;bottom:16px;z-index:9999;width:260px;' +
      'padding:12px;border-radius:12px;background:var(--card-bg,#1c1c1e);' +
      'color:var(--text-color,#fff);box-shadow:0 6px 24px rgba(0,0,0,.35);' +
      'font-size:12px;line-height:1.5';
    card.innerHTML =
      '<div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:8px">' +
      '<b>AirSIM</b><span id="airsim-state">检测中…</span></div>' +
      '<div id="airsim-detail" style="opacity:.75;margin-bottom:8px">—</div>' +
      '<div style="display:flex;gap:6px">' +
      '<button id="airsim-open" class="btn" style="flex:1">控制台</button>' +
      '<button id="airsim-install" class="btn" style="flex:1">安装/升级</button></div>';
    document.body.appendChild(card);

    document.getElementById('airsim-open').onclick = function () {
      window.open(agentBase() + '/ui', '_blank', 'noopener');
    };
    document.getElementById('airsim-install').onclick = install;

    poll();
    setInterval(poll, 5000);
  }

  function poll() {
    var state = document.getElementById('airsim-state');
    var detail = document.getElementById('airsim-detail');
    fetch(agentBase() + '/api/public/status', { cache: 'no-store' })
      .then(function (r) { if (!r.ok) throw new Error('HTTP ' + r.status); return r.json(); })
      .then(function (s) {
        state.textContent = s.call_state === 'idle' ? '在线' : '通话中';
        state.style.color = '#34c759';
        detail.textContent = [s.runtime_profile, s.cellular_state, s.version]
          .filter(Boolean).join(' · ');
      })
      .catch(function () {
        state.textContent = '未安装/未运行';
        state.style.color = '#ff9f0a';
        detail.textContent = '点击「安装/升级」部署模块';
      });
  }

  var BOOTSTRAP_URL = 'https://example.invalid/airsim-u30/install.sh';
  var BOOTSTRAP_SHA = '0000000000000000000000000000000000000000000000000000000000000000';

  function install() {
    if (!confirm('将下载并安装 AirSIM U30 模块（需已开启 UFI-TOOLS 高级功能）。继续？')) return;
    var cmd = 'curl -fsSL --max-time 30 "' + BOOTSTRAP_URL + '" -o /data/local/tmp/airsim-u30-install.sh' +
      ' && echo "' + BOOTSTRAP_SHA + '  /data/local/tmp/airsim-u30-install.sh" | sha256sum -c -' +
      ' && sh /data/local/tmp/airsim-u30-install.sh --channel stable';
    var btn = document.getElementById('airsim-install');
    btn.disabled = true; btn.textContent = '安装中…';
    fetch('/api/root_shell', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', authorization: token() },
      body: JSON.stringify({ command: cmd, timeout: 90000 })
    })
      .then(function (r) { return r.json(); })
      .then(function (j) {
        var ok = j && j.result && j.result.done === true;
        alert(ok ? '安装完成，等待服务启动' : '安装失败：' + (j.error || '未知错误'));
      })
      .catch(function (e) { alert('安装失败：' + e.message); })
      .finally(function () {
        btn.disabled = false; btn.textContent = '安装/升级';
        poll();
      });
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', mount);
  } else {
    mount();
  }
})();
</script>
<!-- [KANO_PLUGIN_END] airsim -->
```

## 附录 B：模块骨架

```sh
# packaging/android-magisk/module.prop
id=airsim-u30
name=AirSIM for U30 Air
version=v0.1.0
versionCode=1
description=airsim-agent + airsim-u30-bridge (arm64)
```

```sh
# packaging/android-magisk/service.sh
#!/system/bin/sh
MODDIR=$(dirname "$0")
LOG=/data/local/airsim/log
mkdir -p "$LOG"

start_bridge() {
  until [ -x "$MODDIR/bin/airsim-u30-bridge" ]; do sleep 5; done
  "$MODDIR/bin/airsim-u30-bridge" --config /data/local/airsim/bridge.json >> "$LOG/bridge.log" 2>&1
}

start_agent() {
  until "$MODDIR/bin/airsim-u30-ctl" health >/dev/null 2>&1; do sleep 2; done
  "$MODDIR/bin/airsim-agent" --profile u30air-android >> "$LOG/agent.log" 2>&1
}

( while :; do start_bridge; sleep 3; done ) &
( while :; do start_agent;  sleep 3; done ) &
```

## 附录 C：探针输出项

`tools/airsim-u30-probe.sh` 只读，输出可粘贴的 capability 报告：

```text
device: ro.product.model / ro.build.fingerprint / uname -r
at:     sendat / atd socket / /dev/smd* / /dev/tty*
audio:  /dev/snd/* / vendor DSP 节点 / 可用采样率
root:   whoami / id / getenforce / magisk -V | ksud -V
ufi:    :2333 可达性 / 高级功能状态 / root_shell 探测
net:    wlan0 地址 / 2333 与 8575 监听归属
```

不输出 IMEI、号码、短信内容、token。

---

## 附录 D：实测记录（2026-10-26）

环境：U30 Air，无线 adb `192.168.0.1:5555`（热点网段 `192.168.0.0/24`），root 经 UFI-TOOLS `/api/root_shell` 取得。

| 项 | 实测值 |
| --- | --- |
| 系统 | `ZTE/U30Air/U30Air:13/TP1A.220624.014/20250304.163149:user/release-keys` |
| 版本 | `U30AirV1.0.0B17`，kernel `5.4.254-android12-9-g7463324543ed` |
| SELinux | `Permissive` |
| 引导 | `verifiedbootstate=green`、`flash.locked=1`、`ro.debuggable=0` |
| root | `u:r:su:s0` 可用；`/system/bin/su` 不存在；`/data/adb` 为空（**无 Magisk/KernelSU**） |
| 无线 adb | `[::]:5555` LISTEN |
| UFI-TOOLS | **4.1.5**（本文其余章节仍按 4.1.3 写，接口未见差异；默认口令已改） |
| 已装插件 | 「打电话测试插件」（ATD/ATA/ATH）、「ws-scrcpy 网页投屏 1.4」 |
| 音频 | `/proc/asound/cards` = `--- no soundcards ---`；`/dev/snd` 只有 `timer`；`/dev/audio_dsp_mem` 不存在；HAL 为 `libsprdaudiohal@7.0.so` / `ro.hardware.audio.primary=whale` |
| AT 命令 | `sendat` → `service call vendor.sprd.hardware.log.ILogControl/default 1 s16 "miscserver" s16 "sendAt <slot> <at>"` |
| AT URC | `/dev/stty_nr0..19`（major 493）读到 `+CESQ` / `+COPS` / `+SPACTSTATSINFO` |
| 蜂窝 | `gsm.sim.state=LOADED,ABSENT`、中国联通、`NR_SA` |

**安全发现**：`/data/data/com.minikano.f50_sms/files/kano_root_shell.sock` 权限为 `srwxrwxrwx`，本机任意应用可直接连上拿 root。这与插件分发策略同等重要：只要 2333 或该 socket 暴露在 LAN，机器就等于无鉴权 root。

**对附录 B 的更正**：本机没有 Magisk/KernelSU，`module.prop` / `service.sh` 那套模块骨架在这台机器上不成立。自启载体需要改为厂商 root 的持久化方式（待确认），或明确接受「重启后需重新拉起」。

