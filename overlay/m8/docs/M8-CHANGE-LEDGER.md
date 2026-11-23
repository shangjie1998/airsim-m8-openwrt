# M8 改动账本

一句话：**`overlay/m8/` 里每个文件为什么存在、改到哪一步、验没验过，全在这张表里。**

规则（见 `PROJECT-NORMS.md` §1）：新增或删除任一 overlay 文件，必须同步改这张表；表里没有的改动不许进包。

## 发版记录

- **2026-11-23 r23 已发布**：main `cd1e508` 推到 `shangjie1998/airsim-m8-openwrt`，Release 标签 `v0.1.0.32-r23-m8`，附件 `airsim-m8-0.1.0.32-r23-m8.tar.gz`（6,097,494 B，sha256 `a5c0f19cb797a7fd4611312fab88b5ad55703ca9a07f80961648e55d0025c9e9`），闸三线上回读通过。U34–U38 行内「全本地/未推远端」字样为开发期状态，以本条为准；待验项见上方 D1–D7。

## 状态标记

| 标记 | 含义 | 能不能发版 |
|---|---|---|
| ✅ 已实测 | 装机后按验收判据跑过，有证据 | 可以 |
| 🟡 代码已改未验 | 编译通过、装机过，但没跑成功用例 | 可以，但发版说明必须写「代码已改，未实机验收」 |
| ⏸ 挂起 | 已知要改，等外部条件（iOS 端 / 自有服务器） | 可以，写进「本版未包含」 |
| ❌ 做不通 | 试过，走不通，已换方案 | 可以，写进「未修复问题」并注明替代方案 |

## 待办欠账（r23 发版时未验，下批再改）

> 下列各项代码已随 r23 进包，但真机用例没跑完；发版说明按 🟡/⏸ 口径写，不写成已验收。

| # | 欠账 | 现状 | 验收条件 |
|---|---|---|---|
| D1 | 三档模式真切换（cell / ap） | netmode 脚本与自检回滚在位；STA 上行链路已真机打通（U38）。蜂窝拨号、热点开启、默认路由落点、失败回滚未在真机跑通 | SIM 注册在位后切 cell 看蜂窝口默认路由；真 AP 下切 ap 看热点起、手机能连 |
| D2 | 网页点「连接」全流程联调 | CLI 走 `netmode wifi` 16.73 秒通；shim `wifi_connect` 包同一路径，前端点击换网 / 已保存网络重连 / forget 未在浏览器真点一遍 | 浏览器扫真实热点、输密码连接成功；换网重连；删除已保存网络 |
| D3 | daemon `/rebind` 监听搬迁 + Bonjour 重注册 | 内部端点已补（先建新 pair/control/PCM 监听再拆旧、重注册主广告 TXT host），未在真实换网后验 | 换 Wi-Fi 后手机不用重新配对，直连地址刷新到新 IP |
| D4 | 防火墙放行规则命中 | wlanadmin 区放行、管理页开关从无线口放行 80/443、直连/mDNS 常驻 nft 规则未在真机核对命中 | `nft list ruleset` 核对规则；上游 Wi-Fi 侧按开关状态验证 80/443 通断、5353 可达 |
| D5 | 真机 iPhone 配对全流程 | 配对服务代码在位，未拿真机走完 | 真实配对→列表指纹→空闲移除→通话中拒绝移除→重启持久化 |
| D6 | 闸三线上回读 | 发 Release 后 `check-release.py` 回读大小/sha256 未跑（缺 PAT） | 发版后用 M8_PAT 跑通闸三 |
| D7 | 既有已知限制（不属本批回归） | 内嵌 webview 后台挂起致 rAF 停摆；在线安装只认 `.apk`；云端通话整通不通、iOS 云端选卡无效（请求体缺 `sim_account`）；AP 热点自身频段双频开放未动 | 单列跟进，不在 r23 承诺范围内 |

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
| G6 | `docs/ui-preview.html` | 玻璃风界面离线预览（三态外观可点、手机/平板/电脑三档），定稿视觉的参照物 | ✅ |

## 六、界面：玻璃风设计系统（批次 1）

