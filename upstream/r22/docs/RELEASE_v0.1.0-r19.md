# R19 · U30 Air 电量、短信与通话状态适配 · 开发预览

R19 汇总此前未公开发布的 R18 候选工作，新增 U30 Air 电量上报，保留 R17 的音频一体安装方案。**不修改 AirSIM iOS/Android 客户端，不替换设备系统或内核。**

## 适用范围与版本选择

**历史版本必须区分：R16 适用于 1031 之后的系统版本线；R12 适用于 1031 之前的系统版本线。不得跨版本线混装。** 1031 是 mu300-linux 系统版本线，不是插件下载日期。

R19 延续新版系统的一体方案，仅提供下列 U30 Air 音频配置：

| 项目 | 范围 |
| --- | --- |
| 设备 | U30 Air，aarch64 |
| 基础系统 | mu300-linux v2026.10.30.1 / OpenWrt 25.12.6 |
| 内核 | `5.4.254-gb50db5b6224c` |
| 内核配置 SHA-256 | `1057cb97e0d2d7f26816e8bf7841faac1153833a01d4893cdc196b75ae6356b3` |
| 包格式 | OpenWrt APK v3，非 Android APK |

**F50 / 7.2.8、其他内核或配置尚未适配；不能仅凭“1031 之后”判断兼容。** 安装器检查匹配条件，不绕过驱动 ABI 检查。

## 变更

- **电量接入**：读取原生 U30 Air 系统电池，通过现有认证 `/v1/health` 和 Agent 心跳提供百分比、充电状态、采样时间。0% 有效，缺失或异常值不伪造，过期样本不刷新成新值；不改充电策略或增加心跳频率。详见 [电量说明](https://github.com/kai-wu-cortex/airsim-mu300/blob/v0.1.0-r19/docs/AIRSIM_BATTERY.md)。
- **通话状态**：取消同号码两分钟冷却，确认空闲后可提交新的拨号操作；同一已认证身份及操作 ID 的重复请求返回持久回执，不再次拨出。原生结束事件与新鲜通话列表确认后，按原通话 ID 发布终态。详见 [通话记录](https://github.com/kai-wu-cortex/airsim-mu300/blob/v0.1.0-r19/docs/R18_REDIAL.md)。
- **短信**：将原生收件箱、保存确认与发送接入 AirSIM 既有协议。保留原短信，不抢占串口；发送超时结果标为未知、不自动重发。短信独立的两分钟重复提交保护保留，每条最多 70 字，不支持 emoji、长短信拆分或多 SIM。详见 [短信说明](https://github.com/kai-wu-cortex/airsim-mu300/blob/v0.1.0-r19/docs/AIRSIM_SMS.md)。
- **固件离线回退**：缺少 DSP 固件时从固定上游获取，失败或校验不符时使用从本地上游克隆保存的包内固定副本，两条路径核对同一 SHA-256。已有固件保留，不从用户设备提取固件发布。独立固件的上游许可未明确，本项目许可证不覆盖它，详见 [来源记录](https://github.com/kai-wu-cortex/airsim-mu300/blob/v0.1.0-r19/docs/DSP_FIRMWARE_PROVENANCE.md)。
- **早期媒体试验代码**：增加绑定通话和配对身份、限时、只接收的下行入口。**普通安装及启动默认关闭，R19 不承诺安装后自动播放运营商公告。** 临时试验需要单独安排，不因存在声卡或 EARLYMEDIA 指示就开启。

## 验证与未完成项

通过插件及固定上游隔离补丁 Agent 的 Go race/vet、全部 29 项 Node 测试、一体安装与固件回退测试、音频准备/ABI 保护测试；核验签名 APK、对应源码、固定依赖哈希与许可证。

此前 R17 的 U30 Air 重启自动准备、两通双向清晰无卡顿已获用户确认。一次 iPhone 0.9.8（160）配合设备临时候选的试听，用户确认真实运营商语音可听、接通前未提前计时、结束界面正常关闭。这是单次用户观察，不是完整客户端日志链路证据。

**本轮发布时 USB 未连接，非变更设备预检无法完成；R19 包未安装实机验收。** 以下仍待验证：

- 实际电量、充放电变化及 AirSIM 显示。
- 真实短信收发、后台推送和云端路径。
- 立即重拨、接通后对端挂断的完整回归。
- 早期媒体转接通后双向语音。
- 离线固件恢复的实机流程、长通话、多路及其他硬件。

因此本版本标为 **Pre-release / 开发预览**，不把自动化通过或发布授权当作实机通过。

## 安装、附件与回退

从本 Release 获取同版 APK、公钥和校验清单，或在符合上述配置的设备上执行：

```sh
cd /tmp
wget -O airsim-install.sh https://github.com/kai-wu-cortex/airsim-mu300/releases/download/v0.1.0-r19/install.sh
cat airsim-install.sh
sh airsim-install.sh --check
sh airsim-install.sh --install
```

先结束通话和短信任务，保存原包与配置。安装器验证固定公钥、哈希、APK 签名、实际空闲及音频条件；保留系统、内核、配对和原短信。需要重启时提示安排一次进入已有 OpenWrt 的重启，不更改默认启动系统。详见 [部署手册](https://github.com/kai-wu-cortex/airsim-mu300/blob/v0.1.0-r19/docs/DEPLOYMENT.md)。

附件为 `luci-app-airsim-0.1.0-r19.apk`、`airsim-dev.pem`、`install.sh`、`airsim-mu300-0.1.0-r19-sources.tar.gz`、`SHA256SUMS`、`SHA256SUMS.sig`。公钥 SHA-256：`0ce703e76ea4b702194c54b90534d7127874bb38ecda17f7ed6b1add2d2cf1de`。源码及附件不含用户设备状态、真实号码、短信、音频、配对凭据或签名私钥。

回退前先结束电话和短信任务，使用设备兼容的可信旧包，保留配置和持久回执。音频模块回退另按部署手册处理，不跨内核强装。

本项目为独立集成，保留上游作者署名，**不是 MU300 Linux/Audio 官方发布**。
