# AirSIM 通话网关 · OpenWrt 手机适配（VIPME-M8 / UMS9620）

把 AirSIM 的通话网关（配对、控制面、通话音频桥、短信）装进一台跑 OpenWrt 的安卓手机，
让 iPhone 在同一局域网里用这台手机的 SIM 卡打电话、收短信。

**安全边界（本项目的硬约束）**：不刷机，不写 boot / bootloader / 分区表 / modem NV，
不改内核、不装为别的内核编译的音频模块、不覆盖厂商脚本；所有落地文件都在自己的命名空间里，
一条命令可完整卸载。

- 上游项目：[kai-wu-cortex/airsim-mu300](https://github.com/kai-wu-cortex/airsim-mu300)
- 本适配仓库：<https://github.com/shangjie1998/airsim-m8-openwrt>
- 当前已验收版本：**`0.1.0.26-r22-m8`**（详见 [版本口径](#版本口径)）

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
- 轮询：`/usr/libexec/airsim-m8/call-forward` 每 `poll` 秒（默认 2）**对在位卡槽逐个**执行
  `MU300_SIM_SLOT=<n> /usr/libexec/unisoc-modem/at AT+CLCC`，按 `dir` / `stat` 判状态：
  `dir=1` 来电、`stat 4/5` 振铃、`stat 0` 通话中；**哪一槽返回了 `+CLCC` 就说明这通电话落在哪张卡**
  （一条 `AT+CLCC` 只能看到一张卡，所以两槽必须分别查；Agent 的 `AT` 走 `mmcli`，只对着主卡，
  副卡来电它根本看不见，不能作为数据源）
- 事件：`ringing` → `answered` → `ended` / `missed`，按各自开关投递；结果写
  `/opt/airsim-m8/data/call-forward-history`（30 条）
- 投递失败（通话起呼瞬间数据面常会短暂中断，`http_code=000`）不丢事件：压入
  `/opt/airsim-m8/data/call-forward-queue`，之后每轮补发一条，超 10 分钟未成功才丢弃
- 配置：`uci airsim-call-forward.forward` —— `url`、`method`、`content_type`、`timeout`、`poll`、
  `expect`、`notify_ringing`、`notify_answered`、`notify_ended`、`notify_missed`、`body`
- 设备信息：本机号、在位卡槽与各槽本机号（`AT+CCID` / `AT+CNUM`）、运营商与信号、开机时长
  整组缓存 5 分钟（`/tmp/call-forward.info`），**通话期间不做这些查询**
- 正文占位符：`{number}` `{time}` `{tag}` `{slot}` `{self}` `{operator}` `{signal}` `{uptime}` `{device}`；
  `{slot}` 展开为 `卡1` / `卡2`，`{self}` 取**来电那张卡**的本机号；
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
scp "airsim-m8-0.1.0.26-r22-m8.tar.gz" root@192.168.9.1:/tmp/

# 2. 设备侧解包安装
ssh root@192.168.9.1
mkdir -p /tmp/m8pkg && cd /tmp/m8pkg
tar xzf "/tmp/airsim-m8-0.1.0.26-r22-m8.tar.gz" -C /tmp/m8pkg
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

> **双卡已支持（0.1.0.26 起）**：两张卡同时插时，来电通知能标出落在哪张卡（正文 `卡槽: 卡1/卡2`，
> `{self}` 取来电那张卡的本机号）。做法是对在位卡槽逐个 `MU300_SIM_SLOT=<n> ... AT+CLCC`，谁返回谁就是。
> 真机验证：卡槽0 来电 `1,1,4,...` 只出现在 slot0；卡槽1 来电只出现在 slot1，通知均 `sent|200`。
> 注意：**Agent 的 `/api/calls/status` 看不到副卡来电**（它的 `AT` 走 `mmcli`，只对一个 modem 生效），
> 所以来电通知不再以 Agent 为数据源，Agent 仅作为 `at` 适配层缺失时的兜底（此时没有卡槽信息）。

| 现象 | 原因 | 状态 |
|---|---|---|
| 通话结束那一瞬 `aplay` 报 `pcm_write: I/O error`，上下行计数冻结，两个 DSP 开关自动回 `off` | **不是故障，是收尾**：对方挂断后语音路由被释放，桥侧写最后一帧必然拿到 I/O error。此前误判成"通话约 50 秒断链"，实际与通话时长无关 | 已解决：按挂断流程处理（`AT+CLCC` 里通话消失即停流），不再当错误 |
| 在**手机屏幕上接听**的来电，采集拿不到下行（`arecord` I/O error / 全零帧）；桥自己发起的通话正常 | 原生接听态独占了采集路由 | 未解，改用 iPhone 接听可正常双向 |
| 屏幕偶尔闪白/黑屏 | 面板 ESD 看门狗循环复位（内核日志 `esd check failed, read value = 0x00`），`/sys/kernel/debug` 下无任何 `*esd*` 节点 | 无软件开关，断电重启恢复 |
| LuCI 显示"未注册"，但 ModemManager 报 connected | 纯展示差异 | 已知 |
| 改设备名后 iOS 还显示旧名 | 配对记录存在 iOS 本地，删设备重新配对才刷新 | 已知 |

---

## 8.1 版本更新说明

### v0.1.0.30-r22-m8（2026-11-09）

> **⚠️ 云端通话仍不可用**，沿用 v0.1.0.29 的结论：媒体能建链、首帧下行 PCM 能发出，但整通电话接不通，
> iPhone 停在「模块未确认接听」。日常请走本地 VoWLAN。

**做了什么**

- **「管理与维护」页面新增「备份与恢复」**：
  - 备份：一键把插件配置（`/etc/config/airsim`、`airsim-call-forward`）、配对凭据
    （`agent-data/`）与运行状态（`data/`）打成 `.tar.gz` 交给浏览器保存到电脑；
    日志、音频、短信正文、号码都不进包。
  - 恢复：把文件拖进虚线框或点「选择备份文件」，读取后仍需**二次确认**才会覆盖；
    覆盖前自动把现场另存到 `/tmp/airsim-before-restore-<时间戳>`，恢复完成后自动重启电话服务。
  - **机器校验**：备份包内写入本机硬件指纹（eMMC CID + 设备树序列号 的 SHA-256 前 8 位）。
    恢复时先比对指纹，不匹配直接拒绝并回显「文件那位 / 本机这位」两个短标识。
  - 安全边界：解包前校验 gzip 完整性并做路径白名单（只允许 `meta.json`、`config/` 下两个配置、
    `agent-data/`、`data/`），包体超过 8 MB 直接拒收。
- **「管理设置」两个按钮统一**：Relay 地址下方「填入官方默认」「改为跟随客户端」由蓝字文字链
  改为与页面一致的灰底次要按钮（同圆角、同高度），「保存设置」仍为蓝色主按钮。
- **「维护工具 → 插件更新」改指自建仓库**：「打开项目 Release」与后端检查更新都改为
  `shangjie1998/airsim-m8-openwrt`，不再指向上游仓库。

真机验证：

```
备份：airsim-m8-backup-20261109-1836.tar.gz（1013 字节），meta 指纹 61ddca24
恢复：本机备份 → ok=true，服务重启；篡改指纹的备份 → 拒绝（file_id deadbeef / local_id 61ddca24）
页面：备份/恢复全链路走通（下载、拖拽选文件、二次确认）；管理设置两个按钮样式已统一
更新：检查更新返回 no_release，提示「M8 为整包发布，此处不做在线安装」；Release 链接实测指向自建仓库
```

**改了什么**

- 来电通知默认口径：新增安装时默认只勾选「来电时推送 · 一响铃就发」，接通 / 挂断 / 未接收默认不勾
  （`notify_ringing=1`，其余 `0`）。
- 备份恢复的路径白名单原先把自己打的包也挡掉了（tar 根条目 `./` 与 `./config/` 目录条目未放行），已修正。
- 版本号解析支持 `-m8` 后缀（`^v?x.y.z-rNN(-m8)?$`），自建 Release 的 tag 能被正确识别。
- 版本串 `0.1.0.29-r22-m8` → `0.1.0.30-r22-m8`。

**未修复问题**

- **云端通话接不通**（顶部红字说明）：媒体能建链、首帧下行 PCM 能发出，但 iPhone 停在
  「模块未确认接听」，整通电话不成立；日常走本地 VoWLAN。
- Go 单测仍有 5 条既有红，与本次改动无关，未在本版处理。

**本版未包含的功能**

- 网页内「下载并更新」在线安装：M8 是整包 `tar.gz` 发布，本版仅把检查更新指向自建仓库并给出
  正确提示，不做在线安装（请在 Release 页下载整包手动安装）。
- iOS 分卡信号显示、上游 R23 的 Tailscale/私网 IP 配对，均未移植。

---

### v0.1.0.29-r22-m8（2026-11-09）

> **⚠️ 本版云端通话不可用。**
> 下面几条网络修复只把"设备能连上中继媒体"这一段走通了（实测媒体建链成功、首帧下行 PCM 已转发），
> 但整通电话仍然接不通：iPhone 端一直停在「模块未确认接听，正在等待 Agent 控制通道恢复」，
> 接通后手机侧也没有声音。**日常使用请走本地 VoWLAN（同一局域网直连），那条路径正常。**

**修复**

- **云端媒体/心跳的 TLS 预算**：媒体拨号 `TCP 8s → 15s`、`TLS 握手 8s → 30s`。这条蜂窝链路
  实测 TLS 要 4.6～7.7 秒，原来 8 秒预算几乎每通都踩线，媒体建链频繁失败。
- **中继地址解析改成并发**：原来先串行查三个自定义 IPv4 DNS（1.1.1.1 / 8.8.8.8 / 223.5.5.5，
  每个 4～5 秒、经常整体超时），改成"系统解析结果并发拨号，谁先通用谁"。
- **保留 `SO_BINDTODEVICE` 绑 `sipa_eth0`**：实测不绑网卡 3 次全挂（12 秒超时、TCP 都没建起来），
  绑了之后 TCP 0.3 秒即可连上。这条属于必需项，不要去掉。
- 心跳失败频率显著下降：修前每隔两三分钟一条 `Agent 云端心跳失败`（TLS 握手超时），修后稳定。

**新增**

- **移植上游 R23 的云端接收缓冲协商**（`audio_buffer_version=1`）：自动维持 200–600ms、
  上限 1000ms；一个通话身份只选一次，跨 WebSocket 重连保留；只作用于 iPhone → 设备的下行。
- **云端拨号请求日志**：记录 `number` 与 `sim_account` 原值，用于区分"客户端没传卡号"与"设备没兑现"。
- **拨号默认卡兜底**：`sim_account` 为空时落到默认语音卡（SIM1），不再沿用 `AT` 上下文残留。

**已知问题**

| 问题 | 现状 |
|---|---|
| **云端通话接不通** | 媒体能建链、首帧 PCM 能发出，但 iPhone 停在「模块未确认接听」且无声音。根因在中继侧（国外 Cloudflare 节点 + 蜂窝），设备端已把预算放到极限 |
| **云端拨号选卡无效** | iOS 云端拨号请求体里没有 `sim_account`（只有 `device_id/command_id/type/number/target_agent_id`），设备只能落到默认卡。真修复需要 iOS 补字段，本次不动 iOS |
| 云端媒体 `answer_to_listen` 偏慢 | 实测 6.77 秒（接听到首帧声音），加上上述不可用问题，体感就是"打不通" |

详见 `docs/PENDING-cloud-dial-missing-sim-account.md`。
网络相关改动与上游的差异、以及"将来换成自建服务器后哪些该回落"，见
`new-chat-6/m8-airsim/saves/2026-11-09-network-tuning/README.md`。

---

## 9. 许可与致谢

- 上游 [airsim-mu300](https://github.com/kai-wu-cortex/airsim-mu300)：Apache-2.0，作者 **kai-wu-cortex**
- 本仓库的适配改动、补丁、安装器、本文档同样按上游许可提供
- 没有上游这套协议实现和 LuCI 页面，这台手机接入 AirSIM 这件事根本不存在起点；本项目只做了
  "把它落到另一种内核和音频路由上"的那部分工作
