# 交接：r13 拨出通话「建立空窗」修复

> **后续复查更正（2026-11-01 14:10）**：直接读取基带发现振铃记录仍然存在，
> 但 `CLCC mode=3` 被仅接受 mode 0 的解析器过滤。下文原先的“基带不列出通话”
> 根因判断不能作为已确认结论。请以 [后续复查与修复记录](MU300_OUTGOING_FOLLOWUP.md)
> 为准；保留下文作为 DeepSeek 修改的历史记录。

状态：**代码完成、单元测试通过、已装到实机验证运行；真实通话验收未完成**。
日期：2026-11-01。范围：只改 MU300 设备端适配（`patches/AirSIM-mu300.patch`），未修改 AirSIM iOS/Android 客户端与上游工作树。

---

## 1. 现象

升级到 mu300-linux **v2026.10.30.1** 后，从 AirSIM 拨号给对方：

1. App 进入通话页，能听到回铃（对端确实在响）；
2. 约 2–3 秒后 App 自己退回拨号页；
3. 对端仍在响/或随后接通，但 App 里已经没有这通电话。

旧系统（v2026.10.26.1 + 当时的 audio extra）没有该现象。

## 2. 根因（实机证据）

历史观测摘要（已移除原始通话历史、时间及真实号码）：多次拨出在 `dialing` 阶段约 2–3 秒即被记录为结束，另有 `active` / `alerting` 记录。原始数据仅本地保留。

规律：

- 每次拨出后，记录在 `dialing` 状态只存活 **2–3 秒**就结束；
- 同一路通话在 **4–6 秒后**重新出现在 CLCC 里（`alerting` 或 `active`），但拿到的是**新的 UUID**；
- 客户端把 `ended` 事件（来自 history 的 `ended_at`）当成通话结束 → 退回拨号页；随后出现的新 UUID 无法挂回 CallKit 的通话。

结论：

1. **拨号回执是成功的**（否则不会有 `dialing` 记录，也没机会跑起通话页）；
2. 这颗 Unisoc 模块在「MO 呼叫建立：dialing → alerting」转换期间会**连续若干秒不把该通话列进 AT+CLCC**；
3. 原 `mu300ApplyCallPoll` 只要某一次成功快照里没有这通电话，就立即写 history + `ended`，没有去抖，也没有 ID 延续。

该缺陷与 `ATD`、号码格式、音频无关；音频链路不参与判断。

## 3. 改动

文件：`patches/AirSIM-mu300.patch`（唯一改动；补丁作用于 `out/` 隔离副本，不碰 AirSim 仓库）。

### 3.1 建立空窗宽容（核心）

`mu300_calls.go`

- 新增 `mu300CallSetupGrace = 10s`、`mu300CallEndGrace = 4s`，按通话**最后已知状态**选择（`dialing` 用 10s，其余 4s）。
- 后台轮询发现通话不在快照里时：
  - 第一次缺失：记下时间，**保留记录**（不进 history、不发 `ended`、`calls.Active` 不变）；
  - 宽限期内：继续保留；
  - 超过宽限期：才确认结束。
- 通话在宽限期内重新出现（同 slot、同方向、同号码）时沿用**原 UUID**，只更新状态。这样客户端看到的是同一通电话从 `dialing` 变成 `alerting`，不会拆成两通。
- 槽位被**另一个身份**占用（方向/号码变化、active→setup 的替换）仍然立刻结束旧记录——这是真实的通话更替，不做宽容。

`main.go`：新增 `mu300GoneSince map[string]time.Time` 字段（`a.mu` 保护）。

`calls.go`：`applyCallPoll` 的 MU300 分派改为传入 `confirmGone=false`。

### 3.2 控制路径不受影响

`mu300RefreshCalls()`（接听/挂断/DTMF/拨号前的即时刷新）保持 `confirmGone=false`，即**立即生效**。所以：

- 用户在 App 里挂断：走即时刷新，通话立刻结束，不等宽限期；
- 拨号前的「当前已有通话」检查也不受宽限期影响。

### 3.3 拨号回执加固（同类问题预防，非本次主因）

`mu300.go`

- 确认窗口 `4s` → `mu300DialConfirmationWindow = 10s`（低于客户端 15s 拨号超时）；
- 循环按墙钟计时，CLCC 轮询失败（`mu300-at: busy`/超时）只重试，不消耗窗口；
- 新增 `mu300DialedCall` / `mu300DialedNumberMatches`：CLCC 号码为空、或与拨出号码互为后缀（网络加/去国标前缀）都接受；
- 测试可用 `mu300DialWindow` 覆盖窗口。

### 3.4 测试

`mu300_test.go` 新增：

- `TestMU300SetupGapKeepsCallIdentity`：dialing 状态下连续空快照 + 通话重新出现，断言 UUID 不变、宽限期后才结束；
- `TestMU300DialConfirmsAfterSlowATChannel`：AT 连续 busy 后仍能确认拨号；
- `TestMU300DialAcceptsNetworkRewrittenNumber`：CLCC 返回 `+86…` 仍确认；
- `TestMU300DialReceiptAndIdempotency` / `TestMU300IncomingAnswerAndStaleHangup` / `TestMU300UncertainATDNeverRetries` 等原用例保持通过。

