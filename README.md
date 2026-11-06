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
- **Never root.** The app runs as you. The single privileged operation (writing a CPU governor) goes through polkit (`pkexec`) to a helper that re-validates an allowlist of exactly one sysfs attribute pattern, and only when the binary is root-owned and not user-writable. There is no "run this command as root" primitive.
- **Honest measurement.** Benchmark comparisons use whole-run statistics and an exact permutation test, and say "insufficient data" instead of guessing.
- **Private.** Nothing leaves your machine unless you explicitly enable external network tests, and the exact destinations are listed first. See [PRIVACY.md](PRIVACY.md).

## Install

TurboShift is a single static binary for Linux (x86-64 or arm64). There is nothing to configure and no account to create.

**Option A: download a release binary** (no Go needed). From the [Releases page](https://github.com/mttcn83/TurboShift/releases) grab `turbo-linux-amd64` (or `-arm64`) and `SHA256SUMS`, then:

```bash
sha256sum -c --ignore-missing SHA256SUMS
sudo install -m755 turbo-linux-amd64 /usr/local/bin/turbo
turbo
```

**Option B: build from source.** Needs Go 1.27+ and `git` (Arch: `sudo pacman -S go git` · Debian/Ubuntu: `sudo apt install golang git` · Fedora: `sudo dnf install golang git` · openSUSE: `sudo zypper install go git`; if your distro's Go is older than 1.27, get it from [go.dev/dl](https://go.dev/dl/)).

```bash
git clone https://github.com/mttcn83/TurboShift.git && cd TurboShift
make build            # builds ./turbo
sudo make install     # installs to /usr/local/bin (PREFIX=... to change)
turbo
```

You can also just run `./turbo` from the build folder. Everything works that way except the one administrator action (`cpu.governor`): for safety TurboShift only hands work to polkit when the binary is root-owned and not writable by your user, so that tweak shows as unavailable until you `sudo make install` (or install it system-wide some other way). See [SECURITY.md](SECURITY.md).

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

## Optimizations included

Deliberately small, each real and measurable:

| ID | What | Tier | Risk | Needs admin |
| --- | --- | --- | --- | --- |
| `power.profile` | System power profile via power-profiles-daemon (performance / balanced / power-saver). Recommends balanced if power-saver is left on while plugged in. | recommended | low | no |
| `net.wifi-powersave` | Wi-Fi power saving per NetworkManager connection, verified on the live radio. Protected by a NetworkManager checkpoint that auto-reverts if connectivity breaks. | situational | low | no |
| `cpu.governor` | CPU frequency governor, only offered when no power manager owns it (avoids fighting TLP/PPD/TuneD). | advanced | medium | yes (polkit) |

More tweaks are on the [roadmap](docs/ROADMAP.md); each must clear the same evidence bar before it ships.

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
| `cpu.governor` says "running from a location you can modify" | Install system-wide: `sudo make install`. |
| Interrupted run / "unfinished changes" dialog | Press **Enter** on *Recover now*, or `turbo snapshots recover`. See [docs/RECOVERY.md](docs/RECOVERY.md). |
| Terminal looks broken after a crash | `reset` (TurboShift restores the screen on normal exit, Ctrl-C and SIGTERM). |
| Something else | Run `turbo doctor --json` and attach the output to an issue. It contains no personal data beyond hardware/OS names. |

## Disclaimer

TurboShift changes system settings. It previews every change, keeps a restore point and verifies the result, but it is provided **as is, without warranty** (see [LICENSE](LICENSE)). Review a preview before confirming, and keep your own backups of anything important. "TurboShift" is a project name only: it is not affiliated with, endorsed by, or derived from any other product or game of a similar name, and it makes no use of third-party logos, fonts or artwork (the banner is plain text).

## Status and honesty

Version `0.1.0` (first public pre-release). What has been **verified**:

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
