# TurboShift

**A free, local-only optimizer for Linux that explains what it does, proves what helped, and can always undo it.**

Run `turbo`. A terminal app opens: scan your PC, see what (if anything) is worth changing, review the exact change, apply it with a restore point, and undo it later. No account, no cloud, no telemetry, no AI, no ads. MIT licensed.

```
 ████████╗██╗   ██╗██████╗ ██████╗  ██████╗ ███████╗██╗  ██╗██╗███████╗████████╗
 ╚══██╔══╝██║   ██║██╔══██╗██╔══██╗██╔═══██╗██╔════╝██║  ██║██║██╔════╝╚══██╔══╝
    ██║   ██║   ██║██████╔╝██████╔╝██║   ██║███████╗███████║██║█████╗     ██║
    ██║   ██║   ██║██╔══██╗██╔══██╗██║   ██║╚════██║██╔══██║██║██╔══╝     ██║
    ██║   ╚██████╔╝██║  ██║██████╔╝╚██████╔╝███████║██║  ██║██║██║        ██║
    ╚═╝    ╚═════╝ ╚═╝  ╚═╝╚═════╝  ╚═════╝ ╚══════╝╚═╝  ╚═╝╚═╝╚═╝        ╚═╝
```

## Why it is different

Most "optimizers" apply a pile of tweaks and hope. TurboShift's rules:

- **Evidence first.** Every recommendation names the measurement behind it ("Power-saver profile is active while plugged in"). "Your PC is already well configured" is a valid, common result, and nothing is changed in that case.
- **No placebo tweaks.** A tweak ships only if it has a real mechanism, a stated trade-off, and a way to measure whether it helped. There is no "RAM cleaner" and no fake speed score.
- **Reversible by construction.** Each change is previewed, journaled to disk *before* it happens, applied, then **verified against the live system** (not just "the command returned 0"). If any step fails, everything already changed is rolled back automatically. A crash mid-change is detected on the next start and offered for recovery.
- **Never root.** The app runs as you. The only privileged operation (writing a few allowlisted kernel attributes such as the CPU governor or a sysctl) goes through polkit (`pkexec`) to a helper that re-validates an exact allowlist of path and value patterns, and only when the binary is root-owned and not user-writable. There is no "run this command as root" primitive.
- **Honest measurement.** Benchmark comparisons use whole-run statistics and an exact permutation test, and say "insufficient data" instead of guessing.
- **Private.** Nothing leaves your machine unless you explicitly enable external network tests, and the exact destinations are listed first. See [PRIVACY.md](PRIVACY.md).

## Install

TurboShift is a single static binary for Linux (x86-64 or arm64). There is nothing to configure and no account to create.

