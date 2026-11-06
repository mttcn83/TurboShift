# Roadmap: ideas that are **not** implemented

Nothing below exists today; the app does not pretend otherwise. Each item must clear the "real mechanism, stated trade-off, measurable" bar before shipping.

## Optimizations (candidates)

The first 54 are shipped (see the README). Still candidates:

- Deliberately **not** planned: disabling CPU vulnerability mitigations (a real security regression for a few percent), hosts-file ad/tracker blocking (a different tool's job, and it edits system files), and Windows-only ideas with no Linux equivalent (MSI mode, device affinities, VBS/Hyper-V, large-send-offload toggles on NICs that Linux already tunes per driver).
- KDE Plasma animation/latency settings (needs a verified reload path; GNOME and Hyprland are covered).
- Intel `xe` driver GPU frequency controls (the `i915` paths are covered).

- GPU: NVIDIA power-management hints (needs `nvidia-smi`/NVML writes, a separate privilege story), hybrid-graphics offload checks.
- Per-game process priority and pausing user-selected background jobs while a game runs.
- zram sizing guidance (with PSI evidence) and `noatime` advice (read-only first).
- Persisting a chosen setting across reboots (today every kernel tweak is deliberately runtime-only).

## Product

- Per-game profiles (the Gaming screen only builds launch options today).
- Scheduled recurring Network Doctor runs with a local history.
- Benchmark Lab: import by arbitrary file path in the TUI (CLI supports it), scrollable comparison pane, CSV export.
- Packaging: AUR, release binaries with checksums.
- Translations.
- `turbo doctor --report` bundle that redacts hostnames/SSIDs for bug reports.

## Verification debt

- Real-hardware apply/rollback runs on more distributions.
- Real polkit and NetworkManager checkpoint verification.
- Real MangoHud log fixtures in the test suite.
