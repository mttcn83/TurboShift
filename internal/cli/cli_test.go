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

	"fmt"
	"github.com/mttcn83/turboshift/internal/app"
	"github.com/mttcn83/turboshift/internal/benchmark"
	"github.com/mttcn83/turboshift/internal/config"
	"github.com/mttcn83/turboshift/internal/privilege"
	"github.com/mttcn83/turboshift/internal/simtest"
	"github.com/mttcn83/turboshift/internal/snapshot"
	"github.com/mttcn83/turboshift/internal/tweaks"
	"math/rand"
	"os/exec"
	"regexp"
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
	var doc struct {
		Tweaks []struct {
			ID string `json:"id"`
		} `json:"tweaks"`
	}
	if err := json.Unmarshal([]byte(out), &doc); err != nil || len(doc.Tweaks) < 3 || doc.Tweaks[0].ID == "" {
		t.Fatalf("rows=%v err=%v", doc.Tweaks, err)
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

func TestPresetCommand(t *testing.T) {
	r := newRig(t, func(m *simtest.Machine) { m.PPD = false; m.Tools["powerprofilesctl"] = false })
	cc, avail := "/proc/sys/net/ipv4/tcp_congestion_control", "/proc/sys/net/ipv4/tcp_available_congestion_control"
	r.mach.FS.Set(cc, "cubic\n").Set(avail, "reno cubic bbr\n")

	code, out, _ := r.run("preset", "list")
	if code != ExitOK {
		t.Fatalf("list: %d", code)
	}
	for _, id := range []string{"gaming", "desktop", "network", "maximum"} {
		if !strings.Contains(out, id) {
			t.Errorf("list is missing %s: %s", id, out)
		}
	}
	var doc struct {
		Presets []map[string]any `json:"presets"`
	}
	if code, out, _ := r.run("preset", "list", "--json"); code != ExitOK || json.Unmarshal([]byte(out), &doc) != nil || len(doc.Presets) != 4 {
		t.Errorf("json list: %d (%d presets)", code, len(doc.Presets))
	}
	if code, _, e := r.run("preset", "show", "nope"); code != ExitUsage || !strings.Contains(e, "unknown preset") {
		t.Errorf("unknown preset: %d %q", code, e)
	}
	if code, _, _ := r.run("preset"); code != ExitUsage {
		t.Errorf("bare preset must be a usage error: %d", code)
	}

	code, out, _ = r.run("preset", "show", "network")
	if code != ExitOK || !strings.Contains(out, "APPLY") || !strings.Contains(out, "net.tcp-cc") || !strings.Contains(out, "apply on this PC") {
		t.Errorf("show: %d %s", code, out)
	}

	// --dry-run changes nothing.
	if code, out, _ = r.run("preset", "apply", "network", "--dry-run"); code != ExitOK || !strings.Contains(out, "Dry run") {
		t.Errorf("dry run: %d %s", code, out)
	}
	if v, _ := r.mach.FS.Get(cc); strings.TrimSpace(v) != "cubic" {
		t.Fatalf("dry run wrote %q", v)
	}

	code, out, e := r.run("preset", "apply", "network", "--yes")
	if code != ExitOK || !strings.Contains(out, "DONE") {
		t.Fatalf("apply: %d out=%s err=%s", code, out, e)
	}
	if v, _ := r.mach.FS.Get(cc); strings.TrimSpace(v) != "bbr" {
		t.Errorf("preset did not apply: %q", v)
	}
	// Applying again: everything is already set.
	if code, out, _ = r.run("preset", "apply", "network", "--yes"); code != ExitOK || !strings.Contains(out, "Nothing to do") {
		t.Errorf("second apply: %d %s", code, out)
	}
	snaps, _, _ := r.app.Store.List()
	if len(snaps) != 1 || snaps[0].Label != "preset:network" {
		t.Fatalf("want one labelled restore point, got %+v", snaps)
	}
	if code, _, e := r.run("snapshots", "restore", snaps[0].ID); code != ExitOK {
		t.Fatalf("restore: %d %s", code, e)
	}
	if v, _ := r.mach.FS.Get(cc); strings.TrimSpace(v) != "cubic" {
		t.Errorf("restore left %q", v)
	}
}

func TestUndoRevertsTheLatestChangeAndHelpShowsQuickStart(t *testing.T) {
	r := newRig(t, func(m *simtest.Machine) { m.PPD = false; m.Tools["powerprofilesctl"] = false })
	cc := "/proc/sys/net/ipv4/tcp_congestion_control"
	r.mach.FS.Set(cc, "cubic\n").Set("/proc/sys/net/ipv4/tcp_available_congestion_control", "cubic bbr\n")

	if code, out, _ := r.run("undo"); code != ExitOK || !strings.Contains(out, "Nothing to undo") {
		t.Errorf("empty undo: %d %q", code, out)
	}
	if code, _, e := r.run("apply", "net.tcp-cc=bbr", "--yes"); code != ExitOK {
		t.Fatalf("apply: %d %s", code, e)
	}
	stdinReader = func() io.Reader { return strings.NewReader("n\n") }
	if code, out, _ := r.run("undo"); code != ExitOK || !strings.Contains(out, "Cancelled") {
		t.Errorf("declined undo: %d %q", code, out)
	}
	if v, _ := r.mach.FS.Get(cc); strings.TrimSpace(v) != "bbr" {
		t.Fatal("a declined undo must change nothing")
	}
	if code, out, e := r.run("undo", "--yes"); code != ExitOK || !strings.Contains(out, "net.tcp-cc") {
		t.Fatalf("undo: %d %s %s", code, out, e)
	}
	if v, _ := r.mach.FS.Get(cc); strings.TrimSpace(v) != "cubic" {
		t.Errorf("not undone: %q", v)
	}
	if _, out, _ := r.run("undo"); !strings.Contains(out, "Nothing to undo") {
		t.Errorf("second undo should have nothing left: %q", out)
	}
	_, help, _ := r.run("--help")
	for _, want := range []string{"Quick start", "turbo undo", "turbo preset apply gaming"} {
		if !strings.Contains(help, want) {
			t.Errorf("help is missing %q", want)
		}
	}
}

func TestTweaksListHidesUnavailableUnlessAsked(t *testing.T) {
	r := newRig(t)
	_, short, _ := r.run("tweaks", "list")
	_, full, _ := r.run("tweaks", "list", "--all")
	if !strings.Contains(short, "not available on this PC") || len(full) <= len(short) || !strings.Contains(full, "unavailable") {
		t.Errorf("default list should collapse unavailable tweaks:\n%s", short)
	}
	var doc struct {
		Tweaks []map[string]any `json:"tweaks"`
	}
	if _, out, _ := r.run("tweaks", "list", "--json"); json.Unmarshal([]byte(out), &doc) != nil || len(doc.Tweaks) < 50 {
		t.Errorf("json must keep every tweak: %d", len(doc.Tweaks))
	}
}

func TestTweaksShowNeverPrintsGoMaps(t *testing.T) {
	r := newRig(t)
	r.mach.FS.Set("/proc/sys/net/ipv4/tcp_ecn", "2\n")
	for _, id := range []string{"net.ecn", "power.profile", "net.tcp-cc"} {
		_, out, _ := r.run("tweaks", "show", id)
		if strings.Contains(out, "map[") {
			t.Errorf("%s leaks a Go map:\n%s", id, out)
		}
	}
}

func TestCompletionScriptsAreValidAndKnowEveryCommand(t *testing.T) {
	r := newRig(t)
	for _, c := range topCommands {
		if c.name == "completion" {
			continue
		}
		if _, _, e := r.run(c.name, "--help"); strings.Contains(e, "unknown command") {
			t.Errorf("completion advertises %q but the CLI does not know it", c.name)
		}
	}
	for shell, check := range map[string][]string{"bash": {"bash", "-n"}, "fish": {"fish", "-n"}} {
		code, script, _ := r.run("completion", shell)
		if code != ExitOK || !strings.Contains(script, "turbo") {
			t.Fatalf("%s: %d %q", shell, code, script)
		}
		for _, c := range topCommands {
			if !strings.Contains(script, c.name) {
				t.Errorf("%s completion lacks %q", shell, c.name)
			}
		}
		bin, err := exec.LookPath(check[0])
		if err != nil {
			t.Logf("%s not installed: syntax of its script not checked", shell)
			continue
		}
		f := filepath.Join(t.TempDir(), "turbo."+shell)
		os.WriteFile(f, []byte(script), 0o600)
		if out, err := exec.Command(bin, check[1], f).CombinedOutput(); err != nil {
			t.Errorf("%s rejects its completion script: %v\n%s", shell, err, out)
		}
	}
	if code, _, _ := r.run("completion", "tcsh"); code != ExitUsage {
		t.Errorf("unknown shell should be a usage error, got %d", code)
	}
	_, ids, _ := r.run("__complete", "tweak-values")
	if !strings.Contains(ids, "net.ecn=\n") || !strings.Contains(ids, "power.profile=\n") {
		t.Errorf("tweak ids missing: %q", ids)
	}
	_, ps, _ := r.run("__complete", "presets")
	if strings.Fields(ps)[0] != "desktop" || !strings.Contains(ps, "gaming") {
		t.Errorf("presets: %q", ps)
	}
}

// The --json documents are a public contract (docs/STABILITY.md): every one is
// an object that carries schema_version, uses snake_case keys all the way
// down, and prints an empty list as [] rather than null.
func TestJSONDocumentsFollowTheContract(t *testing.T) {
	r := newRig(t)
	snake := regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	var walk func(path string, v any)
	walk = func(path string, v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, c := range x {
				switch k {
				case "current", "params", "before", "after": // state maps keyed by the tweak's own names (e.g. "ipv4.dns")
					continue
				}
				if !snake.MatchString(k) {
					t.Errorf("%s: key %q is not snake_case", path, k)
				}
				walk(path+"."+k, c)
			}
		case []any:
			for i, c := range x {
				walk(fmt.Sprintf("%s[%d]", path, i), c)
			}
		}
	}
	for _, args := range [][]string{
		{"doctor", "--json"}, {"tweaks", "list", "--json"}, {"preset", "list", "--json"},
		{"snapshots", "list", "--json"}, {"status", "--json"}, {"network", "--json"}, {"benchmark", "list", "--json"},
	} {
		name := strings.Join(args, " ")
		code, out, e := r.run(args...)
		if code != ExitOK && code != ExitPartial {
			t.Errorf("%s: exit %d %s", name, code, e)
			continue
		}
		var doc map[string]any
		if err := json.Unmarshal([]byte(out), &doc); err != nil {
			t.Errorf("%s: not a JSON object: %v\n%.200s", name, err, out)
			continue
		}
		if doc["schema_version"] != float64(1) {
			t.Errorf("%s: schema_version = %v", name, doc["schema_version"])
		}
		walk(name, doc)
		for k, v := range doc {
			if v == nil && k != "report" {
				t.Errorf("%s: %q is null; use [] or omit", name, k)
			}
		}
	}
}

