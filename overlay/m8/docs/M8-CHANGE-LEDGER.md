# M8 改动账本

一句话：**`overlay/m8/` 里每个文件为什么存在、改到哪一步、验没验过，全在这张表里。**

规则（见 `PROJECT-NORMS.md` §1）：新增或删除任一 overlay 文件，必须同步改这张表；表里没有的改动不许进包。

## 状态标记

| 标记 | 含义 | 能不能发版 |
|---|---|---|
| ✅ 已实测 | 装机后按验收判据跑过，有证据 | 可以 |
| 🟡 代码已改未验 | 编译通过、装机过，但没跑成功用例 | 可以，但发版说明必须写「代码已改，未实机验收」 |
| ⏸ 挂起 | 已知要改，等外部条件（iOS 端 / 自有服务器） | 可以，写进「本版未包含」 |
| ❌ 做不通 | 试过，走不通，已换方案 | 可以，写进「未修复问题」并注明替代方案 |

## 一、音频桥（M8 与 F50 差异最大的一块）

| ID | 文件 | 改了什么 | 为什么必须改 | 状态 |
|---|---|---|---|---|
| A1 | `internal/audio/devices.go` | 端点发现改了：优先认 `FE_ST_VOICE_C`（M8 的 DMA 语音采集），退回 `FE_ST_DUMP`；补 `CaptureRate/Channels/Period/Buffer`、`Playback*`、`NeedsVoiceScene`、`CallProbe` 字段 | F50（5.4 厂商内核）下行是 `FE_ST_DUMP`、48k 立体声采集 / 16k 播放；M8（6.18 主线）`FE_ST_DUMP` 没有后端 DAI，根本打不开，只有 `FE_ST_VOICE_C` 能出 8k 单声道 | ✅ 通话有声音 |
| A2 | `internal/audio/devices.go` | 播放固定 16000 单声道（`16000/1`，period 320，buffer 1920） | M8 的 `FE_ST_VOICE_PCM_P` 只吃 16k 宽带样本；直接灌 8k 会**双倍速**播放 | ✅ |
| A3 | `internal/audio/devices.go` | `CallProbe`：单声道采集 → `mmcli` 探测，否则看 `/usr/bin/mu300-at` 在不在 | M8 上 ModemManager 占着 modem，`mu300-at` 只是个 AT 中继壳，不是 F50 那套栈，探测口径不同 | ✅ |
| A4 | `internal/audio/voice_scene.go`（新文件） | 开/关 AP 语音开关（`S_VOICE_CAP_C` / `S_VOICE_PCM_P`） | 主线内核上这张卡**只有**在 AP voice scene 打开后才送通话音频，不打等于全程静音 | ✅ |
| A5 | `internal/audio/early_media.go` | 在 M8 上直接拒开 early media 采集，返回 `early media capture disabled on this device` | M8 通话全程不读手机麦克风（早期媒体阶段读会抢通路、把通话搞哑） | ✅ |
| A6 | `internal/audio/session.go` | 加 `OnStreamFault` 回调、`micState()`、`recordEvent()` 时间线日志、`isStreamFault()` | 流断是远端挂断最早的信号；有回调才能立刻刷新 UI，不用等下一次轮询（F50 上 URC 事件好用，M8 没有） | ✅ 挂断 1s 内 UI 复位 |

## 二、daemon（`cmd/airsim-mu300d/`）

