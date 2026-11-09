# AirSIM for MU300 v0.1.0-r15 · OpenWrt LuCI 开发预览

本版汇总 r13/r14 的设备端管理与维护功能，并取消插件对系统、OpenWrt 和内核精确版本的安装限制。保持现有 AirSIM iOS/Android 协议，未修改客户端或上游工作树。

## 主要变化

- 安装、在线更新和默认启动不再使用固件/内核版本白名单；保留 aarch64 架构、必要工具、APK 签名、哈希和空闲检查。
- 默认启动复用设备已安装的音频栈，缺少工具时明确提示，不自动装入旧版音频模块；保留已有 `prepare.local`。
- 新增管理与维护页面：局域网/云端策略、Relay 状态与地址限制、LAN 接口及配对/控制/PCM 端口、脱敏诊断、版本说明。
- 默认每 6 小时检查更新，支持关闭自动检查和选择预览频道；用户关闭电话服务、确认后下载并签名校验安装，显示进度与结果，不自动安装或降级。
- 修复安装器把独立更新监控进程当作电话服务的问题。
- MU300 通话状态接受振铃 `CLCC mode=3`，短暂空快照保留通话 ID；加强过期目标、多路快照和重复拨号保护。
- 同步 README、安装/更新说明、AI Agent 硬件适配指南；历史排障文档已去除真实号码及原始通话历史。

## 快速安装

在 MU300 SSH 终端执行：

```sh
cd /tmp
wget -O airsim-install.sh https://github.com/kai-wu-cortex/airsim-mu300/releases/download/v0.1.0-r15/install.sh
cat airsim-install.sh
sh airsim-install.sh --check
sh airsim-install.sh --install
```

先结束全部电话及短信发送。需要 aarch64、APK v3 工具、mu300-at，以及已安装的设备适用音频工具。安装不自动开启配对或拨号；已启用的电话服务升级时可能短暂重启。

已安装 r14：在“管理与维护”启用预览频道，检查更新，关闭电话服务，再点击“下载并更新”并确认。安装完成后刷新页面、手动启动电话服务。
旧版没有在线升级入口时使用上面的脚本。旧 r12 资产保持原样。

## 资产与信任

- `luci-app-airsim-0.1.0-r15.apk`：OpenWrt APK v3 安装包，**不是 Android APK**。
- `airsim-dev.pem`：项目开发签名公钥。
- `install.sh`：固定 r15 安装器。
- `airsim-mu300-0.1.0-r15-sources.tar.gz`：插件、隔离补丁后的 Agent、Go 依赖、音频/ALSA/构建上下文对应源码及许可证。
- `SHA256SUMS`：以上资产的完整性校验清单。

公钥 SHA-256：`0ce703e76ea4b702194c54b90534d7127874bb38ecda17f7ed6b1add2d2cf1de`。
不使用忽略签名选项，不分发私钥、设备配置、用户短信/音频或专有固件。

## 验证与边界

发布检查包括插件及隔离 Agent 的 Go race/vet、28 项 Node 测试、安装/服务脚本语法、APK 签名/元数据/包内容一致性、依赖来源哈希与许可证、源码包检查，以及连接设备的非变更安装预检。
本轮设备预检通过，未安装 r15、未重启、未拨号、未发短信、未录音。

此前 r14 已在 mu300-linux v2026.10.30.1 上安装，管理页面与更新检查通过；通话修复已有单次局域网 dialing → alerting → active → ended 的 ID 连续性证据。
**本版在线下载到安装的完整实机升级链路尚未验收。** 双向可听、长时间振铃、长通话及多路呼叫等待仍未完成验收，之前设备有 `ALSA playback stalled` 记录。本发布不声称修复全部音频问题。

取消版本限制不等于所有硬件或驱动兼容；不修改上游驱动 ABI 检查，不升级 mu300-linux 或 mu300-audio。其他硬件依照根目录 `HARDWARE_ADAPTATION.md` 适配。

本项目是独立 LuCI 插件、Agent 适配与音频桥，不是官方 MU300 Linux/Audio 固件。
感谢 dikeckaan、AirSIM 及 mu300-linux / mu300-audio、OpenWrt/LuCI、ALSA 的上游贡献者；原作者声明与各组件许可证随包和源码保留。