// Garbage arguments must produce a documented exit code and a message, never
// a panic. (network is left out: it really waits for its ping window.)
func TestGarbageArgumentsNeverPanic(t *testing.T) {
	r := newRig(t)
	vocab := []string{"doctor", "status", "tweaks", "list", "show", "apply", "preset", "undo", "snapshots", "restore", "recover",
		"benchmark", "import", "compare", "find", "game", "options", "run", "completion", "bash", "fish", "__complete", "__priv",
		"--json", "--all", "--yes", "--dry-run", "--force", "--top", "--label", "--", "-", "", " ", "=", "a=", "=b", "a=b=c",
		"net.ecn", "net.ecn=1", "net.ecn=", "power.profile=performance", "mem.swappiness=-5", "mem.swappiness=999999999999999999999",
		"gaming", "maximum", "nope", "../../etc/passwd", "\x00", "ünïcode", "💥", "--gamemode", "--log", "-1", "0", "99999999999"}
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < 3000; i++ {
		n := 1 + rng.Intn(5)
		args := make([]string, n)
		for j := range args {
			args[j] = vocab[rng.Intn(len(vocab))]
		}
		if args[0] == "" || args[0] == " " || args[0] == "__priv" || args[0] == "game" && len(args) > 1 && args[1] == "run" {
			continue // __priv is the root helper (refuses non-root); game run would exec
		}
		func() {
			defer func() {
				if p := recover(); p != nil {
					t.Fatalf("panic on %q: %v", args, p)
				}
			}()
			stdinReader = func() io.Reader { return strings.NewReader("") }
			if code, _, _ := r.run(args...); code < 0 || code > 3 {
				t.Fatalf("%q exited %d, outside the documented 0-3", args, code)
			}
		}()
	}
}
