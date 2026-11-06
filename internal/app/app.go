// Package app wires the real OS adapters, stores and engine together. Both
// the TUI and the CLI build on it, so they behave identically.
package app

import (
	"context"
	"os"
	"path/filepath"
	"runtime"

	"github.com/mttcn83/turboshift/internal/benchmark"
	"github.com/mttcn83/turboshift/internal/config"
	"github.com/mttcn83/turboshift/internal/detect"
	"github.com/mttcn83/turboshift/internal/engine"
	"github.com/mttcn83/turboshift/internal/platform"
	"github.com/mttcn83/turboshift/internal/privilege"
	"github.com/mttcn83/turboshift/internal/snapshot"
	"github.com/mttcn83/turboshift/internal/tweaks"
)

type App struct {
	Paths  config.Paths
	Prefs  config.Prefs
	FS     platform.FS
	Run    platform.Runner
	Reg    *tweaks.Registry
	Store  *snapshot.Store
	Bench  *benchmark.Store
	Self   string // absolute path of this binary (for the privileged helper)
	Home   string
	Getenv func(string) string
	// PrivOverride replaces the privilege executor (tests only).
	PrivOverride privilege.Executor
	// PrefsErr is non-nil when config.json was unreadable (defaults are used).
	PrefsErr error
}

// New builds the production App from the process environment.
func New() (*App, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	self, _ := os.Executable()
	if abs, err := filepath.EvalSymlinks(self); err == nil {
		self = abs
	}
	a := &App{
		FS: platform.OSFS{}, Run: platform.ExecRunner{}, Reg: tweaks.Default(),
		Self: self, Home: home, Getenv: os.Getenv,
	}
	a.Paths = config.ResolvePaths(os.Getenv, home)
	a.Prefs, a.PrefsErr = config.Load(a.Paths)
	a.Store = &snapshot.Store{Dir: filepath.Join(a.Paths.State, "snapshots")}
	a.Bench = &benchmark.Store{Dir: filepath.Join(a.Paths.Data, "benchmarks")}
	return a, nil
}

// Detect runs the read-only system probe.
func (a *App) Detect(ctx context.Context) detect.Report {
	return detect.Probe(ctx, detect.Env{FS: a.FS, Run: a.Run, Getenv: a.Getenv, GOARCH: runtime.GOARCH})
}

// Priv returns the privileged-action executor: in-process when already root
// (never the default), otherwise through pkexec.
func (a *App) Priv() privilege.Executor {
	if a.PrivOverride != nil {
		return a.PrivOverride
	}
	if os.Geteuid() == 0 {
		return privilege.Direct{FS: a.FS}
	}
	return privilege.Pkexec{Run: a.Run, Self: a.Self}
}

// Env builds the tweak environment for a detection report.
func (a *App) Env(rep detect.Report) tweaks.Env {
	return tweaks.Env{FS: a.FS, Run: a.Run, Priv: a.Priv(), Report: rep}
}

// Engine builds a transaction engine bound to a report.
func (a *App) Engine(rep detect.Report) *engine.Engine {
	return &engine.Engine{Reg: a.Reg, Store: a.Store, Env: a.Env(rep)}
}

// LogDirs are the directories searched for MangoHud logs.
func (a *App) LogDirs() []string {
	dirs := append([]string{}, a.Prefs.LogDirs...)
	// MangoHud falls back to $HOME when no output_folder is configured.
	dirs = append(dirs, filepath.Join(a.Paths.Data, "mangologs"), a.Home, filepath.Join(a.Home, "mangologs"), "/tmp/MangoHud")
	return dirs
}

// MangoLogDir is where `turbo game run --log` asks MangoHud to write.
func (a *App) MangoLogDir() string { return filepath.Join(a.Paths.Data, "mangologs") }
