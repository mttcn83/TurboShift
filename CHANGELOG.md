# Changelog

## 0.2.0

Catalogue grows from 3 to **26** optimizations, all through the same preview, restore point, verify and undo machinery.

- **CPU & power:** `cpu.epp` (energy preference), `cpu.boost` (turbo/boost), `platform.profile` (firmware profile), `cpu.autogroup`, `pcie.aspm` (PCIe link power saving), `usb.autosuspend`.
- **Network:** `net.dns` (resolver choice via NetworkManager, with checkpoint), `net.tcp-cc` (BBR/CUBIC/...), `net.qdisc` (fq_codel/cake), `net.tcp-fastopen`, `net.tcp-idle`, `net.budget`.
- **Memory:** `mem.swappiness`, `mem.page-cluster`, `mem.vfs-cache`, `mem.compaction`, `mem.thp`.
- **Storage:** `io.scheduler`, `io.readahead`, `mem.dirty` (write-back buffering).
- **Gaming:** `game.max-map-count`, `game.split-lock`, `gpu.amd-level`.
- Each is gated on hardware support and on no power manager owning the setting, previewed with exact before/after values, and restored exactly (including custom starting values). Only live evidence produces recommendations; the rest are offered, never pushed.
- Root helper allowlist widened from one sysfs pattern to 25 anchored path+value patterns (still no command execution, no wildcards across `/`); a rejected batch writes nothing.
- The Doctor's "CPU boost is disabled" finding now links to `cpu.boost`.
- 46 new end-to-end tweak tests (apply, verify, exact restore, unsupported-hardware refusal, power-manager deference, one prompt per change, DNS journal-tampering).

## 0.1.1

Bug-fix release found by a second audit pass (static analysis, parser fuzzing, code review).

- **Fixed:** an automatic rollback that failed halfway (status `rollback_failed`) could never be retried: `recover` ignored it, `restore` refused it, and the error message pointed at `restore`. It is now treated like an interrupted change: offered for recovery at the next start, blocks new changes, and `turbo snapshots recover` finishes it.
- **Fixed:** the CPU governor tweak asked for administrator permission once per CPU thread (dozens of prompts on a modern machine). All threads are now written under one authorisation; the root helper validates every path/value pair against the allowlist before writing any of them.
- **Fixed:** Network Doctor load tests followed HTTP redirects to hosts that were never shown in the "this run will contact" list, and counted an HTTP error page as throughput. Redirects are now limited to the disclosed host and error responses fail the test.
- **Fixed:** `--load-seconds` accepted zero, negative and absurd values; it is now limited to 1-120.
- Added fuzz tests for the MangoHud/system-file parsers (no crashes found) and cleaned up all `staticcheck` findings.

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