| ID | 文件 | 改了什么 | 为什么 | 状态 |
|---|---|---|---|---|
| U1 | `ui.css`（新增） | 设计系统：浅/深两套 token（浅色底 `#F5F5F7`、玻璃卡 `.52`、实体控件 `.78`；深色 `#101013`）+ 卡片/按钮/字段/行/药丸/反馈/分段控件/弹层/拖拽区/短信块，全部带三档响应式 | 原来 `overview.css` 一根大杂烩，每加功能往里追加，同一组件在不同页长得不一样 | ✅ 装机后概览与设置页正常 |
| U2 | `ui.js`（新增） | 外观状态机：`auto`/`light`/`dark` 三态 + 密度 + 透明度，写 `localStorage`，`auto` 监听 `prefers-color-scheme`；`install()` 幂等套用 | 深色要能跟随系统又要能手动定，三页都要认，不能各写一份 | ✅ 切换即时生效、刷新记住 |
| U3 | `settings.js`（新增） | 设置页：外观三选一、密度、卡片背景（玻璃/纯色）、关于（版本 + 设备尾号） | 用户要求把颜色这类偏好单独放一页 | ✅ |
| U4 | `overview.js` | 只换样式来源：`overview.css` → `ui.css`，`render` 开头调 `ui.install()`；rpc 逻辑一行未动 | 让概览先吃到新系统 | ✅ 数据与改前一致 |
| U5 | `m8-pkg/luci/usr/share/luci/menu.d/luci-app-airsim.json` | 新增「设置」菜单项（order 40 → `airsim/settings`） | 新页面要能点进去 | ✅ |
| U6 | `m8-pkg/install.sh` | 拷贝列表加 `ui.css`、`ui.js`、`settings.js` | 漏一个就是白屏或 404 | ✅ |

| U7 | `connect.js`（新增） | 「连接」页：连接模式、Relay 地址（含官方默认/跟随客户端两个快捷键）、LAN 接口、三个端口、运行状态、保存 | 原来连接设置和备份更新挤在同一页，改一个连接模式还得翻半屏 | ✅ |
| U8 | `maintenance.js`（新增） | 「维护」页：备份与恢复（拖拽 + 指纹校验 + 二次确认）、插件更新（自动检查 / 预览版 / 整包发布提示）、脱敏诊断、版本说明 | 同上，维护类操作单独成页 | ✅ |
| U9 | `call.js`（重写） | 「来电通知」页改为自渲染：去掉 `require mu300.common`，确认框/轻提示/忙碌态改用 `ui.js` 的实现，样式全走 `ui.css`；rpc 与取值逻辑原样保留 | 这是唯一一页依赖设备上 vendor 的 178 KB 共享模块，换固件就可能白屏 | ✅ |
| U10 | `m8-pkg/luci/usr/share/luci/menu.d/luci-app-airsim.json` | 菜单重排为 设备概览(10) / 连接(20) / 来电通知(30) / 维护(40) / 设置(50) | 拆页后入口要对上 | ✅ |
| U11 | `install.sh` | 拷贝列表改为 `overview.js connect.js maintenance.js sms.js call.js ui.css ui.js settings.js` | 新页不装就是 404 | ✅ |
| U12 | 删除 `manage.js`、`overview.css` | 旧合并页由 connect + maintenance 接管；`overview.css` 已无人引用（三页全走 `ui.css`） | 留两份样式就是下次打架的源头 | ✅ |

## 七、界面：拆页收尾（批次 3）

