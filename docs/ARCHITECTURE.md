# Architecture

TurboShift is one Go binary (`turbo`) with two front ends over one core.

```
cmd/turbo            entry point: TUI when run in a terminal with no args, else CLI
internal/tui         Bubble Tea v2 application (primary UX)
internal/cli         scriptable subcommands (secondary UX)
internal/app         wires real adapters, stores and the engine together
─────────────────────────────────────────────────────────────────────────────
internal/engine      transactions: preview → journal → apply → verify → rollback → restore
internal/tweaks      the Tweak contract, registry, presets and the concrete tweaks
                     (filetweak.go: kernel files via the root helper; desktop.go: per-user
                     settings via hyprctl/gsettings; one file per family of facts)
internal/snapshot    journal / restore points (atomic, checksummed, locked)
internal/privilege   the only root boundary: allowlisted actions, pkexec helper
─────────────────────────────────────────────────────────────────────────────
internal/detect      read-only system probe → Report (JSON schema v1)
internal/diagnostics findings (pure rules over a Report), live metrics, network doctor
internal/benchmark   MangoHud parser, run store, statistics and A/B comparison
internal/integrations GameMode / MangoHud / GPU launch-option builder
internal/stats       small numeric helpers (median, percentile, CV)
internal/platform    FS and Runner interfaces + in-memory fakes (the OS boundary)
internal/simtest     test-only stateful simulated machine
internal/config      XDG paths and the tiny preferences file
```

## The OS boundary

Everything that touches the machine goes through two interfaces in `platform`:

- `FS` (read/write files, `Statfs`, readlink), implemented by `OSFS` and by `MemFS` for tests.
- `Runner` (execute a program with an argument array, never a shell; 10 s timeout; `LC_ALL=C`), implemented by `ExecRunner`, `FakeRunner`, and `simtest.Machine`.

This is why the same code can be tested against a simulated laptop and run for real, and why tests never need root or a particular distribution.

## Detect → Diagnose

`detect.Probe` produces a `Report`. Unknown is never zero: values that cannot be read are `nil` pointers or empty, and probing problems become `Report.Warnings`. `diagnostics.Diagnose` is a pure function `Report → []Finding` with explicit, documented rules; each finding states the measurement it rests on. There is no scoring model.

## The tweak contract

```go
type Tweak interface {
    Meta() Meta                                  // id, text, tier, risk, scope, options
    Assess(ctx, env) Assessment                  // available? why not? current state? recommend?
    Plan(ctx, env, params) (Plan, error)         // pure preview: Before, After, Changes, Warnings
    Apply(ctx, env, Plan) error
    Read(ctx, env, want State) (State, error)    // live state for verification and drift
    Restore(ctx, env, Plan) error                // idempotent
}
```

Optional: `Checker` (verify the *effective* result, e.g. the live Wi-Fi radio, not just the stored profile) and `Guarded` (a safety net around disruptive changes, e.g. a NetworkManager checkpoint).

State is a flat `map[string]string`, so verification and drift detection are map comparisons.

## The transaction engine

`Apply(requests)`:

1. **Plan** every request against the live system (no writes) and reject conflicts.
2. Take the **exclusive store lock** (one transaction at a time, `flock`).
3. **Journal** the plan to disk (status `pending` → `applying`) *before* touching anything.
4. For each tweak: begin the guard (if any) → `Apply` → **`Verify`** (state compare + `Checker`) → mark the item verified → commit/destroy the guard.
5. On any failure: restore the items already applied in reverse order, abort the guard, mark the snapshot `rolled_back` (or `rollback_failed` with the reason), and return a `TransactionError` that says which.
6. On success: status `committed`.

`Restore(id)` reverts a committed snapshot but first **re-reads** each setting: if something else changed it since, the item is reported as *drifted* and left alone unless `Force` is set. If the live state already equals the saved "before", nothing is re-run (so no needless privilege prompt).

`Recover(id)` handles transactions left in `applying` (crash, power loss): it reverts every item that may have been touched. Because `Restore` is idempotent, items that were never applied are harmless.

## Privilege model

The UI never runs as root. `privilege.Executor` has one method, `Do(Action)`, where an `Action` is an operation name plus a fully validated target (`write-sysfs`, a `cpufreq/scaling_governor` path, a short token). `Pkexec` starts `pkexec <turbo> __priv <op> <path> <value>`; the root side calls `Validate` again. See [SECURITY.md](../SECURITY.md).

## The TUI

A single Bubble Tea `Model` (`internal/tui`).

- **Never blocks.** Scans, applies, restores and network tests run in goroutines and report through messages (`streamMsg` wraps a message plus the channel to keep listening on). Only one long operation runs at a time (`busyState`); Ctrl+C cancels it, and the engine rolls back whatever is half done.
- **Mouse and keyboard are the same paths.** Each render records clickable rectangles; `View.OnMouse` turns a click into an `actMsg{id}`, which `act()` dispatches exactly like the equivalent key.
- **Modal dialogs** (preview, progress, result, confirm, help) are overlays that swallow clicks outside themselves.
- **Rendering is width-exact.** All widgets work on lines whose *visible* width (ANSI-aware, via `x/ansi`) is exact, so panes join without drifting; tabs, dashboards and footers degrade gracefully down to 64×18 and say so below that.
- **Accessibility.** Colour is never the only signal (severity tags are text), the `mono` theme uses only bold/underline/reverse, `NO_COLOR`/`TERM=dumb` select it, and an ASCII glyph set is used for the Linux console and non-UTF-8 locales.
- **Safety in a terminal-only session.** A polkit prompt cannot appear inside a TTY-only UI without corrupting it, so privileged changes are not attempted there; the preview shows the equivalent `turbo apply …` command to run in a normal shell.
- **Test seams.** `noTimers` and `topWindow` make the model deterministic so tests can drive it synchronously (`internal/tui/tui_test.go`).

## Network Doctor

`CollectNetwork` gathers facts (link, router ping, DNS timing, internet ping, optional load tests) with unprivileged ICMP (datagram sockets). `Interpret` is a pure function from those facts to layered `Diagnosis` entries with a certainty level, always attributing a problem to the *lowest* failing layer first (a bad Wi-Fi signal explains downstream latency before DNS is blamed). The consent boundary is structural: any non-LAN destination requires `NetOptions.External`, and `PlanDestinations` lists what will be contacted.

## Benchmark Lab

`ParseMangoHud` → `Frames` → `Summarize` → `Run` (stored as JSON). `Compare` works on **runs**, not frames (frames within a run are autocorrelated): per metric it takes medians per configuration, run-to-run CV, and an exact permutation test; see [BENCHMARK_METHODOLOGY.md](BENCHMARK_METHODOLOGY.md).
