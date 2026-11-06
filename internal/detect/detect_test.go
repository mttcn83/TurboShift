package detect

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/mttcn83/turboshift/internal/platform"
)

// laptopFS builds a fake hybrid Intel+NVIDIA laptop on AC power.
func laptopFS() *platform.MemFS {
	m := platform.NewMemFS()
	m.Set("/etc/os-release", "NAME=\"Arch Linux\"\nPRETTY_NAME=\"CachyOS\"\nID=cachyos\nID_LIKE=arch\n")
	m.Set("/proc/sys/kernel/osrelease", "7.3.1-1-cachyos\n")
	m.Set("/proc/uptime", "1234.56 9999.0\n")
	m.Set("/proc/cpuinfo", "processor\t: 0\nmodel name\t: Intel(R) Core(TM) i7-14650HX\n\nprocessor\t: 1\nmodel name\t: Intel(R) Core(TM) i7-14650HX\n")
	const pol = "/sys/devices/system/cpu/cpu0/cpufreq/"
	m.Set(pol+"scaling_driver", "intel_pstate\n").Set(pol+"scaling_governor", "powersave\n")
	m.Set(pol+"scaling_available_governors", "performance powersave\n")
	m.Set(pol+"energy_performance_preference", "balance_performance\n")
	m.Set(pol+"energy_performance_available_preferences", "default performance balance_performance balance_power power\n")
	m.Set(pol+"cpuinfo_min_freq", "800000\n").Set(pol+"cpuinfo_max_freq", "5200000\n")
	m.Set(pol+"scaling_cur_freq", "1000000\n")
	m.Set("/sys/devices/system/cpu/cpu1/cpufreq/scaling_cur_freq", "3000000\n")
	m.Set("/sys/devices/system/cpu/intel_pstate/no_turbo", "0\n")
	m.Set("/sys/devices/system/cpu/cpu0/thermal_throttle/core_throttle_count", "3\n")
	m.Set("/sys/devices/system/cpu/cpu1/thermal_throttle/core_throttle_count", "4\n")
	m.Set("/sys/devices/system/cpu/cpu0/thermal_throttle/package_throttle_total_time_ms", "90000\n")
	m.Set("/sys/devices/system/cpu/cpu1/thermal_throttle/package_throttle_total_time_ms", "90001\n")
	m.Set("/sys/class/hwmon/hwmon3/name", "coretemp\n")
	m.Set("/sys/class/hwmon/hwmon3/temp1_label", "Package id 0\n").Set("/sys/class/hwmon/hwmon3/temp1_input", "52000\n")
	m.Set("/sys/class/power_supply/ADP0/type", "Mains\n").Set("/sys/class/power_supply/ADP0/online", "1\n")
	m.Set("/sys/class/power_supply/BAT0/type", "Battery\n").Set("/sys/class/power_supply/BAT0/capacity", "87\n").Set("/sys/class/power_supply/BAT0/status", "Charging\n")
	m.Set("/sys/class/power_supply/hid-mouse/type", "Battery\n").Set("/sys/class/power_supply/hid-mouse/scope", "Device\n").Set("/sys/class/power_supply/hid-mouse/capacity", "5\n")
	m.Set("/proc/meminfo", "MemTotal:       32000000 kB\nMemFree: 1 kB\nMemAvailable:   16000000 kB\nSwapTotal:       8000000 kB\nSwapFree:        7000000 kB\n")
	m.Set("/sys/block/zram0/size", "1\n")
	m.Set("/proc/pressure/memory", "some avg10=1.50 avg60=0.00 avg300=0.00 total=1\nfull avg10=0.25 avg60=0.00 avg300=0.00 total=1\n")
	m.Set("/proc/pressure/io", "some avg10=0.00 avg60=0.00 avg300=0.00 total=1\n")
	// GPUs
	m.Set("/sys/class/drm/card0/device/vendor", "0x8086\n").Set("/sys/class/drm/card0/device/device", "0xa7a1\n").Set("/sys/class/drm/card0/device/boot_vga", "1\n")
	m.Symlink("../../../0000:00:02.0", "/sys/class/drm/card0/device")
	m.Symlink("../../../bus/pci/drivers/i915", "/sys/class/drm/card0/device/driver")
	m.Set("/sys/class/drm/card1/device/vendor", "0x10de\n").Set("/sys/class/drm/card1/device/device", "0x2d58\n").Set("/sys/class/drm/card1/device/power/runtime_status", "suspended\n")
	m.Symlink("../../../0000:01:00.0", "/sys/class/drm/card1/device")
	m.Symlink("../../../bus/pci/drivers/nvidia", "/sys/class/drm/card1/device/driver")
	m.Set("/sys/class/drm/card1-DP-1/status", "disconnected\n")
	// Storage
	m.Set("/proc/self/mounts", "/dev/nvme0n1p2 / btrfs rw 0 0\n/dev/nvme0n1p2 /home btrfs rw 0 0\n/dev/nvme0n1p1 /boot vfat rw 0 0\ntmpfs /tmp tmpfs rw 0 0\n/dev/sdb1 /run/media/x ext4 rw 0 0\n")
	m.SetStatfs("/", 1000, 100).SetStatfs("/home", 1000, 100).SetStatfs("/boot", 500, 400)
	// Network
	m.Set("/proc/net/route", "Iface\tDestination\tGateway\tFlags\tRefCnt\tUse\tMetric\tMask\nwlp108s0\t00000000\t0101A8C0\t0003\t0\t0\t600\t00000000\nenp109s0\t00000000\t0100000A\t0003\t0\t0\t100\t00000000\n")
	m.Set("/etc/resolv.conf", "# generated\nnameserver 127.0.0.53\noptions edns0\n")
	m.Set("/sys/class/net/wlp108s0/operstate", "up\n").Set("/sys/class/net/wlp108s0/carrier", "1\n").Set("/sys/class/net/wlp108s0/wireless/x", "")
	m.Set("/sys/class/net/wlp108s0/statistics/rx_errors", "7\n")
	m.Symlink("../../../0000:6c:00.0", "/sys/class/net/wlp108s0/device")
	m.Set("/sys/class/net/lo/operstate", "unknown\n")
	m.Set("/sys/class/net/docker0/operstate", "up\n")
	return m
}

