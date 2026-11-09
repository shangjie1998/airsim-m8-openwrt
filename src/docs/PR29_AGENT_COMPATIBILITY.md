# PR #29：U30 Air 设备端接口适配（R21 开发预览）

日期：2026-11-04。基于已发布 R20（`9b32e4eb44a736f220911696023b8964d2894f55`），
只更新集成仓库和固定 Agent 的隔离补丁。AirSIM 客户端及上游工作树未修改。
对应已合并的 AirSIM PR #29：最终 PR head
`7545484846f3852041f636cd1f383d29bf6edebe`（已核对本地评审源码），
合并提交 `5e32b602aad3e7440549b0d1968b30ab8a7711ef`（评审线程交接记录）。
已按 [最终协议说明](https://github.com/kai-wu-cortex/AirSIM/blob/7545484846f3852041f636cd1f383d29bf6edebe/docs/reviews/pr29-api-compatibility.md)
核对完整 generation、重复 held 接听、短信字段及发布顺序。
固定 Agent 基线仍为 `patches/AIRSIM_BASE` 的
`cc2ade3124ac34bec7d8c441f4b66a57a6dfec62`，未直接升级整个 Agent。

## 通话身份解析

新增 `POST /api/calls/resolve`，局域网经原 VoWLAN 签名认证入口：

```json
{"call_id":"推送中的原始 ID","call_uuid":"小写 UUID","generation":"正整数十进制字符串（也接受 JSON 整数）"}
```

- U30 Air 的 `call_id` 已是本地稳定的 `mu300-<UUID>`，`android_id` 兼容字段与之相同。
  推送的 `call_uuid` 是另一枚独立 UUID，不能直接当作本地控制 ID。
- 只接受 Agent 推送管理器实际生成并保留的完整三元组。没有号码、时间、基带槽号猜测路径。
- 校验三元组后重新读取 CLCC；快照失败或仅有宽限期保留的旧记录返回 409。
  在取得状态后再次校验三元组，避免解析过程中推送代际发生变化。
- 成功返回现有 `CallStatus` 结构。`active` 引用快照里真实存在的匹配来电记录，
  可处于 incoming / waiting / active / held；其他真实通话保留在 `calls`。
  不修改跟踪器默认 active 选择，也不发接听/挂断命令。
- 失效、结束、重启丢失映射、槽号复用均返回 409；缺失或无效三元组同样返回 409。
  generation 接受 JSON 整数和十进制整数字符串，范围为 1 至 9223372036854775807；
  缺失、null、boolean、小数、负数、零、溢出均返回 409，绝不默认成 1。
  JSON 语法错误、未知字段、尾随 JSON 返回 400；不支持的平台返回 404。
- 沿用现有推送管理器的单个最近来电三元组。新来电替换该记录后，旧三元组即使对应
  仍被保持的电话也返回 409，不猜测恢复。此改动不承诺多路云来电映射或跨重启恢复。
- 后续 answer 仍重新校验相同本地 ID 和新鲜基带状态；已 active / held 的同一 ID
  重复 answer 成功返回，不再发送 ATA / CHLD，不切换保持状态或音轨。

新增 VoWLAN POST 白名单 `/api/calls/resolve`、`/api/calls/history/ack`；均保留
现有 HMAC、时间窗、nonce 防重放和请求体签名。历史确认复用原 Agent 实现，
不开放其它控制方法或绕过认证。旧 R20 二进制仍没有这两个 LAN 路由。

## 短信字段

- 原生单卡收件箱消息及其 Relay `incoming_sms` 事件增加可选 `sim_slot: "SIM 1"`。
  这是此 MU300 单槽适配器的槽标签，不是 ICCID、卡身份，也不新增双卡能力。
- 云端 `send_sms` 解码可选 `sim_account` 并传入发送校验；不再静默忽略客户端选择。
- 缺字段/空字段仍走原有默认卡行为。显式指定时只接受现有单槽兼容句柄 `"0"`；
  其它不透明句柄、`"SIM 1"` 标签均拒绝，不能自动回退到默认卡。
- 显式句柄在空闲且通话状态可确认时，通过已有 AT transport 查询 `AT+CPIN?`，
  必须收到精确 `+CPIN: READY` 才可调用原生发送器。无卡、PIN 锁定、查询失败均不发送。
  这里只确认固定槽当前有可用卡，不提供跨换卡的 ICCID 绑定。
- 局域网显式选卡复用同一校验；原短信持久防重复发送、云 command_id 去重与未知结果保护保留。
- 未增加 `sim_slots` 多卡发现声明，未改原生短信内容、删除行为或编码设置。

## 接口边界与发布顺序

云端短信命令入口是 Relay `POST /v1/commands/enqueue`，`type=send_sms` 可带
`sim_account`；Agent 通过既有已认证命令连接接收此字段，不新增同名 HTTP 入口。
短信事件出口是 `POST /v1/events/sms`，本次在已有事件中附加 `sim_slot`。

联合上线顺序按最终协议执行：

1. 先完成兼容设备端 Agent 的安装与授权范围内验收。
2. 更新 Worker，使云端命令传递 `sim_account`、短信事件传递 `sim_slot`。
3. 再更新 iOS 客户端并完成端到端验收；此前不宣称云端选卡可用。

旧 Worker 可能忽略选卡字段；仅完成 U30Air 源码适配不代表云端链路已升级。
旧网关 history/ack 的 404 会被新 iOS 在进程内缓存，网关升级后需重新启动客户端再验证。
U30Air 未开放 `/api/calls/audio/record`，不因客户端缓存可选接口的 404 而增加录音能力。
“忘记设备时清理短信 SIM 偏好”属于已合并的客户端修复，本设备端不操作客户端缓存。

以上仅记录后续部署依赖，不表示已部署 Worker、已安装候选或获得新的实机操作授权。

## 验证与范围

已通过源码与仿真验证：

- 集成插件 `go test -race ./...`、`go vet ./...`。
- 修改后的隔离 Agent 以及从固定基线重新应用补丁的全新 Agent：Go race / vet。
- 全部 29 项 Node 测试；固定基线重建的 Agent 完成 Linux ARM64 静态编译。
- 新增测试覆盖准确三元组、旧 generation、未知 UUID、重启、快照缺失/失败、
  同一槽复用、另一路优先显示、重复 held 接听、无签名/篡改/nonce 重放、
  默认卡/已存在卡/未知句柄/无卡/PIN 锁定/查询失败及云命令去重。
- TLS 测试 Relay 确认 `sim_slot` 实际进入上行 JSON。

未做：设备安装、真实接听/拨号/短信、后台 PushKit → LAN → PCM 联合验收、
实际 Relay 部署兼容验证及 R21 完整包实机升级回归。
保持当前系统、内核、8 kHz PCM 与早期媒体行为。新增功能属于 R21 开发预览，不属于 R20。

待有范围授权后的人工验收：真实来电推送后 LAN 接听、旧推送对新通话拒绝、
同通话重复接听不扰动声音、历史确认、默认卡及明确选卡短信、无卡拒绝。
信令通过不能代替两端音频验收；不以本次测试扩大既有硬件支持范围。
