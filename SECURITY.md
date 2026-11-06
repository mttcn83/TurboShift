# Security

## Reporting a vulnerability

Please open a private security advisory on the repository (or email the maintainers listed there) rather than a public issue. Include the version (`turbo --version`), what you did, and what you expected. We aim to acknowledge within a few days.

## Threat model

TurboShift runs as an ordinary user and changes system settings on that user's behalf. The things worth protecting:

1. **No silent privilege escalation.** The app never runs as root. A warning is shown if it is started as root.
2. **A tiny privileged surface.** The only root operation is `write-sysfs`, and the allowlist is a single anchored pattern: `/sys/devices/system/cpu/cpu<N>/cpufreq/scaling_governor` with a value matching `^[a-z0-9_-]{1,32}$`. It is validated in the unprivileged process (fail fast, before any prompt) *and again* in the root helper (`turbo __priv`, started via `pkexec <path-to-turbo> __priv <op> <path> <value>`). Arguments travel as an argv array; no shell is ever involved. The helper refuses to run unless its effective UID is 0, and refuses to write attributes that do not already exist (it never creates files). The unprivileged side only offers the polkit path when the running binary and every parent directory are root-owned, not group/world-writable and not symlinks (otherwise any process of your user could swap the binary that polkit launches as root); run `sudo make install` to satisfy this.
3. **No data loss, no half-applied states.** Changes are journaled before they happen, written atomically with a SHA-256 checksum and `0600` permissions, protected by an exclusive file lock (one transaction at a time), verified after applying, and rolled back automatically on failure. Interrupted transactions are recovered on next start.
4. **Journal integrity.** A corrupted or tampered restore point fails its checksum and is ignored and reported, never acted on. Restore ids are validated against a strict pattern, so path-like ids cannot reach the filesystem.
5. **Journal values are untrusted too.** Values read back from restore points are re-validated (`^[a-z0-9][a-z0-9_-]{0,31}$`) before they reach a command line, so a tampered journal cannot inject options into `powerprofilesctl`/`nmcli`.
6. **Untrusted input.** Benchmark logs are parsed with bounded line lengths and strict row validation. Command output from system tools is parsed defensively; unexpected output is treated as "unknown", not as a trusted value.
7. **No outbound connections** unless the user explicitly consents (see [PRIVACY.md](PRIVACY.md)).

## Out of scope / known limits

- The pkexec path trusts the polkit configuration of the system (default `auth_admin`). A custom polkit action file is on the roadmap.
- A local attacker who already controls your user account can edit your own state files; the checksum detects accidents and casual tampering, not a determined same-user attacker.
- The privileged flow was verified with a fake executor, unit tests of the allowlist and of the binary-ownership check, and by running the helper's refusals in containers, **not** with a real polkit prompt (see the README's status section).
- Dependencies are checked with `govulncheck` (clean at release) and are all MIT/BSD-3; see [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
