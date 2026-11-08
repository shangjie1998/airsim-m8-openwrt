# M8 适配：编译、打包、微调指南

面向"拿到仓库、要给**另一台**手机（最好还是 UMS9620 系）编译或微调"的人，也面向被交办这件事的 AI。
按顺序做，每一步都有可验证的输出，不要跳步。

---

## 0. 结论先行

三件事决定了这套东西能不能在目标机上跑：

1. **通话音频的两个 FE**（采集 / 播放）叫什么、真实采样率是多少 —— 错了就是没声音或变调
2. **AT 与短信的出口** —— 有没有厂商 `mu300-at` / `mu300-sms`，没有就得走 ModemManager
3. **收短信的来源** —— 厂商栈的存储文件，还是 ModemManager（这台机器上后者拿不到收件）

其余（端口、页面、协议）跟着上游走，不用改。

---

## 1. 仓库里什么是上游、什么是本适配

| 路径 | 说明 |
|---|---|
| `cmd/`, `internal/` | 上游 daemon 代码，本适配改了三处（见第 2 节） |
| `packaging/` | 上游 F50/U30 Air 的部署方案（APK 形态），**与本适配无关**，别混用 |
| `patches/AirSIM-mu300.patch` | 上游 Agent 的 M8 平台补丁（AT 出口、短信后端、平台判断） |
| `patches/AirSIM-e164.patch` | 本机号码补 `+`（E.164） |
| `patches/AirSIM-m8-sms-callfix.patch` | 短信发送时间戳 + 挂断校验放宽 |
| `scripts/prepare-agent.sh` | 从固定上游 commit 导出 Agent 源码树并打三个补丁 |
| `scripts/build.sh` | 交叉编译 daemon + agent |
| `scripts/bump-version.sh` | 版本序号 +1，同步两处 VERSION |
| `m8-pkg/` | 安装包源目录（install/uninstall、shim、LuCI、页面、短信代理） |
| `dist/` | 已构建好的安装包 |
| `README-M8.md` | 使用说明（机型/版本/功能/安装/限制） |
| `docs/M8_BUILD_AND_ADAPT.md` | 本文件 |

上游固定 commit 记在 `patches/AIRSIM_BASE`，改上游版本时同步改这个文件和补丁。

---

## 2. 本适配改了上游的哪三处（改动机器时必须回头看的三段）

### 2.1 `internal/audio/devices.go` —— 端点与采样率

```go
// 采集：优先 DMA 型 voice 采集 FE
if v, ok := seen["FE_ST_VOICE_C"]; ok {          // 8k 单声道
    d.CaptureRate, d.CaptureChannels = 8000, 1
    d.CapturePeriod, d.CaptureBuffer = 640, 1280
} else if v, ok := seen["FE_ST_DUMP"]; ok {      // 上游 F50 路径：48k 双声道
    d.CaptureRate, d.CaptureChannels = 48000, 2
}
// 播放：本机 DSP 按 16k 宽带消费，8k 直通会变调
d.PlaybackRate, d.PlaybackChannels = 16000, 1
d.PlaybackPeriod, d.PlaybackBuffer = 320, 1920
// 有 S_VOICE_CAP_C / S_VOICE_PCM_P 开关才需要 "voice scene"
d.NeedsVoiceScene = d.CaptureChannels == 1
```

判定顺序说明：先看 `/proc/asound/pcm` 里有没有 `FE_ST_VOICE_C`；有就按 8k 单声道配置，
没有才回落到上游 `FE_ST_DUMP`。`_ = d.CaptureChannels == 1` 还用于判断"这是 mainline 构建"，
从而选 `mmcli` 而不是厂商 AT 包装。

### 2.2 `internal/audio/session.go` —— 直通判断与开关时序

上游有个"播放 8k 单声道就直通、不重采样"的快捷路径。本机播放端点真实是 16k，
这条快捷路径会把 8k 帧直接灌进去 → **对端听到两倍速、变调的语音**。
本适配把播放参数改成 16k，快捷路径自然失效，改走 `Upsampler.Convert`（线性插值 + 跨帧 continuity）。

