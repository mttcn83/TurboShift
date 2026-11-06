package engine

import (
	"errors"
	"strings"
	"testing"

	"github.com/mttcn83/turboshift/internal/detect"
	"github.com/mttcn83/turboshift/internal/simtest"
	"github.com/mttcn83/turboshift/internal/snapshot"
	"github.com/mttcn83/turboshift/internal/tweaks"
)

// fileCase describes one kernel-attribute tweak: the seeded files, the
// request, and what must be in the files after Apply and after Restore.
type fileCase struct {
	id, param, value string
	seed             map[string]string // path -> initial contents
	rep              func(*detect.Report)
	want             map[string]string // path -> contents after Apply
	wantActions      int
}

const (
	cpu0   = "/sys/devices/system/cpu/cpu0/cpufreq/"
	cpu1   = "/sys/devices/system/cpu/cpu1/cpufreq/"
	nvme   = "/sys/block/nvme0n1/queue/"
	sda    = "/sys/block/sda/queue/"
	pstate = "/sys/devices/system/cpu/intel_pstate/no_turbo"
)

func swapOn(r *detect.Report) { r.Memory.SwapTotalKB = 8 << 20 }

func fileCases() []fileCase { return append(coreFileCases(), moreFileCases()...) }

func coreFileCases() []fileCase {
	return []fileCase{
		{id: "cpu.epp", param: "preference", value: "performance",
			seed: map[string]string{cpu0 + "energy_performance_preference": "balance_power\n", cpu1 + "energy_performance_preference": "power\n"},
			rep: func(r *detect.Report) {
				r.CPU.EPPOptions = []string{"performance", "balance_performance", "balance_power", "power"}
			},
			want: map[string]string{cpu0 + "energy_performance_preference": "performance\n", cpu1 + "energy_performance_preference": "performance\n"}, wantActions: 2},
		{id: "cpu.boost", param: "boost", value: "on", // intel_pstate no_turbo is inverted
			seed: map[string]string{pstate: "1\n"}, want: map[string]string{pstate: "0\n"}, wantActions: 1},
		{id: "cpu.boost", param: "boost", value: "off",
			seed: map[string]string{"/sys/devices/system/cpu/cpufreq/boost": "1\n"}, want: map[string]string{"/sys/devices/system/cpu/cpufreq/boost": "0\n"}, wantActions: 1},
		{id: "platform.profile", param: "profile", value: "performance",
			seed: map[string]string{"/sys/firmware/acpi/platform_profile": "balanced\n", "/sys/firmware/acpi/platform_profile_choices": "low-power balanced performance\n"},
			want: map[string]string{"/sys/firmware/acpi/platform_profile": "performance\n"}, wantActions: 1},
		{id: "net.tcp-cc", param: "algorithm", value: "bbr",
			seed: map[string]string{"/proc/sys/net/ipv4/tcp_congestion_control": "cubic\n", "/proc/sys/net/ipv4/tcp_available_congestion_control": "reno cubic bbr\n"},
			want: map[string]string{"/proc/sys/net/ipv4/tcp_congestion_control": "bbr\n"}, wantActions: 1},
		{id: "mem.swappiness", param: "swappiness", value: "180", rep: swapOn,
			seed: map[string]string{"/proc/sys/vm/swappiness": "60\n"}, want: map[string]string{"/proc/sys/vm/swappiness": "180\n"}, wantActions: 1},
		{id: "io.scheduler", param: "scheduler", value: "kyber",
			seed: map[string]string{nvme + "scheduler": "[none] mq-deadline kyber\n", sda + "scheduler": "none [mq-deadline] kyber bfq\n",
				"/sys/block/nvme0n1p1/queue/scheduler": "[none] kyber\n", "/sys/block/loop0/queue/scheduler": "[none]\n"},
			want: map[string]string{nvme + "scheduler": "kyber\n", sda + "scheduler": "kyber\n", "/sys/block/nvme0n1p1/queue/scheduler": "[none] kyber\n", "/sys/block/loop0/queue/scheduler": "[none]\n"}, wantActions: 2},
		{id: "io.readahead", param: "kib", value: "1024",
			seed: map[string]string{nvme + "read_ahead_kb": "128\n", sda + "read_ahead_kb": "256\n", nvme + "scheduler": "none\n", sda + "scheduler": "none\n"},
			want: map[string]string{nvme + "read_ahead_kb": "1024\n", sda + "read_ahead_kb": "1024\n"}, wantActions: 2},
		{id: "mem.dirty", param: "mode", value: "responsive", // a custom starting point must be restored exactly
			seed: map[string]string{"/proc/sys/vm/dirty_background_ratio": "7\n", "/proc/sys/vm/dirty_ratio": "15\n"},
			want: map[string]string{"/proc/sys/vm/dirty_background_ratio": "5\n", "/proc/sys/vm/dirty_ratio": "10\n"}, wantActions: 2},
		{id: "net.qdisc", param: "value", value: "cake", seed: map[string]string{"/proc/sys/net/core/default_qdisc": "fq_codel\n"}, want: map[string]string{"/proc/sys/net/core/default_qdisc": "cake\n"}, wantActions: 1},
		{id: "net.tcp-fastopen", param: "value", value: "3", seed: map[string]string{"/proc/sys/net/ipv4/tcp_fastopen": "1\n"}, want: map[string]string{"/proc/sys/net/ipv4/tcp_fastopen": "3\n"}, wantActions: 1},
		{id: "net.tcp-idle", param: "value", value: "0", seed: map[string]string{"/proc/sys/net/ipv4/tcp_slow_start_after_idle": "1\n"}, want: map[string]string{"/proc/sys/net/ipv4/tcp_slow_start_after_idle": "0\n"}, wantActions: 1},
		{id: "net.budget", param: "value", value: "600", seed: map[string]string{"/proc/sys/net/core/netdev_budget": "300\n"}, want: map[string]string{"/proc/sys/net/core/netdev_budget": "600\n"}, wantActions: 1},
		{id: "mem.vfs-cache", param: "value", value: "50", seed: map[string]string{"/proc/sys/vm/vfs_cache_pressure": "100\n"}, want: map[string]string{"/proc/sys/vm/vfs_cache_pressure": "50\n"}, wantActions: 1},
		{id: "mem.page-cluster", param: "value", value: "0", rep: swapOn, seed: map[string]string{"/proc/sys/vm/page-cluster": "3\n"}, want: map[string]string{"/proc/sys/vm/page-cluster": "0\n"}, wantActions: 1},
		{id: "mem.compaction", param: "value", value: "0", seed: map[string]string{"/proc/sys/vm/compaction_proactiveness": "20\n"}, want: map[string]string{"/proc/sys/vm/compaction_proactiveness": "0\n"}, wantActions: 1},
		{id: "mem.thp", param: "value", value: "madvise", seed: map[string]string{"/sys/kernel/mm/transparent_hugepage/enabled": "[always] madvise never\n"}, want: map[string]string{"/sys/kernel/mm/transparent_hugepage/enabled": "madvise\n"}, wantActions: 1},
		{id: "game.max-map-count", param: "value", value: "1048576", seed: map[string]string{"/proc/sys/vm/max_map_count": "65530\n"}, want: map[string]string{"/proc/sys/vm/max_map_count": "1048576\n"}, wantActions: 1},
		{id: "game.split-lock", param: "value", value: "0", seed: map[string]string{"/proc/sys/kernel/split_lock_mitigate": "1\n"}, want: map[string]string{"/proc/sys/kernel/split_lock_mitigate": "0\n"}, wantActions: 1},
		{id: "cpu.autogroup", param: "value", value: "0", seed: map[string]string{"/proc/sys/kernel/sched_autogroup_enabled": "1\n"}, want: map[string]string{"/proc/sys/kernel/sched_autogroup_enabled": "0\n"}, wantActions: 1},
		{id: "usb.autosuspend", param: "value", value: "-1", seed: map[string]string{"/sys/module/usbcore/parameters/autosuspend": "2\n"}, want: map[string]string{"/sys/module/usbcore/parameters/autosuspend": "-1\n"}, wantActions: 1},
		{id: "pcie.aspm", param: "value", value: "performance", seed: map[string]string{"/sys/module/pcie_aspm/parameters/policy": "[default] performance powersave powersupersave\n"}, want: map[string]string{"/sys/module/pcie_aspm/parameters/policy": "performance\n"}, wantActions: 1},
		{id: "gpu.amd-level", param: "level", value: "high",
			rep:  func(r *detect.Report) { r.GPUs = []detect.GPU{{Vendor: "amd", Driver: "amdgpu"}} },
			seed: map[string]string{"/sys/class/drm/card0/device/power_dpm_force_performance_level": "auto\n", "/sys/class/drm/card0-DP-1/status": "connected\n"},
			want: map[string]string{"/sys/class/drm/card0/device/power_dpm_force_performance_level": "high\n"}, wantActions: 1},
	}
}

