# Agent entry point

For any new Linux hardware or call/audio adaptation, read `HARDWARE_ADAPTATION.md`
completely before changing code or operating a device. Then read README.md for
the actual supported platform and known limitations.

- Keep AirSIM iOS/Android and the upstream working tree unchanged unless the user
  explicitly expands the task. Use the pinned source plus an isolated patch.
- Preserve user edits. Do not commit `out/`, `dist/`, `backups/`, credentials,
  device state, real phone numbers, SMS bodies, audio, or signing private keys.
- Real calls/SMS/recording require scoped authorization; do not infer it from a
  request to build or publish software. Never test with emergency numbers.
- Verify signaling and media separately; report untested hardware honestly.
- Before release, run Go race/vet, all Node tests, isolated Agent tests, package
  verification, source/license checks and non-mutating device preflight.
- Preserve the upstream authors' notices. Do not label this integration an
  official MU300 Linux/Audio release or a verified port to untested hardware.
