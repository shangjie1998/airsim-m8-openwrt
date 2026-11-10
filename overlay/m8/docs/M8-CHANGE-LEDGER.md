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
| P4 | 上游 R23 的 Tailscale / 私网 IP 配对 | 未移植 | ⏸ 未做 |
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

### 设备侧修复记录（重刷会复现，现已随包装自动补）

- 现象：`/etc/init.d/e5-bt start` 直接失败，`hciconfig hci0 up` 报 `Can't init device hci0: Address not available (99)`，`BD Address: 00:00:00:00:00:00`。根因是 `/usr/libexec/m8-bluetooth-address` 在 `/mnt/e5-data/misc/bluedroid/bt_config.conf` 缺失时抛 `ValueError`，而本机 `/mnt/e5-data` 整个不存在（厂标地址为全 0）。
- 处理：给 `/lib/firmware/sprd/marlin3lite_pskey.bin` 偏移 20 的 6 字节补一个本地管理地址 `02:50:00:00:e5:02`（= `br-lan` 末字节 +1），改前备份 `…pskey.bin.bak-000000000000`，改完 fsync。
- 结果：`e5-bt start` 正常，`hci0 UP RUNNING`；Wi-Fi/`br-lan` 未受影响。**重刷固件或恢复厂标后需要重做这一步。**
