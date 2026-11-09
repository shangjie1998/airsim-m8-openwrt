# AirSIM for MU300 v0.1.0-r16 · OpenWrt 开发预览

> [!WARNING]
> **安装前必须按系统版本选包：R16 仅适用于 1031 之后的新系统版本；R12 仅适用于 1031 之前的旧系统版本。不得跨版本线混装，也不要仅因 R16 更新就升级旧系统。**
> 这里的 1031 指 mu300-linux 系统版本线，不是插件下载或安装日期。无法确认所属版本线时，先核对系统发布说明再安装；音频模块还必须通过内核、配置与 ABI 检查。

修复 iPhone AirSIM 已接通但上行缺音、卡顿的问题，提供保留当前系统与内核的独立音频恢复扩展。r15 已撤回为草稿；r12 保持原样。本项目不是官方 MU300 Linux/Audio 发布。

## 修复与实机结果

- iPhone 可一次发送 100 ms（5 帧）麦克风音频；旧队列仅 3 帧，可能每批丢 2 帧。新桥使用最多 12 帧的有界队列，并在开始或断流后留 40 ms 缓冲，再按 20 ms 播放。
- 同一台 U30 Air 上，补静音比例由上一候选版约 41% 降至累计约 0.9%。这是传输指标，不是声音质量评分。2026-11-01 用户人工确认双方能听清、卡顿消失或明显改善。
- 三次媒体会话正常退出；挂断后采集/播放均 closed，无新内核 panic。测试系统为 mu300-linux v2026.10.30.1 / OpenWrt 25.12.6，内核 5.4.254-gb50db5b6224c；保持系统和内核原样。
- 重新构建 24 个匹配模块，通过 202 个官方模块的符号版本对照和 978 个导入校验。预留内存必须完整取得，DSP 必须有新的命令回复才算准备成功。
- 增加缺失工具、ALSA 异常退出及无敏感内容的传输事件诊断。ALSA 发生 xrun 时退出，避免已知旧 DMA 重提交路径；这不等于修复全部厂商内核缺陷。

真实测试由用户手动拨号/挂断，未录音。30 分钟通话、网络异常、多路、其他设备、完整公网 Relay 路径仍未验收，维持开发预览。

## 安装插件

结束电话与短信任务后，在设备 SSH 终端执行：

```sh
cd /tmp
wget -O airsim-install.sh https://github.com/kai-wu-cortex/airsim-mu300/releases/download/v0.1.0-r16/install.sh
cat airsim-install.sh
sh airsim-install.sh --check
sh airsim-install.sh --install
```

`luci-app-airsim-0.1.0-r16.apk` 是 OpenWrt APK v3，不能安装到 Android。安装器校验固定公钥、签名和哈希。已有匹配音频扩展及 `prepare.local` 会保留。插件更新不自动更换音频驱动；缺少音频栈的设备还需下述单独恢复。

## 音频恢复扩展

`airsim-audio-0.1.0-r16.tar.gz` 仅适用于通过脚本检查的当前内核与配置，不是跨设备通用模块包。它不含系统镜像、DSP 固件或厂商参数；需要设备已有合法安装的固件。已完成本次恢复的设备不必重复执行 staging，脚本会拒绝覆盖已有扩展/备份。

下载扩展、`SHA256SUMS`、`SHA256SUMS.sig` 和 `airsim-dev.pem`，先核对公钥 SHA-256：

`0ce703e76ea4b702194c54b90534d7127874bb38ecda17f7ed6b1add2d2cf1de`

在电脑上验证：

```sh
openssl dgst -sha256 -verify airsim-dev.pem -signature SHA256SUMS.sig SHA256SUMS
shasum -a 256 airsim-audio-0.1.0-r16.tar.gz
```

将结果与已验证清单中该文件的哈希比较，再传到设备的私有临时目录并解包。先运行 `integration/stage-device.sh --check 解包目录`，通过后在电话空闲、已有恢复连接的情况下执行 `--stage 解包目录`。具体文件、回退及构建说明见 [音频扩展指南](../packaging/openwrt-audio/README.md)。

Staging 保存 root 私有备份，暂时关闭 AirSIM 自启动，只启用早期内存预留；不会重启、刷机、加载音频模块或启动 DSP。随后需单独安排干净的 **Linux 启动**。本次设备默认启动 Android，普通 reboot 不保证返回 Linux，需使用设备已有的 Linux 切换入口，不改变默认启动配置或刷写镜像。

Linux 启动后，先确认 `/sys/module/audio_reserve/parameters/missing` 为 0，`ranges` 为 `0xaf700000,0x300000;0xafa00000,0x600000`，再启动 AirSIM；guarded prepare 会检查模块及现有固件并要求 DSP 回复。成功后用 `/etc/init.d/airsim enable` 恢复自启动，并由用户确认双向实话。若预留或准备失败，停止该步骤，不使用 ANY_BUILD、reset 或旧模块强行继续。

回退使用 `integration/stage-device.sh --rollback`，恢复原脚本/链接/启动项，服务保持停止；已驻留模块需另行安排重启后才会改变。桥二进制若曾手动替换，须另行恢复事先备份的桥；音频回退脚本不管理插件二进制。

## 发布资产与来源

- 插件 APK、固定版本 `install.sh`、开发签名公钥。
- `airsim-mu300-0.1.0-r16-sources.tar.gz`：插件、隔离补丁后的 Agent、Go 依赖、ALSA/旧工具源码及许可证。
- `airsim-audio-0.1.0-r16.tar.gz` 与 `airsim-audio-0.1.0-r16-sources.tar.gz`：新模块、工具及对应已打补丁源码/构建配方；不要再次对源码包重复应用补丁。
- `SHA256SUMS` 与 `SHA256SUMS.sig`：所有公开资产的校验清单及签名。

保留 dikeckaan、AirSIM、Unisoc、kano/kanoqwq、Enceka、e5-linux、OpenWrt/LuCI 和 ALSA 的署名及各组件许可证。未分发设备状态、号码、短信、音频、固件或私钥。[来源与重建](../SOURCES.md) 包含精确提交与构建边界。
