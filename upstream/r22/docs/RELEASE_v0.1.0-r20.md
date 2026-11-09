# R20 · U30 Air 云模式重复配对修复 · 开发预览

修复 AirSIM 在云模式输入正确验证码仍提示配对拒绝、必须切换本地模式才能再次配对的问题。**保持现有 8 kHz、单声道、16 位 PCM 音频和云通话链路，不调整采样率。** 不修改 AirSIM iOS/Android 客户端，不替换设备系统或内核。

## 适用范围与版本选择

**历史版本必须区分：R16 适用于 1031 之后的系统版本线；R12 适用于 1031 之前的系统版本线，不得跨版本线混装。** 1031 指 mu300-linux 系统版本线，不是插件下载日期。

R20 沿用 R19 / R17 一体安装方案，只提供以下 U30 Air 音频扩展配置：

| 项目 | 范围 |
| --- | --- |
| 设备 | U30 Air，aarch64 |
| 基础系统 | mu300-linux v2026.10.30.1 / OpenWrt 25.12.6 |
| 内核 | `5.4.254-gb50db5b6224c` |
| 内核配置 SHA-256 | `1057cb97e0d2d7f26816e8bf7841faac1153833a01d4893cdc196b75ae6356b3` |
| 包格式 | OpenWrt APK v3，非 Android APK |

**F50 / 7.2.8、其他内核或配置尚未适配，不能仅凭“1031 之后”判断兼容。** 上游 v2026.11.02.1 只做了源码比较，不代表新版 ABI 或实机兼容已经验收。安装器保留音频模块匹配检查。

## 本次修复

- 设备端固定 Agent 接收新版客户端发送的 `live_activity_presentation`，持久化并继续传给 Relay。旧版严格解码不认识该字段而返回 400，本地模式省略该字段才会成功。仍严格拒绝未知字段。
- 配对接口以注册已接受且网关密钥保存成功作为成功依据；不再把“云推送未就绪”误报为“配对失败”。原云开关、推送能力和管理策略保留，推送就绪状态另以 `push_configured` 返回。
- 注册失败或 Agent 响应无效时，不再提前保存新网关密钥、消耗客户端名额。保留历史配对与 16 个客户端上限，不自动清除历史记录。

详见 [配对修复记录](https://github.com/kai-wu-cortex/airsim-mu300/blob/v0.1.0-r20/docs/PAIRING_COMPATIBILITY_FIX.md)。短信、电量、通话状态、音频模块及固件离线回退沿用 R19；普通安装的早期媒体试验入口仍默认关闭。

## 验证与边界

- 配对候选 `0.1.0-r19-pairing.1` 已备份安装到 U30 Air，只重启设备端 AirSIM 服务；原有身份、配对密钥与设置保持一致，设备未重启。
- 用户确认 iPhone AirSIM 保持云模式，分别获取新验证码连续配对两次，均成功且不再提示验证码错误；设备端确认云命令连接正常。
- 用户反馈当前云端通话不卡顿，但听感采样率偏低；按用户要求保留现有音频格式。这是用户体验反馈，不是新增宽带语音或长期稳定性认证。
- 发布检查包含插件 Go race/vet、隔离 Agent Go race/vet、全部 Node 测试、一体安装、固件回退、音频准备及 ABI 保护测试，以及 APK 签名、依赖哈希、对应源码、许可证与只读设备预检。

**R20 完整签名 APK 未单独完成实机升级回归；候选二进制的配对验收不能替代整包升级测试。** 真实短信收发与后台推送、实际电量变化、离线固件恢复、完整挂断/重拨回归、长通话、多路和其他硬件仍未完成整体验收。因此维持 **Pre-release / 开发预览**。

## 安装与回退

先结束通话及短信任务，并保存原包和配置。在匹配上述环境的设备上执行：

```sh
cd /tmp
wget -O airsim-install.sh https://github.com/kai-wu-cortex/airsim-mu300/releases/download/v0.1.0-r20/install.sh
cat airsim-install.sh
sh airsim-install.sh --check
sh airsim-install.sh --install
```

安装器验证固定公钥、哈希和 APK 签名，检查设备空闲与音频条件，保留配对、配置和原短信。首次音频准备需要重启时，提示安排一次进入已有 OpenWrt 的重启，不修改默认启动系统。详见 [部署手册](https://github.com/kai-wu-cortex/airsim-mu300/blob/v0.1.0-r20/docs/DEPLOYMENT.md)。

附件：`luci-app-airsim-0.1.0-r20.apk`、`airsim-dev.pem`、`install.sh`、`airsim-mu300-0.1.0-r20-sources.tar.gz`、`SHA256SUMS`、`SHA256SUMS.sig`。

沿用发布公钥 SHA-256：`0ce703e76ea4b702194c54b90534d7127874bb38ecda17f7ed6b1add2d2cf1de`。缺失 DSP 固件时沿用固定上游和包内固定副本回退，已有固件保留。备用固件不来自用户设备；独立许可未明确，项目许可证不覆盖它，见 [来源说明](https://github.com/kai-wu-cortex/airsim-mu300/blob/v0.1.0-r20/docs/DSP_FIRMWARE_PROVENANCE.md)。

回退时先结束通话和短信任务，使用适配本设备的可信旧包；保留配对及持久状态，不跨内核强装音频模块。

本项目是独立集成，保留上游作者署名，**不是 MU300 Linux/Audio 官方发布**。发布物不包含用户设备状态、真实号码、短信、音频、配对凭据或签名私钥。
