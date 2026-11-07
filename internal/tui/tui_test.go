package tui

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mttcn83/turboshift/internal/app"
	"github.com/mttcn83/turboshift/internal/benchmark"
	"github.com/mttcn83/turboshift/internal/config"
	"github.com/mttcn83/turboshift/internal/diagnostics"
	"github.com/mttcn83/turboshift/internal/platform"
	"github.com/mttcn83/turboshift/internal/privilege"
	"github.com/mttcn83/turboshift/internal/simtest"
	"github.com/mttcn83/turboshift/internal/snapshot"
	"github.com/mttcn83/turboshift/internal/tweaks"
)

func init() { tweaks.PollInterval = time.Millisecond }

// rig drives the real Model synchronously against a simulated machine:
// every command the model returns is executed and its message fed back until
// nothing is outstanding. Timers are off, so runs are deterministic.
type rig struct {
	t    *testing.T
	m    *Model
	mach *simtest.Machine
	app  *app.App
	env  map[string]string
}

func newRig(t *testing.T, mut ...func(*simtest.Machine)) *rig {
	t.Helper()
	mach := simtest.New()
	for _, f := range mut {
		f(mach)
	}
	return newRigWithMachine(t, mach, nil)
}

func (r *rig) settle(cmd tea.Cmd) {
	r.t.Helper()
	ch := make(chan tea.Msg, 64)
	out := 0
	spawn := func(c tea.Cmd) {
		if c != nil {
			out++
			go func() { ch <- c() }()
		}
	}
	spawn(cmd)
	deadline := time.After(15 * time.Second)
	for out > 0 {
		select {
		case msg := <-ch:
			out--
			switch v := msg.(type) {
			case nil:
			case tea.BatchMsg:
				for _, c := range v {
					spawn(c)
				}
			default:
				_, next := r.m.Update(msg)
				spawn(next)
			}
		case <-deadline:
			r.t.Fatal("timed out waiting for background work to settle")
		}
	}
}

func keyMsg(k string) tea.KeyPressMsg {
	switch k {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "backspace":
		return tea.KeyPressMsg{Code: tea.KeyBackspace}
	case "space":
		return tea.KeyPressMsg{Code: ' ', Text: " "}
	case "ctrl+c":
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	}
	rs := []rune(k)
	return tea.KeyPressMsg{Code: rs[0], Text: k}
}

func (r *rig) press(keys ...string) {
	r.t.Helper()
	for _, k := range keys {
		_, cmd := r.m.Update(keyMsg(k))
		r.settle(cmd)
	}
}

func (r *rig) typeText(s string) {
	for _, c := range s {
		r.press(string(c))
	}
}

func (r *rig) text() string { return ansi.Strip(r.m.View().Content) }

func (r *rig) wantText(sub string) {
	r.t.Helper()
	if txt := r.text(); !strings.Contains(txt, sub) {
		r.t.Fatalf("screen does not contain %q:\n%s", sub, txt)
	}
}

func (r *rig) snapshots() []*snapshot.Snapshot {
	r.t.Helper()
	s, _, err := r.app.Store.List()
	if err != nil {
		r.t.Fatal(err)
	}
	return s
}

func (r *rig) calledSet() bool { return r.mach.Called("powerprofilesctl set") }

// ---- end-to-end flows ----

func TestOptimizeFlowPreviewApplyVerifyUndo(t *testing.T) {
	r := newRig(t, func(m *simtest.Machine) { m.PPDProfile = "power-saver" })
	r.press("o") // not scanned yet: scans, then previews

	if r.m.modal == nil || r.m.modal.kind != modalPreview {
		t.Fatalf("expected a preview dialog:\n%s", r.text())
	}
	r.wantText("Review before applying")
	r.wantText("power-saver")
	r.wantText("balanced")
	if r.calledSet() || len(r.snapshots()) != 0 || r.mach.PPDProfile != "power-saver" {
		t.Fatal("the preview must not change anything or create a restore point")
	}

	r.press("enter") // apply
	r.wantText("Applied and verified")
	if r.mach.PPDProfile != "balanced" {
		t.Fatalf("profile = %q, want balanced", r.mach.PPDProfile)
	}
	snaps := r.snapshots()
	if len(snaps) != 1 || snaps[0].Status != snapshot.StatusCommitted {
		t.Fatalf("snapshots = %+v", snaps)
	}

	r.press("u") // undo from the result dialog
	r.wantText("Restore finished")
	if r.mach.PPDProfile != "power-saver" {
		t.Fatalf("undo left profile = %q", r.mach.PPDProfile)
	}
	if s := r.snapshots(); len(s) != 1 || s[0].Status != snapshot.StatusRestored {
		t.Fatalf("after undo: %+v", s)
	}
}

