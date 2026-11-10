# Changelog

## Unreleased

- New `turbo check [--json] [--reapply [--yes]]`: compares what your restore points say is applied with the live system. Each setting is reported as holding, reset (back at its original value, typically after a restart for "until reboot" settings), or changed by something else. `--reapply` puts back only the reset ones, with a new restore point; settings someone else changed are never touched. Exits 3 when anything is not holding, so it works in scripts and login hooks.

## 1.1.0

Honest risks, plain wording, an Apply button, a filterable catalogue and three new optimizations (57 in total). No breaking changes; the `--json` documents and IDs only gain fields.

**Honest about risk**

- Every optimization's trade-off was re-read against what it really does. Settings that can lose unsaved data on a power cut (`mem.dirty`, `mem.writeback-timing`), starve the system of memory (`mem.min-free`) or weaken a safety net now say so, and their risk is Medium instead of Low. A test fails if a setting that can lose data claims to be harmless.
- The tier label now means something. *Recommended* is only low-risk settings, and **Optimize My PC** applies only what is both Recommended and suggested by this PC's own readings (for example the Wi-Fi power-saving switch when the card is actually dozing). Everything else stays an offer.
- `game.split-lock` left the Gaming preset (it is a security-relevant switch, not a speed-up), and the preset descriptions no longer imply a gain. Nothing here promises a speed-up; measure with Benchmark Lab.
- Restoring a restore point now writes back only the settings that really differ from what the point recorded, so a restore can no longer touch a setting the change never reached.

**Plain wording and Apply**

- Every description and every option is rewritten in everyday words ("Let editors and sync tools watch more files"). The original technical text is still there under **Technical details** in the app and `turbo tweaks show`.
- New **Apply** button (key `a`): applies the setting under the cursor, or everything you ticked. It always previews first.
- A *Restart needed* line appears in the description, the preview and the result **only** for settings that need a restart to take effect; for the others nothing is shown. (Today every kernel setting is runtime-only, so none shows it yet; the plumbing and tests are there for ones that do.)

**Filtering**

- Optimizer: filter by category (`c`), tier (`t`), view all / **suggested for this PC** (`v`) and **no password needed** (`w`), on top of search (`/`). `Esc` clears them; the chips above the list are clickable and the header says what is being shown.
- CLI: `turbo tweaks list [--suggested] [--tier T] [--max-risk R] [--category C] [--no-password] [words...]`, with shell completion.

**New and improved optimizations**

- `sys.watchers` (Recommended): raise the inotify watch limit. The low default is a classic cause of "ENOSPC: System limit for number of file watchers reached" in editors, file sync and dev servers.
- `audio.quantum`: set PipeWire's buffer size to stop crackling (larger) or lower latency (smaller). Read and written with `pw-metadata`.
- `desktop.hypr-vrr`: adaptive sync on Hyprland (classic config only).
- `net.buffers` now raises the TCP autotuning ceilings (`tcp_rmem`/`tcp_wmem`) too; before, it changed only limits that normal TCP traffic ignores.
- `cpu.governor` is suggested only where it really matters (no power manager, a power-saving governor on AC); `cpu.epp`, `cpu.boost`, `mem.page-cluster`, `game.max-map-count` and `net.qdisc` were promoted to Recommended on evidence that they are low-risk and widely useful.
- The privileged helper allowlist and numeric ceilings cover the new settings (space-separated triples included), fuzzed.
- The README catalogue is now generated from the registry and a test fails if it is out of date.

**Fixed**

- Hyprland 0.57 prints boolean options as `{"bool": ...}`; the tweaks misread that. A machine using `hyprland.lua` rejects `hyprctl keyword`; the tweaks now report "unavailable" with that reason instead of failing at apply time.

**Not verified**

- No gain is claimed for any setting. Real writes of `net.core.*`, inotify, `hypr-vrr` and `audio.quantum` on live hardware, and the polkit prompt, were not exercised by the automated tests (the real-kernel test runs inside a network namespace that refuses some of them; those are covered by simulation).

## 1.0.0

First stable release. The 54 optimizations, presets and one-key goals from 0.3.0 are unchanged; 1.0 is about trust: a real-kernel test, a hardened helper, a stable contract and the bugs that testing found.

**Breaking changes from 0.3.0 (the only time they will be made without a new major version, see [docs/STABILITY.md](docs/STABILITY.md)):**

- Every `--json` document is now an object with `"schema_version": 1` and `snake_case` keys throughout. `tweaks list --json` and `preset list --json` used to print a bare array with `PascalCase` keys; they now print `{"schema_version":1,"tweaks":[...]}` / `{"schema_version":1,"presets":[...]}` with `id`, `name`, ... `network --json` destinations, `top_processes` and `benchmark compare --json` metrics were renamed the same way. Empty lists are `[]`, not `null`. `benchmark list --json` is new.

**Legal and privacy audit**

- `turbo licenses` and the new `THIRD_PARTY_NOTICES.md` now reproduce the Go licence text (it was only linked before) and the licence of every linked module, and both are built into the binary, so every copy of `turbo` carries the notices its licences require. A test fails if a linked module is missing.
- `turbo doctor --json` / `network --json` leaked the router's address, DNS servers and every network interface name (VPN and Docker names included) while the README claimed they held no personal data. New `--redact` removes private IPs, MAC addresses and interface names; README now says to use it for bug reports.
- Network Doctor consent (CLI and TUI) now says that every external host learns your public IP address, and that load-test URLs must be servers you own or whose terms allow it. PRIVACY.md is rewritten and accurate (it still described a single admin action and an earlier feature set).
- Absolute performance promises ("instantly", "never wait", "fix crashes", "no input delay") rewritten to describe the mechanism; the README states there is no performance guarantee.
- The DNS tweak's trade-off now says the provider receives your lookups and IP and that managed networks may forbid it; the Gaming screen and `turbo game options` warn that some online games' anti-cheat or terms restrict overlays and injected libraries.
- Test fixtures no longer contain a real NetworkManager UUID or the author's network interface names.
- New guard tests (`legal_test.go`): only the opt-in Network Doctor may open connections, no unreviewed URL in code, no remote resource (image, font, script, tracker) in any document, documents link only to reviewed hosts, no performance promises in tweak text, no personal data. CLI and TUI tests now use a fake network, so the test suite can never touch the network (a TUI soak run had been pinging a real address).
- README: disclaimer expanded (no warranty, no performance promise, managed machines, online games, trademark acknowledgements); CONTRIBUTING states the licensing of contributions. SECURITY.md no longer points to a reporting feature that may be off. `govulncheck` is clean.

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
