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

## Which tweaks need what

Each tweak appears as *available* only when its kernel file exists and nothing else owns it, so on any machine a large part of the 54 will correctly read "not available":

| Needs | Tweaks |
| --- | --- |
| intel_pstate or amd-pstate (active mode), no power manager | `cpu.epp`, `cpu.boost` (also needs the kernel boost switch) |
| ACPI platform profile (many laptops), no power manager | `platform.profile` |
| NetworkManager (+ `iw` for Wi-Fi checks) | `net.wifi-powersave`, `net.dns` |
| Kernel with the algorithm/module loaded | `net.tcp-cc` (BBR needs `tcp_bbr`), `net.qdisc` |
| Swap or zram configured | `mem.swappiness`, `mem.page-cluster` |
| AMD GPU with amdgpu | `gpu.amd-level` |
| Linux 6.2+ | `game.split-lock` |
| Any Linux | the remaining sysctl/sysfs tweaks |

## Optional tools

Used when present, otherwise the dependent feature explains what is missing: `powerprofilesctl`, `nmcli`, `iw`, `busctl`, `systemctl`, `pkexec`, `nvidia-smi`, `gamemoderun`, `mangohud`.

## Verification matrix (honest)

| Environment | Status |
| --- | --- |
| Simulated machine (tests, `-race`) | Full apply / verify / rollback / restore / recovery paths exercised |
| Arch-based hybrid-graphics laptop, read-only run of the real binary | Detection, Doctor, Optimizer preview, TUI screens verified |
| Real kernel, throw-away network namespace (`make kernel-test`, repeatable): apply, live verify, drift-aware restore, rollback after a refused write, `undo` | **Passed** for `net.notsent-lowat`, `net.ecn`, `net.mtu-probing`, `net.tcp-idle`, `net.tcp-fastopen` on kernel 7.3 (CachyOS); the host's own settings were checked unchanged afterwards |
| Applying changes with real `powerprofilesctl` / `nmcli` / real kernel files on real hardware (the other 49 tweaks) | **Not yet verified**; every tweak is tested end to end against the simulated machine only (Hyprland and GNOME tweaks use a fake `hyprctl`/`gsettings`, so real option names across Hyprland versions are unverified) |
| Real NetworkManager checkpoint, real polkit prompt | **Not yet verified** |
| Real MangoHud logs | **Not yet verified** (parser written from MangoHud's source) |
| Container matrix (`scripts/distro-test.sh`): Arch, Ubuntu 24.04, Debian 12, Fedora 42, openSUSE Tumbleweed; root and uid 1000 | **Passed 10/10**: clean install, CLI, graceful "not available" on missing power manager, privileged-helper refusals, TUI in a pty at 80×24 and large, all 11 unit-test packages. Containers have no power manager/Wi-Fi/GPU, so tweaks themselves were not exercised there |
| Real hardware on Debian/Ubuntu/Fedora/openSUSE, AMD-only or Intel-only GPUs, KDE/GNOME specifics | **Not yet verified** |
| ARM / non-x86 | Detection is generic; unverified |

## Terminals

Tested in a 256-colour xterm-compatible terminal emulator. Falls back to ASCII on `TERM=linux`/`dumb` and non-UTF-8 locales; monochrome via `NO_COLOR`. Minimum size 64×18; sizes from 64×18 to 200×50 are covered by automated layout tests.