func tunableRig(t *testing.T, c fileCase) *rig {
	t.Helper()
	r := newRig(t, noManagers)
	for p, v := range c.seed {
		r.m.FS.Set(p, v)
	}
	rep := detect.Probe(ctx, detect.Env{FS: r.m.FS, Run: r.m})
	if c.rep != nil {
		c.rep(&rep)
	}
	r.eng.Env.Report = rep
	return r
}

func TestEveryKernelTweakAppliesVerifiesAndRestoresExactly(t *testing.T) {
	for _, c := range fileCases() {
		t.Run(c.id+"="+c.value, func(t *testing.T) {
			r := tunableRig(t, c)
			snap, err := r.eng.Apply(ctx, []Request{{c.id, map[string]string{c.param: c.value}}}, "t", nil)
			if err != nil {
				t.Fatalf("apply: %v", err)
			}
			if snap.Status != snapshot.StatusCommitted {
				t.Fatalf("status %s", snap.Status)
			}
			for p, want := range c.want {
				if got, _ := r.m.FS.Get(p); got != want {
					t.Errorf("after apply %s = %q, want %q", p, got, want)
				}
			}
			if len(r.priv.Actions) != c.wantActions || r.priv.Batches != 1 {
				t.Errorf("actions=%d (want %d) prompts=%d (want 1)", len(r.priv.Actions), c.wantActions, r.priv.Batches)
			}
			if _, err := r.eng.Restore(ctx, snap.ID, RestoreOptions{}, nil); err != nil {
				t.Fatalf("restore: %v", err)
			}
			for p, orig := range c.seed {
				// Only attributes this tweak manages change; everything else must be untouched.
				if _, managed := c.want[p]; !managed {
					if got, _ := r.m.FS.Get(p); got != orig {
						t.Errorf("unmanaged %s was modified: %q", p, got)
					}
					continue
				}
				got, _ := r.m.FS.Get(p)
				if strings.TrimSpace(tweaksActive(got)) != strings.TrimSpace(tweaksActive(orig)) {
					t.Errorf("after restore %s = %q, want original %q", p, got, orig)
				}
			}
		})
	}
}