func TestOptimizeWhenAlreadyOptimalChangesNothing(t *testing.T) {
	r := newRig(t)
	r.press("o")
	r.wantText("Nothing needs changing")
	r.wantText("No setting was changed")
	if r.calledSet() || len(r.snapshots()) != 0 {
		t.Fatal("an already-optimal system must not be touched")
	}
}

func TestPreviewCancelLeavesSystemUntouched(t *testing.T) {
	r := newRig(t)
	r.press("3") // Optimizer (auto-scans)
	if !r.m.scanned {
		t.Fatal("entering the Optimizer should scan")
	}
	r.press("space") // tick the first tweak (power profile)
	for i := 0; i < 4; i++ {
		req, _ := r.m.requestFor(r.m.visibleTweaks()[0])
		if req.Params["profile"] == "performance" {
			break
		}
		r.press("right")
	}
	r.press("a")
	r.wantText("Review before applying")
	r.wantText("performance")
	r.press("esc")
	if r.m.modal != nil {
		t.Fatal("Esc should close the preview")
	}
	if r.calledSet() || len(r.snapshots()) != 0 || r.mach.PPDProfile != "balanced" {
		t.Fatal("cancelling must leave the system as it was")
	}
}

func TestApplyFailureRollsBackAndExplains(t *testing.T) {
	r := newRig(t, func(m *simtest.Machine) { m.PPDProfile = "power-saver" })
	r.mach.Fail["ppd-set"] = fmt.Errorf("simulated daemon failure")
	r.press("o", "enter")
	if r.mach.PPDProfile != "power-saver" {
		t.Fatalf("failed apply left profile = %q", r.mach.PPDProfile)
	}
	r.wantText("Nothing was changed")
	if s := r.snapshots(); len(s) != 1 || s[0].Status != snapshot.StatusRolledBack {
		t.Fatalf("snapshot = %+v", s)
	}
}

func TestOptimizerSearchFiltersCatalog(t *testing.T) {
	r := newRig(t)
	r.press("3", "/")
	if !r.m.opt.search.active {
		t.Fatal("/ should start a search")
	}
	r.typeText("wifi")
	r.press("enter")
	vis := r.m.visibleTweaks()
	if len(vis) != 1 || vis[0].Meta().ID != "net.wifi-powersave" {
		t.Fatalf("filter result: %d", len(vis))
	}
	r.press("esc")
	if len(r.m.visibleTweaks()) < 2 {
		t.Fatal("Esc should clear the filter")
	}
}

func TestSlashFromAnotherScreenOpensSearch(t *testing.T) {
	r := newRig(t)
	r.press("6", "/")
	if r.m.screen != ScrOptimizer || !r.m.opt.search.active {
		t.Fatalf("screen=%v active=%v", r.m.screen, r.m.opt.search.active)
	}
}

func TestPrivilegedChangeBlockedInTerminalOnlySession(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root")
	}
	r := newRig(t, func(m *simtest.Machine) { m.PPD = false })
	r.env["DISPLAY"], r.env["WAYLAND_DISPLAY"] = "", ""
	r.press("3")
	idx := -1
	for i, tw := range r.m.visibleTweaks() {
		if tw.Meta().ID == "cpu.governor" {
			idx = i
		}
	}
	if idx < 0 || !r.m.assess["cpu.governor"].Available {
		t.Fatalf("cpu.governor should be available here: %+v", r.m.assess["cpu.governor"])
	}
	r.m.opt.cursor = idx
	r.press("space")
	for i := 0; i < 3 && r.m.requestsParam("cpu.governor") != "performance"; i++ {
		r.press("right")
	}
	r.press("a")
	if r.m.modal == nil || r.m.modal.kind != modalPreview || len(r.m.modal.buttons) != 1 {
		t.Fatalf("expected a preview with no Apply button:\n%s", r.text())
	}
	r.wantText("turbo apply cpu.governor=performance")
}