| ID | 文件 | 改了什么 | 为什么 | 状态 |
|---|---|---|---|---|
| U13 | `sms.js` | 由 `baseclass` 子视图改独立 view：自带 `as-root`/页面头/页脚/版本胶囊，样式走 `ui.css`；新增「全部已读」「清空短信」两个按钮 + 操作提示条 + `marking`/`clearing` 双锁；`clearEverything()` 先弹 `ui.dialog` 红色二次确认 | 短信不再塞在概览里，手机上少滚一屏；批量操作需要明确入口 | ✅ 真机验收：列表/已读计数/二次确认均正常 |
| U14 | `overview.js` | 去掉 `require view.airsim.sms` 与布局里的 `sms.render()` | 短信独立成页后概览重复 | ✅ 概览四张卡（设备状态/启动服务/配对/拨号）正常，无短信卡 |
| U15 | `m8-pkg/luci/opt/airsim-m8/bin/sms-dash.py`、`m8-pkg/luci/usr/libexec/rpcd/airsim-sms`、`m8-pkg/luci/usr/share/rpcd/acl.d/luci-app-airsim.json` | 后端加 `sms_read_all`（本地面板标记全部已读，不碰 SIM）与 `sms_clear_batch`（`limit` 1–10，默认 4，逐条 `e5-sms delete` 分批真删，同步剔除已读标记）；rpcd 声明补两方法；ACL write 白名单放行 | 固件 `e5-sms` 只有单条 `delete`，一次删太多会被 20 秒 rpc 超时掐断 | 🟡 `sms_read_all` 实测 `{"ok":true,"marked":N}`；`sms_clear_batch` 代码已装未实删（设备上只剩真实短信，未拿它试删） |
| U16 | `ui.css` | 追加 `#tabmenu{position:sticky;top:56px;z-index:24}` + 玻璃背景 + blur，及 `.as-sms-ops` | 长页滚动时菜单跟着走，不用回顶 | ✅ 滚动 900px 处实测 `tabtop=56 pos=sticky`，窄屏 390px 也钉住 |
| U17 | `ui.js` | `stylesheet()` 的 href 末尾拼 `?v=<CSSVERSION>`（当前 `20261109`） | `L.resource()` 只带固件版本，固件不变时改 CSS 浏览器一直吃旧缓存 | ✅ 改一次 CSS 必须同步改这个常量，已写进经验文档 |
| U18 | `m8-pkg/luci/usr/share/luci/menu.d/luci-app-airsim.json` | 插入 `admin/services/airsim/sms`（order 35） | 短信页要能点进去 | ✅ 菜单 6 项：设备概览\|连接\|来电通知\|短信\|维护\|设置 |

## 六、没修好 / 做不通 / 挂起的（对外必须如实写）

| ID | 事项 | 结论 | 状态 |
|---|---|---|---|
| P1 | 云端通话整通不通 | 媒体能建链、首帧下行 PCM 能发出，iPhone 停在「模块未确认接听」。**每版 Release 红字标注** | ❌ 接不通 |
| P2 | iOS 端选卡 / 默认卡 | 本机网页选卡生效；iOS 走云端请求体缺 `sim_account`，选了卡还是会用错卡 | ⏸ 等 iOS 端更新后重跑 |
| P3 | 网页内在线安装 | 上游 `selectRelease` 只认 `luci-app-airsim-<tag>.apk`，`update-install` 是 apk 流程，M8 发 tar.gz。**改方案为只换链接 + 明确提示** | ❌ 做不通（已换方案） |
| P4 | 上游 R23 的 Tailscale / 私网 IP 配对 | 未移植。**2026-11-23 评估结论：不跟 r23 地基**——缓冲协商 M8 已自带，Tailscale 属独立可选特性；详见 `docs/R23-BASELINE-EVAL.md` | ⏸ 未做，保持 r22 基线 |
| P5 | 中继延迟 | 走的是作者的上游中继（境外云），跨境延迟属外部条件，自有服务器后再比方案 | ⏸ 等自有服务器 |
| P6 | 既有 Go 单测 5 条红 | 与 M8 改动无关，未处理 | ❌ 已知 |
| P7 | 设备重新配对 | 0.1.0.31 装机后 `configured` 仍需 iPhone 端重配 | ⏸ 待用户操作 |

## 八、蓝牙开关（批次 4，通用实现）

