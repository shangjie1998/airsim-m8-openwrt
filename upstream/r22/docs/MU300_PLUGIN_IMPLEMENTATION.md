# AirSIM for MU300 implementation

First delivery: LuCI plugin, native MU300 Agent profile, loopback PCM bridge,
encrypted pairing compatible with AirSIM Bonjour discovery, and offline packaging.
Cloud media uses the existing Agent/Relay wire protocol. Revision r6 adds the existing
VoWLAN HMAC health/HTTP/PCM protocol and Bonjour discovery on LAN only. Manual-IP
pairing and AirSIM client SMS forwarding remain unsupported. Revision r11 adds native
LuCI SMS management (firmware mu300dash RPC, not Agent SMS). Revision r7 supplies MU300 canonical call UUIDs
and signed operation-to-call receipts through the existing client protocol.

## Current source installation policy

Firmware, OpenWrt and kernel release numbers no longer gate plugin installation,
online updates or default preparation. Architecture, package-format support,
required executable tools, signatures, hashes and idle checks remain. Default
preparation reuses installed audio tools and does not auto-install bundled legacy
kernel modules. Existing `prepare.local` overrides remain unchanged. This policy
is included in release r15; the earlier r14 device evidence is not an r15 installation test.
See [installation compatibility](INSTALL_COMPATIBILITY.md). Historical evidence below
records the behavior of those builds, not a current firmware allowlist.

## Device evidence (2026-10-30)

- U30 Air, mu300-linux v2026.10.26.1, OpenWrt 25.12.6, kernel 5.4.254-gb50db5b6224c.
- mu300-audio v1.0.5: 23 modules loaded, DSP command response, duplex hostless scene works.
- An incoming call was answered and the caller confirmed receiving audio.
- Teardown crashed in aplay -> fe_hw_free -> mcdt_dac_dma_disable -> mcdt_da_fifo_clr;
  pstore records synchronous external abort and kernel panic. Root cause is not yet established.
- Sound-card numbering changed across reboot. Discover by card and PCM name.

## Acceptance gates

1. Unit tests: streaming resampling, PCM handshake/framing, exclusive sessions,
   pairing compatibility/expiry/replay, AT error handling, profile isolation.
2. Static linux/arm64 builds and plugin JavaScript/shell validation.
3. Device: installation, authenticated LuCI RPC, health, idle boot, scene start/stop.
4. Human call: incoming/outgoing, intelligible bidirectional voice, remote/local hangup,
   repeated calls, disconnect, 30-minute call; no panic or orphan media processes.
5. Native APK package install/remove/upgrade and recovery validation.

Passing an idle check or synthetic PCM handshake is not a substitute for gate 4.
Never mark release-ready until these device acceptance gates pass.

## Implemented

- Native signed APK v3, isolated bundled ALSA user-space, matching legacy audio extra.
- LuCI authenticated RPC and overview; persistent configuration and procd service instances.
- MU300 Agent profile, serialized mu300-atd transport, CLCC polling, existing call control,
  cloud Relay media path. No QDC507 USB recovery or router configuration is started.
- Loopback-only single-session bridge, streaming FIR 48k stereo -> 8k mono,
  8k -> 16k interpolation, bounded incoming queue, silent frames and playback watchdog.
- Single owner of scene teardown; AP streams close before the hostless scene/power hold.
- 120-second X25519/HKDF/AES-GCM pairing, five-failure limit, replay rejection;
  mDNS advertisement bound to the configured LAN interface.
- VoWLAN control 8581 and PCM 8582; paired secrets persisted with mode 0600,
  signed health identity, 30-second clock window and nonce replay cache. Raw AT,
  debug, push reconfiguration, shell and router endpoints are not proxied.
  r7 queries Agent capability before reporting canonical_call_ids=true. The
  android_id wire field carries a MU300 UUID, not an Android Telecom identifier.
  No iPhone source changes are required.
  r5 pairings must be repeated because the old adapter discarded vowlan_secret.

## Verified so far

- r13 local candidate: management/maintenance view, stop-before-save settings,
  local/cloud policy and Relay destination restriction without credential forwarding,
  configurable LAN pairing/control/PCM ports, allowlisted diagnostic JSON and manual
  GitHub release checks. See `MU300_MANAGEMENT.md` for evidence and deployment limits;
  not a hardware acceptance or published release.
- r12 public release preparation (r11 runtime features retained): standard README, root hardware-adaptation guide
  and AGENTS.md entry point, pinned-source/license inventory, source bundle,
  underscore-prefixed generated vendor trees to avoid Go package scanning on repeat
  builds; fixed-version installer with fingerprint/signature verification and fail-closed
  idle checks. Seventeen Node tests, plugin/isolated Agent race suites and vet pass;
  real MU300 installer --check passes without changing service state. APK carries
  upstream and Go dependency notices. Audio kit download is unavailable; no claim
  of a fresh kernel-driver rebuild or completed call-stability acceptance.