func laptopRunner() *platform.FakeRunner {
	r := platform.NewFakeRunner()
	r.On("systemctl is-active power-profiles-daemon.service tuned.service tlp.service auto-cpufreq.service cpupower.service",
		platform.Reply{Stdout: "active\ninactive\ninactive\ninactive\ninactive\n", Code: 3})
	r.On("systemctl is-active NetworkManager.service", platform.Reply{Stdout: "active\n"})
	r.On("powerprofilesctl get", platform.Reply{Stdout: "balanced\n"})
	r.On("powerprofilesctl list", platform.Reply{Stdout: "  performance:\n    CpuDriver:\tintel_pstate\n\n* balanced:\n    CpuDriver:\tintel_pstate\n\n  power-saver:\n    CpuDriver:\tintel_pstate\n"})
	r.On("lspci -mm -nn -D", platform.Reply{Stdout: `0000:00:02.0 "VGA compatible controller [0300]" "Intel Corporation [8086]" "Raptor Lake-S UHD Graphics [a788]" -ra1 -p00
0000:01:00.0 "VGA compatible controller [0300]" "NVIDIA Corporation [10de]" "GB206M [GeForce RTX 5060 Max-Q / Mobile] [2d58]" -ra1
0000:00:14.0 "USB controller [0c03]" "Intel Corporation [8086]" "USB 3.0 [7a60]"
`})
	r.On("iw dev wlp108s0 get power_save", platform.Reply{Stdout: "Power save: on\n"})
	r.Install("iw")
	return r
}

func probe(t *testing.T, fs *platform.MemFS, r *platform.FakeRunner) Report {
	t.Helper()
	return Probe(context.Background(), Env{FS: fs, Run: r, GOARCH: "amd64",
		Getenv: func(k string) string { return map[string]string{"XDG_SESSION_TYPE": "wayland"}[k] }})
}

