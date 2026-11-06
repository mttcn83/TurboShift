# Privacy

TurboShift is local software. It has **no telemetry, no analytics, no crash reporting, no accounts, no update checks, and no cloud or AI features**. There is no code that sends data to a TurboShift server because there is no TurboShift server.

## What it reads

Only your own machine, as your own user: `/proc`, `/sys`, `/etc/os-release`, `/etc/resolv.conf`, and the output of local tools (`systemctl`, `powerprofilesctl`, `nmcli`, `iw`, `nvidia-smi`, `busctl`). It never reads your files, browser data, credentials or other users' data. Process names and CPU use (shown in the Doctor) come from `/proc` and stay on screen.

## What it writes

Only to these directories (XDG-aware) and only things it created:

| Path | Contents |
| --- | --- |
| `~/.config/turboshift/config.json` | Theme, ASCII mode, ping target, DNS lookup name, extra log folders. |
| `~/.local/state/turboshift/snapshots/` | Restore points: tweak ids and the before/after values of settings it changed. |
| `~/.local/share/turboshift/benchmarks/` | Summaries of MangoHud logs you imported. Original logs are never modified or deleted. |
| `~/.local/share/turboshift/mangologs/` | Where `turbo game run --log` asks MangoHud to write. |

Deleting these directories removes everything TurboShift has ever stored.

## What touches the network

By default **nothing**. The Network Doctor tests your link and your router (a local address) only.

If, and only if, you enable *Allow tests beyond my router* (TUI key `e`, CLI `--external`), the Network Doctor sends:

- ICMP echo packets to the ping target (default `1.1.1.1`),
- DNS queries for one lookup name (default `example.com`) to each of your configured resolvers,
- optionally, an HTTP download or upload to a URL **you** typed, to measure latency under load.

The full list of hosts is displayed before you run it, and it is exactly what is contacted. You can change the defaults in Settings. Wi-Fi network names are shown on screen but are omitted from `--json` output unless you pass `--include-ssid`.

TurboShift makes no other network connection. It does not check for updates.

## Privileged operations

The only administrator action is writing a CPU governor, requested through polkit and limited to one sysfs path pattern. See [SECURITY.md](SECURITY.md).