func (m *Model) requestsParam(id string) string {
	t, _ := m.app.Reg.Get(id)
	req, _ := m.requestFor(t)
	return req.Params[t.Meta().ParamName]
}

func TestInterruptedChangeOfferedAtStartupAndRecovered(t *testing.T) {
	mach := simtest.New()
	mach.PPDProfile = "performance" // as the crashed run left it
	rr := newRigWithMachine(t, mach, func(a *app.App) {
		s := a.Store.New("crashed run")
		s.Status = snapshot.StatusApplying
		s.Items = []snapshot.Item{{TweakID: "power.profile", Name: "Power profile",
			Before: map[string]string{"profile": "balanced"}, After: map[string]string{"profile": "performance"}, Status: snapshot.ItemApplying}}
		if err := a.Store.Save(s); err != nil {
			t.Fatal(err)
		}
	})
	if rr.m.modal == nil || rr.m.modal.title != "Unfinished changes found" {
		t.Fatalf("expected the recovery dialog:\n%s", rr.text())
	}
	rr.press("enter")
	rr.wantText("Recovered")
	if mach.PPDProfile != "balanced" {
		t.Fatalf("profile = %q", mach.PPDProfile)
	}
}

// newRigWithMachine is newRig with a prepared machine and a hook that runs before startup.
func newRigWithMachine(t *testing.T, mach *simtest.Machine, pre func(*app.App)) *rig {
	t.Helper()
	dir := t.TempDir()
	r := &rig{t: t, mach: mach, env: map[string]string{"DISPLAY": ":0", "LANG": "en_US.UTF-8"}}
	r.app = &app.App{
		Paths: config.Paths{Config: filepath.Join(dir, "config"), State: filepath.Join(dir, "state"), Data: filepath.Join(dir, "data")},
		Prefs: config.Prefs{}.Defaults(),
		FS:    mach.FS, Run: mach, Reg: tweaks.Default(),
		Store: &snapshot.Store{Dir: filepath.Join(dir, "state", "snapshots")},
		Bench: &benchmark.Store{Dir: filepath.Join(dir, "data", "benchmarks")},
		Self:  "/usr/bin/turbo", Home: "/home/test",
		Getenv:       func(k string) string { return r.env[k] },
		PrivOverride: &privilege.Fake{FS: mach.FS},
	}
	if pre != nil {
		pre(r.app)
	}
	r.m = NewModel(context.Background(), r.app)
	r.m.noTimers, r.m.topWindow = true, time.Millisecond
	r.m.w, r.m.h = 110, 34
	r.settle(r.m.Init())
	return r
}

func TestDoctorFixJumpsToOptimizerWithoutApplying(t *testing.T) {
	r := newRig(t, func(m *simtest.Machine) { m.PPDProfile = "power-saver" })
	r.press("2")
	fs := r.m.sortedFindings()
	at := -1
	for i, f := range fs {
		if f.Fix != nil {
			at = i
		}
	}
	if at < 0 {
		t.Fatalf("expected a finding with a fix: %+v", fs)
	}
	r.m.doc.cursor = at
	r.press("enter")
	if r.m.screen != ScrOptimizer || !r.m.opt.checked["power.profile"] {
		t.Fatalf("screen=%v checked=%v", r.m.screen, r.m.opt.checked)
	}
	if r.calledSet() || len(r.snapshots()) != 0 {
		t.Fatal("jumping to the Optimizer must not apply anything")
	}
}

