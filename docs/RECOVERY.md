# Recovery

Every change TurboShift makes is journaled first, so there is always a way back. Three situations:

## 1. You want to undo a change

- **TUI:** open the **Vault** (`7`), select the restore point, press `Enter`, confirm. Or press `u` in the result dialog right after applying.
- **CLI:** `turbo snapshots list`, then `turbo snapshots restore <id>`.

If another program changed one of the settings after TurboShift did, that item is **left alone and reported as drifted**; TurboShift will not silently overwrite someone else's change. To restore anyway: Vault `f`, or `turbo snapshots restore <id> --force`.

## 2. A change failed

Nothing for you to do. The engine reverts everything already changed in that transaction and the result dialog says "Nothing was changed" and why. The restore point is marked `rolled_back`.

If the dialog says **Rollback incomplete**, the failure was followed by a failure to revert. The message names what could not be reverted. The restore point is marked `rollback_failed` and is treated like an interrupted change: the TUI offers *Recover now* on the next start, or run `turbo snapshots recover` once the cause is gone (for example the daemon is running again). It retries only the items that are still changed. New changes are refused until it succeeds. `turbo snapshots show <id>` shows the journal (before/after values per setting) if you prefer to fix a setting by hand.

## 3. TurboShift was interrupted mid-change

A crash, power loss or `kill -9` during a transaction leaves a snapshot in the `applying` state (or `rollback_failed`, see above). On the next start the TUI shows **Unfinished changes found** and offers *Recover now*; from the CLI use `turbo snapshots recover`. Recovery reverts every item that may have been touched, using the "before" values saved *before* the change started. New changes are refused until recovery is done, so you cannot stack a change on a half-applied one.

## Manual last resort

Restore points are plain JSON in `~/.local/state/turboshift/snapshots/<id>.json`. Each item lists `before` values you can set by hand:

- Power profile: `powerprofilesctl set <before>`
- CPU governor: `echo <before> | sudo tee /sys/devices/system/cpu/cpu*/cpufreq/scaling_governor`
- Wi-Fi power saving: `nmcli connection modify <uuid> 802-11-wireless.powersave <before>` then `nmcli device reapply <iface>`

A NetworkManager checkpoint created for a Wi-Fi change rolls itself back after 60 seconds if it is not committed, so a broken connection heals even if TurboShift dies.

## What a restore point does *not* cover

Only settings that TurboShift's own tweaks manage. It is not a system backup, does not protect your files, and does not undo changes made by other tools.
