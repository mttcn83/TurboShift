# Stability promise (1.0)

From 1.0.0 TurboShift follows [semantic versioning](https://semver.org). This page lists what scripts and other tools may rely on. Anything not listed (wording of human output, screen layouts, key bindings, the order of lines) can change in any release.

## Stable (changes only in a new major version)

**Commands and flags.** The command names, sub-commands and flags in `turbo --help`: `doctor`, `status`, `network`, `tweaks list|show`, `apply`, `preset list|show|apply`, `undo`, `snapshots list|show|restore|recover`, `benchmark …`, `game …`, `completion`. New commands and flags may be added in minor versions.

**Tweak and preset IDs.** An ID such as `net.ecn` or `gaming` is never renamed or reused for something else. A tweak may be retired in a minor version (the engine then restores old restore points that mention it as "no longer known" instead of guessing).

**Exit codes.** `0` success · `1` operation failed · `2` usage error · `3` a transaction was rolled back, or a restore left items untouched.

**`--json` documents.** Every document is a JSON object with `"schema_version": 1` and `snake_case` keys:

| Command | Top-level keys |
| --- | --- |
| `doctor --json` | `schema_version`, `report` (the hardware/OS probe, which has its own `schema_version`), `findings`, `top_processes` (only with `--top`) |
| `tweaks list --json` | `schema_version`, `tweaks[]`: `id name category tier risk scope available current reason recommended` |
| `preset list --json` | `schema_version`, `presets[]`: `id name summary best cost items[]{tweak_id,value}` |
| `snapshots list --json` | `schema_version`, `snapshots[]` (restore points, below), `corrupt[]` (ids that failed their integrity check) |
| `status --json` | `schema_version`, `warnings notices snapshots interrupted corrupt_snapshots` |
| `network --json` | `schema_version`, `link gateway destinations skipped diagnosis …` |
| `benchmark list --json` | `schema_version`, `runs[]` |
| `benchmark compare A B --json` | `schema_version`, `a b overall summary metrics[] warnings` |

Within a major version keys are only **added**, never renamed, removed or re-typed. Empty lists are `[]`, never `null`. Maps keyed by the tweak's own state names (`current`, `params`, `before`, `after`) can contain any key those tweaks use (for example `ipv4.dns`). Read unknown keys and values defensively.

**Restore points.** Files under `$XDG_STATE_HOME/turboshift/snapshots/` carry a `format` number (1) and a SHA-256 checksum. Every 1.x release restores every restore point written by any earlier release back to 0.1. A restore point written by a *newer* format is refused with a clear message instead of being half-read.

**What the root helper accepts.** `turbo __priv` is an internal interface and not for scripting, but its security rule is stable: it writes only the allowlisted kernel attributes (see [SECURITY.md](../SECURITY.md)). The allowlist may grow in minor versions with new tweaks; it never grows to commands, wildcards across `/`, or files outside `/proc/sys` and `/sys`.

## Not promised

- That any tweak makes any particular PC faster. TurboShift previews, applies, verifies and undoes changes; whether the change helps is for you to measure (every tweak says how).
- Output of the interactive app, human-readable CLI text, and colours.
- Behaviour on kernels or desktops that rename the settings a tweak touches. The tweak then reports itself unavailable; it does not guess.
