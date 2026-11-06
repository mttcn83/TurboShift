package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mttcn83/turboshift/internal/app"
	"github.com/mttcn83/turboshift/internal/benchmark"
	"github.com/mttcn83/turboshift/internal/config"
	"github.com/mttcn83/turboshift/internal/privilege"
	"github.com/mttcn83/turboshift/internal/simtest"
	"github.com/mttcn83/turboshift/internal/snapshot"
	"github.com/mttcn83/turboshift/internal/tweaks"
)

func init() { tweaks.PollInterval = time.Millisecond }

type rig struct {
	t    *testing.T
	mach *simtest.Machine
	app  *app.App
}

func newRig(t *testing.T, mut ...func(*simtest.Machine)) *rig {
	t.Helper()
	mach := simtest.New()
	for _, f := range mut {
		f(mach)
	}
	dir := t.TempDir()
	return &rig{t: t, mach: mach, app: &app.App{
		Paths: config.Paths{Config: filepath.Join(dir, "c"), State: filepath.Join(dir, "s"), Data: filepath.Join(dir, "d")},
		Prefs: config.Prefs{}.Defaults(),
		FS:    mach.FS, Run: mach, Reg: tweaks.Default(),
		Store: &snapshot.Store{Dir: filepath.Join(dir, "s", "snaps")},
		Bench: &benchmark.Store{Dir: filepath.Join(dir, "d", "bench")},
		Self:  "/usr/bin/turbo", Home: "/home/test",
		Getenv:       func(string) string { return "" },
		PrivOverride: &privilege.Fake{FS: mach.FS},
	}}
}

// run executes one CLI invocation and returns (exit code, stdout, stderr).
func (r *rig) run(args ...string) (int, string, string) {
	r.t.Helper()
	var so, se bytes.Buffer
	code := Run(context.Background(), r.app, args, &so, &se)
	return code, so.String(), se.String()
}

func TestHelpAndVersion(t *testing.T) {
	r := newRig(t)
	for _, args := range [][]string{nil, {"--help"}, {"help"}, {"-h"}} {
		code, out, _ := r.run(args...)
		if code != ExitOK || !strings.Contains(out, "Usage: turbo") || !strings.Contains(out, "Exit codes") {
			t.Errorf("%v: code=%d out=%q", args, code, out)
		}
	}
	code, out, _ := r.run("--version")
	if code != 0 || !strings.HasPrefix(out, "turbo ") {
		t.Errorf("version: %d %q", code, out)
	}
}

func TestUnknownCommandIsUsageError(t *testing.T) {
	code, out, errOut := newRig(t).run("frobnicate")
	if code != ExitUsage || out != "" || !strings.Contains(errOut, `unknown command "frobnicate"`) {
		t.Errorf("code=%d out=%q err=%q", code, out, errOut)
	}
}

func TestDoctorJSONIsCleanAndStable(t *testing.T) {
	code, out, errOut := newRig(t).run("doctor", "--json")
	if code != ExitOK {
		t.Fatalf("code %d: %s", code, errOut)
	}
	if strings.Contains(out, "\x1b") {
		t.Error("ANSI escapes in JSON output")
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, out)
	}
	for _, k := range []string{"Report", "Findings"} {
		if _, ok := v[k]; !ok {
			// field names are Go-style if the struct has no tags; accept either casing
			if _, ok2 := v[strings.ToLower(k)]; !ok2 {
				t.Errorf("missing %q in %v", k, keys(v))
			}
		}
	}
}

func keys(m map[string]any) []string {
	var k []string
	for x := range m {
		k = append(k, x)
	}
	return k
}

func TestDoctorTextHasNoColourWhenPiped(t *testing.T) {
	code, out, _ := newRig(t).run("doctor")
	if code != ExitOK || strings.Contains(out, "\x1b[") {
		t.Errorf("code=%d, escapes=%v", code, strings.Contains(out, "\x1b["))
	}
}

