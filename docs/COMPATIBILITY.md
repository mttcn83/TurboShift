# Compatibility

TurboShift is **Linux-only** and developed on Arch/CachyOS (kernel 7.x, Wayland/Hyprland, Intel + NVIDIA hybrid laptop). It uses no distribution-specific package manager. The binary is static (no libc dependency), x86-64 and arm64.

## What is detected and from where

| Area | Source |
| --- | --- |
| OS, kernel, session | `/etc/os-release`, `/proc/sys/kernel/osrelease`, `XDG_SESSION_TYPE`, `XDG_CURRENT_DESKTOP` |
| CPU, governor, EPP, turbo, throttle counters | `/proc/cpuinfo`, `/sys/devices/system/cpu/*/cpufreq`, `thermal_throttle` |
| Power managers | `systemctl is-active` for power-profiles-daemon, TuneD, TLP, auto-cpufreq, … |
| Memory, zram, pressure | `/proc/meminfo`, `/sys/block` (zram devices), `/proc/pressure/*` |
| GPUs | `/sys/class/drm`, PCI ids, runtime PM status; load/VRAM from `nvidia-smi` only when the dGPU is already awake |
| Storage | `/proc/self/mounts` + `statfs` |
| Network | `/proc/net/route`, `/sys/class/net`, `/etc/resolv.conf`, `iw` |

Anything unreadable is reported as unknown with a reason; it is never invented.

## Optional tools

Used when present, otherwise the dependent feature explains what is missing: `powerprofilesctl`, `nmcli`, `iw`, `busctl`, `systemctl`, `pkexec`, `nvidia-smi`, `gamemoderun`, `mangohud`.

## Verification matrix (honest)

| Environment | Status |
| --- | --- |
| Simulated machine (tests, `-race`) | Full apply / verify / rollback / restore / recovery paths exercised |
| Arch-based hybrid-graphics laptop, read-only run of the real binary | Detection, Doctor, Optimizer preview, TUI screens verified |
| Applying changes with real `powerprofilesctl` / `nmcli` | **Not yet verified** |
| Real NetworkManager checkpoint, real polkit prompt | **Not yet verified** |
| Real MangoHud logs | **Not yet verified** (parser written from MangoHud's source) |
| Container matrix (`scripts/distro-test.sh`): Arch, Ubuntu 24.04, Debian 12, Fedora 42, openSUSE Tumbleweed; root and uid 1000 | **Passed 10/10**: clean install, CLI, graceful "not available" on missing power manager, privileged-helper refusals, TUI in a pty at 80×24 and large, all 11 unit-test packages. Containers have no power manager/Wi-Fi/GPU, so tweaks themselves were not exercised there |
| Real hardware on Debian/Ubuntu/Fedora/openSUSE, AMD-only or Intel-only GPUs, KDE/GNOME specifics | **Not yet verified** |
| ARM / non-x86 | Detection is generic; unverified |

## Terminals

Tested in a 256-colour xterm-compatible terminal emulator. Falls back to ASCII on `TERM=linux`/`dumb` and non-UTF-8 locales; monochrome via `NO_COLOR`. Minimum size 64×18; sizes from 64×18 to 200×50 are covered by automated layout tests.
