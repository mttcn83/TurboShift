# Release process

1. `make check` must pass (gofmt, vet, `go test -race`).
2. Update `internal/version/version.go` and `CHANGELOG.md` (user-visible changes, and what is still unverified).
3. Smoke-test the *installed* binary, not `go run`:
   ```bash
   make build && sudo make install
   turbo --version && turbo doctor && turbo tweaks list && turbo apply power.profile=balanced --dry-run
   turbo            # open the TUI, visit every screen, quit with q
   ```
4. On a real machine, exercise one full apply → verify → undo cycle of a low-risk tweak (`power.profile`) and confirm `turbo snapshots list` shows `restored`. Do the same under a graphical session for `cpu.governor` to exercise the real polkit prompt. Record the result in `docs/COMPATIBILITY.md`.
5. Tag `vX.Y.Z`; build static, stripped binaries with `go build -trimpath -ldflags "-s -w"` for `linux/amd64` and `linux/arm64`; publish them with SHA-256 checksums.
6. Packaging (AUR, Nix, Flatpak is not applicable) follows a stable release; the binary has no runtime dependencies beyond the optional tools.

Versioning: `0.x` until the JSON schemas (`detect` report, `doctor --json`, snapshot files) are declared stable; snapshot files carry a version so older restore points remain restorable.