| ID | 文件 | 改了什么 | 为什么 | 状态 |
|---|---|---|---|---|
| U19 | `m8-pkg/luci/opt/airsim-m8/bin/airsim-bt.py`（新增） | 通用蓝牙后端：`bt_get` / `bt_set`。逻辑全在 python，机型差异只从 `/etc/config/airsim-bt` 读（backend / service / flag_uci / autostart_uci / power_on / tty / baud / module / adapter / label）。`backend=service` 交给现成 init 脚本；`backend=attach` 自己 modprobe + rfkill 解锁 + btattach。开：写 uci + start + 补 `hciconfig up`；关：先 `bluetoothctl power off` 再 stop 再 `killall btattach` | 用户要 M8 和 F50 共用同一份代码、F50 只改配置；F50 是单卡但框架相同 | ✅ 真机：关 → 3 s 返回 `attached=false`；开 → 7 s 返回 `attached=true, up=true` |
| U20 | `m8-pkg/luci/usr/libexec/rpcd/airsim-bt`（新增）、`m8-pkg/luci/etc/config/airsim-bt`（新增） | rpcd shim 声明 `bt_get` / `bt_set({enable})` 并转 python；配置默认值按 M8 写（`service=e5-bt`、`flag_uci=e5-bluetooth.main.enabled`、`tty=/dev/ttyBT0`、`baud=3000000`、`module=sprdbt_tty`）。配置节必须是**具名节** `config main 'main'`，匿名节 `uci get airsim-bt.main` 取不到 | rpcd 对象要能被 ubus 调；机型差异单列一份文件，F50 换配置即可 | ✅ `ubus list` 出现 `airsim-bt`，`ubus call` 正常 |
| U21 | `m8-pkg/luci/www/luci-static/resources/view/airsim/settings.js`、`m8-pkg/luci/usr/share/rpcd/acl.d/luci-app-airsim.json`、`m8-pkg/install.sh` | 设置页加「蓝牙」卡：开/关分段控件 + 状态说明 + 控制器/服务两行；`bt.supported === false` 时整张卡不出现；控制器那行做成活节点，开关一动跟着改。ACL 加 read `bt_get` / write `bt_set`；install.sh 拷 `airsim-bt` shim、`airsim-bt.py`、`/etc/config/airsim-bt`（后者已存在就不覆盖） | 开关要有入口；F50 装同一份包、换一份配置就有开关，装错机型也不会误显示 | ✅ 浏览器实测：开 → 关 → 开 均生效，`hci0 UP RUNNING`、`bluetoothctl` 能列出控制器，`br-lan` 与 Wi-Fi 未受影响 |

| U22 | `m8-pkg/luci/opt/airsim-m8/bin/m8-bt-address-fix.py`（新增，M8 专用）、`m8-pkg/install.sh` | 出厂 btmac 全 0 时给 PSKEY 补一个本地管理地址；install.sh 拷到 `/opt/airsim-m8/bin/` 并**每次装机跑一遍**（合法地址时脚本自己不动手，改前备份 `…pskey.bin.bak-<old>`） | 重刷固件后厂标还是全 0，vendor 的 `m8-bluetooth-address` 又依赖本机不存在的 `/mnt/e5-data`，不补这一步蓝牙永远起不来。文件头已标注「仅 VIPME-M8，F50 不要装」 | ✅ 本机执行过：`address repaired: 00:00:00:00:00:00 -> 02:50:00:00:e5:02` |

## 九、工程治理：补丁登记 + 三道闸（批次 5，跟 F50 口径）

| ID | 文件 | 改了什么 | 为什么 | 状态 |
|---|---|---|---|---|
| U23 | `m8-pkg/../patches/manifest.tsv`（新增）、`patches/AIRSIM_BASE`、`patches/AirSIM-mu300.patch`（从 `src/patches` 取回） | Agent 补丁改由 manifest 驱动：每行 = 补丁文件 / active\|merged / 关键标记串。`overlay/m8/scripts/prepare-agent.sh` 重写成按表顺序打补丁，打不上立即失败；manifest 里 `merged` 的只校验标记串是否存在 | 原来补丁清单硬编码在脚本里，且 `patches/` 缺 `AIRSIM_BASE` 与上游主补丁，脚本实际跑不起来；上游改词导致补丁静默失效也没人发现 | ✅ `sh overlay/m8/scripts/prepare-agent.sh` 四个补丁全部 applied |
| U24 | `overlay/m8/scripts/check-patches.py`（新增） | 闸零：在「基线提交的干净导出树」上逐个 `git apply --check` + 真打 + 校验标记串；基线与上游 HEAD 的落后量只提示不阻断 | 补丁只在装机时炸，改在构建期就拦住 | ✅ 4 个补丁全过（首轮真抓出 2 条标记写错） |
| U25 | `scripts/check-package.py`（新增）、`scripts/check-release.py`（新增） | 闸二：解包抽查 VERSION / 两个二进制里的版本串 / 14 个必需文件 / `usr/libexec` 白名单 / 附件名 ASCII。闸三：发完 Release 回读线上大小与 sha256 | 闸一只证明仓库自洽，管不住「源码改了包里还是旧的」；闸三防挂错文件、上传截断 | ✅ 闸二对 `0.1.0.32` 现包通过（31 个文件）；闸三待令牌（`/tmp/m8pat.txt` 已随重启清掉） |

