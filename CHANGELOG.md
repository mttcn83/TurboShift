# Changelog

## 0.1.0

First public pre-release.

- Interactive TUI: Dashboard, System Doctor, Optimizer, Gaming, Network Doctor, Benchmark Lab, Snapshot Vault, Settings; mouse, keyboard, search, themes (dark, light, colour-blind, mono), ASCII mode.
- Transaction engine: preview, journal, apply, verify against live state, automatic rollback, drift-aware restore, crash recovery, file locking.
- Tweaks: `power.profile`, `net.wifi-powersave` (NetworkManager checkpoint guard), `cpu.governor` (polkit, allowlisted).
- Diagnostics: rule-based findings with evidence, live metrics, layered network diagnosis with explicit consent for external tests.
- Benchmark Lab: MangoHud import, exact permutation-test comparisons with honest verdicts.
- CLI for scripting with stable exit codes and `--json`.

Pre-release audit hardening: the polkit helper path is offered only for a root-owned, non-user-writable binary; the root helper never creates missing sysfs attributes; values restored from a journal are validated before use in commands; third-party licence notices added; cross-distro container test script (`scripts/distro-test.sh`).

**Verified:** clean install, CLI and TUI smoke tests and the full unit suite on Arch, Ubuntu 24.04, Debian 12, Fedora 42 and openSUSE Tumbleweed containers (root and non-root).

**Unverified:** real apply on a live system, real NetworkManager checkpoints, real polkit prompt, real MangoHud logs, tweaks on real hardware of distributions other than Arch/CachyOS. See `docs/COMPATIBILITY.md`.
