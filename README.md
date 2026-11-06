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

Keys: `1`-`8` or `Tab` switch screens · `↑↓` move · `Space` select · `Enter` open/confirm · `/` search · `?` help · `q` quit. The mouse works everywhere (tabs, rows, buttons, dialogs, wheel). `NO_COLOR` and `TERM=dumb` are honoured; the Linux console and non-UTF-8 locales get an ASCII-only rendering automatically.

### A typical session

1. `turbo` → press **Enter** on the Dashboard (scan).
2. Press `o` (**Optimize My PC**). If something is recommended you see a **preview** of the exact change, its trade-off and how to measure it. Nothing has happened yet.
3. **Enter** applies it: restore point → apply → verify → result. The result dialog has **Undo** (`u`).
4. Later: **Vault** shows the restore point; `Enter` restores it. Settings that another program changed since are *left alone and reported* unless you choose force.

## Optimizations included (26)

Every entry is a real kernel or system setting with a stated mechanism, a trade-off and a way to measure it. Nothing is applied until you pick a value and confirm the preview; each change gets a restore point, is verified against the live system, and can be undone.

**What "optimization" means here.** These settings help *specific* situations (a power-saving mode left on while plugged in, a slow resolver, a game that hits the old memory-mapping limit, a stalling disk). On a machine that is already configured well, most of them change nothing, and TurboShift says so instead of pretending: it only *recommends* the few that live evidence supports (for example "power-saver profile while plugged in") and offers the rest for you to try and measure in Benchmark Lab or Network Doctor. Each one is shown only when your hardware supports it and no power manager (power-profiles-daemon, TLP, TuneD) already owns the setting.

| ID | What it does | Tier | Risk | Admin | Lasts |
| --- | --- | --- | --- | --- | --- |
| `power.profile` | Pick the system power profile (Performance / Balanced / Power saver) through your desktop's own power service | recommended | low | no | persistent |
| `cpu.governor` | Set the CPU frequency governor on every core. Only offered when no power manager is running | advanced | medium | yes | until reboot |
| `cpu.epp` | Tell the CPU's own frequency controller whether to favour speed or power saving | situational | low | yes | until reboot |
| `cpu.boost` | Allow or forbid the CPU to run above its base clock | situational | low | yes | until reboot |
| `platform.profile` | Switch the laptop firmware between quiet, balanced and performance modes | advanced | low | yes | until reboot |
| `cpu.autogroup` | Keep the desktop responsive while something heavy compiles or renders | advanced | low | yes | until reboot |
| `pcie.aspm` | Keep PCI Express links fully awake to avoid the wake-up latency of power-saving link states | advanced | low | yes | until reboot |
| `usb.autosuspend` | Stop mice, keyboards and headsets from going to sleep between uses, removing the wake-up delay | situational | low | yes | until reboot |
| `net.wifi-powersave` | Turn Wi-Fi power saving off (or back on) for the network you are connected to | situational | low | no | persistent |
| `net.dns` | Use a different DNS resolver on this connection if your router's or ISP's is slow | situational | low | no | persistent |
| `net.tcp-cc` | Choose how TCP connections react to loss and delay (downloads, web, game launchers) | advanced | low | yes | until reboot |
| `net.qdisc` | Pick how queued packets are scheduled on network interfaces, to keep latency low when the link is busy | advanced | low | yes | until reboot |
| `net.tcp-fastopen` | Skip one network round trip when reconnecting to servers you have visited before | advanced | low | yes | until reboot |
| `net.tcp-idle` | Stop long-lived connections from slowing down after a short pause | advanced | low | yes | until reboot |
| `net.budget` | Let the kernel process more incoming packets per interrupt on very busy links | advanced | low | yes | until reboot |
| `mem.swappiness` | Set how eagerly the kernel moves idle memory to swap | advanced | low | yes | until reboot |
| `mem.page-cluster` | Reduce the delay when memory is swapped back in from fast swap such as zram | advanced | low | yes | until reboot |
| `mem.vfs-cache` | Keep directory and file lookup caches in memory longer | advanced | low | yes | until reboot |
| `mem.compaction` | Stop the kernel from defragmenting memory in the background while you are working | advanced | low | yes | until reboot |
| `mem.thp` | Choose when the kernel backs memory with large pages | advanced | low | yes | until reboot |
| `io.scheduler` | Choose how the kernel orders reads and writes to your disks | advanced | low | yes | until reboot |
| `io.readahead` | Prefetch more data when you read large files sequentially (game loading, video, backups) | advanced | low | yes | until reboot |
| `mem.dirty` | Control how much unwritten data may collect in RAM before the kernel flushes it to disk | advanced | low | yes | until reboot |
| `game.max-map-count` | Raise the limit on memory regions a single program may map (some games crash or stutter at the old limit) | situational | low | yes | until reboot |
| `game.split-lock` | Stop the kernel from slowing programs that use split-lock memory operations (affects some Windows games under Wine/Proton) | situational | low | yes | until reboot |
| `gpu.amd-level` | Hold an AMD graphics card at high clocks to avoid ramp-up lag in games | situational | medium | yes | until reboot |

