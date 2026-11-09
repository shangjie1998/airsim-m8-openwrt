# v0.1.0-r12 — 首个公开开发预览

> [!WARNING]
> **安装前必须按系统版本选包：R16 仅适用于 1031 之后的新系统版本；R12 仅适用于 1031 之前的旧系统版本。不得跨版本线混装，也不要仅因 R16 更新就升级旧系统。**
> 这里的 1031 指 mu300-linux 系统版本线，不是插件下载或安装日期。无法确认所属版本线时，先核对系统发布说明再安装；音频模块还必须通过内核、配置与 ABI 检查。

AirSIM for MU300 是 **OpenWrt LuCI 通话网关插件**，不是 Android APK，也不是刷机固件。

r12 延续已安装验证的 r11 通话/短信功能，完善发布文档、安装器与许可证，并修复
重复构建时 Go 误扫描生成的依赖源码目录的问题。

## 快速安装

仅适用于已安装 mu300-linux `v2026.10.26.1`、OpenWrt `25.12.6`、内核
`5.4.254-gb50db5b6224c` 的 aarch64 U30 Air 验证环境。其他设备请勿强装。
结束通话与短信任务，在设备 SSH 中运行：

```sh
cd /tmp
wget -O airsim-install.sh https://github.com/kai-wu-cortex/airsim-mu300/releases/download/v0.1.0-r12/install.sh
less airsim-install.sh
sh airsim-install.sh --check
sh airsim-install.sh --install
```

安装后打开 LuCI → 服务 → AirSIM，启动服务，获取配对码，在同一 LAN 的 AirSIM
客户端添加设备。首次启动可能从上游下载 DSP 固件。本 Release 不包含专有固件。
检查公钥指纹、手动安装与回退说明请见 [README](https://github.com/kai-wu-cortex/airsim-mu300/blob/v0.1.0-r12/README.md)。

## 包含内容

- 设备状态/服务管理、SIM 本机号码读取、限时配对与 Bonjour。
- 签名 VoWLAN 控制/PCM 网关、MU300 原生 AT 后端和单会话音频桥。
- LuCI 拨号、明确选中来电接听、明确选中通话挂断。
- 左下短信列表、未读计数、全文、发送确认与后台发送状态。
- 根目录 [HARDWARE_ADAPTATION.md](https://github.com/kai-wu-cortex/airsim-mu300/blob/v0.1.0-r12/HARDWARE_ADAPTATION.md)：给其他 AI Agent 的跨硬件通话适配指引。

## 资产说明

| 文件 | 用途 |
| --- | --- |
| `luci-app-airsim-0.1.0-r12.apk` | 签名 OpenWrt 安装包 |
| `install.sh` | 固定版本、平台检查、公钥指纹/包校验、空闲检查安装器 |
| `airsim-dev.pem` | 开发签名公钥；不包含私钥 |
| `SHA256SUMS` | Release 下载完整性校验 |
| `airsim-mu300-0.1.0-r12-sources.tar.gz` | 插件、Agent、依赖对应源码、构建信息与许可证 |
| `mu300-audio-5.4.tar.gz` / `mu300-audio-v1.0.5-SHA256SUMS` | 原上游 v1.0.5 固定依赖的字节级镜像；已包含在 APK 中，一般用户不用单独安装 |

## 验证与已知限制

- 插件 Go race/vet、隔离 Agent 测试、17 项 Node 测试通过；实机 r11 页面、短信列表和安装前检查通过。
- 以前实机测试已验证拨出/接听控制、短时 PCM，并由测试者确认收到清晰测试语音。
- **尚未完成** iPhone 全流程双向语音、呼叫等待媒体切换、30 分钟通话和反复挂断验收。
- 基带快照短暂缺失与通话 ID 连续性仍需实机回归；不得认为多路身份问题已完全解决。
- 曾观察到音频关闭路径内核异常；新生命周期实现不代表所有内核风险已消除。
- 网页控制不提供浏览器声音；短信仅设备本地管理，70 字限制，未擅自发送真实测试短信。
- 局域网媒体是认证后原始 PCM，不是 TLS 或端到端加密媒体；只在可信 LAN 使用。
- 原音频内核 build kit 下载失效；源码/生成脚本已保留，但音频驱动从零重建未在本次验证。

这是 **prerelease**，不是高可用/紧急通信方案。安装前请保留可信旧包与私有配置备份。

## 致谢与许可证

感谢 **dikeckaan** 的 [mu300-linux](https://github.com/dikeckaan/mu300-linux) 和
[mu300-audio](https://github.com/dikeckaan/mu300-audio)，以及 AirSIM、OpenWrt/LuCI、ALSA、
kanoqwq/e5-linux、Enceka 和相关驱动贡献者。原音频仓库目前不可访问，
[源码镜像](https://github.com/kai-wu-cortex/mu300-audio) 保留原作者署名。

独立适配工程，不是上游官方发布。自身/AirSIM 代码采用 PolyForm Noncommercial 1.0.0，
音频/ALSA 等分别保留 GPL/LGPL/MIT 许可证，详见仓库 NOTICE、THIRD_PARTY_NOTICES.md 和 SOURCES.md。

> 历史版本说明：本页描述 r12 的发布行为。当前源码已取消插件的系统/内核精确版本限制；已发布的 r12 文件不会随源码修改而变化。新策略见 [安装兼容性说明](INSTALL_COMPATIBILITY.md)。