## 4. 验证状态

| 项目 | 结果 |
| --- | --- |
| Agent `go test -race ./...` + `go vet` | ✅ 通过 |
| 插件 `go test -race ./...` + `go vet` | ✅ 通过 |
| 补丁在干净基线 `git apply --check` | ✅ 通过（`scripts/prepare-agent.sh`） |
| 二进制构建 aarch64 | ✅ `out/linux-arm64/` |
| 实机安装 + 服务启动 + `/api/health` | ✅ r13 测试二进制运行中 |
| **真实拨号（振铃期间不掉线）** | ⛔ **未验证** |
| 对方接听 / 双向语音 | ⛔ 未验证（本改动不涉及音频） |
| 多路、长通话、反复挂断、云 Relay 路径 | ⛔ 未验证 |

## 5. 当时的验证方式

当时在 mu300-linux v2026.10.30.1 / aarch64 环境中，以 r12 APK 加 r13 Agent
热修复进行观察，没有重新打包 APK。该状态仅是历史证据，不能用于判断当前设备版本。
原始设备状态、备份路径及回滚命令仅在本地交接文件中保留；恢复时请使用该设备实际保存的可信备份。

## 6. 风险与边界

1. **真实挂断会晚 4 秒**才在 App 里结束（`dialing` 阶段误拨/被拒最多 10 秒）。主动挂断不受影响。
2. 若该模块的空窗在某些网络下**超过 10 秒**，仍会拆成两通。`mu300CallSetupGrace` 是单点常量，可调。
3. 修复在 Agent 内，LAN（VoWLAN）与云 Relay 两条路径同时覆盖；**不需要改客户端**。
4. 音频未纳入本次验收。注意 `mu300-audio-dsp status` 提示：extra 的驱动是按 `v2026.10.19.1` 内核构建的，与当前 `v2026.10.30.1` 的 5.4 内核**不是同一构建**（当前声卡仍在，说明走的是镜像自带驱动）。若要长期使用，应换匹配的 mu300-audio。
5. 设备上跑的是本地 r13 构建 + r12 APK，版本号不一致；发布前必须走正常打包流程（APK + SHA256SUMS + 版本号）。
6. 凭据与设备私有状态不随源码或 Release 发布。
7. 历史问题：当时 `prepare` 的版本白名单只认 `v2026.10.26.1`。后续源码已移除该分支，改为检查必需音频工具；缺少工具时提示安装适用于设备的音频栈，不自动安装旧模块。该策略随 r15 发布，历史实机记录不代表 r15 已安装，见 [安装兼容性说明](INSTALL_COMPATIBILITY.md)。

## 7. 下一步

1. **实机拨号验收（stage C/E）**：拨一个需要等待接听的号码 → 观察振铃期间不退回拨号页 → 接听后确认 App 状态变 `active` → 双方说话确认音频 → 挂断后 App 立即结束。
2. 观察命令（只读，设备上执行）：
   ```sh
   while :; do
     curl -s http://127.0.0.1:8575/api/calls/status |
       jsonfilter -e '@.active.id' -e '@.active.state' -e '@.history[0].id' -e '@.history[0].state'
     sleep 1
   done
   ```
   期望：dialing 期间即使 CLCC 短暂为空，`active.id` 保持不变；重新出现时 id 相同、状态变 alerting/active。
3. 通过后：`VERSION` 定版 → `sh scripts/package-apk.sh` 出 APK/SHA256SUMS → 更新 `docs/MU300_PLUGIN_IMPLEMENTATION.md` 与 README 的 r13 说明 → 提交。
4. 若空窗仍被拆：把 `mu300CallSetupGrace` 提到 15s 再测（注意 App 端拨号超时是 15s，拨号确认窗口不要超过它）。
5. 评估是否改用 URC（`NO CARRIER`/`^DSCI`）作为呼叫结束的独立佐证，替代纯 CLCC 轮询（`mu300-atd` 会把 URC 写进 `/run/mu300-at/urc/stty_nr0.log`，但当前 Agent 的 `mu300Command` 只解析命令应答）。

## 8. 重建与测试命令

```sh
go test -race ./...
go vet ./...
AGENT_TREE=$(sh scripts/prepare-agent.sh)
(cd "$AGENT_TREE/module/module-agent" && go test -race ./... && go vet ./...)
sh scripts/build.sh        # 产出 out/linux-arm64/{airsim-agent,airsim-mu300d}
sh scripts/package-apk.sh  # 需要 Docker + out/signing/airsim-dev.pem
```

## 9. 给下一个 Agent 的启动提示

> 先读 `HARDWARE_ADAPTATION.md`、`README.md` 和后续复查记录。先核对目标设备当前安装版本与可信备份，不要沿用本历史记录中的运行状态。真实拨号/接听须有设备所有者的明确授权；不得使用紧急号码。