同时这一层负责：先开开关 → 等 → `aplay` → 开采集开关 → 等 1.5 s → `arecord`；结束与异常路径都关回 `off`。

### 2.3 品牌显示名（只改文案，不改标识）

`internal/gateway/gateway.go`（health 的 `device_name` / `model`，iOS 配对后显示的就是它）、
`cmd/airsim-mu300d/main.go`（mDNS 实例与 TXT、`--status` 的 `product`）、`management.go`（UA/product）。
**保留**：`agent_kind=mu300`、Go module、二进制名、软链名、`mu300-` 呼叫前缀、升级检查 URL。

---

## 3. 工具链

Go 1.24.7（darwin/arm64 或 linux/amd64 都行，交叉编译到 arm64）：

```sh
# 官方 go.dev/dl 在国内很慢；实测阿里云镜像 10+ MB/s
curl -fsSL -o /tmp/go.tgz https://mirrors.aliyun.com/golang/go1.24.7.darwin-arm64.tar.gz
mkdir -p /tmp/go-sdk && tar xzf /tmp/go.tgz -C /tmp/go-sdk
export PATH=/tmp/go-sdk/go/bin:$PATH
export GOPROXY=https://goproxy.cn,direct
export GOFLAGS=-mod=mod
go version    # go version go1.24.7 darwin/arm64
```

Linux 机器换成 `go1.24.7.linux-amd64.tar.gz` 即可。

---

## 4. 编 daemon

```sh
cd airsim-mu300
VERSION=$(cat VERSION)
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -buildvcs=false -trimpath \
  -ldflags="-s -w -X main.version=$VERSION" \
  -o out/linux-arm64/airsim-mu300d ./cmd/airsim-mu300d
```

产物约 7.4 MB 静态 ELF，`file` 显示 `ELF 64-bit LSB executable, ARM aarch64`。

---

## 5. 编 Agent（必须先导出源码树 + 打补丁）

```sh
sh scripts/prepare-agent.sh
# 输出一个临时目录，例：out/agent-source.AeN3dw
```

脚本做的事：`git archive` 固定 commit 的 `module/module-agent` → `git init` 建基线 commit →
依次 `apply --check` + `apply` 三个补丁 → 从上游仓库取回 `go.mod` / `go.sum`。

> **坑**：上游 `module/module-agent/.gitattributes` 把 `go.mod`、`go.sum` 标了 `export-ignore`，
> `git archive` 不会导出它们。不补回来，`go build` 会报
> `cannot find package "github.com/damonto/euicc-go/lpa"`。脚本已经自动补，别手动删这两行。

```sh
cd out/agent-source.XXXXXX/module/module-agent
VERSION=$(cat /Users/.../airsim-mu300/VERSION)
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -buildvcs=false -trimpath \
  -ldflags="-s -w -X main.agentVersion=0.4.7-mu300.$VERSION" -o /tmp/airsim-agent .
```

> **坑**：不要在 `out/agent-source.*` 的父目录用 `go build ./...` —— 那棵树没有 `go.mod`，
> Go 会按外层 module 解析并报 `does not contain package`。必须 `cd` 进去、`go build .`。

---

## 6. 改一个功能后怎么走完整流程

```sh
# 1) 版本 +1（同步源码树与打包目录的 VERSION）
sh scripts/bump-version.sh

# 2) 重编受影响的二进制（改了 Go 代码就两个都编，简单可靠）
#    见第 4、5 节

# 3) 把产物放进打包源目录
cp out/linux-arm64/airsim-mu300d m8-pkg/build/airsim-mu300d
cp /tmp/airsim-agent             m8-pkg/build/airsim-agent

# 4) 打包
cd m8-pkg/build
tar czf "../airsim-m8-$(cat VERSION).tar.gz" ./

# 5) 装机
scp "../airsim-m8-$(cat VERSION).tar.gz" root@<设备IP>:/tmp/
ssh root@<设备IP> "rm -rf /tmp/m8pkg && mkdir -p /tmp/m8pkg && \
  tar xzf '/tmp/airsim-m8-$(cat VERSION).tar.gz' -C /tmp/m8pkg && cd /tmp/m8pkg && sh install.sh"
```

