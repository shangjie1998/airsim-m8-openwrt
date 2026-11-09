# 挂起：云端拨号不带卡号，设备端无解（等 iOS 端补字段）

状态：**未修复**，2026-11-09 挂起。用户决定等 iOS 端更新后由 iOS 发送 `sim_account` 再解决；
当前主力使用场景是本地 VoWLAN，云端这条不影响日常。

## 现象

iPhone 未连设备 Wi-Fi（走云端模式）时，在 App 里选 **SIM1（卡一）** 拨号，设备却从
**SIM2（卡二，广电）** 拨出去。

## 实测证据（设备侧日志）

```
公网拨号请求   number=15998104018  sim_account=""
通话记录       slot:1  state:alerting
```

## 根因

1. iPhone 云端拨号请求体里**没有任何卡号字段**。iOS 源码 `VoIPPushController.swift:1501`
   的 `enqueueCloudCommand` 组包只有：
   ```
   device_id / device_secret / command_id / type / number / target_agent_id
   ```
   `startCloudOutgoingCall()`（同文件 1398 行）签名里也没有 sim 参数。
   本地路径（`AppModel.swift:3923` → `selectedSIMSlotID(for:)`）才带 `sim_account`，
   云端 fallback 走的是 `VoIPPushController.shared.startCloudOutgoingCall(number:target:)`，
   拿不到选择。

2. 设备端 `mu300SlotFromAccount("")` 返回 `-1`，原逻辑 `-1` 表示「不切卡，沿用当前 AT 上下文」。
   而上一通（例如打 10099 走 SIM2）用 `AT+SPACTCARD` 把上下文停在了 slot 1，
   下一通不带卡号就跟过去了 —— 于是「选卡一，从卡二拨出」。

3. 顺带澄清一个容易误判的点：设备侧默认卡**是卡一**，不是卡二。
   ```
   slot 0  SIM1  +8613166773059  联通   default:true  is_default_voice:true
   slot 1  SIM2  +8619282491150  广电   default:false is_default_voice:false
   ```
   「默认变卡二」只是 AT 上下文残留造成的错觉。

## 已在设备上生效的临时兜底（不是正式修复）

`mu300.go` 的 `mu300DialFor()`：`sim_account` 为空时不再沿用上下文，改成落到
`a.mu300DefaultVoiceSlot()`（默认语音卡，即 slot 0 / SIM1）。新增函数
`mu300DefaultVoiceSlot()`。这样「默认选 SIM1」时拨出与界面对得上。

**局限**：iPhone 在云端模式下**根本发不出选择**，所以用户在 App 里切到 SIM2 再打，
设备仍会用 SIM1 —— 这一层必须等 iOS 端把 `sim_account` 加进云端拨号请求体才能真修。

## 真正的修复方向（等 iOS）

iOS `startCloudOutgoingCall(number:target:)` 增加 `simAccount` 参数，
在 `enqueueCloudCommand` 的 body 里补 `sim_account`（值沿用 `selectedSIMSlotID(for:)`
的 `"0"` / `"1"`）。设备端 `mu300SlotFromAccount` 已支持 `0` / `1` / `SIM1` / `SIM2`，
无需再改设备。

## 本轮顺带修好、与本条无关的两件事

- 云端媒体/心跳的 TLS 预算：媒体拨号 TCP 8s→15s、握手 8s→30s（蜂窝链路上 TLS 实测
  4.6~7.7 秒，原来几乎每通踩线）。
- 拨号地址解析：原先串行查三个自定义 IPv4 DNS（每个 4~5 秒、常超时），改成系统解析结果
  并发拨号，谁先通用谁。**必须保留 SO_BINDTODEVICE 绑 `sipa_eth0`** —— 实测不绑网卡
  3 次全挂（12 秒超时），绑了 TCP 0.3 秒就通。