// tweaksActive reduces "none [mq-deadline] kyber" to the active entry so a
// restore (which writes only the name) compares equal to the original listing.
func tweaksActive(s string) string {
	if i := strings.Index(s, "["); i >= 0 {
		if j := strings.Index(s[i:], "]"); j > 0 {
			return s[i+1 : i+j]
		}
	}
	return s
}

func TestKernelTweaksRefuseWhenUnsupportedAndWriteNothing(t *testing.T) {
	// A machine with none of the files (VM, container, other hardware).
	for _, c := range fileCases() {
		t.Run(c.id, func(t *testing.T) {
			r := newRig(t, noManagers)
			r.eng.Env.Report = detect.Probe(ctx, detect.Env{FS: r.m.FS, Run: r.m})
			if c.id == "mem.swappiness" || c.id == "mem.page-cluster" {
				r.eng.Env.Report.Memory.SwapTotalKB = 1
			}
			_, err := r.eng.Apply(ctx, []Request{{c.id, map[string]string{c.param: c.value}}}, "t", nil)
			if err == nil {
				t.Fatal("expected a refusal on a system without the setting")
			}
			if len(r.priv.Actions) != 0 {
				t.Errorf("an unsupported tweak wrote something: %+v", r.priv.Actions)
			}
		})
	}
}