| ID | 文件 | 改了什么 | 为什么 | 状态 |
|---|---|---|---|---|
| B1 | `main.go` | `OnStreamFault` 接上 `POST 127.0.0.1:8575/api/calls/refresh` | 同上，M8 没有 URC 事件可等 | ✅ |
| B2 | `main.go` | `keepLogsBounded()` 裁剪 `/var/log` 大日志 | M8 根分区只有 4GB，原生 modem 栈会无限写 `e5-android-log.txt`，不裁会写满 | ✅ |
| B3 | `main.go` | zeroconf 服务名/属性改成 `AirSIM-VIPME-M8-…`、`model=VIPME-M8` | 局域网上与 F50 同名会被 iOS 认错机型 | ✅ 配对发现正常 |
| B4 | `main.go` | 新增 `/pair/clients`、`/pair/remove` 两个管理接口；状态里带 `product=AirSIM for VIPME-M8` | 网页要能列出已配对客户端、能删掉旧配对（换手机后清不掉会一直显示旧设备） | ✅ 弹层里能列出、能删 |
| B5 | `main.go` | 工具自检按 `CallProbe` 分流（`CheckMMTools` / `CheckTools`） | M8 走 mmcli，缺 `mmcli` 和缺 `mu300-at` 是两种完全不同的事故 | ✅ |
| B6 | `internal/gateway/gateway.go` | 配对凭据管理：`ClientInfo`/`Clients`（只暴露密钥尾号 + 最后在线时间，2 分钟窗口判在线）、`Remove` 按序号删除、key 文件原子替换 | 上游只有一份 `seen` 表，网页无法列出「已配对客户端」。换手机后旧配对清不掉、也看不出哪台在线 | ✅ 弹层列出在线状态、删除生效 |
| C1 | `calls.go` | `/api/calls/dial` 接受 `card`（`"0"`/`"1"`），转成 `sim_account` 下发；非 0/1 直接报错 | 网页要能「用哪张卡拨出去」。不上这个字段，两张卡都沿用主卡的 AT 上下文，第二张卡根本拨不出去 | ✅ 网页拨出卡生效 / ⏸ **iOS 云端选卡仍不生效**（见 P2） |
| D1 | `management.go` | `releasePage`/`releasesAPI` 改指 `shangjie1998/airsim-m8-openwrt`；`releasePattern` 加 `(-m8)?`；`no_release` 文案改「M8 为整包发布…」；UA 改 `AirSIM-VIPME-M8-update-check` | 插件更新原本指向上游 `kai-wu-cortex/airsim-mu300`，改自建仓库后必须一起改正则，否则 `-r22-m8` 永不匹配、页面永久「无可用更新」 | ✅ 页面显示「M8 为整包发布，此处不做在线安装…」 |

## 三、安装骨架（`m8-pkg/`）

| ID | 文件 | 作用 | 状态 |
|---|---|---|---|
| E1 | `install.sh` / `uninstall.sh` | 落地 / 完整卸载，全部在 `/opt/airsim-m8` 自有命名空间 | ✅ 装机 rc=0、打完可整卸 |
| E2 | `airsim-m8.init` | procd 服务定义；数据目录钉到真分区（不是 tmpfs） | ✅ 重启不丢配对 |
| E3 | `mu300-at`（新） | AT 中继壳：一条复合命令按槽切换再发指令 | ✅ 分槽命令生效 |
| E4 | `mu300-sms`、`sms-pool-sync.py`、`sms-dash.py` | 短信池镜像与网页短信页 | ✅ 双卡短信收发 |
| E5 | `m8-pkg/airsim-call-forward`、`m8-pkg/airsim-call-forward.init`、`m8-pkg/airsim-call-forward.config` | 来电转发（主程序 + procd 服务 + 配置） | ✅ |
| E6 | `luci/usr/libexec/rpcd/airsim` | 含 `backup_make` / `backup_restore` / `airsim_fingerprint`（指纹 + tar 白名单 + 8MB 上限） | ✅ 下载/拖拽/二次确认全通 |
| E7 | `luci/usr/libexec/rpcd/airsim-call`、`airsim-sms` | 通话 / 短信专用后端 | ✅ |
| E8 | `m8-pkg/luci/www/luci-static/resources/view/airsim/overview.js`、`manage.js`、`call.js`、`sms.js`、`overview.css` | 自建 4 个页面；按钮统一 `as-button as-secondary`；管理设置两按钮；插件更新链接 | ✅ |
| E9 | `m8-pkg/luci/usr/share/luci/menu.d/luci-app-airsim.json`、`m8-pkg/luci/usr/share/rpcd/acl.d/luci-app-airsim.json` | 菜单与 ACL（旧布局的 `airsim-status.js`/`luci-api.py` 等 6 个残留已在 0.1.0.31 清掉，从未安装） | ✅ |
| E10 | `VERSION` | 版本唯一真源 | ✅ |