- r11: added an SMS card below device status, beside the dial card; native
  mu300dash list/show/send/job RPC only, no Agent or iOS/Android source changes.
  Thirteen JS tests and plugin Go race/vet checks pass. Installed idle r10 -> r11;
  boot ID unchanged, Agent reachable, voice/media idle. Real authenticated LuCI
  loads the new card, lists four messages/four unread, and shows the composer.
  No existing message was opened/marked read and no SMS was sent during validation;
  native job state remains none. Actual sending/delivery awaits an explicitly
  approved recipient and message. Outbound limited to 70 BMP characters, explicit
  confirmation, no automatic resend after uncertain results; shared native job
  status is not a per-request delivery receipt. SMS content is rendered as text.
- r10: aligned status / service-pairing card bottoms; added authenticated LuCI
  dial, explicit-target answer/hangup, call list and inline confirmed feedback.
  Loopback admin endpoint allowlists actions and payloads; rpcd adds write-only
  call_action (no public/unsigned control endpoint). Calls use the existing r9
  Agent and stable call IDs; AirSIM upstream files are not modified.
  Go race suites + vet and six framework-free JS view tests pass (validation,
  busy/double-click, retry operation identity, selection not retargeted, offline
  disablement). Installed idle r9 -> r10, unchanged boot ID, real desktop LuCI
  screenshot verifies alignment and the dial card. Invalid-number RPC rejected
  without dial. User-operated LuCI displayed dial success and a connected call;
  this is control-plane evidence, not browser audio or duplex acceptance.
- r9: built from a clean pinned upstream archive plus the MU300-owned patch;
  both Agent and plugin race suites and vet pass. A second independently prepared
  source tree passes the same Agent tests. AirSIM Agent worktree diff/untracked
  listing is unchanged during this task. No iOS/Android files were edited.
  Installed r8 -> r9 on the idle MU300; procd Agent and bridge are running,
  signed VoWLAN health proof verified, canonical IDs advertised, SIM number read,
  boot ID unchanged. Waiting-call acceptance/targeted hangup currently awaits the
  user's real iPhone test; synthetic tests are not counted as hardware acceptance.
- r8: left status / right controls LuCI layout, inline confirmed service results,
  pairing/Bonjour guidance, and periodic/manual own-SIM number read implemented.
  Real desktop LuCI rendering and CNUM number read verified; all management UI
  interactions and narrow-screen rendering still need complete acceptance.
- r7: signed VoWLAN dial receipt verified on real hardware; approved test peer
  answered (dialing -> alerting -> active). Authenticated PCM ran for ~14 seconds:
  223040 downlink bytes, peak 8221, and 215680 uplink bytes, followed by scoped
  hangup. Boot ID unchanged. The test peer confirmed the Chinese speech was clear.
  A subsequent real incoming call was answered via the signed gateway (ATA),
  reached active, and streamed ~14 seconds of duplex PCM: 223360 downlink bytes,
  peak 19303, and 216000 uplink bytes, then scoped hangup completed. These tests
  use the existing client wire protocol; actual iPhone UI acceptance remains separate.
- MU300 tests cover incoming ATA, scoped CHUP, canonical identity consistency,
  modem slot reuse, stale answer/hangup rejection, and at-most-once ATD on retries
  including ambiguous command failure. Both Go suites pass with the race detector.
- Updated Agent patch applies cleanly against the documented base commit.
- Agent existing suite and MU300 profile tests pass.
- Plugin race-enabled tests, independent Node-generated crypto vector, resampling and
  anti-alias filtering, protocol rejection and management route checks pass.
- APK signature verified on the device; install and r1 -> r2 upgrade passed.
- Agent/bridge run under procd, loopback binding verified, LuCI RPC status and pair start/cancel work.
- Bonjour advertisement from the MU300 was discovered on macOS with dns-sd.
- r6 installed on hardware; LAN-only ports 8581/8582 are listening, unsigned
  health requests return 401, and macOS discovers _airsim-vowlan._tcp. Boot ID
  unchanged. Signed identity/persistence/replay tests pass locally; the iPhone
  still needs to repeat pairing and validate its real route. No iPhone files edited.
- Native uninstall restored the original upstream voice startup link, retained private
  persistent state, removed plugin executables, and did not change the device boot ID.
- A five-second no-call hardware test produced 249 downlink frames and 104 uplink frames;
  hardware handles closed and boot ID stayed unchanged. This does not establish sustained
  uplink performance: the longer no-call test hit playback stall, which is now a reported failure.

## Pending before a release

- Real iPhone discovery, pairing, cloud incoming/outgoing and intelligible duplex audio.
- Repeat remote/local hangup regression and 30-minute call. Original panic is not yet proven fixed.
- F50 hardware and additional kernel/system compatibility.
- Real-client VoWLAN call verification, manual-IP pairing,
  SMS backend, diagnostics export, public software feed.
- Production signing/trust distribution and release-source/licence completeness audit.
