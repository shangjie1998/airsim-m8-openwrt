# R23 上游基线迁移评估（只读，2026-11-23）

## 结论

**不跟 r23 地基，M8 继续钉 r22 基线（`cc2ade3124ac34bec7d8c441f4b66a57a6dfec62`）。**
r23 两块新东西里，云端缓冲协商 M8 已经通过自有 Agent 补丁带回；剩下的 Tailscale 私网配对属于独立可选特性，等真机有需求再单独摘段移植，不值得为此换地基。

## 基线现状

- M8：上游基线 r22，Agent 补丁按 `overlay/m8/patches/manifest.tsv` 顺序打进隔离源码树；daemon（`src/cmd`、`src/internal`）由 `src/` 原版 + `overlay/m8/` 叠加合成。
- F50：基线 r23 + r26 客户端管理补丁；对照树在 `airsim-f50-openwrt/upstream/r23/META.md` 与 `src/plugin/`。

## r23 增量逐项对账

| r23 增量 | 内容 | M8 现状 | 处置 |
|---|---|---|---|
| 云端接收缓冲协商 | `audio_buffer_version=1`；自动 200–600 ms / 上限 1000 ms；stable 240–720 / 760；manual 240/480/720/960；按通话身份绑定、重连保留策略、挂断清空 | **已带回**：隔离 Agent 源码里已有 `cloud_audio_policy.go` 与 `cloud_audio_policy_test.go`（四档/上限/身份/重连/清空用例） | 无需再动 |
| Tailscale / 私网 IP 配对 | daemon `direct.go`（X25519/HKDF/AES-GCM 六位码、`route_proof` 签名、CGNAT/链路本地来源边界、隧道接口绑定与端口冲突回滚）；`main.go`/`management.go` 隧道配置；前端 `connect.js` 隧道接口输入校验、`overview.js` 隧道 IP 展示 | **未移植**：M8 daemon 无 `tunnel_interface`、`direct.go`、`route_proof`；前端无隧道入口 | 挂起（P4），不随本轮搬 |
| 其他 r23 杂项 | 云通话策略小修、安装脚本/打包调整 | 随 r22 基线已有对应 M8 自有实现 | 不单独追 |

## 为什么不换地基

1. **收益面窄**：换 r23 唯一的新能力是 Tailscale 配对；缓冲协商 M8 已自带。
2. **成本高且不可离线验证**：换地基要重做分层迁移、重打 M8 三个 Agent 补丁（e164、sms-callfix、fast-hangup、mu300 主补丁）、把 M8 专属改动（双卡 `sim_account`、16k 宽带播放、蓝牙栈、早期媒体 trial、云早期媒体状态）在新基线上重新落位，再全量真机重验。当前设备离线，无法验收。
3. **上游自己都没验收**：r23 发布说明与 F50 文档均标注「真实 Tailscale 两端配对、重连/网络切换、双向听感尚未验收」。在一条未验收的新地基上叠 M8 专属改动，风险叠加。

## 将来若要摘 Tailscale（独立任务，另走 S/R/D 分档）

- 摘 `direct.go` + `direct_test.go` 整文件移植；`main.go`/`management.go` 只接隧道配置读写与监听绑定段，不搬 F50 单卡/音频配件包改动。
- 前端只补 `connect.js` 隧道接口字段（校验：`tailscale0` 类名、非 LAN/lo）与 `overview.js` 隧道 IP 展示；留空即关闭隧道监听，LAN/Bonjour 路径不动。
- 前置条件：设备在线、两端 Tailscale 可互访；先离线 `go vet`/`go test`/arm64 编译，再真机两端配对 + 双向通话验收。

## 评估依据

- `airsim-f50-openwrt/src/plugin/docs/RELEASE_v0.1.0-r23.md`、`TAILSCALE_PAIRING.md`、`CLOUD_AUDIO_BUFFER.md`
- `airsim-f50-openwrt/upstream/r23/META.md`（r22→r23 差异文件清单）
- M8 隔离 Agent 源码 `out/agent-source.*/module/module-agent/cloud_audio_policy*.go`
