# Changelog

## 1.0.0

First stable release. The 54 optimizations, presets and one-key goals from 0.3.0 are unchanged; 1.0 is about trust: a real-kernel test, a hardened helper, a stable contract and the bugs that testing found.

**Breaking changes from 0.3.0 (the only time they will be made without a new major version, see [docs/STABILITY.md](docs/STABILITY.md)):**

- Every `--json` document is now an object with `"schema_version": 1` and `snake_case` keys throughout. `tweaks list --json` and `preset list --json` used to print a bare array with `PascalCase` keys; they now print `{"schema_version":1,"tweaks":[...]}` / `{"schema_version":1,"presets":[...]}` with `id`, `name`, ... `network --json` destinations, `top_processes` and `benchmark compare --json` metrics were renamed the same way. Empty lists are `[]`, not `null`. `benchmark list --json` is new.

**Fixed**

- The "terminal too small" notice was one 79-column line, so it overflowed exactly the terminals it was meant for. It now wraps to the terminal and never draws more rows than exist. (Found by the new random-input torture test.)
- `turbo tweaks show` printed the current value as a raw Go map (`map[tcp_ecn:2]`); it now prints the state in words ("CUBIC", "Never suspend"), like the app does.
- Asking for BBR when `tcp_bbr` is not loaded said only `allowed: cubic, reno`. It now says BBR is a kernel module that is not loaded and gives the `modprobe` command.
- Restore points now carry a `format` number. A file from a newer format is refused with a clear message instead of being misread; every older file (back to 0.1) still loads and restores (covered by a test).

**Added**

- `turbo completion bash|fish` (installed by `make install`): completes commands, flags, tweak IDs, presets and restore-point IDs.
- `make kernel-test` / `scripts/kernel-test.sh`: apply, live verification, drift-aware restore, automatic rollback after a write the kernel refuses, and `undo`, against the real `/proc/sys` of a throw-away user+network namespace, with a check that the host's own settings are untouched.
- Numeric ceilings in the root helper for the three settings where digits alone could do harm (`min_free_kbytes` 4 GiB, `max_map_count` 2^31-1, `tcp_notsent_lowat` 2^32-1), a fuzz test that no accepted request can leave `/proc/sys` and `/sys`, seeded random key/click/resize torture tests for the whole TUI, and a garbage-arguments test for the CLI.
- [docs/STABILITY.md](docs/STABILITY.md): what scripts may rely on from 1.0.

**Verified:** gofmt, `go vet`, staticcheck, `go test -race ./...`; 15 real-kernel checks passing (`make kernel-test`: five network tweaks plus the shared engine path, in a network namespace, host unchanged); ~15 million fuzz executions of the helper allowlist with no violation; 135 seeds x 600 random TUI events with no panic or overflow; the real binary in a pty at 110x34 and 80x24 (read-only); the 5-distribution container matrix as root and uid 1000.

**Not verified (so not claimed):** that any tweak makes any workload faster (no benchmark is shipped; each tweak says how to measure it); applying CPU, power, GPU, disk, memory-timing and Hyprland/GNOME tweaks on real hardware (their files cannot be changed from a test namespace); the real polkit prompt and the polkit action; NetworkManager checkpoints; MangoHud logs; other distributions or non-x86 on real hardware.

## 0.3.0

Catalogue grows from 26 to **54** optimizations, adds ready-made presets, and every description is rewritten in plain English.

- **New kernel tweaks (28):** `net.notsent-lowat`, `net.buffers`, `net.backlog`, `net.mtu-probing`, `net.ecn`; `cpu.min-perf`, `cpu.hwp-boost`, `sys.watchdog`; new *Input & Latency* category with `input.mouse-poll`, `input.kb-poll`, `input.pad-poll` (USB report rate); `mem.zswap`, `mem.zswap-compressor`, `mem.thp-defrag`, `mem.min-free`, `mem.watermark-scale`, `mem.ksm`, `mem.stat-interval`, `mem.writeback-timing`; `io.rq-affinity`, `io.nomerges`, `io.add-random`; `gpu.intel-min-clock`, `gpu.dgpu-awake`.
- **New desktop tweaks (4):** new *Desktop & Graphics* category with `desktop.hypr-effects` (animations, blur, shadows), `desktop.hypr-tearing`, `desktop.hypr-direct-scanout` and `desktop.gnome-animations`. They need no root, go through `hyprctl`/`gsettings`, and are restored key by key.
- **Presets:** `gaming`, `desktop`, `network`, `maximum`. TUI: press `p` in the Optimizer (ticks the applicable items, nothing is applied until you confirm the preview). CLI: `turbo preset list | show | apply [--dry-run] [--yes]`. Items that do not fit the machine are skipped and listed.
- **Plain-English rewrite:** every tweak has a human title, a one-sentence benefit, an explanation, a new *Who it helps* line, the real trade-off and how to measure it. The Optimizer shows option labels ("Never suspend") instead of raw values ("-1"), and gives the tweak name more room in the list.
- **One password prompt per preset:** `make install` now also installs a polkit action for `turbo __priv` (active local sessions keep one authorisation for polkit's normal retention time). Not yet exercised against a live polkit daemon.
- **Easier to read at first look:** the Dashboard now shows a "What do you want to do?" box with one-key goals (`g` Gaming, `d` Snappy desktop, `n` Network, `m` Maximum) and a plain next-step line; `u` undoes the last change. Picking a goal or a preset goes straight to the preview (scanning first if needed). The Optimizer has a category filter (`c`) and shows human option labels. CLI: `turbo undo`, a Quick start block in `--help`, and `tweaks list` grouped by category that hides tweaks your PC cannot do (`--all` shows them).
- Root helper allowlist widened from 25 to 45 anchored path+value rules (still no command execution or wildcards across `/`); new rules are covered by accept/deny tests, including `BoolChoice` read-back for zswap.
- Tests: an end-to-end apply, verify and exact-restore case for every kernel tweak, fake `hyprctl`/`gsettings` in the simulated machine, preset resolution tests, and a guard that every tweak has a test and complete plain-language metadata.
- Deliberately not added: disabling CPU mitigations, hosts-file ad blocking and Windows-only items (see `docs/ROADMAP.md`).

**Verified:** gofmt, `go vet`, staticcheck and `go test -race ./...` clean; clean install, CLI/TUI smoke (including the new preset commands) and the full unit suite on Arch, Ubuntu 24.04, Debian 12, Fedora 42 and openSUSE Tumbleweed containers as root and uid 1000 (10 runs, 515 checks, 0 failures); read-only `turbo tweaks list` and `turbo preset show` on the author's real CachyOS/Hyprland machine.

**Unverified:** applying any of the new tweaks on real hardware (so no performance gain is claimed), the polkit action against a live polkit daemon, real `hyprctl` option names across Hyprland versions.

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
