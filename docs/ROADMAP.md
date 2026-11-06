# Roadmap: ideas that are **not** implemented

Nothing below exists today; the app does not pretend otherwise. Each item must clear the "real mechanism, stated trade-off, measurable" bar before shipping.

## Optimizations (candidates)

The first 26 are shipped (see the README). Still candidates:

- GPU: NVIDIA power-management hints (needs `nvidia-smi`/NVML writes, a separate privilege story), hybrid-graphics offload checks.
- Per-game process priority and pausing user-selected background jobs while a game runs.
- zram sizing guidance (with PSI evidence) and `noatime` advice (read-only first).
- Persisting a chosen setting across reboots (today every kernel tweak is deliberately runtime-only).
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
