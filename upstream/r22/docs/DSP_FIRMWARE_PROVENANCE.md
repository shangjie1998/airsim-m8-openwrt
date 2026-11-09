# DSP 固件固定副本与离线回退

R18 增加固件备用副本，避免首次安装依赖上游仓库持续在线。已有 `/lib/firmware/agdsp.bin` 保留并复用；仅缺失时执行获取流程。

1. 尝试上游固定 HTTPS Release 地址，整体最多等待 25 秒。
2. 下载失败、文件不完整或 SHA-256 不符时，改用签名 APK 中 `/usr/lib/airsim/deps/agdsp.bin`。
3. 备用副本也必须通过同一个固定 SHA-256，复制后再次检查；任何检查失败都不会安装该固件或继续暂存音频扩展。
4. 通过检查后原子安装，并记录来源为 `upstream` 或 `bundled_backup`。后续启动/升级已有文件时不再次下载。

## 来源与固定身份

| 项目 | 固定值 |
| --- | --- |
| 上游仓库 | https://github.com/dikeckaan/mu300-audio-firmware |
| 原始提交 / v1.0 | `8a3fbd2cd0556796d0ffef5be05b4c21a743f868` |
| 文件 | `agdsp.bin`，6,291,456 字节 |
| SHA-256 | `378ceea78dbf013e10625e71efd6fab487d727e371884ae8daf078da2828b937` |
| 固定下载地址 | https://github.com/dikeckaan/mu300-audio-firmware/releases/download/v1.0/agdsp.bin |

副本从公开上游提交提取，不从用户设备导出。保留上游原始 Git 对象、提交和作者记录；不改称本项目自研固件。该二进制与内核/音频模块仍是不同组件，备用下载机制不扩大硬件支持范围。

## 本地构建与备份

`scripts/fetch-firmware.sh` 优先使用已校验的本地缓存；缓存缺失时从固定提交的本地镜像提取。镜像也不存在时才尝试克隆上游或下载固定 Release。不会自动更新到上游 HEAD，不会 prune 已保存镜像。通过 `AIRSIM_FIRMWARE_REPO` 指定已有镜像、`AIRSIM_FIRMWARE_CACHE` 指定持久缓存目录。

```sh
AIRSIM_FIRMWARE_REPO=/absolute/path/mu300-audio-firmware.git \
AIRSIM_FIRMWARE_CACHE=/absolute/path/firmware-cache \
sh scripts/fetch-firmware.sh
```

默认缓存为工作树 `out/firmware-backup/`。固件、镜像和离线 Git bundle 保存在本地，排除于代码提交之外；已取得的包内副本不依赖该开发机路径或网络。

## 许可记录

包元数据中的 `LicenseRef-Undocumented-DSP-Firmware` 指向本节，表示未提供许可声明的独立固件，不表示额外授权。

2026-11-02 核查，上游仓库和 v1.0 发布没有 LICENSE 或明确再分发许可文件。原作者及固件权利人的权利保留；本项目的 PolyForm/GPL/MIT 许可不覆盖此二进制，也不将公开可下载视为获得开源许可。R18 按维护者要求保留本地克隆，并将固定副本纳入签名 APK 的离线回退候选；未单独创建公开固件仓库。R18 未公开发布；维护者随后明确要求以 R19 开发预览发布，离线回退仍未实机验收。此打包决定不代表已经取得或确认独立固件的再分发许可。本文件不授予任何额外权利。