## 十、前端 A 档：设计系统跟 F50（批次 6，只动框架层）

| ID | 文件 | 改了什么 | 为什么 | 状态 |
|---|---|---|---|---|
| U26 | `m8-pkg/luci/www/luci-static/resources/view/airsim/ui.js`、`ui.css` | `ui.js` 整份采用 F50 口径（新增 `field` 标签封装、Promise 版 `dialog` 带 `titleId`、`toast` 句柄、`busy`、`stampEpoch`、`menu` 自绘菜单），`menu()` 的 PAGES 按 M8 六页改回（概览/连接/来电通知/短信/维护/设置，去掉 net/audio）；去掉 F50 音频页专用 `latestChecker`；`CSSVERSION` 升 `20261123`。`ui.css` 整份采用 F50 口径，保住 M8 四处：`--as-pad` 自适应 clamp（含 compact 档）、`.as-root` `max-width:1440px`、`.as-actions` column 上下排、`.as-history-row` 3 列 grid + 480px 断点 | F50 设计系统是 M8 的纯超集（三遍评估：API 超集、CSS 超集、rpcd 依赖不冲突）；框架层先换，页面改写留 B 档 | 🟡 离线三遍自检过（API/类名覆盖、`node --check` 语法、残留排查）；设备离线，真机六页截图验收待上线 |

## 十一、前端 B 档：页面改进按 M8 映射改写（批次 7）

| ID | 文件 | 改了什么 | 为什么 | 状态 |
|---|---|---|---|---|
| U27 | `overview.js` | 客户端弹窗：`recent` 算在线；勾选/确认/回执按 `id \|\| index` 取值并带尾号兜底；删除回执先判 `ok===false` 显示后端原因，`removed/total` 空值兜底；拨出卡选择块按双卡探测显隐。RPC 仍走 `airsim.pair_clients/pair_remove`，参数仍 `indexes` | 搬 F50 弹窗的健壮性改进；r26 指纹模型（B-4）落地后再切 `ids` | 🟡 离线自检过（语法/RPC 对账/diff 复核）；真机待上线 |
| U28 | `maintenance.js` | 备份、恢复拆成两张卡，文案 `as-lead`；RPC 仍走 `airsim.backup_make/backup_restore`，文件名与 Release 链接保 M8 | F50 一卡一事布局，宽屏并排窄屏上下；不搬 F50 `airsim-backup` 对象与 flowfix 卡 | 🟡 同上，真机待上线 |
| U29 | `call.js`、`sms.js`、`connect.js` | call 保存提示补 `aria-live`；sms 双卡选择按探测显隐、状态改彩色标签、已读回执区分 0 条、单卡确认文案不提发送卡；connect 取值补 null 兜底 | 无障碍 + 单卡兼容 + 空值不渲染成 "null"；不搬 glass 自绘开关、Tailscale 字段 | 🟡 同上，真机待上线 |

## 十二、B-4：客户端管理 r26 指纹模型（批次 8，daemon + rpcd + 前端）

| ID | 文件 | 改了什么 | 为什么 | 状态 |
|---|---|---|---|---|
| U30 | `internal/gateway/gateway.go`、`internal/gateway/early_media.go`、`internal/gateway/gateway_test.go`、`cmd/airsim-mu300d/pair_clients.go`、`cmd/airsim-mu300d/pair_clients_test.go`、`cmd/airsim-mu300d/pairing_test.go`、`cmd/airsim-mu300d/main.go` | 客户端标识改裸 SHA-256 hex 指纹；取消 16 名额上限；`RemoveClientIdle(owner)` 按指纹持久化移除；PCM/早期媒体准入先验指纹在册；移除带通话/音频会话守卫；loopback 守卫 + 64hex 严格校验；claim 非法密钥 400、配对码消费后 defer 关窗 | 跟 F50 r26 模型；修老模型「按序号删第 2 个误伤第 1 个」的坑；已移除设备不能再开音频会话 | 🟡 离线 `go vet`/`go test ./...` 全绿、arm64 静态编译过；真机待上线 |
| U31 | `rpcd/airsim`、`overview.js` | `pair_remove` 参数 `indexes` → `ids`（64hex 逗号串），shim 逐个 POST `{"id":…}` 聚合计数；前端 `ids` 接线 | 配套 daemon 新模型；ACL 方法名不变 | 🟡 同上；**新旧二进制/前端不可混装** |

