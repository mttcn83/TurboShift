# Roadmap: ideas that are **not** implemented

Nothing below exists today; the app does not pretend otherwise. Each item must clear the "real mechanism, stated trade-off, measurable" bar before shipping.

## Optimizations (candidates)

- Memory: zram sizing and swappiness guidance (with PSI evidence), `vm.max_map_count` for specific games.
- Storage: I/O scheduler per device type, `noatime` advice (read-only advice first).
- Network: DNS resolver choice with measured lookup times, congestion control (BBR) with before/after Network Doctor runs.
- GPU: NVIDIA/AMD power management hints, hybrid-graphics offload checks.
- CPU: EPP and boost control through the existing power manager when one owns it.
- A polkit action file with a clear prompt text for the privileged helper.

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
