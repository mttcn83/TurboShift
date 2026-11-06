package engine

import (
	"strings"
	"testing"

	"github.com/mttcn83/turboshift/internal/detect"
	"github.com/mttcn83/turboshift/internal/simtest"
	"github.com/mttcn83/turboshift/internal/snapshot"
	"github.com/mttcn83/turboshift/internal/tweaks"
)

const (
	vm   = "/proc/sys/vm/"
	netc = "/proc/sys/net/core/"
	neti = "/proc/sys/net/ipv4/"
	drm0 = "/sys/class/drm/card0/"
	gpu  = "/sys/bus/pci/devices/0000:01:00.0/power/control"
)

func one(id, param, value, path, from, to string, rep func(*detect.Report)) fileCase {
	return fileCase{id: id, param: param, value: value, rep: rep,
		seed: map[string]string{path: from + "\n"}, want: map[string]string{path: to + "\n"}, wantActions: 1}
}

func moreFileCases() []fileCase {
	return []fileCase{
		one("net.notsent-lowat", "value", "131072", neti+"tcp_notsent_lowat", "4294967295", "131072", nil),
		{id: "net.buffers", param: "size", value: "fast",
			seed: map[string]string{netc + "rmem_max": "212992\n", netc + "wmem_max": "212992\n"},
			want: map[string]string{netc + "rmem_max": "16777216\n", netc + "wmem_max": "16777216\n"}, wantActions: 2},
		one("net.backlog", "value", "5000", netc+"netdev_max_backlog", "1000", "5000", nil),
		one("net.mtu-probing", "value", "1", neti+"tcp_mtu_probing", "0", "1", nil),
		one("net.ecn", "value", "1", neti+"tcp_ecn", "2", "1", nil),
		one("cpu.min-perf", "value", "100", "/sys/devices/system/cpu/intel_pstate/min_perf_pct", "11", "100", nil),
		one("cpu.hwp-boost", "value", "1", "/sys/devices/system/cpu/intel_pstate/hwp_dynamic_boost", "0", "1", nil),
		one("sys.watchdog", "value", "0", "/proc/sys/kernel/nmi_watchdog", "1", "0", nil),
		one("input.mouse-poll", "value", "1", "/sys/module/usbhid/parameters/mousepoll", "0", "1", nil),
		one("input.kb-poll", "value", "1", "/sys/module/usbhid/parameters/kbpoll", "0", "1", nil),
		one("input.pad-poll", "value", "2", "/sys/module/usbhid/parameters/jspoll", "0", "2", nil),
		one("mem.zswap", "value", "1", "/sys/module/zswap/parameters/enabled", "0", "1", swapOn),
		{id: "mem.zswap-compressor", param: "value", value: "zstd",
			seed: map[string]string{"/sys/module/zswap/parameters/enabled": "1\n", "/sys/module/zswap/parameters/compressor": "lz4\n",
				"/proc/crypto": "name         : lz4\ndriver       : lz4-generic\n\nname         : zstd\ndriver       : zstd-generic\n"},
			want: map[string]string{"/sys/module/zswap/parameters/compressor": "zstd\n"}, wantActions: 1},
		one("mem.thp-defrag", "value", "defer+madvise", "/sys/kernel/mm/transparent_hugepage/defrag", "always defer defer+madvise [madvise] never", "defer+madvise", nil),
		one("mem.min-free", "value", "131072", vm+"min_free_kbytes", "67584", "131072", nil),
		one("mem.watermark-scale", "value", "50", vm+"watermark_scale_factor", "10", "50", nil),
		one("mem.ksm", "value", "0", "/sys/kernel/mm/ksm/run", "1", "0", nil),
		{id: "mem.writeback-timing", param: "mode", value: "smooth",
			seed: map[string]string{vm + "dirty_writeback_centisecs": "500\n", vm + "dirty_expire_centisecs": "3000\n"},
			want: map[string]string{vm + "dirty_writeback_centisecs": "100\n", vm + "dirty_expire_centisecs": "1500\n"}, wantActions: 2},
		one("mem.stat-interval", "value", "10", vm+"stat_interval", "1", "10", nil),
		{id: "io.rq-affinity", param: "value", value: "2",
			seed: map[string]string{nvme + "rq_affinity": "1\n", sda + "rq_affinity": "1\n", nvme + "scheduler": "none\n", sda + "scheduler": "none\n"},
			want: map[string]string{nvme + "rq_affinity": "2\n", sda + "rq_affinity": "2\n"}, wantActions: 2},
		{id: "io.nomerges", param: "value", value: "2",
			seed: map[string]string{nvme + "nomerges": "0\n", nvme + "scheduler": "none\n"},
			want: map[string]string{nvme + "nomerges": "2\n"}, wantActions: 1},
		{id: "io.add-random", param: "value", value: "0",
			seed: map[string]string{nvme + "add_random": "1\n", nvme + "scheduler": "none\n"},
			want: map[string]string{nvme + "add_random": "0\n"}, wantActions: 1},
		{id: "gpu.intel-min-clock", param: "mhz", value: "700",
			seed: map[string]string{drm0 + "gt_min_freq_mhz": "300\n", drm0 + "gt_RPn_freq_mhz": "300\n", drm0 + "gt_max_freq_mhz": "1100\n"},
			want: map[string]string{drm0 + "gt_min_freq_mhz": "700\n"}, wantActions: 1},
		{id: "gpu.dgpu-awake", param: "power", value: "on",
			rep: func(r *detect.Report) {
				r.GPUs = []detect.GPU{{Vendor: "nvidia", Slot: "0000:01:00.0"}, {Vendor: "intel", Slot: "0000:00:02.0", BootVGA: true}}
			},
			seed: map[string]string{gpu: "auto\n", "/sys/bus/pci/devices/0000:00:02.0/power/control": "auto\n"},
			want: map[string]string{gpu: "on\n"}, wantActions: 1},
	}
}

