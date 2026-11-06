// Package config resolves XDG locations and loads the tiny preferences file.
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// Paths are TurboShift's on-disk locations (all under XDG base directories).
type Paths struct {
	Config string // preferences
	State  string // snapshots, journal, lock
	Data   string // imported benchmark runs, launchers
}

// ResolvePaths follows the XDG Base Directory spec using the given getenv.
func ResolvePaths(getenv func(string) string, home string) Paths {
	pick := func(env, fallback string) string {
		if v := getenv(env); v != "" && filepath.IsAbs(v) {
			return filepath.Join(v, "turboshift")
		}
		return filepath.Join(home, fallback, "turboshift")
	}
	return Paths{
		Config: pick("XDG_CONFIG_HOME", ".config"),
		State:  pick("XDG_STATE_HOME", ".local/state"),
		Data:   pick("XDG_DATA_HOME", ".local/share"),
	}
}

// Prefs are user preferences. Nothing here is sensitive and nothing is
// ever transmitted. Zero values are sensible defaults.
type Prefs struct {
	// Theme: "auto" (color when the terminal supports it), "mono" or
	// "colorblind" (Okabe-Ito palette).
	Theme string `json:"theme,omitempty"`
	// ASCII forces ASCII-only drawing characters.
	ASCII bool `json:"ascii,omitempty"`
	// PingTarget is the external host used by opt-in Network Doctor tests.
	PingTarget string `json:"ping_target,omitempty"`
	// LookupName is the hostname resolved by the opt-in DNS timing test.
	LookupName string `json:"lookup_name,omitempty"`
	// LogDirs are extra directories scanned for MangoHud logs.
	LogDirs []string `json:"log_dirs,omitempty"`
}

// Defaults fills unset fields with documented defaults.
func (p Prefs) Defaults() Prefs {
	if p.Theme == "" {
		p.Theme = "auto"
	}
	if p.PingTarget == "" {
		p.PingTarget = "1.1.1.1"
	}
	if p.LookupName == "" {
		p.LookupName = "example.com"
	}
	return p
}

func prefsFile(p Paths) string { return filepath.Join(p.Config, "config.json") }

// Load reads preferences; a missing file yields defaults.
func Load(p Paths) (Prefs, error) {
	b, err := os.ReadFile(prefsFile(p))
	if errors.Is(err, os.ErrNotExist) {
		return Prefs{}.Defaults(), nil
	}
	if err != nil {
		return Prefs{}.Defaults(), err
	}
	var pr Prefs
	if err := json.Unmarshal(b, &pr); err != nil {
		return Prefs{}.Defaults(), errors.New("config.json is not valid JSON (ignoring it): " + err.Error())
	}
	return pr.Defaults(), nil
}

// Save writes preferences atomically.
func Save(p Paths, pr Prefs) error {
	if err := os.MkdirAll(p.Config, 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(pr, "", "  ")
	tmp := prefsFile(p) + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, prefsFile(p))
}