func TestVaultListsAndRestoresWithConfirmation(t *testing.T) {
	r := newRig(t, func(m *simtest.Machine) { m.PPDProfile = "power-saver" })
	r.press("o", "enter", "esc") // optimize, then close the result
	if r.mach.PPDProfile != "balanced" {
		t.Fatal("setup: apply failed")
	}
	r.press("7")
	if len(r.m.vt.snaps) != 1 {
		t.Fatalf("vault has %d snapshots", len(r.m.vt.snaps))
	}
	r.press("enter")
	if r.m.modal == nil || r.m.modal.kind != modalConfirm {
		t.Fatalf("restore must ask first:\n%s", r.text())
	}
	if r.mach.PPDProfile != "balanced" {
		t.Fatal("asking must not restore yet")
	}
	r.press("esc")
	if r.mach.PPDProfile != "balanced" {
		t.Fatal("cancelling must not restore")
	}
	r.press("enter", "enter")
	if r.mach.PPDProfile != "power-saver" {
		t.Fatalf("restore left %q", r.mach.PPDProfile)
	}
}

func TestGamingBuildsLaunchOptions(t *testing.T) {
	r := newRig(t)
	r.press("4")
	r.wantText("%command%")
	if strings.Contains(r.text(), "gamemoderun") {
		t.Fatal("GameMode is not installed here, so it must not be preselected")
	}
	r.press("space") // GameMode on
	r.wantText("gamemoderun")
	_, cmd := r.m.Update(keyMsg("c"))
	if cmd == nil {
		t.Fatal("c should produce a clipboard command")
	}
	r.press("down", "down", "space") // record a log
	r.wantText("MANGOHUD_CONFIG=")
}

func TestNetworkExternalConsentIsRequired(t *testing.T) {
	r := newRig(t)
	r.press("5")
	r.wantText("This run will contact exactly")
	listed := regexp.MustCompile(`external\s+1\.1\.1\.1`)
	if listed.MatchString(r.text()) {
		t.Fatal("external destinations must not be listed before consent")
	}
	r.press("u")
	r.typeText("https://example.com/big.bin")
	r.press("enter", "r")
	if r.m.busy != nil || r.m.net.res != nil {
		t.Fatal("a download URL without external consent must not run")
	}
	r.press("e")
	if !listed.MatchString(r.text()) {
		t.Fatalf("after consent the external destination must be listed:\n%s", r.text())
	}
}

func TestBenchmarkImportAndCompare(t *testing.T) {
	r := newRig(t)
	log := func(ms float64) string {
		var b strings.Builder
		b.WriteString("os,cpu,gpu,ram,kernel,driver,cpuscheduler\nTest,CPU,GPU,16000000,7.3,,powersave\n")
		b.WriteString("fps,frametime,cpu_load,cpu_power,gpu_load,cpu_temp,gpu_temp,gpu_core_clock,gpu_mem_clock,gpu_vram_used,gpu_power,ram_used,swap_used,process_rss,cpu_mhz,elapsed\n")
		var el float64
		for i := 0; i < 400; i++ {
			el += ms * 1e6
			fmt.Fprintf(&b, "%.1f,%.3f,40,25,70,60,55,2000,9000,4000,60,8,0,2,4000,%d\n", 1000/ms, ms, int64(el))
		}
		return b.String()
	}
	r.mach.FS.Set("/home/test/Game_2026-11-06_12-00-00.csv", log(10))
	r.mach.FS.Set("/home/test/Game_2026-11-06_13-00-00.csv", log(8))

	r.press("6")
	if len(r.m.bn.logs) != 2 {
		t.Fatalf("logs found: %v", r.m.bn.logs)
	}
	importAt := func(label string) {
		rows := r.m.benchRows()
		for i, row := range rows {
			if row.kind == "log" {
				r.m.bn.cursor = i
				break
			}
		}
		r.press("enter")
		if !r.m.bn.label.active {
			t.Fatal("importing should ask for a label")
		}
		r.m.bn.label.val = ""
		r.typeText(label)
		r.press("enter")
	}
	importAt("balanced")
	importAt("performance")
	if len(r.m.bn.runs) != 2 {
		t.Fatalf("imported %d runs", len(r.m.bn.runs))
	}
	// Select both configurations and compare.
	for i, row := range r.m.benchRows() {
		if row.kind == "label" {
			r.m.bn.cursor = i
			r.press("space")
		}
	}
	r.press("c")
	if r.m.bn.cmp == nil {
		t.Fatal("no comparison produced")
	}
	// One run per configuration can never be called significant; the UI must say so.
	r.wantText("Insufficient")
}