## 四、Agent 侧（不进 daemon，走 `scripts/prepare-agent.sh`）

| ID | 补丁 | 改了什么 | 为什么 | 状态 |
|---|---|---|---|---|
| F1 | `patches/AirSIM-e164.patch` | SIM 自报号码渲染成 `+8613…` 的 E.164 | 某些卡只存带国家码的裸形式，iOS 认不出号码 | ✅ |
| F2 | `patches/AirSIM-m8-sms-callfix.patch` | 结束宽限期不再否决 hangup/answer/DTMF；短信回复带发送时间 | 宽限期把正常挂断当成误触，挂断按钮点了没反应 | ✅ |
| F3 | `patches/AirSIM-m8-fast-hangup.patch` | 通话中每秒轮询一次状态 | 这版固件没有可用的 URC 事件，否则挂断后状态滞后两三秒 | ✅ 挂断 1s 内响应 |
| F4 | `scripts/prepare-agent.sh` | 依次打上面 3 个补丁 + 上游原有的 `AirSIM-mu300.patch`；补 `module-agent/go.mod`、`go.sum` | 上游 `.gitattributes` 把 agent 的 go.mod/go.sum 标了 `export-ignore`，`git archive` 会漏掉它们，模块模式编译直接失败 | ✅ agent 能编出来 |

## 五、文档

| ID | 文件 | 说明 | 状态 |
|---|---|---|---|
| G1 | `docs/PROJECT-NORMS.md` | 37 条项目规范 | ✅ 0.1.0.31 入库 |
| G2 | `docs/M8_BUILD_AND_ADAPT.md` | 编译、二次开发、坑表 | ✅ |
| G3 | `docs/PENDING-cloud-dial-missing-sim-account.md` | 云端拨号缺 `sim_account` 的排查记录 | ⏸ 挂起 |
| G4 | `docs/UPSTREAM_README.md` | 上游 README 原文存档（根 `README-M8.md` 是自建版） | ✅ |
| G5 | `README.md`（overlay 覆盖上游同名文件） | 改成 M8 自建说明 | ✅ |

## 六、没修好 / 做不通 / 挂起的（对外必须如实写）

| ID | 事项 | 结论 | 状态 |
|---|---|---|---|
| P1 | 云端通话整通不通 | 媒体能建链、首帧下行 PCM 能发出，iPhone 停在「模块未确认接听」。**每版 Release 红字标注** | ❌ 接不通 |
| P2 | iOS 端选卡 / 默认卡 | 本机网页选卡生效；iOS 走云端请求体缺 `sim_account`，选了卡还是会用错卡 | ⏸ 等 iOS 端更新后重跑 |
| P3 | 网页内在线安装 | 上游 `selectRelease` 只认 `luci-app-airsim-<tag>.apk`，`update-install` 是 apk 流程，M8 发 tar.gz。**改方案为只换链接 + 明确提示** | ❌ 做不通（已换方案） |
| P4 | 上游 R23 的 Tailscale / 私网 IP 配对 | 未移植 | ⏸ 未做 |
| P5 | 中继延迟 | 走的是作者的上游中继（境外云），跨境延迟属外部条件，自有服务器后再比方案 | ⏸ 等自有服务器 |
| P6 | 既有 Go 单测 5 条红 | 与 M8 改动无关，未处理 | ❌ 已知 |
| P7 | 设备重新配对 | 0.1.0.31 装机后 `configured` 仍需 iPhone 端重配 | ⏸ 待用户操作 |