// ---- Hyprland / GNOME ------------------------------------------------------

func hyprMachine(m *simtest.Machine) {
	m.Tools["hyprctl"] = true
	m.Hypr = map[string]int{"animations:enabled": 1, "decoration:blur:enabled": 1, "decoration:shadow:enabled": 0,
		"general:allow_tearing": 0, "render:direct_scanout": 0}
}

func TestHyprlandEffectsApplyVerifyAndRestorePerKey(t *testing.T) {
	r := newRig(t, hyprMachine)
	snap, err := r.eng.Apply(ctx, []Request{{"desktop.hypr-effects", map[string]string{"effects": "off"}}}, "t", nil)
	if err != nil || snap.Status != snapshot.StatusCommitted {
		t.Fatalf("apply: %v %v", err, snap.Status)
	}
	for k, v := range r.m.Hypr {
		if strings.HasPrefix(k, "general") || strings.HasPrefix(k, "render") {
			continue
		}
		if v != 0 {
			t.Errorf("%s = %d, want 0", k, v)
		}
	}
	if len(r.priv.Actions) != 0 {
		t.Errorf("a user-level tweak asked for administrator rights: %+v", r.priv.Actions)
	}
	if _, err := r.eng.Restore(ctx, snap.ID, RestoreOptions{}, nil); err != nil {
		t.Fatal(err)
	}
	// Shadows were already off: the original mixed state must come back exactly.
	if r.m.Hypr["animations:enabled"] != 1 || r.m.Hypr["decoration:blur:enabled"] != 1 || r.m.Hypr["decoration:shadow:enabled"] != 0 {
		t.Errorf("not restored: %+v", r.m.Hypr)
	}
}

func TestHyprlandTweaksSimpleOptionsAndFailure(t *testing.T) {
	for id, c := range map[string][3]string{
		"desktop.hypr-tearing":        {"tearing", "on", "general:allow_tearing"},
		"desktop.hypr-direct-scanout": {"scanout", "on", "render:direct_scanout"},
	} {
		r := newRig(t, hyprMachine)
		if _, err := r.eng.Apply(ctx, []Request{{id, map[string]string{c[0]: c[1]}}}, "t", nil); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		if r.m.Hypr[c[2]] != 1 {
			t.Errorf("%s did not set %s", id, c[2])
		}
	}
	r := newRig(t, hyprMachine, func(m *simtest.Machine) { m.Fail["hypr-keyword"] = errBoom })
	if _, err := r.eng.Apply(ctx, []Request{{"desktop.hypr-effects", map[string]string{"effects": "off"}}}, "t", nil); err == nil {
		t.Fatal("a failing hyprctl must fail the transaction")
	}
	if r.m.Hypr["animations:enabled"] != 1 {
		t.Error("failed apply left state changed")
	}
}

func TestDesktopTweaksUnavailableWithoutTheDesktop(t *testing.T) {
	r := newRig(t) // no hyprctl, no gsettings
	for _, id := range []string{"desktop.hypr-effects", "desktop.hypr-tearing", "desktop.hypr-direct-scanout", "desktop.gnome-animations"} {
		tw, _ := r.eng.Reg.Get(id)
		if a := tw.Assess(ctx, r.eng.Env); a.Available || a.Reason == "" {
			t.Errorf("%s must be unavailable with a reason: %+v", id, a)
		}
	}
	// Hyprland installed but not running.
	r = newRig(t, func(m *simtest.Machine) { m.Tools["hyprctl"] = true })
	tw, _ := r.eng.Reg.Get("desktop.hypr-effects")
	if a := tw.Assess(ctx, r.eng.Env); a.Available || !strings.Contains(a.Reason, "not running") {
		t.Errorf("want a not-running reason: %+v", a)
	}
}