安装包目录结构（tar 顶层）：

```
airsim-mu300d  airsim-agent  airsim-m8.init  install.sh  uninstall.sh
mu300-at  mu300-sms  sms-pool-sync.py  VERSION
luci/...   （rpcd、menu.d、acl.d、view/airsim、etc/config/airsim、opt/airsim-m8/bin/sms-dash.py）
```

`m8-pkg/build/` 是 tar 的**唯一来源**：任何文件先在 `m8-pkg/` 里改，再打包。直接改解包后的副本会被下次
打包覆盖掉（已经踩过）。

---

## 7. 装机验证清单（每次都要跑）

在设备上执行，逐条比对：

```sh
# 服务与三个实例
/etc/init.d/airsim-m8 status
pgrep -af 'airsim-mu300d|airsim-agent|sms-pool-sync'

# daemon 状态：audio_available 必须 true；devices 在顶层，不在 audio 下
curl -s 127.0.0.1:7581/status | tr ',' '\n' | grep -E 'audio_available|Capture|Playback|product|version'

# Agent 健康：ok、号码带 +、AT 连续失败为 0
curl -s 127.0.0.1:8575/api/health

# 麦克风/播放端点是否真能打开（通话中才验得准）
amixer -c 0 cget "name=S_VOICE_CAP_C SWITCH"
amixer -c 0 cget "name=S_VOICE_PCM_P SWITCH"

# 短信：发一条给自己能收的号，确认返回带 timestamp、池里出现 dir: mo
curl -s -X POST -H 'Content-Type: application/json' \
  -d '{"phone":"+861xxxxxxxxxx","message":"m8 test"}' 127.0.0.1:8575/api/sms/send
grep -rl 'dir: mo' /etc/mu300/sms-pool/msg | tail -1 | xargs sed -n '1,7p'

# 通话：拨一个客服号，接通后挂断，必须返回 {"hung_up":true}
curl -s -X POST -H 'Content-Type: application/json' \
  -d '{"number":"10086","operation_id":"t-1"}' 127.0.0.1:8575/api/calls/dial
curl -s 127.0.0.1:8575/api/calls/status          # 取 id
curl -s -X POST -H 'Content-Type: application/json' \
  -d '{"call_id":"mu300-..."}' 127.0.0.1:8575/api/calls/hangup

# 固件没被动过（装前/卸后两次 md5 必须一致）
md5sum /usr/libexec/rpcd/mu300dash

# 卸载可逆
sh /opt/airsim-m8/uninstall.sh && ls /opt/airsim-m8 /etc/init.d/airsim-m8
```

---

## 8. 音频改坏了怎么定位（别靠听感）

对端听到"变调 / 急促 / 变身"这类描述无法判案，用频谱：

```sh
# 让对端把通话录下来（m4a 也行）
ffmpeg -y -i call.m4a -ac 1 -ar 16000 call.wav
python3 - <<'EOF'
import wave, numpy as np
w = wave.open("call.wav"); n = w.getnframes()
d = np.frombuffer(w.readframes(n), dtype=np.int16).astype(float)
sr = w.getframerate(); seg = sr // 2
for i in range(0, n - seg, seg):
    f = np.abs(np.fft.rfft(d[i:i+seg])); k = f.argmax()
    print(f"{i/sr:6.1f}s peak={k*sr/seg:7.1f}Hz")
EOF
```

- 峰值正好是"你实际上推的频率"的**整数倍** → 采样率不匹配（多半是 2 倍速）
- 峰值对得上 → 本地链路没问题，去查网络/射频

---

## 9. 换机型时的判断顺序（把这张表当 checklist）

