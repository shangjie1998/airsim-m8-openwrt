# R18 设备端拨号回执交接（2026-11-03）

后续用户明确要求取消两分钟限制，并只在 U30Air 侧完成基带结束到 AirSIM 的通知。该后续修复已实现，当前行为及验收以 [R18_REDIAL.md](../R18_REDIAL.md) 为准。下文保留 8041906 初始交接记录，其中“保留两分钟保护”为当时状态，已被后续要求取代。

承接客户端 `2026-11-03-u30-r18-client-call-safety.md`。本轮只改独立 U30Air 工作树及固定 Agent 的隔离补丁，未修改客户端、系统、内核、DSP 或设备服务；没有真实拨号、短信、录音。R18 继续暂停发布。本轮候选未安装，设备仍运行此前 `45ec46c` 候选，客户端新修复也尚未在本线程验收。

## 已实现的设备端行为

- 对 `(已认证配对身份, operation_id)` 保存完整拨号占位、参数摘要及回执，不再只依赖进程内 map / 最近一次操作摘要。网关从通过 HMAC 校验的配对密钥派生内部身份，忽略调用方伪造的内部身份请求头。
- 原子写入、文件及目录 fsync 成功后才可能发送 ATD。崩溃、丢回复、回执保存失败留下 `unknown` 占位，同一操作不得再执行。确认的回执及精确 UUID 保留至重启之后；同 ID 改号码或 SIM 冲突。
- 参数、配对身份、操作 ID 只保存摘要；持久文件不存号码、密钥或通话音频。文件为 0600，目录为 0700。无自动淘汰，容量 4096；满、损坏、符号链接或写入失败时停止接受新的拨号，而不是清空后继续执行。
- 最终结束时间独立保留，历史 ACK 不能删除操作回执。重启后没有新鲜通话缓存时，查询的 `call_state` 为 `unknown`，不猜测通话已结束，也不按同号码认领其他电话。
- 拨号确认绑定 `mu300DialedCall` 匹配到的具体槽对应 UUID，不能把优先显示的另一通电话当成此拨号回执。
- 本地管理与 Relay 使用不同内部作用域。Relay 继续保留原有 `cloud-<command ID digest>` 兼容编号，并获得跨重启保存；尚未建立 Relay 身份与 WLAN 配对身份之间的可靠映射，不能宣称跨路由持久记录已统一。客户端“提交后不换路再拨”约束仍必需。
- **保留同号两分钟保护**，没有按新 ID、App 版本字符串或任意请求头绕过。此前自动重拨来源仍缺现场同步证据；手动立即重拨的联合验收未完成。

升级前历史操作只有旧版最近一次保护记录，不可能从新日志重建全部旧操作。新持久回执保障从该候选安装后的登记开始；保留旧 `dial.json`，不删除旧保护。

## 新增只读接口

```
GET /api/calls/operation?operation_id=<URL encoded original operation ID>
```

使用原有 VoWLAN 请求 HMAC（含完整 query、timestamp、全新 nonce）。网关按配对身份选择记录；未知操作/其他身份返回 404，记录错误返回 503，非法查询返回 400。404 不证明旧版本没有执行，客户端不能据此自动拨号。查询不执行 AT、不查号码匹配、不占用媒体、不重发命令。

200 示例：

```json
{
  "version": 1,
  "agent_id": "mu300-fixture-agent",
  "operation_id": "fixture-intent",
  "status": "confirmed",
  "android_id": "mu300-fixture-call",
  "backend_id": "mu300-fixture-call",
  "call_state": "ended",
  "ended_at": "2026-11-03T00:00:00Z",
  "operation_proof": "base64url-hmac",
  "status_proof": "base64url-hmac"
}
```

`status` 为 `confirmed` / `unknown` / `rejected`。`unknown` 是已登记但不能确认是否执行/归属，不能重发。`rejected` 是该操作未发送 ATD 就拒绝。确认回执只表示原通话的归属；当前状态必须看 `call_state`，不能仅据 `confirmed` 启动媒体或重新显示通话。

确认回执的 `operation_proof` 保留客户端现有 `AIRSIM-DIAL1` 格式。`unknown` / `rejected` 没有通话 ID 或拨号 proof，`call_state=unknown`。

`status_proof` 使用同一配对密钥作 HMAC-SHA256、base64url 无 padding。原文按下面九个字段依次以 LF 拼接，不额外添加 LF：

1. `AIRSIM-OPERATION1`
2. 此次 GET 的 nonce
3. `agent_id`
4. `operation_id`
5. `status`
6. `android_id`（缺失用空字符串）
7. `backend_id`（缺失用空字符串）
8. `call_state`
9. `ended_at`（缺失用空字符串）

最后字段为空时，字段分隔符使原文自然以 LF 结尾。客户端必须校验 nonce、已配对 Agent、原 operation ID、固定枚举以及 proof；不能只验证旧拨号 proof 就信任新增状态。该查询是新增后端协议，当前客户端交接只提出了需求，尚未确认已实现调用和 proof 验证。

## early media 状态

**未支持，未宣称可用。** 当前认证控制入口对 `/api/calls/early-media` 返回 404，`AIRSIMEM1` 不进入旧 `AIRSIMVWL1` 双向桥，不返回 READY，不启动 DSP/采集/播放。已增加回归，确保新版客户端探测可回到本地等待音，且新握手不会意外降级为双向媒体。

当前硬件证据只证明接通后的双向音频。缺少未接通时真实下行音源、receive-only 驱动生命周期、与原通话及配对身份绑定的授权/撤销路径。不能只取消 active 检查，也不能伪造 early_media capability 或运营商公告。后续需实现并单独验收客户端提出的 `AIRSIM-EARLY-MEDIA1` 授权和 `AIRSIMEM1` 会话。

## 验证与下一步

新增 Agent 回归覆盖跨重启同回执、多个旧意图/未知提交、写入失败、损坏/符号链接/满容量、参数及 SIM 冲突、并发、配对身份隔离、只读查询、已结束回执与错投其他通话。网关回归覆盖内部身份防伪、请求防重放、Agent/操作/原通话签名绑定、无能力 early media 安全回退。均用 fixture，非实机通话。

完整 Go race/vet（插件及重新导出补丁后的 Agent）和 29 项 Node 测试通过。硬件只读预检通过：指定 USB U30 Air 仍匹配原音频 ABI，通话、媒体、短信空闲。包核验记录另附本机候选状态，不能视为已安装。

下一步先安装并验证客户端新候选，再安排用户手动拒接与正常通话，采集对端拒接、基带事件、Agent revision、iPhone 结束时序。只有双方语义和无重复 ATD 证据充分后，另做可验收的解除两分钟保护方案；本轮没有宣称已经恢复立即手动重拨。