## 十三、前端玻璃层：移植 F50 `glass.js` 元件库（批次 9，方案乙）

| ID | 文件 | 改了什么 | 为什么 | 状态 |
|---|---|---|---|---|
| U32 | `m8-pkg/luci/www/luci-static/resources/view/airsim/glass.js`（新增）；同目录 `overview.js`/`connect.js`/`call.js`/`sms.js`/`maintenance.js`/`settings.js`；`m8-pkg/install.sh` 白名单、`m8-pkg/uninstall.sh` 清理清单、`scripts/check-package.py` 必需文件表 | 整份移植 F50 玻璃运行时库：`--g-*` token（浅/深两套）、卡面高光描边/镜面 `::before`/深投影/降级底色、自绘圆钮 `switch/switchSet/switchToggle`；六页 `require view.airsim.glass` 并在根节点注入 `glass.css()`。call.js「启用来电通知」改隐藏真值源 + 圆钮镜像，取值/填表/校验不动；维护/概览勾选、设置分段选择器保留原控件。装机脚本补 glass.js 白名单，卸载清单与闸二必需表同步 | A 档只搬了 ui.css/ui.js 基础口径，卡面反光/高光/投影与圆钮开关整层留在对端；真机对比 M8 卡片偏实发闷，用户选方案乙整份移植 | ✅ 真机六页渲染通过：概览 4 / 连接 5 / 来电 5（1 圆钮）/ 短信 / 维护 5 / 设置 4；计算样式实测 blur(24px) saturate(1.8)、26px 圆角、镜面反光层在位；闸零 4 补丁过、闸二现包 32 文件过，包/源码/设备 md5 三方一致。**改动全本地，未提交未推远端未传 Release**；真机配对全流程待手机验收。排障留档：本轮曾误判为前端回归，真因是内嵌 webview hidden 致 rAF 停摆、LuCI RPC 批量队列不 flush，详见 saves/2026-11-23-glass-port 快照 README |

## 十四、菜单接线补齐：六页挂自绘菜单，藏掉原生页签黑条（批次 10）

| ID | 文件 | 改了什么 | 为什么 | 状态 |
|---|---|---|---|---|
| U33 | `m8-pkg/luci/www/luci-static/resources/view/airsim/overview.js`/`connect.js`/`call.js`/`sms.js`/`maintenance.js`/`settings.js` | 六页根节点在 `glass.css()` 之后补 `(ui.menu ? ui.menu('<id>') : '')`，与 F50 各页挂载点一致；页码分别 overview/connect/call/sms/maintenance/settings。`ui.menu()` 原本就在 ui.js 里（A 档已搬），但六页根节点从没调用，自绘 `.as-menu` 没渲染，`body.as-menu-on` 类也没加上，ui.css 里藏原生 `#tabmenu`/`header .nav`/`footer` 的规则一直没触发 | 真机截图顶部一道实心黑横条：那是 LuCI 主题原生 `#tabmenu` 页签条；F50 靠自绘菜单挂出后加 `as-menu-on` 类把它藏掉。M8 漏了调用点，黑条一直留着 | ✅ 真机六页验过：自绘菜单 6 药丸、当前页蓝底白字高亮归属全对；原生 `#tabmenu` `display:none`；概览 4 / 连接 5 / 来电 5 / 短信 1 / 维护 5 / 设置 4 卡正常。设备/源码 md5 一致。**改动全本地，未提交未推远端未传 Release**；快照 `saves/2026-11-23-glass-port/pre-menu/` + `diff-menu-*.patch` |

## 十五、联网页整页移植：上网通道切换 + Wi-Fi 管理（批次 11，A 档整搬）