| 现象 | 大概率原因 | 改哪 |
|---|---|---|
| `arecord` 报 `no backend DAIs enabled` | 这个采集 FE 在这台机器上没配 DSP 路由 | 换另一个采集 FE（`/proc/asound/pcm` 里找带 `c` 且相邻 `VOICE` 的） |
| 有声音但对端说变调/太快 | 播放端点真实采样率与配置不一致 | `devices.go` 的 `PlaybackRate`，让"直通"分支不成立 |
| 双向都静音 | 没开 mixer 开关，或开关名不同 | `amixer -c 0 contents \| grep -i voice` 查到真名后改 `session.go` |
| `aplay` 启动 1 秒就 `I/O error` | 开关顺序反了（先 aplay 后开开关） | 保持"开开关 → 等 → aplay" |
| AT 全部失败 | 没有厂商 `mu300-at`，或它不认 stdin | 检查 `devices.go` 的 `CallProbe`，必要时用自己的 shim |
| 收不到短信 | 短信不在 ModemManager 里 | `sms-pool-sync.py` 换镜像源（厂商存储文件 / `AT+CMGL`） |
| Agent 报池条目非法 | 池文件字段或 `length` 语义不对 | 对齐 `parseMU300PoolMessage`，`length` = 正文字节数 |
| LuCI 页面 `-32002 Access denied` | ACL 写成数组 | 改成对象→方法数组映射，退出 LuCI 重新登录 |
| 页面 403 | 文件权限不是 644 | `chmod 644` 页面文件 |

---

## 10. 绝不能做的事（会伤机器）

1. 不要刷机、不要改 boot / 分区表 / modem NV。所有落地只写根分区 `/opt`、`/etc`、`/usr` 下的自有文件。
2. 不要覆盖固件同名文件。**这条机器自带 `/usr/libexec/rpcd/mu300dash`**（厂商页面在用），
   所以本适配的短信后端叫 `airsim-sms`。同名覆盖不可恢复（没有 rom/overlay 备份）。
3. 不要在设备上 `ln -sf /dev/null /dev/null`（会变成自指软链，procd 全线报错）。
4. 不要远程 `reboot`（被安全策略拦截；重启会清空 `/tmp`，测试脚本要重传）。
5. 不要用 `nohup` / `pkill` / `sleep 0.7` —— 这台机器的 busybox 没这些；用 `setsid ... &`、
   `pgrep -f` + `kill`、整数 `sleep`。
6. scp 直写 `/usr`、`/opt` 会失败（只读或权限受限），一律先落 `/tmp` 再设备侧 `cp`。

---

## 11. 实测基线（换机型时用来对比）

daemon `/status`（关键字段）：

```json
{"audio_available":true,"product":"AirSIM for VIPME-M8","version":"0.1.0.1-r22-m8",
 "devices":{"Card":0,"Capture":"hw:0,9","Playback":"hw:0,18",
            "CaptureRate":8000,"CaptureChannels":1,"CapturePeriod":640,"CaptureBuffer":1280,
            "PlaybackRate":16000,"PlaybackChannels":1,"PlaybackPeriod":320,"PlaybackBuffer":1920,
            "NeedsVoiceScene":true,"CallProbe":"mmcli"}}
```

Agent `/api/health`：

```json
{"ok":true,"phone_number":"+8613166773059","platform":"mu300-arm64-openwrt",
 "at_device":"mu300-atd","at_consecutive_failures":0,
 "version":"0.4.7-mu300.0.1.0.1-r22-m8"}
```

短信池条目（收到）：

```
dir: mt / id: 1000000000 / from: +8615998104018 / length: 6
received: 1794052734 / scts: 2026-11-07T19:58:54+08:00

好的
```

短信池条目（发出，本适配新增）：

```
dir: mo / id: 1794109333 / to: +8615998104018 / length: 20
received: 1794109333 / scts: 2026-11-08T11:42:13+0800

M8 timefix 20261108
```

---

## 12. 音质基线（数值参考）

修复前后用同一段 44 s 通话录音做 FFT：

| | 用户推的频率 | 对端录音峰值 | 结论 |
|---|---|---|---|
| 修复前 | 200 Hz / 400 Hz | 400 Hz / 800 Hz（幅度压过所有人声） | 2 倍速 |
| 修复后 | 200 Hz / 400 Hz | 200 Hz / 400 Hz（mag 167~183） | 正确 |

判断标准：峰值与预期频率**偏差不超过 ±10 Hz**、且没有整数倍分量。