func TestProbeHybridLaptop(t *testing.T) {
	rep := probe(t, laptopFS(), laptopRunner())

	if rep.System.DistroID != "cachyos" || rep.System.Kernel != "7.3.1-1-cachyos" || rep.System.UptimeSec != 1234 || rep.System.SessionType != "wayland" {
		t.Errorf("system: %+v", rep.System)
	}
	c := rep.CPU
	if c.Threads != 2 || c.Driver != "intel_pstate" || c.Governor != "powersave" || c.EPP != "balance_performance" {
		t.Errorf("cpu: %+v", c)
	}
	if c.CurMHz == nil || *c.CurMHz != 2000 || c.MaxMHz == nil || *c.MaxMHz != 5200 {
		t.Errorf("cpu freqs: cur=%v max=%v", c.CurMHz, c.MaxMHz)
	}
	if c.TurboEnabled == nil || !*c.TurboEnabled {
		t.Error("turbo should be enabled (no_turbo=0)")
	}
	// core counters 3 and 4: the maximum, never the sum (package values repeat per CPU)
	if c.ThrottleEvents == nil || *c.ThrottleEvents != 4 {
		t.Errorf("throttle events: %v", c.ThrottleEvents)
	}
	if c.ThrottleSeconds == nil || *c.ThrottleSeconds != 90.001 {
		t.Errorf("throttle seconds: %v", c.ThrottleSeconds)
	}
	if c.TempC == nil || *c.TempC != 52 {
		t.Errorf("temp: %v", c.TempC)
	}
	p := rep.Power
	if p.OnAC == nil || !*p.OnAC {
		t.Error("should be on AC")
	}
	if p.Battery == nil || p.Battery.Percent != 87 {
		t.Errorf("battery should be the laptop's, not the mouse's: %+v", p.Battery)
	}
	if p.PPDProfile != "balanced" || strings.Join(p.PPDProfiles, ",") != "performance,balanced,power-saver" {
		t.Errorf("ppd: %q %v", p.PPDProfile, p.PPDProfiles)
	}
	if !p.Managers[0].Active || p.Managers[1].Active {
		t.Errorf("managers: %+v", p.Managers)
	}
	if rep.Memory.TotalKB != 32000000 || rep.Memory.ZramDevices != 1 || rep.Memory.PSIMemSome == nil || *rep.Memory.PSIMemSome != 1.5 {
		t.Errorf("memory: %+v", rep.Memory)
	}
	if len(rep.GPUs) != 2 {
		t.Fatalf("gpus: %+v", rep.GPUs)
	}
	nv := rep.GPUs[1]
	if nv.Vendor != "nvidia" || nv.Driver != "nvidia" || nv.RuntimeStatus != "suspended" || !strings.Contains(nv.Name, "RTX 5060") {
		t.Errorf("nvidia gpu: %+v", nv)
	}
	if !rep.GPUs[0].BootVGA || rep.GPUs[0].Vendor != "intel" {
		t.Errorf("intel gpu: %+v", rep.GPUs[0])
	}
	// btrfs subvolume dedupe: / and /home share a device; /run/media skipped.
	if len(rep.Storage) != 2 || rep.Storage[1].Path != "/" && rep.Storage[0].Path != "/" {
		t.Errorf("storage: %+v", rep.Storage)
	}
	n := rep.Network
	if n.DefaultIface != "enp109s0" || n.Gateway != "10.0.0.1" {
		t.Errorf("lowest-metric default route should win: %s %s", n.DefaultIface, n.Gateway)
	}
	if n.ResolverMode != "systemd-resolved" || !n.NetworkManager {
		t.Errorf("net: %+v", n)
	}
	kinds := map[string]string{}
	for _, i := range n.Interfaces {
		kinds[i.Name] = i.Kind
	}
	if kinds["wlp108s0"] != "wifi" || kinds["lo"] != "loopback" || kinds["docker0"] != "virtual" {
		t.Errorf("iface kinds: %v", kinds)
	}
	if rep.HasTool("gamemoderun") || !rep.HasTool("iw") {
		t.Error("tool detection wrong")
	}
}

