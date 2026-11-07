# Contributing

Thanks for helping. TurboShift is intentionally small; the quality bar matters more than the feature count.

## Build and test

```bash
make check     # gofmt + go vet + go test -race ./...
make kernel-test   # apply/verify/restore/rollback against a real kernel, in a throw-away namespace
make build     # ./turbo
```

Go 1.27+. Tests never touch the real system: they run against `internal/simtest`, a stateful simulated machine (fake `/proc`/`/sys`, scripted `powerprofilesctl`/`nmcli`/`iw`/`busctl`/`systemctl`, injectable failures).

## Rules for a new optimization

An optimization is accepted only if it has **all** of:

1. A real mechanism you can explain in two sentences (no folklore, no "RAM cleaning").
2. A stated trade-off (battery, heat, latency, risk).
3. A way for the user to measure whether it helped (`Evidence` text, ideally a Network Doctor or Benchmark Lab workflow).
4. A `tweaks.Tweak` implementation with `Assess`, `Plan` (pure, no side effects), `Apply`, `Read`, `Restore` (idempotent), and where the stored value can differ from the effective one, a `Checker`.
5. Engine tests: happy path, verify failure, restore, drift, and unavailable/conflict cases.
6. Honest availability: if it cannot work here (another manager owns the setting, tool missing), `Assess` says why.

Anything needing root must go through `internal/privilege` by adding an *allowlisted action*, never a generic command runner.

## Code style

- `gofmt`, `go vet`, no new dependencies without a clear need.
- Separate data collection (`detect`, `diagnostics`) from presentation (`tui`, `cli`); keep decision logic in pure functions that take a report.
- Errors say what failed, why when known, and what to do next.
- TUI changes need a render check at 64×18, 80×24 and 120×40, a no-colour/ASCII check, and keyboard *and* mouse paths.

## Licensing of contributions

TurboShift is MIT licensed. By opening a pull request you confirm that you wrote the change (or have the right to submit it) and that it may be distributed under the MIT licence. Do not paste code, text or data from sources with other licences (GPL, proprietary, forum posts of unknown origin) and do not copy another product's wording or screenshots; descriptions of kernel settings must be written in your own words from the kernel documentation or your own testing. New dependencies need a permissive licence (MIT, BSD, Apache-2.0) and an entry from `scripts/gen-notices.sh`.

## Pull requests

Describe the user-visible change and how you tested it. State what you could **not** test; that is more useful than a claim of "works".

## Continuous integration

A ready-made GitHub Actions workflow is in [docs/ci.yml.example](docs/ci.yml.example). To enable it, copy it to `.github/workflows/ci.yml` (it was not committed there because the token used for the first push had no `workflow` scope). It runs gofmt, `go vet`, `go test -race` and a smoke test.