func TestSettingsThemeCyclesAndPersists(t *testing.T) {
	r := newRig(t)
	r.press("8", "enter")
	if r.app.Prefs.Theme == "" || r.app.Prefs.Theme == "auto" {
		t.Fatalf("theme = %q", r.app.Prefs.Theme)
	}
	b, err := os.ReadFile(filepath.Join(r.app.Paths.Config, "config.json"))
	if err != nil || !strings.Contains(string(b), r.app.Prefs.Theme) {
		t.Fatalf("not persisted: %v %s", err, b)
	}
}

// ---- rendering ----

var sgrColor = regexp.MustCompile(`\x1b\[[0-9;]*(38|48|3[0-7]|4[0-7]|9[0-7])[;m]`)

func TestEveryScreenFitsEveryTerminalSize(t *testing.T) {
	r := newRig(t, func(m *simtest.Machine) { m.PPDProfile = "power-saver" })
	r.press("s") // populate findings/assessments
	for _, sz := range [][2]int{{64, 18}, {80, 24}, {100, 30}, {120, 40}, {200, 50}} {
		for sc := Screen(0); sc < numScreens; sc++ {
			r.m.w, r.m.h, r.m.screen = sz[0], sz[1], sc
			lines := strings.Split(r.m.View().Content, "\n")
			if len(lines) != sz[1] {
				t.Errorf("%s @%dx%d: %d lines, want %d", screenNames[sc], sz[0], sz[1], len(lines), sz[1])
			}
			for i, l := range lines {
				if w := ansi.StringWidth(l); w > sz[0] {
					t.Errorf("%s @%dx%d line %d is %d cols wide:\n%s", screenNames[sc], sz[0], sz[1], i, w, ansi.Strip(l))
					break
				}
			}
		}
	}
}

func TestModalsFitSmallTerminals(t *testing.T) {
	r := newRig(t, func(m *simtest.Machine) { m.PPDProfile = "power-saver" })
	r.press("o")
	for _, sz := range [][2]int{{64, 18}, {80, 24}, {120, 40}} {
		r.m.w, r.m.h = sz[0], sz[1]
		lines := strings.Split(r.m.View().Content, "\n")
		if len(lines) != sz[1] {
			t.Errorf("%dx%d: %d lines", sz[0], sz[1], len(lines))
		}
		for _, l := range lines {
			if ansi.StringWidth(l) > sz[0] {
				t.Errorf("%dx%d: overlong line %q", sz[0], sz[1], ansi.Strip(l))
			}
		}
	}
}

func TestTooSmallTerminalExplainsItself(t *testing.T) {
	r := newRig(t)
	r.m.w, r.m.h = 40, 10
	r.wantText("Terminal too small")
}

func TestMonoThemeAndASCIIUseNoColourOrBoxGlyphs(t *testing.T) {
	r := newRig(t)
	r.app.Prefs.Theme, r.app.Prefs.ASCII = "mono", true
	r.m.applyPrefs()
	r.press("s")
	for sc := Screen(0); sc < numScreens; sc++ {
		r.m.screen = sc
		out := r.m.View().Content
		if sgrColor.MatchString(out) {
			t.Fatalf("%s: colour escape in mono theme: %q", screenNames[sc], sgrColor.FindString(out))
		}
		for _, c := range ansi.Strip(out) {
			if c > 127 && c != '°' {
				t.Fatalf("%s: non-ASCII character %q with ASCII mode on", screenNames[sc], c)
			}
		}
	}
}

func TestNoColorEnvironmentSelectsMono(t *testing.T) {
	r := newRig(t)
	r.env["NO_COLOR"] = "1"
	r.m.applyPrefs()
	if r.m.th.Name != "mono" {
		t.Fatalf("theme = %s", r.m.th.Name)
	}
	r.env["NO_COLOR"], r.env["TERM"] = "", "linux"
	r.m.applyPrefs()
	if r.m.gl.Full != "#" {
		t.Fatal("the Linux console should get ASCII glyphs")
	}
}