func TestGnomeAnimationsRoundTrip(t *testing.T) {
	r := newRig(t, func(m *simtest.Machine) {
		m.Tools["gsettings"] = true
		m.GSettings = map[string]string{"enable-animations": "true"}
	})
	snap, err := r.eng.Apply(ctx, []Request{{"desktop.gnome-animations", map[string]string{"animations": "off"}}}, "t", nil)
	if err != nil || r.m.GSettings["enable-animations"] != "false" {
		t.Fatalf("apply: %v %v", err, r.m.GSettings)
	}
	if _, err := r.eng.Restore(ctx, snap.ID, RestoreOptions{}, nil); err != nil || r.m.GSettings["enable-animations"] != "true" {
		t.Fatalf("restore: %v %v", err, r.m.GSettings)
	}
}

func TestEveryTweakHasTestsAndPlainMetadata(t *testing.T) {
	covered := map[string]bool{"power.profile": true, "cpu.governor": true, "net.wifi-powersave": true, "net.dns": true,
		"desktop.hypr-effects": true, "desktop.hypr-tearing": true, "desktop.hypr-direct-scanout": true, "desktop.gnome-animations": true}
	for _, c := range fileCases() {
		covered[c.id] = true
	}
	all := tweaks.Default().All()
	if len(all) < 50 {
		t.Errorf("catalogue has %d tweaks, want at least 50", len(all))
	}
	for _, tw := range all {
		m := tw.Meta()
		if !covered[m.ID] {
			t.Errorf("%s has no end-to-end test case", m.ID)
		}
		for name, v := range map[string]string{"name": m.Name, "summary": m.Summary, "details": m.Details, "helps": m.Helps, "tradeoff": m.Tradeoff, "evidence": m.Evidence} {
			if strings.TrimSpace(v) == "" {
				t.Errorf("%s: empty %s", m.ID, name)
			}
		}
		if !strings.HasSuffix(m.Summary, ".") {
			t.Errorf("%s: summary should be a sentence: %q", m.ID, m.Summary)
		}
		if m.Category == "" || m.Tier == "" || m.Risk == 0 {
			t.Errorf("%s: incomplete classification", m.ID)
		}
	}
}

var errBoom = errorString("boom")

type errorString string

func (e errorString) Error() string { return string(e) }

func TestPresetsReferenceRealTweaksAndOptions(t *testing.T) {
	reg := tweaks.Default()
	for _, p := range tweaks.Presets() {
		seen := map[string]bool{}
		for _, it := range p.Items {
			tw, ok := reg.Get(it.TweakID)
			if !ok {
				t.Errorf("preset %s: unknown tweak %s", p.ID, it.TweakID)
				continue
			}
			if seen[it.TweakID] {
				t.Errorf("preset %s lists %s twice", p.ID, it.TweakID)
			}
			seen[it.TweakID] = true
			if err := tweaks.CheckParam(tw.Meta(), nil, it.Value); err != nil && tw.Meta().ID != "gpu.intel-min-clock" {
				t.Errorf("preset %s: %v", p.ID, err)
			}
		}
	}
}

func TestResolvePresetSkipsWhatDoesNotApplyAndNeverWritesWhileResolving(t *testing.T) {
	r := newRig(t, noManagers)
	r.m.FS.Set("/proc/sys/net/ipv4/tcp_congestion_control", "cubic\n").Set("/proc/sys/net/ipv4/tcp_available_congestion_control", "reno cubic bbr\n").
		Set("/proc/sys/net/ipv4/tcp_ecn", "1\n") // already what the preset wants
	r.eng.Env.Report = detect.Probe(ctx, detect.Env{FS: r.m.FS, Run: r.m})
	p, _ := tweaks.PresetByID("network")
	reqs, skipped := r.eng.ResolvePreset(ctx, p)
	if len(reqs) != 2 { // bbr and Wi-Fi power saving
		t.Fatalf("want bbr + wifi, got %+v", reqs)
	}
	var already, unavailable bool
	for _, s := range skipped {
		already = already || (s.TweakID == "net.ecn" && s.Already)
		unavailable = unavailable || (s.TweakID == "net.qdisc" && !s.Already && s.Reason != "")
	}
	if !already || !unavailable {
		t.Errorf("skipped list wrong: %+v", skipped)
	}
	if len(r.priv.Actions) != 0 {
		t.Error("resolving a preset must not write anything")
	}
	snap, err := r.eng.Apply(ctx, reqs, "preset:network", nil)
	if err != nil || snap.Status != snapshot.StatusCommitted {
		t.Fatalf("apply: %v", err)
	}
	if _, err := r.eng.Restore(ctx, snap.ID, RestoreOptions{}, nil); err != nil {
		t.Fatal(err)
	}
	if v, _ := r.m.FS.Get("/proc/sys/net/ipv4/tcp_congestion_control"); strings.TrimSpace(v) != "cubic" {
		t.Errorf("not restored: %q", v)
	}
}