func TestPowerManagedTweaksStayOutOfTheWay(t *testing.T) {
	for _, id := range []string{"cpu.epp", "cpu.boost", "platform.profile"} {
		r := newRig(t) // power-profiles-daemon is active
		r.m.FS.Set("/sys/firmware/acpi/platform_profile", "balanced\n").Set("/sys/firmware/acpi/platform_profile_choices", "balanced performance\n").
			Set(pstate, "1\n").Set(cpu0+"energy_performance_preference", "power\n")
		r.eng.Env.Report = detect.Probe(ctx, detect.Env{FS: r.m.FS, Run: r.m})
		r.eng.Env.Report.CPU.EPPOptions = []string{"performance", "power"}
		tw, _ := r.eng.Reg.Get(id)
		a := tw.Assess(ctx, r.eng.Env)
		if a.Available || !strings.Contains(a.Reason, "power-profiles-daemon") {
			t.Errorf("%s must defer to the power manager: %+v", id, a)
		}
	}
}

func TestKernelTweakRejectsValuesTheMachineDoesNotOffer(t *testing.T) {
	c := fileCases()[4] // net.tcp-cc: bbr is listed
	r := tunableRig(t, c)
	if _, err := r.eng.Apply(ctx, []Request{{"net.tcp-cc", map[string]string{"algorithm": "vegas"}}}, "t", nil); err == nil || !strings.Contains(err.Error(), "allowed") {
		t.Errorf("vegas is not loaded and must be refused: %v", err)
	}
	if len(r.priv.Actions) != 0 {
		t.Error("wrote despite refusal")
	}
	// Already in the requested state: nothing to do, nothing written.
	if _, err := r.eng.Apply(ctx, []Request{{"net.tcp-cc", map[string]string{"algorithm": "cubic"}}}, "t", nil); err == nil || !strings.Contains(err.Error(), "nothing to change") {
		t.Errorf("want no-change, got %v", err)
	}
}

func TestRecommendationsOnlyWhenTheEvidenceIsThere(t *testing.T) {
	r := tunableRig(t, fileCases()[0]) // cpu.epp, on AC with a power-saving preference
	tw, _ := r.eng.Reg.Get("cpu.epp")
	if a := tw.Assess(ctx, r.eng.Env); a.Recommend == nil || a.Recommend.Params["preference"] != "balance_performance" {
		t.Errorf("AC + power-saving EPP should recommend balance_performance: %+v", a.Recommend)
	}
	for _, id := range []string{"net.tcp-cc", "mem.swappiness", "mem.thp", "io.scheduler", "net.qdisc", "game.max-map-count"} {
		// These depend on the workload: they are offered, never pushed.
		c := fileCase{}
		for _, fc := range fileCases() {
			if fc.id == id {
				c = fc
				break
			}
		}
		rr := tunableRig(t, c)
		tw, _ := rr.eng.Reg.Get(id)
		if a := tw.Assess(ctx, rr.eng.Env); a.Available && a.Recommend != nil {
			t.Errorf("%s must not be recommended automatically: %+v", id, a.Recommend)
		}
	}
}

// ---- net.dns ----

func dnsReq(v string) Request { return Request{"net.dns", map[string]string{"resolver": v}} }