func TestDarkThemeActuallyUsesColour(t *testing.T) {
	r := newRig(t)
	if !sgrColor.MatchString(r.m.View().Content) {
		t.Fatal("expected colour in the default theme")
	}
}

// ---- mouse ----

func (r *rig) click(x, y int) {
	r.t.Helper()
	v := r.m.View()
	cmd := v.OnMouse(tea.MouseClickMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft}))
	r.settle(cmd)
}

func (r *rig) hitFor(id string) hit {
	r.t.Helper()
	r.m.View()
	for _, h := range r.m.hits {
		if h.id == id {
			return h
		}
	}
	r.t.Fatalf("no clickable area %q; have %v", id, r.m.hits)
	return hit{}
}

func TestMouseTabsAndButtons(t *testing.T) {
	r := newRig(t, func(m *simtest.Machine) { m.PPDProfile = "power-saver" })
	h := r.hitFor("tab:2")
	r.click(h.x+1, h.y)
	if r.m.screen != ScrOptimizer {
		t.Fatalf("clicking the Optimizer tab landed on %v", r.m.screen)
	}
	r.press("1")
	h = r.hitFor("optimize")
	r.click(h.x+2, h.y) // "Optimize My PC" button
	if r.m.modal == nil || r.m.modal.kind != modalPreview {
		t.Fatalf("button should open the preview:\n%s", r.text())
	}
	// Clicking behind the dialog must not trigger anything.
	r.click(1, 0)
	if r.m.screen != ScrDashboard {
		t.Fatal("clicks outside a dialog must be swallowed")
	}
	h = r.hitFor("modal:apply")
	r.click(h.x+1, h.y)
	if r.mach.PPDProfile != "balanced" {
		t.Fatalf("clicking Apply did not apply (profile %q)", r.mach.PPDProfile)
	}
}

func TestMouseWheelMovesList(t *testing.T) {
	r := newRig(t)
	r.press("3")
	_, _ = r.m.Update(tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelDown}))
	if r.m.opt.cursor != 1 {
		t.Fatalf("cursor = %d", r.m.opt.cursor)
	}
}

// ---- safety ----

func TestQuitIsRefusedWhileOperationRuns(t *testing.T) {
	r := newRig(t)
	r.m.busy = &busyState{kind: "apply"}
	_, cmd := r.m.Update(keyMsg("q"))
	if cmd != nil {
		t.Fatal("q must not quit mid-transaction")
	}
}

func TestCtrlCCancelsRunningOperationInsteadOfQuitting(t *testing.T) {
	r := newRig(t)
	cancelled := false
	r.m.busy = &busyState{kind: "scan", cancel: func() { cancelled = true }}
	_, cmd := r.m.Update(keyMsg("ctrl+c"))
	if !cancelled || cmd != nil {
		t.Fatalf("cancelled=%v cmd=%v", cancelled, cmd != nil)
	}
}

func netSeed(m *simtest.Machine) {
	m.FS.Set("/proc/sys/net/ipv4/tcp_congestion_control", "cubic\n").Set("/proc/sys/net/ipv4/tcp_available_congestion_control", "cubic bbr\n").
		Set("/proc/sys/net/ipv4/tcp_ecn", "2\n").Set("/proc/sys/net/ipv4/tcp_slow_start_after_idle", "0\n")
}

