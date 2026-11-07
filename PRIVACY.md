# Privacy

TurboShift is local software. It has **no telemetry, no analytics, no crash reporting, no accounts, no update checks, no advertising, and no cloud or AI features**. There is no code that sends data to a TurboShift server because there is no TurboShift server. It loads **no web fonts, scripts, images or any other resource from a website**: a terminal program has none, and this repository's documents link to no third-party tracker or CDN. A test (`legal_test.go`) fails the build if network code or an unreviewed URL appears anywhere outside the Network Doctor.

## What it reads

Only your own machine, as your own user: `/proc`, `/sys`, `/etc/os-release`, `/etc/resolv.conf`, and the output of local tools when they are installed: `systemctl`, `powerprofilesctl`, `tuned-adm`, `nmcli`, `iw`, `ethtool`, `tc`, `busctl`, `lspci`, `sensors`, `nvidia-smi`, `hyprctl` and `gsettings`. It never reads your documents, browser data, passwords, keys or other users' data. Process names and CPU use (shown in the Doctor with `--top`) come from `/proc` and stay on screen.

## What it writes

Only to these directories (XDG-aware), and only things it created:

| Path | Contents |
| --- | --- |
| `~/.config/turboshift/config.json` | Theme, ASCII mode, ping target, DNS lookup name, extra log folders. |
| `~/.local/state/turboshift/snapshots/` | Restore points: tweak ids and the before/after values of the settings it changed. For the DNS tweak that includes your connection's DNS server addresses and its NetworkManager connection id. |
| `~/.local/share/turboshift/benchmarks/` | Summaries of MangoHud logs you imported. Original logs are never modified or deleted. |
| `~/.local/share/turboshift/mangologs/` | Where `turbo game run --log` asks MangoHud to write. |

Deleting these directories removes everything TurboShift has ever stored. It also changes the **system settings you approve** (see the preview before every change); those changes are what the restore points undo.

## What touches the network

By default **nothing**. The Network Doctor tests your link and your router (a local address) only.

If, and only if, you enable *Allow tests beyond my router* (TUI key `e`, CLI `--external`), the Network Doctor sends, and the full host list is shown first:

- ICMP echo packets to the ping target (default `1.1.1.1`, a public resolver run by Cloudflare),
- DNS queries for one lookup name (default `example.com`) to each of your configured resolvers (which usually forward them to your ISP or another upstream),
- optionally, an HTTP(S) download or upload to a URL **you** typed, to measure latency under load. TurboShift ships with **no** default test URL and follows redirects only within the host you were shown. Uploads send only zero-filled data, never yours.

**Every external host sees your public IP address**, as with any internet connection, and may log it under its own policy. The ping target and the URL are yours to choose; use a server you own or a service whose terms allow automated tests, and remember a load test uses real bandwidth (it can count against a data cap). Change the defaults in Settings or with `--target`, `--lookup`, `--load-url` and `--upload-url`.

TurboShift makes no other network connection. It does not check for updates. The `game run` command starts the program you name, and that program's network use is its own.

## The DNS tweak sends your lookups to a company

`net.dns` is the one optimization that changes where your traffic goes. If you choose Cloudflare, Quad9 or Google, that provider receives every domain name you look up and your IP address, under its own privacy policy and terms. It is never part of a preset and never applied automatically; the preview names the provider before you confirm. Do not use it on a network whose policy requires its own DNS.

## Sharing output in a bug report

`turbo doctor --json` and `turbo network --json` contain hardware and OS names plus **your router's and DNS servers' addresses and the names of your network interfaces** (which can show a VPN, Docker or the PCI layout). Add `--redact` before pasting them anywhere public: it replaces private IP addresses, MAC addresses and interface names. Wi-Fi network names are omitted from `network --json` unless you pass `--include-ssid`. Read the output once yourself before posting; redaction is a safety net, not a guarantee.

## Privileged operations

Some optimizations write kernel settings as root through `pkexec`, limited to a fixed allowlist of `/proc/sys` and `/sys` attributes and value patterns. See [SECURITY.md](SECURITY.md).