func TestDNSSwitchesAndRestoresUnderCheckpoint(t *testing.T) {
	r := newRig(t)
	r.m.DNS, r.m.IgnoreAutoDNS = "192.168.1.1", "no"
	snap, err := r.eng.Apply(ctx, []Request{dnsReq("cloudflare")}, "t", nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.m.DNS != "1.1.1.1,1.0.0.1" || r.m.IgnoreAutoDNS != "yes" {
		t.Errorf("dns=%q ignore=%q", r.m.DNS, r.m.IgnoreAutoDNS)
	}
	if r.m.Checkpoints != 1 || r.m.Destroyed != 1 {
		t.Errorf("a NetworkManager checkpoint must guard the change and be released: %+v", r.m)
	}
	if _, err := r.eng.Restore(ctx, snap.ID, RestoreOptions{}, nil); err != nil {
		t.Fatal(err)
	}
	if r.m.DNS != "192.168.1.1" || r.m.IgnoreAutoDNS != "no" {
		t.Errorf("not restored: dns=%q ignore=%q", r.m.DNS, r.m.IgnoreAutoDNS)
	}
	// automatic goes back to "no manual servers".
	if _, err := r.eng.Apply(ctx, []Request{dnsReq("quad9")}, "t", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := r.eng.Apply(ctx, []Request{dnsReq("automatic")}, "t", nil); err != nil {
		t.Fatal(err)
	}
	if r.m.DNS != "" || r.m.IgnoreAutoDNS != "no" {
		t.Errorf("automatic: dns=%q ignore=%q", r.m.DNS, r.m.IgnoreAutoDNS)
	}
}

func TestDNSFailureRollsBackAndTamperedJournalCannotInject(t *testing.T) {
	r := newRig(t, func(m *simtest.Machine) { m.Fail["nm-reapply"] = errors.New("boom") })
	r.m.DNS, r.m.IgnoreAutoDNS = "192.168.1.1", "no"
	if _, err := r.eng.Apply(ctx, []Request{dnsReq("google")}, "t", nil); err == nil {
		t.Fatal("reapply failure must fail the transaction")
	}
	if r.m.DNS != "192.168.1.1" || r.m.IgnoreAutoDNS != "no" {
		t.Errorf("not rolled back: dns=%q ignore=%q", r.m.DNS, r.m.IgnoreAutoDNS)
	}

	// A journal edited to carry an option or hostname must never reach nmcli.
	tw, _ := r.eng.Reg.Get("net.dns")
	evil := tweaks.Plan{TweakID: "net.dns", Before: tweaks.State{"ipv4.dns": "--help,evil.example", "ipv4.ignore-auto-dns": "no"}}
	before := len(r.m.Calls)
	if err := tw.Restore(ctx, r.eng.Env, evil); err == nil {
		t.Error("tampered DNS value accepted")
	}
	for _, c := range r.m.Calls[before:] {
		if strings.Contains(c, "evil") || strings.Contains(c, "--help") {
			t.Errorf("tampered value reached a command: %s", c)
		}
	}
}

func TestMissingBBRExplainsHowToEnableIt(t *testing.T) {
	r := newRig(t, func(m *simtest.Machine) {
		m.FS.Set("/proc/sys/net/ipv4/tcp_congestion_control", "cubic\n").Set("/proc/sys/net/ipv4/tcp_available_congestion_control", "reno cubic\n")
	})
	_, err := r.eng.Apply(ctx, []Request{{"net.tcp-cc", map[string]string{"algorithm": "bbr"}}}, "t", nil)
	if err == nil || !strings.Contains(err.Error(), "modprobe tcp_bbr") || !strings.Contains(err.Error(), "cubic, reno") {
		t.Fatalf("want a modprobe hint naming what is available, got %v", err)
	}
	if len(r.priv.Actions) != 0 {
		t.Error("wrote despite refusal")
	}
}