func TestPresetFromTheOptimizerSelectsAndPreviewsButAppliesNothing(t *testing.T) {
	r := newRig(t, netSeed)
	r.press("3") // Optimizer, auto-scans
	r.press("p")
	if r.m.modal == nil || r.m.modal.title != "Presets: one-step bundles" {
		t.Fatalf("p should open the presets dialog:\n%s", r.text())
	}
	r.wantText("Cut network lag")
	r.wantText("settings apply here")
	r.press("n")
	if r.m.modal == nil || r.m.modal.kind != modalPreview {
		t.Fatalf("choosing a preset should go straight to the preview:\n%s", r.text())
	}
	r.wantText("TCP congestion")
	c := r.m.opt.checked
	if !c["net.tcp-cc"] || !c["net.ecn"] || c["net.tcp-idle"] || c["net.qdisc"] || c["cpu.boost"] {
		t.Errorf("wrong selection: %v", c)
	}
	if len(r.snapshots()) != 0 || r.mach.Called("nmcli connection modify") {
		t.Fatal("nothing may change before the preview is confirmed")
	}
	r.press("esc") // cancelled: still ticked so the user can adjust
	if r.m.modal != nil || !r.m.opt.checked["net.tcp-cc"] || len(r.snapshots()) != 0 {
		t.Fatal("cancelling must leave the selection and change nothing")
	}
}

func TestDashboardGoalKeyScansThenPreviewsThenUndoes(t *testing.T) {
	r := newRig(t, netSeed)
	r.wantText("What do you want to do?")
	r.wantText("Cut network lag")
	r.wantText("Start here:")
	r.press("n") // not scanned yet: scans, then previews
	if r.m.modal == nil || r.m.modal.kind != modalPreview {
		t.Fatalf("one key should reach the preview:\n%s", r.text())
	}
	if len(r.snapshots()) != 0 {
		t.Fatal("the preview must not create a restore point")
	}
	r.press("enter")
	r.wantText("Applied and verified")
	if v, _ := r.mach.FS.Get("/proc/sys/net/ipv4/tcp_congestion_control"); strings.TrimSpace(v) != "bbr" {
		t.Fatalf("not applied: %q", v)
	}
	r.press("esc", "1", "u")
	if r.m.modal == nil || r.m.modal.kind != modalConfirm {
		t.Fatalf("u should ask to undo the last change:\n%s", r.text())
	}
	r.press("enter")
	r.wantText("Restore finished")
	if v, _ := r.mach.FS.Get("/proc/sys/net/ipv4/tcp_congestion_control"); strings.TrimSpace(v) != "cubic" {
		t.Errorf("not undone: %q", v)
	}
}

func TestUndoWithNothingToUndoSaysSo(t *testing.T) {
	r := newRig(t)
	r.press("u")
	if r.m.modal != nil {
		t.Fatal("no dialog expected")
	}
	r.wantText("Nothing to undo")
}

func TestOptimizerCategoryFilterCycles(t *testing.T) {
	r := newRig(t)
	r.press("3")
	all := len(r.m.visibleTweaks())
	r.press("c")
	first := r.m.visibleTweaks()
	if len(first) == 0 || len(first) >= all {
		t.Fatalf("category filter should narrow the list: %d of %d", len(first), all)
	}
	cat := first[0].Meta().Category
	for _, tw := range first {
		if tw.Meta().Category != cat {
			t.Fatal("mixed categories under a filter")
		}
	}
	r.wantText("category: " + string(cat))
	r.press("esc")
	if len(r.m.visibleTweaks()) != all {
		t.Fatal("Esc should show every category again")
	}
	for i := 0; i < 20 && r.m.opt.cat != ""; i++ { // cycling returns to All
		r.press("c")
	}
	if len(r.m.visibleTweaks()) != all {
		t.Fatal("cycling past the last category should return to All")
	}
}

// fakeNet answers every probe at once so TUI tests can never touch the network.
type fakeNet struct{}

func (fakeNet) Ping(_ context.Context, host string, n int, _ time.Duration) (diagnostics.PingStats, error) {
	return diagnostics.PingStats{Target: host, Sent: n, Received: n, AvgMs: 5, P50Ms: 5, P95Ms: 6, MaxMs: 7, MinMs: 4}, nil
}
func (fakeNet) Time(context.Context, string, string) (time.Duration, error) {
	return 8 * time.Millisecond, nil
}
func (fakeNet) Load(context.Context, string, bool) (float64, error) { return 100, nil }

func init() {
	netDeps = func(fs platform.FS, run platform.Runner) diagnostics.NetDeps {
		return diagnostics.NetDeps{Pinger: fakeNet{}, DNS: fakeNet{}, Loader: fakeNet{}, FS: fs, Run: run}
	}
}