| ID | 文件 | 改了什么 | 为什么 | 状态 |
|---|---|---|---|---|
| U34 | `m8-pkg/luci/usr/libexec/rpcd/airsim-net`（新增）；`m8-pkg/luci/usr/bin/airsim-netmode`（新增）；`m8-pkg/luci/usr/libexec/airsim-net-probe`（新增）；`m8-pkg/luci/usr/libexec/airsim-flowfix`（新增）；`m8-pkg/luci/www/luci-static/resources/view/airsim/net.js`（新增）；`m8-pkg/luci/www/luci-static/resources/view/airsim/ui.js`；`m8-pkg/luci/usr/share/rpcd/acl.d/luci-app-airsim.json`；`m8-pkg/luci/usr/share/luci/menu.d/luci-app-airsim.json`；`m8-pkg/install.sh`；`m8-pkg/uninstall.sh`；`scripts/check-package.py`；`cmd/airsim-mu300d/main.go` | 整页整搬 F50 联网链路：rpcd 对象 `airsim-net`（screen/probe/status/mode_set/wifi_scan/wifi_saved/wifi_connect/wifi_forget/admin_status/admin_set/rebind/direct_status/direct_sync/flowfix_check/flowfix_fix）；模式切换脚本 cell/wifi/ap/restore + 自检回滚；只读能力探测；flowtable 体检（6.18 内核自动 skip）；前端四张卡（上网方式滑块、Wi-Fi 扫描/手输、已保存网络、管理页安全开关 + 直连端口/mDNS 自检）。M8 适配：probe 桥口改运行时 `brctl show` 探测（uci ports 为空不再误判）；蜂窝口按 modemmanager 取 `l3_device`；AT 探测包 `timeout 3` 防挂死；`mobile-data down` 兜底先探后调；ACL 并入 M8 单文件口径；菜单插「联网」序位；卸载清单补 airsim-bt 既有缺口 + 本批新文件；闸二白名单/必需表同步。daemon 补内部 `POST /rebind`（127.0.0.1:7581）：先建新 pair/control/PCM 监听再拆旧的，已建连接不断；重注册 Bonjour 主广告 TXT host；有进行中配对会话时不动配对代理广告 | 用户拍板 A 档整页整搬：F50 那道联网页 M8 此前没有；M8 真机 phy0 同时支持 STA/AP 双频，STA 上行硬件可行，后端原本空。高风险项（三档切换/运行期重绑/防火墙区）需真机真 AP 联调 | 🟡 离线：`go vet`/`go test ./cmd/airsim-mu300d/` 绿、arm64 交叉编译过；四脚本 `sh -n` 过；`node --check` net.js/ui.js 过；ACL/菜单 JSON 合法；闸零补丁过、账本闸过、闸二本地包 37 文件过。真机只读冒烟过：probe 桥口 `usb0`/`lan_ok=true`、modemmanager 口名取到、netmode 指纹 `wifi-pending`、shim `list`/`screen` 五块返回、flowfix 6.18 `skip`；前端 10 个 RPC 方法与 shim/ACL 三方对账无未授权。模式切换/连 Wi-Fi/rebind/防火墙放行未在真机真 AP 上跑过（SIM 未注册；新 daemon 已随 r23 装机，见 U35）。**改动全本地，未提交未推远端未传 Release**；快照 `saves/2026-11-23-net-port/` |

## 十六、r23 升号 + 本地打包（批次 12，联网页同批装机包）