func TestProbeEmptyMachineDoesNotPanicAndWarns(t *testing.T) {
	rep := probe(t, platform.NewMemFS(), platform.NewFakeRunner())
	if rep.SchemaVersion != SchemaVersion {
		t.Error("schema version missing")
	}
	if rep.CPU.Driver != "" || rep.Power.OnAC != nil {
		t.Error("unknown values must stay unknown, not zero")
	}
	if !rep.Power.Unknown {
		t.Error("missing systemctl should mark managers unknown")
	}
	if len(rep.Warnings) == 0 {
		t.Error("expected warnings for an unrecognisable machine")
	}
	if _, err := json.Marshal(rep); err != nil {
		t.Fatal(err)
	}
}

func TestMalformedInputsAreIgnored(t *testing.T) {
	fs := laptopFS()
	fs.Set("/proc/meminfo", "garbage\nMemTotal: abc kB\n\x00\x00")
	fs.Set("/proc/pressure/memory", "some avg10=oops\n")
	fs.Set("/proc/net/route", "Iface\nthis is not a route table\n")
	fs.Set("/sys/devices/system/cpu/cpu0/cpufreq/cpuinfo_max_freq", "not-a-number")
	rep := probe(t, fs, laptopRunner())
	if rep.Memory.TotalKB != 0 || rep.Memory.PSIMemSome != nil {
		t.Errorf("malformed values must be unknown: %+v", rep.Memory)
	}
	if rep.Network.Gateway != "" || rep.CPU.MaxMHz != nil {
		t.Errorf("malformed route/freq: %+v %v", rep.Network, rep.CPU.MaxMHz)
	}
}

func TestSystemctlOutputMismatchMarksUnknown(t *testing.T) {
	r := laptopRunner()
	r.On("systemctl is-active power-profiles-daemon.service tuned.service tlp.service auto-cpufreq.service cpupower.service",
		platform.Reply{Stdout: "active\n", Code: 3})
	rep := probe(t, laptopFS(), r)
	if !rep.Power.Unknown || len(rep.Warnings) == 0 {
		t.Error("unexpected systemctl output should be flagged, not trusted")
	}
}

func TestParsers(t *testing.T) {
	if i, g := ParseDefaultRoute("Iface\tDestination\tGateway\tFlags\tRefCnt\tUse\tMetric\tMask\neth0\t00000000\t0100A8C0\t0001\t0\t0\t0\t00000000\n"); i != "eth0" || g != "192.168.0.1" {
		t.Errorf("route: %s %s", i, g)
	}
	// RTF_UP not set -> ignored
	if i, _ := ParseDefaultRoute("h\neth0\t00000000\t0100A8C0\t0002\t0\t0\t0\t00000000\n"); i != "" {
		t.Error("down route used")
	}
	if got := ParseIWPowerSave("Power save: off\n"); got != "off" {
		t.Error(got)
	}
	if ns := ParseResolvConf("nameserver 1.1.1.1\n# nameserver 9.9.9.9\nnameserver 8.8.8.8"); len(ns) != 2 {
		t.Error(ns)
	}
	if got := unescapeMount(`/mnt/my\040disk`); got != "/mnt/my disk" {
		t.Error(got)
	}
	if FormatBytes(1536) != "1.5 KiB" || FormatBytes(12) != "12 B" {
		t.Error("FormatBytes")
	}
}

func TestReportJSONIsStable(t *testing.T) {
	rep := probe(t, laptopFS(), laptopRunner())
	b, err := json.Marshal(rep)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"schema_version", "system", "cpu", "power", "memory", "gpus", "storage", "network", "tools"} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing top-level key %q", k)
		}
	}
	if strings.Contains(string(b), "hostname") {
		t.Error("report must not contain hostname")
	}
}
