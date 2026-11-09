# U30 Air 云端早期媒体 v1 协商对齐（R22 发布记录）

依据只读交接 `/Users/kyle/.codex/worktrees/voble/AirSIM/docs/handoff/2026-11-05-all-transport-early-media.md`，在已发布 R21 提交 `32c3cd1b6ab5ebcfd8f4c8deeb53b8c0456907c5` 之后维护设备端补丁。初次协议协作仅修改源码，未安装或发布。随后用户明确要求 commit、push 并发布 R22；本次仍不修改 AirSIM 客户端、上游工作树或 Relay，不安装、不重启、不拨号、不录音。

## 协议

- `answer` 可选携带整数 `early_media_version: 1`。必须有显式一致的 call_id / call_uuid / generation，才记为成功协商。先检查原始字段，不能利用旧协议补入的默认 generation 或缺省 UUID 授权早期媒体。
- 没有字段或不支持的版本，仍执行正常媒体订阅及等待接通，不进入早期下行。请求了不支持的协商时可返回同身份 `early_media_unavailable`。错误/过期通话身份继续拒绝控制。
- MU300 CLCC `dialing` / `alerting` 均映射到客户端未接通 DIALING 阶段；要求当前唯一 outgoing、当前 Relay 描述符绑定的本地拨号回执、新鲜 CLCC 及本通 native EARLYMEDIA 信号。多通并存拒绝全局下行采集。
- 沿用 R21 的内部 HTTP receive-only 源；该源只有采集到首帧并通过实时状态复查才返回 200 和 PCM。Agent 读到完整首帧后，先发送带完整身份的 `early_media_ready_v1`，再转发二进制帧。不是 Android 的 AIRSIMRX01 桥，但具备相同的只接收准入和首帧语义。
- 不把 early READY / PCM READY 当作接通。打开正常双向桥之前和握手之后都要求新鲜、实际 active/held；状态已变则不发送 active、不安装上行 socket。
- 源失败发送同身份 `early_media_unavailable`，继续等待接通，不挂断、不重拨。同一 WebSocket 只进行一次获准的早期采集尝试；未有 EARLYMEDIA 信号时等待，不把尚未就绪误报为可用。
- 早期流继续逐帧检查归属及状态；取消、结束、替换或多通并存立即撤销。已接通/结束/失去归属后不再发送迟到 early ready 或 unavailable 来覆盖新状态。清理与云端生命周期 HTTP 请求无依赖。

VoWLAN v1、租约、签名和网关握手未改。U30 Air 不增加 VoBLE 或 Android AudioRecord 能力；这些由客户端线程及其硬件适配端维护。

## 与已发布 R21 的区别

2026-11-05 已按用户明确要求重新发布 R21，Release/标签和设备候选不会因本次协作消息而撤回。原 R21 的云端早期媒体无需新 offer，并发送 `early_media`；R22 补丁改成显式协商及 `early_media_ready_v1`，因此必须与新版客户端联合验收。先前 R21 实机通过结论不能转移为本轮协议已验收。

## 验证与待验收

自动化覆盖完整/缺失/旧身份、版本协商、旧客户端正常等待、多通拒绝、源失败且信令不结束、首帧前的 ready 身份与顺序、接通前无上行、断开/替换退出，以及本机 TCP 模拟 PCM 握手期间状态变化：即使返回 READY 也不能提前 active；真实 active 后正常建立双向 PCM。

已从固定上游重新导出并成功应用补丁；隔离 Agent race/vet、插件 race/vet 与全部 29 项 Node 回归通过。真实客户端 v1 协商、运营商公告、拒接/忙线/无人接听、立即挂断、接通切换及重连仍待用户授权后手动实机验收。本轮不沿用此前候选安装或真实通话授权，不执行设备操作。

## 用户反馈与 R22 发布范围

2026-11-05，客户端线程报告 iPhone 170 未识别 R21 的 `early_media`，已在 171 兼容旧状态与 `early_media_ready_v1`。随后用户明确反馈“验收通过，commit push，然后重新发布 R22”。记录为当前通话体验通过；本线程没有安装新 v1 Agent，因此不能将反馈写成 R22 APK 或新协商路径已完成单独实机验收。R22 纳入本文件描述的协议补丁，R21 保留。完整 APK 升级、新协商路径、长时稳定性、多路及其他硬件仍按各自证据记录。