**Option A: download a release binary** (no Go needed). From the [Releases page](https://github.com/mttcn83/TurboShift/releases) get `turbo-linux-amd64` (or `-arm64`) and `SHA256SUMS`, then:

```bash
sha256sum -c --ignore-missing SHA256SUMS
sudo install -m755 turbo-linux-amd64 /usr/local/bin/turbo
turbo
```

**Option B: build from source** (about a minute). Needs Go 1.27+ and `git` (Arch: `sudo pacman -S go git` · Debian/Ubuntu: `sudo apt install golang git` · Fedora: `sudo dnf install golang git` · openSUSE: `sudo zypper install go git`; if your distro's Go is older than 1.27, get it from [go.dev/dl](https://go.dev/dl/)).

```bash
git clone https://github.com/mttcn83/TurboShift.git && cd TurboShift
make build            # builds ./turbo
sudo make install     # installs to /usr/local/bin (PREFIX=... to change)
turbo
```

Maintainers can produce static amd64/arm64 binaries with checksums via `make release`. You can also just run `./turbo` from the build folder. Everything works that way except the one administrator action (every tweak marked *Admin: yes*): for safety TurboShift only hands work to polkit when the binary is root-owned and not writable by your user, so those tweaks show as unavailable until you `sudo make install` (or install it system-wide some other way). See [SECURITY.md](SECURITY.md).

`sudo make install` also installs tab completion for **bash and fish** (`turbo completion bash|fish` prints the scripts if you prefer to place them yourself); it completes commands, tweak IDs, presets and restore-point IDs.

Uninstall with `sudo make uninstall`. TurboShift keeps its data in your XDG directories (`~/.config/turboshift`, `~/.local/state/turboshift`, `~/.local/share/turboshift`); delete them to remove everything it ever stored.

## The app

| Screen | What it does |
| --- | --- |
| **1 Dashboard** | Live CPU/RAM/GPU/network gauges, hardware card, health summary, **Scan My PC** and **Optimize My PC**. |
| **2 Doctor** | Findings sorted worst-first, each with its evidence; findings with a safe fix jump to the Optimizer (nothing is applied by jumping). |
| **3 Optimizer** | Searchable catalog: tier (recommended / situational / advanced), risk, current → proposed value, trade-off, and how to measure it. |
| **4 Gaming** | Builds Steam launch options (GameMode, MangoHud overlay/log, hybrid-GPU offload) and copies them. Changes no system settings. |
| **5 Network** | Layered diagnosis: link → router → DNS → internet → load. Local-only by default; external tests need explicit consent. |
| **6 Benchmark** | Import MangoHud logs, label them by configuration, compare two configurations with verdicts and uncertainty. |
| **7 Vault** | Every restore point with its journal; undo, force-undo, recover interrupted changes. |
| **8 Settings** | Theme (incl. colour-blind-safe and no-colour), ASCII-only mode, network test targets, privacy statement. |

Keys: `1`-`8` or `Tab` switch screens · `↑↓` move · `Space` select · `Enter` open/confirm · `/` search · `?` help · `q` quit. On the Dashboard, `g` `d` `n` `m` start the Gaming / Snappy desktop / Network / Maximum goals (always previewed first) and `u` undoes the last change. In the Optimizer, `p` picks a preset and `c` cycles the category filter. The mouse works everywhere (tabs, rows, buttons, dialogs, wheel). `NO_COLOR` and `TERM=dumb` are honoured; the Linux console and non-UTF-8 locales get an ASCII-only rendering automatically.

### A typical session

1. `turbo` → press **Enter** on the Dashboard (scan).
2. Press `o` (**Optimize My PC**). If something is recommended you see a **preview** of the exact change, its trade-off and how to measure it. Nothing has happened yet.
3. **Enter** applies it: restore point → apply → verify → result. The result dialog has **Undo** (`u`).
4. Later: **Vault** shows the restore point; `Enter` restores it. Settings that another program changed since are *left alone and reported* unless you choose force.

## Optimizations included (54)

Every entry is a real kernel or desktop setting with a plain-language explanation of what it does, who it helps, what it costs and how to measure it. Nothing is applied until you pick a value and confirm the preview; each change gets a restore point, is verified against the live system, and can be undone.

**What "optimization" means here.** These settings help *specific* situations (a power-saving mode left on while plugged in, an input device polled at 125 Hz, a game that hits the old memory-mapping limit, a stalling disk, a compositor spending GPU time on blur). On a machine that is already configured well, many of them change nothing, and TurboShift says so instead of pretending: it only *recommends* the few that live evidence supports and offers the rest for you to try and measure in Benchmark Lab or Network Doctor. Each one is shown only when your hardware supports it and no power manager (power-profiles-daemon, TLP, TuneD) already owns the setting. No gain figures are promised anywhere, because none have been measured on real hardware yet.

### Presets

One keypress ticks a ready-made bundle; you still review the exact preview before anything changes.

| Preset | Goal |
| --- | --- |
| `gaming` | Fast CPU/GPU, 1000 Hz USB input, fewer kernel-induced stutters. |
| `desktop` | A snappier everyday desktop: quicker file operations, fewer stalls when copying files or swapping. |
| `network` | Low ping under load, faster TCP start and recovery, steadier Wi-Fi. |
| `maximum` | Gaming plus the strongest "always ready" settings. Hot and loud; for plugged-in sessions. |

In the app press `p` in the Optimizer. From the shell: `turbo preset list`, `turbo preset show gaming`, `turbo preset apply gaming [--dry-run] [--yes]`. Items that do not apply to your machine (no AMD GPU, no Hyprland, a power manager in charge...) are skipped and listed, never forced.

### CPU & Power (12)

| ID | What you get | Tier | Admin | Lasts |
| --- | --- | --- | --- | --- |
| `power.profile` | **Performance / balanced / power-saver mode**: Switch the whole system between speed and battery saving with one setting. | recommended | no | persistent |
| `cpu.governor` | **CPU speed policy (governor)**: Tell every CPU core how eagerly to run fast. Offered only when no power manager is in charge. | advanced | yes | until reboot |
| `cpu.epp` | **Make the CPU favour speed over saving power**: Tell the CPU's own speed controller to ramp up faster, or to save more power. | situational | yes | until reboot |
| `cpu.boost` | **Let the CPU run faster than its base speed (turbo)**: Allow or forbid the CPU to run above its base clock. | situational | yes | until reboot |
| `cpu.min-perf` | **Keep the CPU from idling low**: Raise the lowest speed the CPU is allowed to drop to, so it reacts instantly instead of ramping up. | situational | yes | until reboot |
| `cpu.hwp-boost` | **Instant CPU boost after waiting**: Make the CPU jump to full speed the moment a program resumes after waiting on disk or network. | situational | yes | until reboot |
| `platform.profile` | **Laptop fan and power mode**: Switch the laptop firmware between quiet, balanced and performance modes. | advanced | yes | until reboot |
| `cpu.autogroup` | **Keep the desktop smooth during heavy builds**: Give each terminal and session its own fair share of CPU, so one huge job cannot make the rest sluggish. | advanced | yes | until reboot |
| `sys.watchdog` | **Turn off the CPU lock-up detector**: Stop the kernel from waking every CPU once every few seconds just to check that it has not frozen. | situational | yes | until reboot |
| `pcie.aspm` | **Stop PCIe devices from dozing off (lower latency)**: Keep PCI Express links fully awake so NVMe drives, GPUs and network cards answer instantly. | advanced | yes | until reboot |
| `usb.autosuspend` | **Stop USB devices from falling asleep**: Keep mice, keyboards and headsets awake so the first click or sound after a pause is not delayed. | situational | yes | until reboot |
| `mem.stat-interval` | **Fewer background interruptions**: Make the kernel update its memory statistics less often, so it interrupts your CPU cores less. | situational | yes | until reboot |

### Input & Latency (3)

| ID | What you get | Tier | Admin | Lasts |
| --- | --- | --- | --- | --- |
| `input.mouse-poll` | **Faster USB mouse reports**: Ask USB mice to report as often as possible, so your input reaches the game sooner. | situational | yes | until reboot |
| `input.kb-poll` | **Faster USB keyboard reports**: Ask USB keyboards to report as often as possible, so your input reaches the game sooner. | situational | yes | until reboot |
| `input.pad-poll` | **Faster USB controller reports**: Ask USB controllers and joysticks to report as often as possible, so your input reaches the game sooner. | situational | yes | until reboot |

### Network (12)

| ID | What you get | Tier | Admin | Lasts |
| --- | --- | --- | --- | --- |
| `net.wifi-powersave` | **Steadier Wi-Fi (turn off radio power saving)**: Stop the Wi-Fi radio from napping between packets, which can add small, random delays. | situational | no | persistent |
| `net.dns` | **Faster website and game-server lookups (DNS)**: Use a different DNS service on this connection if your router's or ISP's is slow. | situational | no | persistent |
| `net.tcp-cc` | **Better download speed on lossy or long links (BBR)**: Choose how downloads and web traffic react to lost packets and delay. | advanced | yes | until reboot |
| `net.qdisc` | **Less lag while downloading (fight bufferbloat)**: Keep ping low when the connection is busy by scheduling packets fairly. | advanced | yes | until reboot |
| `net.tcp-fastopen` | **Reconnect to websites one round trip sooner**: Send the first data together with the connection request on repeat visits. | advanced | yes | until reboot |
| `net.tcp-idle` | **Keep connections fast after a pause**: Stop long-lived connections from slowing down after a short pause. | advanced | yes | until reboot |
| `net.notsent-lowat` | **Less lag on busy TCP connections**: Stop programs from stuffing the send queue, so interactive traffic (remote desktop, SSH, streaming) reacts sooner. | advanced | yes | until reboot |
| `net.buffers` | **Faster downloads on fast or far links**: Allow bigger network buffers so a single download can fill a gigabit or long-distance connection. | advanced | yes | until reboot |
| `net.budget` | **Handle very busy network links without hiccups**: Let the kernel process more incoming packets per interrupt. | advanced | yes | until reboot |
| `net.backlog` | **Stop dropping packets in bursts**: Make the incoming-packet queue longer so short bursts are not thrown away. | advanced | yes | until reboot |
| `net.mtu-probing` | **Recover from stuck connections on odd networks**: Let TCP find a working packet size by itself when a VPN, tunnel or hotel network silently drops big packets. | situational | yes | until reboot |
| `net.ecn` | **Smoother traffic with modern routers (ECN)**: Let routers tell your connection to slow down by marking packets instead of dropping them. | advanced | yes | until reboot |

### Memory (11)

| ID | What you get | Tier | Admin | Lasts |
| --- | --- | --- | --- | --- |
| `mem.swappiness` | **Keep your programs in RAM longer (swappiness)**: Set how eagerly the kernel moves idle memory out to swap. | advanced | yes | until reboot |
| `mem.zswap` | **Compress memory before swapping to disk**: Keep swapped-out pages compressed in RAM so a nearly full machine stalls far less. | advanced | yes | until reboot |
| `mem.zswap-compressor` | **Pick the memory compression speed**: Trade compression strength against CPU time for zswap. | advanced | yes | until reboot |
| `mem.page-cluster` | **Faster recovery from swap (zram or SSD)**: Read back only the page you need when memory is swapped in. | advanced | yes | until reboot |
| `mem.vfs-cache` | **Remember file locations longer**: Keep directory and file-name caches in memory longer, so repeated lookups are fast. | advanced | yes | until reboot |
| `mem.compaction` | **Stop background memory tidying**: Stop the kernel from defragmenting memory in the background while you are working. | advanced | yes | until reboot |
| `mem.thp` | **Choose how memory is handed out in big chunks (THP)**: Decide when the kernel backs memory with large pages. | advanced | yes | until reboot |
| `mem.thp-defrag` | **No stutter when memory gets large pages**: Stop programs from freezing while the kernel tidies memory to make large pages. | advanced | yes | until reboot |
| `mem.min-free` | **Keep some RAM always free**: Hold back a bigger reserve of free memory so programs never have to wait for the kernel to clear space. | advanced | yes | until reboot |
| `mem.watermark-scale` | **Start freeing memory earlier**: Have the kernel begin clearing memory in the background sooner, so programs rarely wait for it. | advanced | yes | until reboot |
| `mem.ksm` | **Stop scanning memory for duplicates**: Turn off a background thread that hunts for identical pages, which is wasted work outside of VM hosts. | situational | yes | until reboot |

### Storage (7)

| ID | What you get | Tier | Admin | Lasts |
| --- | --- | --- | --- | --- |
| `io.scheduler` | **Choose how the kernel orders disk requests**: Pick the policy that decides which disk requests go first. | advanced | yes | until reboot |
| `io.readahead` | **Read ahead further on big files**: Prefetch more data when you read large files in order (game loading, video, backups). | advanced | yes | until reboot |
| `io.rq-affinity` | **Finish disk requests on the same CPU**: Complete each disk request on the core that asked for it, keeping its data in that core's fast cache. | advanced | yes | until reboot |
| `io.nomerges` | **Skip merging on very fast drives**: Let NVMe drives take small requests as they are, instead of spending CPU to merge them first. | advanced | yes | until reboot |
| `io.add-random` | **Skip wasted work on every disk request**: Stop SSDs from contributing the timing of every request to the random-number pool, which is not needed. | advanced | yes | until reboot |
| `mem.dirty` | **Avoid long freezes after big file copies**: Control how much unsaved data may pile up in RAM before the kernel writes it to disk. | advanced | yes | until reboot |
| `mem.writeback-timing` | **Smoother disk writes**: Control how often unsaved data is pushed to disk, to avoid long pauses or save laptop battery. | advanced | yes | until reboot |

### Gaming (5)

| ID | What you get | Tier | Admin | Lasts |
| --- | --- | --- | --- | --- |
| `game.max-map-count` | **Fix game crashes from running out of memory maps**: Raise the limit on memory regions a single program may map. | situational | yes | until reboot |
| `game.split-lock` | **Remove a stutter in some Windows games**: Stop the kernel from slowing programs that use split-lock memory operations. | situational | yes | until reboot |
| `gpu.amd-level` | **Keep the AMD graphics card at full speed**: Hold an AMD graphics card at high clocks so games never wait for it to speed up. | situational | yes | until reboot |
| `gpu.intel-min-clock` | **Wake the Intel graphics faster**: Raise the lowest speed of Intel integrated graphics so it is ready the moment a frame arrives. | situational | yes | until reboot |
| `gpu.dgpu-awake` | **Keep the dedicated graphics card awake**: Stop the discrete GPU from powering down between uses, so games and apps start instantly. | situational | yes | until reboot |

### Desktop & Graphics (4)

| ID | What you get | Tier | Admin | Lasts |
| --- | --- | --- | --- | --- |
| `desktop.hypr-effects` | **Plain, fast Hyprland (no animations, blur or shadows)**: Switch off window animations, background blur and drop shadows so the compositor does far less work per frame. | situational | no | this session |
| `desktop.hypr-tearing` | **Lower input lag in games (allow tearing)**: Let full-screen games skip waiting for the screen refresh, so your mouse and keyboard show up sooner. | situational | no | this session |
| `desktop.hypr-direct-scanout` | **Full-screen games skip the compositor**: Send a full-screen game directly to the monitor instead of copying it through the desktop first. | situational | no | this session |
| `desktop.gnome-animations` | **Instant GNOME / GTK interface (no animations)**: Turn off GNOME and GTK animations so windows, menus and workspaces respond instantly. | situational | no | persistent |

All kernel settings are written through one root helper that accepts only an exact allowlist of attributes and value patterns (see [SECURITY.md](SECURITY.md)); the 47 tweaks that need it ask for administrator permission once per tweak, and only work from a system-wide install (`sudo make install`, which also installs a polkit action so one password prompt covers a whole preset for about five minutes). Settings marked *until reboot* reset on restart; none edits a config file. Hyprland settings last until its config reloads.

Things deliberately **not** included: disabling CPU vulnerability mitigations, hosts-file ad blocking, and Windows-only items such as MSI interrupts, device affinities or Hyper-V/VBS switches. See the [roadmap](docs/ROADMAP.md) for the reasoning and for what may come later.

## Command line (secondary)

The TUI is the primary interface; the CLI exists for scripting, SSH and bug reports.

```
turbo doctor [--json] [--top]          scan and explain
turbo status                           summary and pending recovery
turbo tweaks list [--all] | show <id>   (list hides what your PC cannot do; --all shows it)
turbo undo [--yes]                      revert the last change TurboShift made
turbo apply power.profile=performance [--dry-run] [--yes]
turbo preset list | show <id> | apply <id> [--dry-run] [--yes]
turbo snapshots list | show <id> | restore <id> [--force] | recover
turbo network [--external] [--target H] [--lookup NAME] [--json]
turbo benchmark import <log.csv> --label NAME | list | compare A B | find
turbo game options [--gamemode] [--mangohud] [--log SECONDS] [--gpu nvidia|mesa]
turbo game run [options] -- <command> [args...]
```

Exit codes: `0` ok · `1` error · `2` usage error · `3` change rolled back / restore left items untouched. `--json` output is plain JSON (no colour, no banners), diagnostics go to stderr.

## Troubleshooting

| Symptom | What to do |
| --- | --- |
| Garbled boxes or odd glyphs | Use a UTF-8 terminal, or turn on **Settings → ASCII-only drawing**. `TERM=linux` and non-UTF-8 locales fall back automatically. |
| Colours hard to read | **Settings → Theme**: light, colour-blind or mono; `NO_COLOR=1 turbo` removes colour. |
| An optimization says "not available" | The Optimizer's detail pane gives the exact reason (e.g. `power-profiles-daemon` not installed). That is expected on machines without the feature. |
| An administrator tweak says "running from a location you can modify" | Install system-wide: `sudo make install`. |
| Interrupted run / "unfinished changes" dialog | Press **Enter** on *Recover now*, or `turbo snapshots recover`. See [docs/RECOVERY.md](docs/RECOVERY.md). |
| Terminal looks broken after a crash | `reset` (TurboShift restores the screen on normal exit, Ctrl-C and SIGTERM). |
| Something else | Run `turbo doctor --json` and attach the output to an issue. It contains no personal data beyond hardware/OS names. |

## Disclaimer

TurboShift changes system settings. It previews every change, keeps a restore point and verifies the result, but it is provided **as is, without warranty** (see [LICENSE](LICENSE)). Review a preview before confirming, and keep your own backups of anything important. "TurboShift" is a project name only: it is not affiliated with, endorsed by, or derived from any other product or game of a similar name, and it makes no use of third-party logos, fonts or artwork (the banner is plain text).

## Status and honesty

Version `1.0.0`. The command line, tweak and preset IDs, exit codes, `--json` documents and restore-point files are covered by a [stability promise](docs/STABILITY.md). What has been **verified**, and how:

- **Unit and integration tests** (`make test`, with the race detector): detection against fake `/proc` and `/sys`, the transaction engine (apply, verify, rollback, drift, crash recovery, locking, cancellation), the privilege allowlist (including a fuzz test that no accepted request can leave `/proc/sys` and `/sys`), diagnostics, benchmark statistics, CLI behaviour, exit codes and JSON contracts, restoring a restore point written by the first release, and the TUI driven end to end (scan → preview → apply → verify → undo, mouse hit-testing, every screen from 64×18 to 200×60, no-colour and ASCII output) plus seeded random key/click/resize torture runs.
- **A real kernel** (`make kernel-test`): apply, live verification, drift-aware restore, automatic rollback after a refused write, and `undo`, run against the real `/proc/sys` of a throw-away network namespace (so your system's settings are never touched). It exercises the five network tweaks that namespace allows (`net.notsent-lowat`, `net.ecn`, `net.mtu-probing`, `net.tcp-idle`, `net.tcp-fastopen`) and the shared engine path every kernel tweak uses.
- **The real binary in a pseudo-terminal** on an Arch-based laptop (CachyOS, Hyprland), read-only: dashboard, goals, scan, doctor, Optimizer, presets, dialogs, quit.
- **A clean-install smoke test plus the whole unit-test suite in disposable containers** on Arch, Ubuntu 24.04, Debian 12, Fedora 42 and openSUSE Tumbleweed, as root and as an ordinary user (`scripts/distro-test.sh`).

What has **not** been verified, and is therefore not claimed:

- **Performance gains.** No benchmark in this repository shows any tweak making any workload faster. Each tweak states the mechanism, the trade-off and how to measure it on your PC (Benchmark Lab helps); the result is yours to judge.
- Applying the CPU, power, GPU, disk, memory-timing and Hyprland/GNOME tweaks on real hardware (they use the same tested engine, but their kernel files cannot be changed from a test namespace).
- The real `pkexec`/polkit prompt flow and the polkit action file, real NetworkManager checkpoints, real MangoHud logs.
- Real hardware on other distributions, AMD-only or Intel-only GPUs, ARM.

See [docs/COMPATIBILITY.md](docs/COMPATIBILITY.md). Please report what you find.

## Documentation

[Architecture](docs/ARCHITECTURE.md) · [Recovery](docs/RECOVERY.md) · [Benchmark methodology](docs/BENCHMARK_METHODOLOGY.md) · [Compatibility](docs/COMPATIBILITY.md) · [Stability promise](docs/STABILITY.md) · [Roadmap](docs/ROADMAP.md) · [Release process](docs/RELEASE.md) · [Privacy](PRIVACY.md) · [Security](SECURITY.md) · [Third-party licences](THIRD_PARTY_NOTICES.md) · [Contributing](CONTRIBUTING.md) · [Changelog](CHANGELOG.md)

## License

MIT, see [LICENSE](LICENSE). Dependencies are all MIT or BSD-3-Clause; their notices are in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
