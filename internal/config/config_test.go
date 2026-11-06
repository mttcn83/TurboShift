package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolvePathsFollowsXDG(t *testing.T) {
	env := map[string]string{"XDG_CONFIG_HOME": "/xdg/c", "XDG_STATE_HOME": "relative/ignored"}
	p := ResolvePaths(func(k string) string { return env[k] }, "/home/u")
	if p.Config != "/xdg/c/turboshift" {
		t.Errorf("config = %s", p.Config)
	}
	if p.State != "/home/u/.local/state/turboshift" { // relative XDG values are invalid per spec
		t.Errorf("state = %s", p.State)
	}
	if p.Data != "/home/u/.local/share/turboshift" {
		t.Errorf("data = %s", p.Data)
	}
}

func TestLoadMissingGivesDefaults(t *testing.T) {
	pr, err := Load(Paths{Config: t.TempDir()})
	if err != nil || pr.PingTarget == "" || pr.LookupName == "" || pr.Theme != "auto" {
		t.Errorf("%+v %v", pr, err)
	}
}

func TestSaveLoadRoundTripAtomicAndPrivate(t *testing.T) {
	p := Paths{Config: filepath.Join(t.TempDir(), "new", "dir")}
	in := Prefs{Theme: "mono", ASCII: true, PingTarget: "9.9.9.9", LogDirs: []string{"/games/logs"}}
	if err := Save(p, in); err != nil {
		t.Fatal(err)
	}
	out, err := Load(p)
	if err != nil || out.Theme != "mono" || !out.ASCII || out.PingTarget != "9.9.9.9" || out.LogDirs[0] != "/games/logs" {
		t.Errorf("%+v %v", out, err)
	}
	if out.LookupName == "" {
		t.Error("unset fields should be filled with defaults")
	}
	fi, _ := os.Stat(filepath.Join(p.Config, "config.json"))
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("mode %v", fi.Mode().Perm())
	}
	if _, err := os.Stat(filepath.Join(p.Config, "config.json.tmp")); err == nil {
		t.Error("temp file left behind")
	}
}

func TestCorruptConfigFallsBackWithExplanation(t *testing.T) {
	p := Paths{Config: t.TempDir()}
	_ = os.WriteFile(filepath.Join(p.Config, "config.json"), []byte("{not json"), 0o600)
	pr, err := Load(p)
	if err == nil || !strings.Contains(err.Error(), "not valid JSON") {
		t.Errorf("err = %v", err)
	}
	if pr.PingTarget == "" {
		t.Error("defaults must still be usable")
	}
}