| ID | 文件 | 改了什么 | 为什么 | 状态 |
|---|---|---|---|---|
| U35 | `VERSION`、`m8-pkg/VERSION`；`README.md` 安装示例包名 | 版本真源 `0.1.0.32-r22-m8` → `0.1.0.32-r23-m8`（两处一致）；daemon 带 `-X main.version=r23`、agent 带 `-X main.agentVersion=0.4.7-mu300.r23` 重编；新二进制归位 `out/linux-arm64/`（清掉旧 out agent 缺后缀遗留） | 批次 11 联网页 + daemon `/rebind` 要同批装机，先升号出本地包；调试阶段不推远端不传 Release | ✅ 真机装机过：服务 running、`audio_available=true`、agent `0.4.7-mu300.0.1.0.32-r23-m8`、MANIFEST r23、`airsim-net` 等五个 ubus 对象在册；闸零/账本闸/闸二本地包 37 文件过；`go vet`/`go test ./...` 绿。模式切换/连 Wi-Fi/rebind/防火墙放行未在真机真 AP 上跑过。**改动全本地，未提交未推远端未传 Release**；快照 `saves/2026-11-23-r23-pack/` |
| U36 | `m8-pkg/luci/usr/libexec/rpcd/airsim-net`；`m8-pkg/luci/www/luci-static/resources/view/airsim/net.js` | net_screen 聚合 probe 补下发 `band_count`；前端新增 `dualBand = band_count >= 2` 判定：双频时机顶药丸「Wi-Fi 双频」、扫描副标题/限制区改双频口径（2.4G 与 5G 都扫），单频 5G 机型保留 F50 原信道文案 | 真机扫描发现 M8 phy0 双频（Band 1 2.4G + Band 2 5G 都在），但页面照搬 F50 单射频「只在 5G 工作、2.4G 连不上」口径，与硬件不符；probe 早算出 band_count 却没传到前端 | ✅ 真机热推 + rpcd 重启：`screen` 回 `band_count:2`，药丸/副标题/限制区双频文案在位，扫描列表 5G ch36/149 与 2.4G ch8/10/11 都出；两文件 md5 源码/设备一致；闸二重出包 37 文件过。真实关联 AP/拿地址/rebind 仍待真 AP 联调。**改动全本地**；快照 `saves/2026-11-23-net-dualband/` |
| U37 | `m8-pkg/luci/usr/libexec/rpcd/airsim-net`；`m8-pkg/luci/usr/libexec/airsim-net-probe` | 删掉 F50 单射频遗留的两段频段约束：5G 定向频率扫描分支、`allowed_chan` 信道过滤（`filtered` 字段保留恒 0）；probe 新增 `bands`（按 phy 频率段判 24g/5g/6g，不信 uci band），`band_count>1` 时 `band_label=双频`；screen 聚合补下发 `bands` | 用户明确要求 5G/2.4G 不限制频段；uci `radio0.band=5g` 只是 AP 基线配置，不能当 STA 上行的频段牢笼 | ✅ 真机热推：probe `bands="5g 24g"`、`band_label=双频`；`wifi_scan` 8 热点 `filtered=0`，5G ch36/149 与 2.4G ch1/3/5/8/10 都出，页面无过滤提示；两文件 md5 一致；闸二 37 文件过。AP 热点自身双频开放未动；真实关联/拿地址/rebind 待真 AP 联调。**改动全本地**；快照 `saves/2026-11-23-net-nobandlock/` |
| U38 | `m8-pkg/luci/usr/bin/airsim-netmode` | 新增 `radio_dis/radio_on/radio_off/radio_sync`：AP 与 STA 都关才关 radio 总开关，任一路由在用即放开；save_base 增记 `prev_radio`，rollback/restore 按基线恢复 radio；do_cell/do_wifi（待配置分支+主分支）/do_ap（两段）/rollback/do_restore 在 set_ap/set_sta 之后、wifi_reload 前调 radio_sync | 真机点连接报 `XHR request timed out`：根因是 M8 固件基线 `radio0.disabled=1`，原脚本只翻 iface 段 disabled、从不碰 radio 设备段；netifd 在 radio disabled 时不建 netdev（wwan0 NO_DEVICE、wpa 无 per-iface 实例），check_wifi 空等 30 秒回滚，前端 20 秒 RPC 窗口先超时。手动开 radio 后 6 秒内 wwan0 生成、关联拿地址 | ✅ 真机复现故障态（radio 关/无无线口/wwan down）后跑新脚本：16.73 秒 rc=0，wwan0 关联 PDCN 5G ch36（-54 dBm），DHCP 192.168.50.30/24，默认路由落 wwan0，指纹 `wifi`；设备/源码 md5 一致。cell/ap 真实切换仍待 SIM/真 AP；**改动全本地，未提交未推远端未传 Release**；快照 `saves/2026-11-23-net-radio/` |

### 设备侧修复记录（重刷会复现，现已随包装自动补）

- 现象：`/etc/init.d/e5-bt start` 直接失败，`hciconfig hci0 up` 报 `Can't init device hci0: Address not available (99)`，`BD Address: 00:00:00:00:00:00`。根因是 `/usr/libexec/m8-bluetooth-address` 在 `/mnt/e5-data/misc/bluedroid/bt_config.conf` 缺失时抛 `ValueError`，而本机 `/mnt/e5-data` 整个不存在（厂标地址为全 0）。
- 处理：给 `/lib/firmware/sprd/marlin3lite_pskey.bin` 偏移 20 的 6 字节补一个本地管理地址 `02:50:00:00:e5:02`（= `br-lan` 末字节 +1），改前备份 `…pskey.bin.bak-000000000000`，改完 fsync。
- 结果：`e5-bt start` 正常，`hci0 UP RUNNING`；Wi-Fi/`br-lan` 未受影响。**重刷固件或恢复厂标后需要重做这一步。**