All kernel settings are written through one root helper that accepts only an exact allowlist of attributes and value patterns (see [SECURITY.md](SECURITY.md)); the 20 tweaks that need it ask for administrator permission once per change, and only work from a system-wide install (`sudo make install`). Settings marked *until reboot* reset on restart; none edits a config file.

More are on the [roadmap](docs/ROADMAP.md); each must clear the same evidence bar.

## Command line (secondary)

The TUI is the primary interface; the CLI exists for scripting, SSH and bug reports.

```
turbo doctor [--json] [--top]          scan and explain
turbo status                           summary and pending recovery
turbo tweaks list | show <id>
turbo apply power.profile=performance [--dry-run] [--yes]
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

Version `0.2.0` (public pre-release). What has been **verified**:

- Unit and integration tests (`make test`, run with the race detector): detection against fake `/proc` and `/sys`, the transaction engine (apply, verify, rollback, drift, crash recovery, locking, concurrency, cancellation), the privilege allowlist, diagnostics, benchmark statistics, CLI behaviour and exit codes, and the TUI driven end to end (scan → preview → apply → verify → undo, mouse hit-testing, every screen at 64×18 up to 200×50, no-colour and ASCII output).
- The real binary run in a pseudo-terminal on an Arch-based machine, **read-only**: dashboard, scan, doctor, optimizer preview, all screens, dialogs, quit.
- A clean-install smoke test plus the whole unit-test suite in disposable containers on **Arch, Ubuntu 24.04, Debian 12, Fedora 42 and openSUSE Tumbleweed**, as root and as an ordinary user (`scripts/distro-test.sh`): install, `--help`/`--version`, doctor/status/tweaks/snapshots/benchmark/game/network commands, clean refusal on unsupported hardware, privileged-helper refusals, and the TUI driven in a pseudo-terminal at 80×24 and a large size.

What has **not** been verified, and is therefore not claimed:

- Applying changes on a real system (all apply/rollback logic is tested against a simulated machine, not real `powerprofilesctl`/`nmcli`).
- Real NetworkManager checkpoints, and the real `pkexec`/polkit prompt flow.
- Importing real MangoHud logs (the parser follows MangoHud's source; no real log was available).
- Real hardware on other distributions (containers have no power manager, Wi-Fi or GPU, so this proves install, detection and graceful degradation there, not the tweaks themselves), AMD/Intel-only GPUs.

See [docs/COMPATIBILITY.md](docs/COMPATIBILITY.md). Please report what you find.

## Documentation

[Architecture](docs/ARCHITECTURE.md) · [Recovery](docs/RECOVERY.md) · [Benchmark methodology](docs/BENCHMARK_METHODOLOGY.md) · [Compatibility](docs/COMPATIBILITY.md) · [Roadmap](docs/ROADMAP.md) · [Release process](docs/RELEASE.md) · [Privacy](PRIVACY.md) · [Security](SECURITY.md) · [Third-party licences](THIRD_PARTY_NOTICES.md) · [Contributing](CONTRIBUTING.md) · [Changelog](CHANGELOG.md)

## License

MIT, see [LICENSE](LICENSE). Dependencies are all MIT or BSD-3-Clause; their notices are in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