func TestTweaksListAndShow(t *testing.T) {
	r := newRig(t)
	code, out, _ := r.run("tweaks", "list", "--json")
	if code != 0 {
		t.Fatal(code)
	}
	var rows []struct{ ID string }
	if err := json.Unmarshal([]byte(out), &rows); err != nil || len(rows) < 3 {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
	if code, out, _ := r.run("tweaks", "show", "power.profile"); code != 0 || !strings.Contains(out, "Trade-off") {
		t.Errorf("show: %d %q", code, out)
	}
	if code, _, e := r.run("tweaks", "show", "nope"); code != ExitUsage || !strings.Contains(e, "unknown tweak") {
		t.Errorf("unknown tweak: %d %q", code, e)
	}
}

func TestApplyDryRunChangesNothing(t *testing.T) {
	r := newRig(t)
	code, out, _ := r.run("apply", "power.profile=performance", "--dry-run")
	if code != 0 || !strings.Contains(out, "Dry run") || !strings.Contains(out, "balanced") {
		t.Errorf("code=%d out=%q", code, out)
	}
	if r.mach.PPDProfile != "balanced" || r.mach.Called("powerprofilesctl set") {
		t.Error("dry run touched the system")
	}
	if s, _, _ := r.app.Store.List(); len(s) != 0 {
		t.Error("dry run created a restore point")
	}
}

func TestApplyThenRestoreRoundTrip(t *testing.T) {
	r := newRig(t)
	code, out, errOut := r.run("apply", "power.profile=performance", "--yes", "--label", "t")
	if code != 0 || r.mach.PPDProfile != "performance" {
		t.Fatalf("apply: code=%d profile=%s out=%s err=%s", code, r.mach.PPDProfile, out, errOut)
	}
	snaps, _, _ := r.app.Store.List()
	if len(snaps) != 1 || snaps[0].Status != snapshot.StatusCommitted {
		t.Fatalf("snapshots: %+v", snaps)
	}
	if !strings.Contains(out, snaps[0].ID) {
		t.Error("output should name the restore point")
	}
	code, _, errOut = r.run("snapshots", "restore", snaps[0].ID)
	if code != 0 || r.mach.PPDProfile != "balanced" {
		t.Fatalf("restore: code=%d profile=%s err=%s", code, r.mach.PPDProfile, errOut)
	}
}

func TestApplyAlreadySetIsNotAnError(t *testing.T) {
	r := newRig(t)
	code, out, _ := r.run("apply", "power.profile=balanced", "--yes")
	if code != ExitOK || !strings.Contains(out, "already set") {
		t.Errorf("code=%d out=%q", code, out)
	}
	if s, _, _ := r.app.Store.List(); len(s) != 0 {
		t.Error("no-op created a restore point")
	}
}

func TestApplyConfirmationPromptDefaultsToNo(t *testing.T) {
	r := newRig(t)
	old := stdinReader
	defer func() { stdinReader = old }()
	stdinReader = func() io.Reader { return strings.NewReader("\n") }
	code, out, _ := r.run("apply", "power.profile=performance")
	if code != 0 || !strings.Contains(out, "Cancelled") || r.mach.PPDProfile != "balanced" {
		t.Errorf("code=%d profile=%s out=%q", code, r.mach.PPDProfile, out)
	}
	stdinReader = func() io.Reader { return strings.NewReader("y\n") }
	if code, _, _ := r.run("apply", "power.profile=performance"); code != 0 || r.mach.PPDProfile != "performance" {
		t.Errorf("yes: code=%d profile=%s", code, r.mach.PPDProfile)
	}
}

func TestApplyFailureRollsBackWithExitPartial(t *testing.T) {
	r := newRig(t)
	r.mach.Fail["ppd-set"] = io.ErrUnexpectedEOF
	code, out, _ := r.run("apply", "power.profile=performance", "--yes")
	if code != ExitPartial || r.mach.PPDProfile != "balanced" {
		t.Errorf("code=%d profile=%s out=%q", code, r.mach.PPDProfile, out)
	}
}

func TestApplyMalformedArguments(t *testing.T) {
	r := newRig(t)
	for _, args := range [][]string{
		{"apply"},
		{"apply", "power.profile"},
		{"apply", "power.profile="},
		{"apply", "power.profile=a", "power.profile=b"},
		{"apply", "no.such.tweak=1"},
	} {
		if code, _, e := r.run(args...); code != ExitUsage || e == "" {
			t.Errorf("%v: code=%d err=%q", args, code, e)
		}
	}
	// A bad value is a failed plan, not a usage error, and must change nothing.
	if code, _, _ := r.run("apply", "power.profile=turbo9000", "--yes"); code == ExitOK {
		t.Error("an invalid value must not succeed")
	}
	if r.mach.Called("powerprofilesctl set") {
		t.Error("an invalid value reached the system")
	}
}

func TestSnapshotsEmptyStateIsFriendly(t *testing.T) {
	code, out, _ := newRig(t).run("snapshots", "list")
	if code != 0 || !strings.Contains(out, "No restore points yet") {
		t.Errorf("code=%d out=%q", code, out)
	}
	code, out, _ = newRig(t).run("snapshots", "recover")
	if code != 0 || !strings.Contains(out, "Nothing to recover") {
		t.Errorf("recover: code=%d out=%q", code, out)
	}
	if code, _, _ := newRig(t).run("snapshots", "restore", "../../etc/passwd"); code == 0 {
		t.Error("a path-like id must be rejected")
	}
}

func TestNetworkExternalLoadNeedsConsent(t *testing.T) {
	code, _, e := newRig(t).run("network", "--load-url", "https://example.com/x")
	if code != ExitUsage || !strings.Contains(e, "--external") {
		t.Errorf("code=%d err=%q", code, e)
	}
}

func TestBenchmarkErrorsAreExplained(t *testing.T) {
	r := newRig(t)
	if code, _, e := r.run("benchmark", "compare", "a", "b"); code != ExitUsage || !strings.Contains(e, "no runs labelled") {
		t.Errorf("compare: %d %q", code, e)
	}
	if code, _, _ := r.run("benchmark", "import", "x.csv"); code != ExitUsage {
		t.Errorf("import without --label: %d", code)
	}
	code, out, _ := r.run("benchmark", "list")
	if code != 0 || !strings.Contains(out, "No runs yet") {
		t.Errorf("list: %d %q", code, out)
	}
	r.mach.FS.Set("/home/test/G_2026-11-06_10-00-00.csv", "garbage")
	code, out, _ = r.run("benchmark", "find")
	if code != 0 || !strings.Contains(out, "G_2026-11-06_10-00-00.csv") {
		t.Errorf("find: %d %q", code, out)
	}
	if code, _, _ := r.run("benchmark", "import", "/home/test/G_2026-11-06_10-00-00.csv", "--label", "x"); code != ExitError {
		t.Errorf("a non-log must fail cleanly, got %d", code)
	}
}

func TestGameOptions(t *testing.T) {
	r := newRig(t)
	code, out, _ := r.run("game", "options", "--gamemode", "--mangohud")
	if code != 0 || strings.TrimSpace(out) != "gamemoderun mangohud %command%" {
		t.Errorf("code=%d out=%q", code, out)
	}
	code, out, _ = r.run("game", "options", "--log", "30", "--gpu", "nvidia")
	if code != 0 || !strings.Contains(out, "MANGOHUD_CONFIG=") || !strings.Contains(out, "__NV_PRIME_RENDER_OFFLOAD=1") {
		t.Errorf("log+gpu: %d %q", code, out)
	}
	if code, _, _ := r.run("game", "options", "--gpu", "bogus"); code != ExitUsage {
		t.Errorf("bad gpu: %d", code)
	}
	if code, _, _ := r.run("game", "run"); code != ExitUsage {
		t.Errorf("run without command: %d", code)
	}
}

func TestPrivilegedHelperRefusesNonRootAndBadRequests(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("asserts the non-root refusal; the root path is covered by scripts/container-smoke.sh")
	}
	r := newRig(t)
	// Not root: the helper must refuse before looking at anything.
	if code, _, e := r.run("__priv", "write-sysfs", "/sys/devices/system/cpu/cpu0/cpufreq/scaling_governor", "performance"); code != ExitError || !strings.Contains(e, "must run as root") {
		t.Errorf("code=%d err=%q", code, e)
	}
}
