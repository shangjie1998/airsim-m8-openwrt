# AirSIM for VIPME-M8 — 把通话网关装进 OpenWrt 手机

把 AirSIM 的通话网关（配对、控制面、通话音频桥、短信）装进一台跑 OpenWrt 的中兴 / 紫光展锐
UMS9620 安卓手机，让 iPhone 在同一局域网里用这台手机的 SIM 卡接打电话、收发短信。

**不是刷机包，不是 APK，不改内核。** 只在自己的命名空间里落地文件，一条命令可完整卸载。

| | |
|---|---|
| 本适配仓库 | <https://github.com/shangjie1998/airsim-m8-openwrt> |
| 上游项目 | <https://github.com/kai-wu-cortex/airsim-mu300> |
| 已验收版本 | **`0.1.0.1-r22-m8`** |
| 安装包 | [Release `v0.1.0.1-r22-m8`](https://github.com/shangjie1998/airsim-m8-openwrt/releases/tag/v0.1.0.1-r22-m8) · 5.7 MB（仓库内另有 [`dist/`](dist/) 同款） |
| 使用者手册 | [README-M8.md](README-M8.md) —— 机型全景、逐功能调用说明、换机型核对表 |
| 编译与微调 | [docs/M8_BUILD_AND_ADAPT.md](docs/M8_BUILD_AND_ADAPT.md) —— 工具链、编译坑、改功能后的流程 |
| 上游原说明 | [docs/UPSTREAM_README.md](docs/UPSTREAM_README.md) —— U30 Air / F50 的 R22 原文，保留备查 |

---

## 1. 适配环境（全部为真机实测值）

| 项目 | 实测值 |
|---|---|
| 机型 | **VIPME-M8**，`Spreadtrum UMS9620` SoC（与 MU300 / F50 同 SoC） |
| 系统 | **OpenWrt 25.12.5 `r33051-f5dae5ece4`**，target `armsr/armv8`，arch `aarch64_generic`，包管理 `apk-tools 3.0.5` |
| 系统主线 | [openwrt/openwrt](https://github.com/openwrt/openwrt)（`r33051` 为上游主线版本号；本机固件由移植者自建，刷机包为 `m8-openwrt-25.12.5-r20261106.183308`） |
| 内核 | **`6.18.54-m8-probe12`**（自建 mainline 变体，非厂商 5.4） |
| 声卡 | `card0 = sprdphone-sc2730` |
| 通话采集 | `FE_ST_VOICE_C` = `hw:0,9`，8 kHz 单声道带 DMA |
| 通话播放 | `FE_ST_VOICE_PCM_P` = `hw:0,18`，按 16 kHz 宽带消费 |
| 调制解调器 | ModemManager，plugin `unisoc`，AT 端口 `wwan0at0`，IMEI `866766061031893` |
| 根分区 | `/dev/mmcblk0p74`（`e5root`，ext4 4.0 GB），装机只写这一分区 |
| 网络 | `br-lan 192.168.9.1/24` |

**安全边界（硬约束）**：不刷机，不写 boot / bootloader / 分区表 / modem NV，不改内核，不覆盖厂商脚本
（厂商 `/usr/libexec/rpcd/mu300dash` 装/卸前后 md5 必须一致）。

## 2. 相对上游改了什么

上游代码（daemon、Agent、LuCI、AirSIM 协议）一行没重写，改的都是"另一种内核、另一种音频路由"下的设备侧差异：

| # | 差异 | 位置 |
|---|---|---|
| 1 | 播放采样率按 **16 kHz + 20 ms 周期**下发，走 `Upsampler` 而不是 8 kHz 直通（否则对端听到"2 倍速、变身"） | `internal/audio/devices.go`、`session.go` |
| 2 | 本机号码补 **`+`** 成 E.164（`86…` → `+86…`） | `patches/AirSIM-e164.patch` |
| 3 | 对外显示名 **VIPME-M8**（health / mDNS / 云端注册 / LuCI），内部标识 `mu300` 一律保留 | `gateway.go`、`main.go`、`build/luci/` |
| 4 | 发送短信写入 **发件池 + 时间戳**，同步器与 LuCI 列表识别 `dir: mo` | `build/mu300-sms`、`sms-pool-sync.py`、`sms-dash.py` |
| 5 | 挂断/接听的完整性校验改成"刷新即通过"，不再被 grace 缓存卡死 | `patches/AirSIM-m8-sms-callfix.patch` |
| 6 | LuCI 复用**上游原生页面**（`airsim` / `airsim-sms` 两个 ubus 对象 + 菜单 + ACL），未自建页面 | `m8-pkg/luci/` |

每处"调用系统哪里"（哪个 ALSA 设备、哪个 mmcli 命令、哪个 rpcd 对象、哪个 procd 实例）在
[README-M8.md 第 3 节](README-M8.md#3-每个功能调用了系统的哪里) 逐条写死。

## 3. 直接安装

前置：root、`/proc/asound/pcm` 里同时有 `FE_ST_VOICE_C` 与 `FE_ST_VOICE_PCM_P`、有 `mmcli`、`/opt` 剩余 > 20 MB、
没有手工残留的 `airsim-mu300d` / `airsim-agent`。安装器会逐项检查，任一不满足直接退出。

```sh
# 主机侧：从 Release 下载，再传到设备（版本号第四段是本地改动序号，不带括号）
curl -LO "https://github.com/shangjie1998/airsim-m8-openwrt/releases/download/v0.1.0.1-r22-m8/airsim-m8-0.1.0.1-r22-m8.tar.gz"
scp airsim-m8-0.1.0.1-r22-m8.tar.gz root@192.168.9.1:/tmp/

# 设备侧：解包安装（设备侧直接写系统路径会失败，必须先落 /tmp）
ssh root@192.168.9.1
mkdir -p /tmp/m8pkg && cd /tmp/m8pkg
tar xzf /tmp/airsim-m8-0.1.0.1-r22-m8.tar.gz -C /tmp/m8pkg
sh install.sh
```

装完脚本打印三项：procd 服务 `running`、daemon `/status` 的 `audio_available`、Agent `/api/health`。
随后在 LuCI → 服务 → AirSIM 里取配对码，用 iPhone 的 AirSIM 配对。

卸载：

```sh
sh /opt/airsim-m8/uninstall.sh
```

停服务 → 取消自启 → 两个 DSP 开关打回 `off` → 删三个 shim、软链、`/etc/mu300`、`/opt/airsim-m8` → 重启 rpcd。
厂商 `e5-voice-native` 与 `mu300dash` 一个字节不碰。

## 4. 已知限制

| 现象 | 说明 |
|---|---|
| 通话约 50 秒后断链 | `aplay` 报 `pcm_write I/O error`，上下行帧计数冻结，DSP 开关自动回落；未解 |
| 屏幕接听来电采不到下行 | 原生拨号器接听态独占采集路由（`arecord` 直接 I/O error）；改用 iOS 端接听即可 |
| 偶尔闪白屏 | 面板 ESD 看门狗循环复位（`esd check failed` + `esd recovery end`），内核侧无软件开关，与安装包无关 |
| iOS 已配对设备仍显示旧名 | 设备名缓存在 App 本地，改名后需删设备重新配对 |
| `cellular_registration` 显示"未注册" | 状态字段口径问题，ModemManager 实际报 connected，不影响通话 |

## 5. 版本口径

`0.1.0.<N>-r22-m8`：`0.1.0`、`r22` 与上游版本号对齐，`<N>` 为本次适配的打包序号（第四段），每改动打包一次 +1，
`m8` 是机型标识。`scripts/bump-version.sh` 负责 +1 并同步两处 `VERSION`。

## 6. 想让 AI 帮你装到别的手机

把仓库链接交给 AI，让它按顺序读：

1. [docs/M8_BUILD_AND_ADAPT.md](docs/M8_BUILD_AND_ADAPT.md) —— 先跑通编译（Go 1.24.7 + 阿里云镜像），再改参数
2. [README-M8.md 第 6 节](README-M8.md) —— 10 项核对表：声卡名、采集/播放 FE、采样率、ModemManager 端口、分区、网络
3. 改 `internal/audio/devices.go` 的设备参数与 `VERSION`，`bump-version.sh` 升号，重打 tar，`scp` 到 `/tmp` 再装

不要做的六件事（写进文档、也在文档里逐条解释）：不刷机、不动分区表、不改 modem NV、不覆盖厂商同名脚本、
不装别内核编的音频模块、不远程 `reboot`。

## 7. 上游与致谢

本仓库是**独立适配工程**，不是 [kai-wu-cortex/airsim-mu300](https://github.com/kai-wu-cortex/airsim-mu300) 的官方发布。

| 项目 | 用途 | 作者 |
|---|---|---|
| [AirSIM](https://github.com/kai-wu-cortex/AirSIM) | 配对 / VoWLAN / Relay 协议、Agent 基线 | AirSIM contributors |
| [airsim-mu300](https://github.com/kai-wu-cortex/airsim-mu300) | daemon、Agent、LuCI、音频桥与打包流程，本适配的直接上游 | **kai-wu-cortex** |
| [mu300-linux](https://github.com/dikeckaan/mu300-linux) | OpenWrt 侧 AT 服务、原生短信与设备基础设施 | **dikeckaan** 及贡献者 |
| [mu300-audio](https://github.com/dikeckaan/mu300-audio) | 音频驱动、DSP 场景与工具 | **dikeckaan** 及贡献者 |
| [OpenWrt / LuCI](https://openwrt.org/) · [ALSA](https://www.alsa-project.org/) | 管理界面、包管理、音频工具 | 各上游贡献者 |

感谢 **kai-wu-cortex** 把 AirSIM 的 MU300 侧完整开源——本适配的 daemon、Agent、LuCI 页面和协议实现全部来自它；
感谢 **dikeckaan** 在 MU300 Linux 与通话音频栈上的工作，以及音频上游注明的 **kano / kanoqwq、Enceka、e5-linux**
与 Unisoc 驱动贡献者。上游引用不代表作者对本适配的背书。

## 8. 许可证

本适配沿用上游非商业定位，见 [LICENSE](LICENSE)、[NOTICE](NOTICE)。
AirSIM Agent 为 **PolyForm Noncommercial 1.0.0**；mu300-audio 按文件采用 **GPL-2.0 / MIT**；
ALSA 按组件采用 **LGPL-2.1-or-later / GPL-2.0-or-later**。各组件保留各自权利，
整包不能称为无商业限制的开源软件。依赖与源码说明见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)、[SOURCES.md](SOURCES.md)。
