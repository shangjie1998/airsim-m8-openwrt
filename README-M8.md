# AirSIM 通话网关 · OpenWrt 手机适配（VIPME-M8 / UMS9620）

把 AirSIM 的通话网关（配对、控制面、通话音频桥、短信）装进一台跑 OpenWrt 的安卓手机，
让 iPhone 在同一局域网里用这台手机的 SIM 卡打电话、收短信。

**安全边界（本项目的硬约束）**：不刷机，不写 boot / bootloader / 分区表 / modem NV，
不改内核、不装为别的内核编译的音频模块、不覆盖厂商脚本；所有落地文件都在自己的命名空间里，
一条命令可完整卸载。

- 上游项目：[kai-wu-cortex/airsim-mu300](https://github.com/kai-wu-cortex/airsim-mu300)
- 本适配仓库：<https://github.com/shangjie1998/airsim-m8-openwrt>
- 当前已验收版本：**`0.1.0.24-r22-m8`**（详见 [版本口径](#版本口径)）

> 感谢上游作者 **kai-wu-cortex** 开源 [airsim-mu300](https://github.com/kai-wu-cortex/airsim-mu300)。
> 本仓库的 daemon、Agent、LuCI 页面、AirSIM 协议实现全部来自上游；这里做的是"让同一套东西在
> 另一种内核、另一种音频路由的手机上跑起来"的设备侧适配。上游 Apache-2.0，本适配同样遵循。

---

## 1. 适配机型与环境（实测值）

| 项目 | 实测值 |
|---|---|
| 机型 | **VIPME-M8**（device-tree `VIPME M8`，`Spreadtrum UMS9620 SoC`） |
| SoC | 紫光展锐 / 中兴 **UMS9620**（arm64，与 F50、MU300 同 SoC） |
| 系统 | **OpenWrt 25.12.5 r33051-f5dae5ece4**，target `armsr/armv8`，arch `aarch64_generic`，包管理 **apk-tools 3.0.5** |
| 内核 | **6.18.54-m8-probe12**（自建，mainline 变体，非厂商 5.4） |
| 声卡 | `card0 = sprdphone-sc2730` |
| 通话采集 FE | `FE_ST_VOICE_C` = `hw:0,9`（8 kHz 单声道，带 DMA） |
| 通话播放 FE | `FE_ST_VOICE_PCM_P` = `hw:0,18`（按 16 kHz 宽带消费） |
| 混音开关 | `S_VOICE_CAP_C SWITCH`、`S_VOICE_PCM_P SWITCH`（`amixer -c 0`） |
| 调制解调器 | **ModemManager**（plugin `unisoc`，AT 端口 `wwan0at0`），IMEI `866766061031893` |
| 电话栈 | 厂商 `e5-voice-native` / `e5-sms-*` / `e5-modemd` 常驻 |
| 根分区 | `/dev/mmcblk0p74`（`e5root`，ext4，4.0 GB）—— 装机只动这里 |
| 网口 | `br-lan 192.168.9.1/24` |

关于固件来源：这台机器的 OpenWrt 是**社区为 UMS9620 移植的自建固件**（例：刷机包
`m8-openwrt-25.12.5-r20261106.183308-g3503709f-sadea10ac-k6.18.54`），内核由移植者自己编。
OpenWrt 版本号与上游主线对应，主线仓库：<https://github.com/openwrt/openwrt>（`r33051-f5dae5ece4` 可在
主线 tree 里查到）。**本项目不提供刷机包**，也不建议为了装这个网关去换固件。

> 换一台手机（哪怕同 SoC）不等于能直接装：采集 FE 名字、采样率、mixer 开关、调制解调器归属
> 都可能不同。第 6 节给了逐项核对方法和改哪里。

---

## 2. 装了什么（全景）

全部组件装进 `/opt/airsim-m8`，外加为让上游认识这台机器而加的几个小东西。

```
/opt/airsim-m8/
├── bin/
│   ├── airsim-mu300d          网关 daemon（8580/8581/8582 + 本机 7580/7581）
│   ├── airsim-agent            Agent（本机 8575，SIM/短信/通话/推送）
│   ├── sms-pool-sync.py        短信池镜像（e5-sms → /etc/mu300/sms-pool/msg）
│   └── sms-dash.py             LuCI 短信面板后端（读池 + 转 Agent）
├── data/                       运行态（agent-id、短信已读、发送防重发），权限 700
├── MANIFEST                    版本与安装时间
└── uninstall.sh                一键卸载（只删自有路径）
```

外部只增加这些（全部自有，不覆盖固件文件）：

| 路径 | 作用 |
|---|---|
| `/etc/init.d/airsim-m8` | procd 服务，三实例，开机自启、崩溃重启 |
| `/usr/lib/airsim/airsim-mu300d`、`/usr/lib/airsim/airsim-agent` | 软链（上游 rpcd / 监管逻辑写死这两个路径） |
| `/usr/bin/mu300-at` | AT 中转 shim → `mmcli -m any --command` |
| `/opt/mu300/bin/mu300-sms` | 发短信 shim → ModemManager messaging API |
| `/etc/mu300/sms-pool/msg/` | 短信池目录（Agent 读的接口） |
| `/usr/libexec/rpcd/airsim`、`/usr/libexec/rpcd/airsim-sms` | LuCI 的两个 ubus 后端 |
| `/usr/share/luci/menu.d/luci-app-airsim.json`、`/usr/share/rpcd/acl.d/luci-app-airsim.json` | 菜单与 ACL |
| `/www/luci-static/resources/view/airsim/{overview,manage,sms}.js`、`overview.css` | 上游原生页面 |
| `/usr/libexec/airsim-m8/call-forward` | 来电通知轮询脚本（`AT+CLCC` → 状态机 → webhook） |
| `/etc/init.d/airsim-call-forward` | procd 服务（START=97）；功能关闭时也常驻，开关不用重启 |
| `/usr/libexec/rpcd/airsim-call` | 来电通知的 ubus 后端（`call_get` / `call_status` / `call_save` / `call_test` / `call_clear`） |
| `/www/luci-static/resources/view/airsim/call.js` | 来电通知页面，版式与字段照原生短信转发页复制 |
| `/etc/config/airsim-call-forward` | uci 配置（段名**必须**是 `forward`，匿名段读不到） |
| `/opt/airsim-m8/data/call-forward-history` | 投递历史 30 条，每行 `时间戳\|事件\|结果\|HTTP码` |
| `/etc/config/airsim` | uci 配置 |

**故意不改的东西**：`agent_kind=mu300`（iOS 靠它识别设备类型）、Go module 路径、二进制名、
AT/短信 shim 名字、呼叫 ID 前缀 `mu300-`、升级检查 URL。改了会让客户端认不出来。

---

## 3. 每个功能调用了系统的哪里

### 3.1 通话下行采集（对端 → 手机 → iPhone）

- 打开 ALSA 采集端点 `hw:0,9`（`FE_ST_VOICE_C`），8 kHz / 单声道 / S16LE，period 640、buffer 1280
- 先打开混音开关 `amixer -c 0 cset "name=S_VOICE_CAP_C SWITCH" on`，再 `arecord`
- 每 20 ms 一帧（320 字节），送进 daemon 的 PCM 出口（8582，握手 `AIRSIMVWL1`）
- 关闭时把开关打回 `off`，异常路径也保证关回

> 这台机器上 `FE_ST_DUMP`（`hw:0,16`）**打不开**（`no backend DAIs enabled`），
> `/dev/audio_pipe_voice` 被厂商 `e5-voice-native` 常驻独占 —— 所以采集只能走 `FE_ST_VOICE_C`。

### 3.2 通话上行播放（iPhone → 手机 → 对端）

- 打开 `hw:0,18`（`FE_ST_VOICE_PCM_P`），**16 kHz / 单声道 / S16LE**，period 320、buffer 1920
- 先开 `S_VOICE_PCM_P SWITCH`，等一会儿，再 `aplay`（顺序反过来会立刻 `pcm_write: I/O error`）
- AirSIM 的帧是 8 kHz，这里经**线性插值升采样** 8k→16k，再写入 DSP

> 这一条是本次最大的坑：`FE_ST_VOICE_PCM_P` 虽然和 8 kHz 采集成对，实际按 **16 kHz 宽带**消费。
> 早期照抄"8k 直通"会让对端听到**两倍速、变调**的声音（频谱实测峰值整数倍）。改播放参数到 16k
> 后自然走上采样器，问题消失。

### 3.3 通话控制（拨号 / 接听 / 挂断 / DTMF / 通话列表）

- 通话状态：`mmcli --voice-list-calls`（ModemManager Voice 接口），解析 `+CLCC` 语义
- AT 指令出口：上游写死 `exec /usr/bin/mu300-at -t N -`（命令走 stdin，最后要 `OK`），
  本机的 `/usr/bin/mu300-at` 把它转成 `mmcli -m any --command '<AT>'`
- 具体命令：拨号 `ATD<号码>;`、接听 `ATA`、挂断/多方 `AT+CHLD=...`、DTMF `AT+VTS="d"`
- 每通电话有稳定 UUID（`mu300-<uuid>`），号码/方向变化会重新分配，避免复用旧身份
- 安全校验：控制前先刷新一次 CLCC；格式异常、重复槽位、无效编号一律拒绝，不发命令

> 挂断曾报"基带通话状态暂未完整确认"，根因是完整性校验要求"刷新条数 == 缓存条数"，
> 而缓存里本来会保留刚结束通话的 grace 缓冲，永远对不上。已改为"刷新成功即可控制"，
> 目标身份仍按 index 精确匹配。

### 3.4 短信接收

- 厂商栈 `e5-sms-receive` 把收到的短信写进 `/etc/e5-sms/inbox.json`
- `sms-pool-sync.py` 每 10 s 读一次，只取 `direction == "in"`，写成池文件
  `/etc/mu300/sms-pool/msg/<id>`：

```
dir: mt
id: <同文件名>
from: +86...
length: <正文字节的字节数>
received: <epoch 秒>
scts: <SCTS 时间串>

正文
```

- 契约很硬：字段是 `key: value`（**冒号加空格**，不是 `=`），`length` 必须等于**正文字节数**
  （中文按 UTF-8 字节算），否则 Agent 判为坏条目
- **不要试图从 ModemManager 收短信**：这台机器的 MM 只看得见自己发出去的，`AT+CMGL` 也是空的
  （`AT+CPMS?` 却报 `"SM",4,50`）；收短信归厂商栈
- 池文件被删不复活（`state` 记已写过 id），源侧消息消失则条目算过期删除

### 3.5 短信发送

- Agent → `/opt/mu300/bin/mu300-sms`，stdin 是 `号码\n正文`，输出必须匹配 `^sent (reference [0-9]+)$`
- shim → `mmcli -m any --messaging-create-sms="number=...,text='...'"` → `mmcli -s <SMS> --send`
- 正文必须**单引号包裹**，否则含空格/中文标点会被属性串解析器截断；正文里的 `'` 与换行会被剔除
- 发送成功后 shim 追加一条**发件池记录**（`dir: mo` + `received` + `scts`），
  LuCI 短信页据此显示"已发送 + 时间"；Agent 解析池时会**主动跳过** `dir: mo`，不会串成收到的短信
- Agent 的发送响应带 `timestamp`，客户端据此渲染发出气泡的时间
- 有 120 秒防重发锁（`/opt/airsim-m8/data/mu300-sms/send.json`），失败也上锁

### 3.6 本机号码（E.164）

- `AT+CNUM` 第二字段拿到号码，SIM 里常存裸 `86...`
- 上游 Go Agent 不补 `+`；上游 Android 侧用 `SubscriptionInfo.getNumber()` 是带 `+` 的 E.164
- 本适配加了 `normalizeSelfNumber()`：已有 `+` 原样、`00` 开头换 `+`、`86`+11 位补 `+`
- 实测 health 返回 `"phone_number":"+8613166773059"`

### 3.7 配对与局域网发现

| 端口 | 用途 | 范围 |
|---|---|---|
| 8580 | 配对（Bonjour 六位码） | 局域网 |
| 8581 | 控制面（AirSIM 客户端） | 局域网 |
| 8582 | PCM 音频（握手 → `AIRSIMREADY` → 双向 320 字节帧） | 局域网 |
| 7580 / 7581 | 内部音频桥 / 状态与 manage | 仅 `127.0.0.1` |
| 8575 | Agent（health / sms / calls） | 仅 `127.0.0.1` |

- mDNS：`_airsim-vowlan._tcp`，实例 `AirSIM-VIPME-M8-<id后8位>`，host `VIPME-M8.local`，
  TXT 里 `device_name=AirSIM VIPME-M8`、`model=VIPME-M8`、`agent_kind=mu300`
- iOS 配对后在"设备名"处显示的就是 `device_name` / `model`

### 3.8 LuCI 界面（用上游原生页面，未重构）

菜单：**服务 → AirSIM**（`admin/services/airsim`）

- 概览页 `overview.js`：服务/Agent/音频状态、通话列表、拨号/接听/挂断、配对码、本机号码
- 管理页 `manage.js`：读写 mode / relay_url / interface / 三个端口，诊断、版本与更新
- 短信面板 `sms.js`：分页列表、查看全文并标记已读、发送

后端挂 LuCI 自带的 rpcd，两个对象：

| 对象 | 方法 | 来源 |
|---|---|---|
| `airsim` | `status` `settings_get` `settings_save` `diagnostics` `updates*` `pair_start` `pair_cancel` `number_refresh` `start` `stop` `call_action` | 上游 `rpcd/airsim`，只把服务名换成 `airsim-m8`，启停改调 `/etc/init.d/airsim-m8` |
| `airsim-sms` | `sms_list` `sms_show` `sms_send` `sms_job` | 自写，以短信池为源，发送转 Agent `POST /api/sms/send` |

两个必须知道的坑：

- ACL 必须是**对象→方法数组映射**（`"read":{"ubus":{"airsim":["status"]}}`），写成数组会
  `-32002 Access denied`；改完 ACL **必须退出 LuCI 重新登录**（ACL 在登录时加载）
- 页面文件权限 `644`，否则 uhttpd 返回 403

### 3.9 服务形态（procd）

`/etc/init.d/airsim-m8` 起三个实例：

1. `daemon` —— `airsim-mu300d -data /opt/airsim-m8/data`
2. `agent` —— `airsim-mu300d -run-agent`，由上游 `runManagedAgent` 拉起 `/usr/lib/airsim/airsim-agent`，
   带 `AIRSIM_RUNTIME_PROFILE=mu300-openwrt`、`AIRSIM_VOICE_BACKEND=mu300`、
   `AIRSIM_MU300_MODE=cloud`、`AIRSIM_MU300_PCM_ADDRESS=127.0.0.1:7580`
3. `smspool` —— `sms-pool-sync.py`

停服务时会把两个 DSP 开关强制打回 `off`，避免服务停了但 mixer 路由还挂着。

---

### 3.10 来电通知（网页）

- 页面：LuCI → 服务 → AirSIM → **来电通知**，文件 `/www/luci-static/resources/view/airsim/call.js`；
  卡片结构、字段名、历史表格都照上游原生短信转发页（`mud-card` / `mud-fwd-*`）复制，不自创版式
- 后端：`/usr/libexec/rpcd/airsim-call` 提供 `call_get` / `call_status` / `call_save` / `call_test` / `call_clear`
- 轮询：`/usr/libexec/airsim-m8/call-forward` 每 `poll` 秒（默认 2）用 `mu300-at` 读一次 `AT+CLCC`，
  按 `dir` / `stat` 判状态：`dir=1` 来电、`stat 4/5` 振铃、`stat 0` 通话中
- 事件：`ringing` → `answered` → `ended` / `missed`，按各自开关投递；结果写
  `/opt/airsim-m8/data/call-forward-history`（30 条）
- 配置：`uci airsim-call-forward.forward` —— `url`、`method`、`content_type`、`timeout`、`poll`、
  `expect`、`notify_ringing`、`notify_answered`、`notify_ended`、`notify_missed`、`body`
- 设备信息：本机号取 Agent `/api/status` 的 `phone_number`，运营商与信号取 `mmcli`，
  开机时长读 `/proc/uptime`；整组缓存 5 分钟（`/tmp/call-forward.info`），**通话期间不碰 modem**
- 正文占位符：`{number}` `{time}` `{tag}` `{self}` `{operator}` `{signal}` `{uptime}` `{device}`；
  `{tag}` 展开为 `#CALL #CALL_IN` / `#CALL_ANSWER` / `#CALL_END` / `#CALL_MISSED`
- 默认**关闭**（`enabled=0`）；页面开关即生效，不重启服务，也不影响短信转发

## 4. 直接安装

前置条件（安装器会逐项检查，任一项不满足直接退出，不会装出个哑巴系统）：

1. root 身份
2. `/proc/asound/pcm` 里同时有 `FE_ST_VOICE_C` 和 `FE_ST_VOICE_PCM_P`
3. `mmcli`（ModemManager）存在
4. `/opt` 剩余 > 20 MB
5. 没有手工起的残留 `airsim-mu300d` / `airsim-agent`（否则 procd 会在 8582/8575 上撞车无限重试）

```sh
# 1. 把安装包传到设备 /tmp（scp 直写系统路径会失败，必须先落 /tmp）
scp "airsim-m8-0.1.0.24-r22-m8.tar.gz" root@192.168.9.1:/tmp/

# 2. 设备侧解包安装
ssh root@192.168.9.1
mkdir -p /tmp/m8pkg && cd /tmp/m8pkg
tar xzf "/tmp/airsim-m8-0.1.0.24-r22-m8.tar.gz" -C /tmp/m8pkg
sh install.sh
```

安装包在仓库 `dist/` 下（也见 [Releases](https://github.com/shangjie1998/airsim-m8-openwrt/releases)）。

装完脚本会打印三条状态：服务 `running`、daemon `/status`（看 `audio_available`）、Agent `/api/health`。
安装器与卸载器都会打印厂商 `/usr/libexec/rpcd/mu300dash` 的 md5，两次一致才说明固件文件没被动过。

## 5. 卸载

```sh
sh /opt/airsim-m8/uninstall.sh
```

停服务 → 取消自启 → 两个 DSP 开关打回 `off` → 删三个 shim、软链、`/etc/mu300`、`/opt/mu300`（空目录）、
`/opt/airsim-m8` → 重启 rpcd。厂商 `e5-voice-native` 不受影响，厂商 `mu300dash` 一个字节不碰。

---

## 6. 换一台手机：要核对什么、改哪里

给 AI 的用法：把这份文档 + 你的设备信息交给它，让它按下面顺序核对。**每一行都要用实测值，
不要照抄本文的端口名/开关名。**

| # | 核对项 | 怎么查 | 不一致时改哪 |
|---|---|---|---|
| 1 | 通话采集端点 | `cat /proc/asound/pcm`，找 `FE_ST_VOICE_C` / `FE_ST_DUMP` | `internal/audio/devices.go` 的 `seen[...]` 选择逻辑 |
| 2 | 采集采样率/声道 | `arecord -D hw:X,Y -r 8000 -c 1 -f S16_LE` 试录 + 频谱 | 同上 `CaptureRate/Channels/Period/Buffer` |
| 3 | 播放端点与真实采样率 | `aplay -D hw:X,Y` 推正弦，让对端录回来做 FFT | 同上 `PlaybackRate/Channels/Period/Buffer`（**倍数错就会变调**） |
| 4 | mixer 开关名 | `amixer -c 0 contents \| grep -i voice` | `internal/audio/session.go` 里的 cset 名字 |
| 5 | AT 出口 | `ls /usr/bin/mu300-at`；没有厂商栈就 `which mmcli` | `/usr/bin/mu300-at` shim 或 `devices.go` 的 `CallProbe` |
| 6 | 短信来源 | 有 `/etc/e5-sms/inbox.json` 就镜像它；只有 MM 就只能试 `AT+CMGL` | `sms-pool-sync.py` 的 `native_inbox()` |
| 7 | 短信池契约 | Agent 侧解析在 `module-agent/mu300_sms.go` 的 `parseMU300PoolMessage` | 字段名/`length` 语义必须与解析源码一致 |
| 8 | 端口与路径 | 端口是否被别的服务占用 | `main.go` / `init.d` / `packaging` |
| 9 | 服务名、菜单名 | 避免与固件同名文件冲突 | `install.sh` / `menu.d` / `acl.d` |
| 10 | 内核/固件差异 | `uname -r`、`/etc/openwrt_release` | 安装器的前置检查按机型放宽或加严 |

编译与二次开发的完整步骤见 **[docs/M8_BUILD_AND_ADAPT.md](docs/M8_BUILD_AND_ADAPT.md)**。

---

## 7. 版本口径

统一一个串：**`0.1.0.<N>-r22-m8`**

- `0.1.0`、`r22`：上游版本，跟上游一致，不动
- `<N>`：本地改动序号（版本第四段），**每打包发布一次 +1**，从 `1` 起
  早期用过 `(N)` 括号写法，但 GitHub 会在上传 Release 附件时把文件名里的括号改成点
  （`airsim-m8-0.1.0(2)-r22-m8.tar.gz` 变成 `airsim-m8-0.1.0.2.-r22-m8.tar.gz`，上传还会失败），
  所以改成了纯点号：`0.1.0.1`、`0.1.0.2`……
- `-m8`：设备名

```sh
sh scripts/bump-version.sh        # 自动 +1，并同步打包目录的 VERSION
```

包名 = `airsim-m8-<VERSION>.tar.gz`，MANIFEST = `airsim-m8 <VERSION>, installed <时间>`，
daemon `/status.version` 与 Agent `version`（`0.4.7-mu300.<VERSION>`）同串。

---

## 8. 已知限制（都能复现，已记录，不是"偶发"）

> **双卡未解决**：两张卡同时插时，设备版无法区分来电落在哪张卡（Agent 的 `callRecord` 无卡槽字段），
> 通知里只能给号码和时间，给不出卡槽。当前只在单卡（联通 `46001`）上验证过。
> 上游 PR 版已按 `MU300_SIM_SLOT` 逐卡查询 `AT+CLCC`，设备版待跟进。

| 现象 | 原因 | 状态 |
|---|---|---|
| 通话约 50 秒后 `aplay` 报 `pcm_write: I/O error`，上下行计数冻结，两个 DSP 开关自动回 `off` | 厂商原生语音栈在通话中途抢回 voice 路由 | 未解 |
| 在**手机屏幕上接听**的来电，采集拿不到下行（`arecord` I/O error / 全零帧）；桥自己发起的通话正常 | 原生接听态独占了采集路由 | 未解，改用 iPhone 接听可正常双向 |
| 屏幕偶尔闪白/黑屏 | 面板 ESD 看门狗循环复位（内核日志 `esd check failed, read value = 0x00`），`/sys/kernel/debug` 下无任何 `*esd*` 节点 | 无软件开关，断电重启恢复 |
| LuCI 显示"未注册"，但 ModemManager 报 connected | 纯展示差异 | 已知 |
| 改设备名后 iOS 还显示旧名 | 配对记录存在 iOS 本地，删设备重新配对才刷新 | 已知 |

---

## 9. 许可与致谢

- 上游 [airsim-mu300](https://github.com/kai-wu-cortex/airsim-mu300)：Apache-2.0，作者 **kai-wu-cortex**
- 本仓库的适配改动、补丁、安装器、本文档同样按上游许可提供
- 没有上游这套协议实现和 LuCI 页面，这台手机接入 AirSIM 这件事根本不存在起点；本项目只做了
  "把它落到另一种内核和音频路由上"的那部分工作
