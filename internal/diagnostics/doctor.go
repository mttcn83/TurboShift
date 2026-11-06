// Package diagnostics turns raw detection data into plain-language findings,
// live metrics and network diagnoses. Everything here is read-only.
package diagnostics

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/mttcn83/turboshift/internal/detect"
)

type Severity int

const (
	SevOK Severity = iota
	SevInfo
	SevNotice
	SevWarn
)

func (s Severity) String() string {
	return [...]string{"ok", "info", "notice", "warning"}[s]
}

func (s Severity) MarshalJSON() ([]byte, error) { return json.Marshal(s.String()) }

// Fix links a finding to a tweak that addresses it, if one exists.
type Fix struct {
	TweakID string            `json:"tweak_id"`
	Params  map[string]string `json:"params"`
}

type Finding struct {
	ID     string   `json:"id"`
	Area   string   `json:"area"` // CPU, Power, Memory, Storage, GPU, Network, Gaming
	Sev    Severity `json:"severity"`
	Title  string   `json:"title"`
	Detail string   `json:"detail,omitempty"`
	Fix    *Fix     `json:"fix,omitempty"`
}

// Diagnose evaluates the report with explicit, documented rules. There is no
// scoring model and no AI: every finding states the measurement behind it.
// "Nothing found" is a legitimate result and yields OK findings.
func Diagnose(r detect.Report) []Finding {
	var f []Finding
	add := func(x Finding) { f = append(f, x) }

	// ---- power ownership ----
	var active []string
	for _, m := range r.Power.Managers {
		if m.Active {
			active = append(active, m.Name)
		}
	}
	switch {
	case len(active) > 1:
		add(Finding{ID: "power.conflict", Area: "Power", Sev: SevWarn,
			Title:  "Several power managers are running: " + strings.Join(active, ", "),
			Detail: "Each one writes CPU/power settings, so they overwrite each other and results become unpredictable. Keep one and disable the others (TurboShift will not choose for you)."})
	case len(active) == 1:
		add(Finding{ID: "power.owner", Area: "Power", Sev: SevOK, Title: active[0] + " manages power policy",
			Detail: "A single manager owns CPU power settings, which is the healthy setup. TurboShift works through it instead of fighting it."})
	case r.Power.Unknown:
		add(Finding{ID: "power.owner", Area: "Power", Sev: SevInfo, Title: "Could not tell which power manager is active",
			Detail: "systemctl was unavailable, so direct CPU changes are disabled to stay safe."})
	default:
		add(Finding{ID: "power.owner", Area: "Power", Sev: SevInfo, Title: "No power manager is running",
			Detail: "CPU power policy comes straight from the kernel defaults. Installing power-profiles-daemon (or TuneD) gives you easy, reversible profiles."})
	}

	onAC := r.Power.OnAC != nil && *r.Power.OnAC
	if r.Power.PPDProfile == "power-saver" && onAC {
		add(Finding{ID: "power.saver-on-ac", Area: "Power", Sev: SevWarn,
			Title:  "Power-saver profile is active while plugged in",
			Detail: "This profile limits performance to save battery, but you are on AC power, so you get the slowdown without the benefit.",
			Fix:    &Fix{"power.profile", map[string]string{"profile": "balanced"}}})
	} else if r.Power.PPDProfile == "performance" && r.Power.OnAC != nil && !*r.Power.OnAC {
		add(Finding{ID: "power.perf-on-battery", Area: "Power", Sev: SevNotice,
			Title:  "Performance profile is active on battery",
			Detail: "Fine for a short gaming session, but it drains the battery noticeably faster and runs hotter."})
	} else if r.Power.PPDProfile != "" {
		add(Finding{ID: "power.profile", Area: "Power", Sev: SevOK,
			Title: "Power profile: " + r.Power.PPDProfile, Detail: "Nothing to change here."})
	}

	// ---- CPU ----
	c := r.CPU
	if c.TurboEnabled != nil && !*c.TurboEnabled {
		add(Finding{ID: "cpu.turbo-off", Area: "CPU", Sev: SevWarn, Title: "CPU turbo/boost is disabled",
			Detail: "The CPU cannot exceed its base clock, which can cost a lot of single-thread speed. This is a firmware or kernel setting (BIOS, intel_pstate no_turbo, cpufreq boost). If it is only the kernel switch, the Optimizer's CPU turbo / boost item can turn it back on (never automatically); a BIOS setting has to be changed in the BIOS.",
			Fix:    &Fix{"cpu.boost", map[string]string{"boost": "on"}}})
	}
	switch {
	case c.ThrottleSeconds != nil && r.System.UptimeSec > 600:
		frac := *c.ThrottleSeconds / float64(r.System.UptimeSec)
		mins := *c.ThrottleSeconds / 60
		switch {
		case frac >= 0.20:
			add(Finding{ID: "cpu.thermal", Area: "CPU", Sev: SevWarn,
				Title:  fmt.Sprintf("CPU was thermally throttled %.0f%% of the time since boot (%.0f min)", frac*100, mins),
				Detail: "The kernel counts time the CPU package was held back by its thermal limit. A fifth of uptime or more suggests a cooling problem: dust, fans, airflow or thermal paste. Disabling thermal protection is never recommended."})
		case frac >= 0.05:
			add(Finding{ID: "cpu.thermal", Area: "CPU", Sev: SevNotice,
				Title:  fmt.Sprintf("CPU was thermally throttled %.0f%% of the time since boot (%.0f min)", frac*100, mins),
				Detail: "Expected after long heavy sessions on a laptop. If it happens during light use, check airflow, fans and dust. This counter cannot tell power-limit from temperature-limit time precisely."})
		default:
			add(Finding{ID: "cpu.thermal", Area: "CPU", Sev: SevOK, Title: fmt.Sprintf("Thermal throttling is minimal (%.1f%% of uptime)", frac*100)})
		}
	case c.ThrottleEvents != nil && *c.ThrottleEvents == 0:
		add(Finding{ID: "cpu.thermal", Area: "CPU", Sev: SevOK, Title: "No CPU thermal throttling since boot"})
	}
	if c.TempC != nil && *c.TempC >= 95 {
		add(Finding{ID: "cpu.hot", Area: "CPU", Sev: SevWarn, Title: fmt.Sprintf("CPU is very hot right now (%.0f °C)", *c.TempC),
			Detail: "Close to the throttle point. Check cooling and what is using the CPU."})
	}
	if c.Driver == "acpi-cpufreq" && c.Governor == "powersave" && onAC {
		add(Finding{ID: "cpu.governor-powersave", Area: "CPU", Sev: SevNotice, Title: "Governor is 'powersave' on AC (acpi-cpufreq)",
			Detail: "With the acpi-cpufreq driver, 'powersave' pins the lowest clock. 'schedutil' or 'performance' respond much better."})
	} else if (c.Driver == "intel_pstate" || strings.HasPrefix(c.Driver, "amd-pstate")) && c.Governor == "powersave" {
		add(Finding{ID: "cpu.governor-note", Area: "CPU", Sev: SevInfo, Title: "Governor 'powersave' with " + c.Driver + " is normal",
			Detail: "With this driver 'powersave' still scales up quickly; the energy/performance preference (set by your power profile) matters more than the governor name."})
	}

	// ---- memory ----
	m := r.Memory
	if m.TotalKB > 0 {
		availPct := float64(m.AvailableKB) / float64(m.TotalKB) * 100
		switch {
		case m.PSIMemFull != nil && *m.PSIMemFull >= 1:
			add(Finding{ID: "mem.pressure", Area: "Memory", Sev: SevWarn, Title: fmt.Sprintf("Programs are stalling on memory (%.1f%% of time, last 10 s)", *m.PSIMemFull),
				Detail: "The kernel reports that all tasks were blocked waiting for memory. Close heavy apps or add RAM/zram."})
		case m.PSIMemSome != nil && *m.PSIMemSome >= 10:
			add(Finding{ID: "mem.pressure", Area: "Memory", Sev: SevNotice, Title: fmt.Sprintf("Some memory pressure (%.1f%% stall, last 10 s)", *m.PSIMemSome),
				Detail: "Some tasks are waiting for memory reclaim. Usually transient; look at the top memory users."})
		case availPct < 10:
			add(Finding{ID: "mem.low", Area: "Memory", Sev: SevNotice, Title: fmt.Sprintf("Only %.0f%% of RAM is available", availPct),
				Detail: "Not stalling yet, but there is little headroom for a game."})
		default:
			add(Finding{ID: "mem.ok", Area: "Memory", Sev: SevOK, Title: fmt.Sprintf("Memory is healthy (%.0f%% available)", availPct)})
		}
		gib := float64(m.TotalKB) / 1048576
		if m.SwapTotalKB == 0 && m.ZramDevices == 0 && gib <= 16 {
			add(Finding{ID: "mem.no-swap", Area: "Memory", Sev: SevInfo, Title: "No swap or zram configured",
				Detail: "With no swap, running out of RAM means the kernel kills programs instead of slowing down. zram (compressed RAM swap) is a cheap safeguard; TurboShift does not change swap automatically."})
		}
		if m.SwapTotalKB > 0 && m.SwapFreeKB < m.SwapTotalKB/2 && m.ZramDevices == 0 {
			add(Finding{ID: "mem.swap-heavy", Area: "Memory", Sev: SevNotice, Title: "Swap is more than half used",
				Detail: "Memory has been under sustained pressure; swapping to disk is slow compared with RAM."})
		}
	}
	if m.PSIIOSome != nil && *m.PSIIOSome >= 20 {
		add(Finding{ID: "io.pressure", Area: "Storage", Sev: SevNotice, Title: fmt.Sprintf("Disk I/O is a bottleneck right now (%.0f%% stall)", *m.PSIIOSome),
			Detail: "Tasks are waiting for the disk. Check for a background backup, indexer or update."})
	}
	if m.PSICPUSome != nil && *m.PSICPUSome >= 50 {
		add(Finding{ID: "cpu.pressure", Area: "CPU", Sev: SevNotice, Title: fmt.Sprintf("CPU is saturated right now (%.0f%% of tasks waiting)", *m.PSICPUSome),
			Detail: "More work wants to run than there are CPU cores. See the top CPU users."})
	}

	// ---- storage ----
	for _, d := range r.Storage {
		pct := float64(d.FreeBytes) / float64(d.TotalBytes) * 100
		// Small boot/EFI partitions are normally tiny; judge them by percentage only.
		smallOK := strings.HasPrefix(d.Path, "/boot")
		switch {
		case pct < 5 || (!smallOK && pct < 20 && d.FreeBytes < 2<<30):
			add(Finding{ID: "storage.full:" + d.Path, Area: "Storage", Sev: SevWarn,
				Title:  fmt.Sprintf("%s is almost full (%s free, %.0f%%)", d.Path, detect.FormatBytes(d.FreeBytes), pct),
				Detail: "Very little free space can make writes slow and cause failed updates or game installs. TurboShift never deletes files for you."})
		case pct < 10:
			add(Finding{ID: "storage.low:" + d.Path, Area: "Storage", Sev: SevNotice,
				Title: fmt.Sprintf("%s is getting full (%s free, %.0f%%)", d.Path, detect.FormatBytes(d.FreeBytes), pct)})
		}
	}
	if len(r.Storage) > 0 && !hasPrefix(f, "storage.") {
		add(Finding{ID: "storage.ok", Area: "Storage", Sev: SevOK, Title: "Free disk space is fine on all mounted volumes"})
	}

	// ---- GPU ----
	var igpu, dgpu *detect.GPU
	for i := range r.GPUs {
		g := &r.GPUs[i]
		if g.Vendor == "nvidia" && g.Driver == "nouveau" {
			add(Finding{ID: "gpu.nouveau", Area: "GPU", Sev: SevWarn, Title: "NVIDIA GPU is using the open-source nouveau driver",
				Detail: "nouveau generally cannot reach the clocks or features of NVIDIA's own driver, so 3D performance is usually far lower. Install the nvidia driver package for your distribution."})
		}
		if g.Vendor == "nvidia" && g.Driver == "" {
			add(Finding{ID: "gpu.no-driver", Area: "GPU", Sev: SevWarn, Title: "NVIDIA GPU has no driver bound",
				Detail: "Games will fall back to the integrated GPU or software rendering."})
		}
		if g.Vendor == "nvidia" || (g.Vendor == "amd" && !g.BootVGA && len(r.GPUs) > 1) {
			dgpu = g
		} else if g.BootVGA || g.Vendor == "intel" || g.Vendor == "amd" {
			igpu = g
		}
	}
	if igpu != nil && dgpu != nil {
		add(Finding{ID: "gpu.hybrid", Area: "GPU", Sev: SevInfo, Title: "Hybrid graphics: " + gpuLabel(*igpu) + " + " + gpuLabel(*dgpu),
			Detail: "Games run on the integrated GPU unless you request the discrete one per game (NVIDIA: __NV_PRIME_RENDER_OFFLOAD=1 __GLX_VENDOR_LIBRARY_NAME=nvidia, or the prime-run wrapper; AMD/Mesa: DRI_PRIME=1). If a game feels slow, check which GPU it uses."})
	}
	if len(r.GPUs) == 0 {
		add(Finding{ID: "gpu.none", Area: "GPU", Sev: SevInfo, Title: "No GPU found in /sys/class/drm", Detail: "Headless system or virtual machine."})
	}

	// ---- network ----
	n := r.Network
	if n.DefaultIface == "" {
		add(Finding{ID: "net.no-route", Area: "Network", Sev: SevNotice, Title: "No default network route",
			Detail: "This computer is not connected to a network."})
	}
	if n.WifiPowerSave == "on" && onAC {
		add(Finding{ID: "net.wifi-powersave", Area: "Network", Sev: SevNotice, Title: "Wi-Fi power saving is on while plugged in",
			Detail: "The radio dozes between packets, which can add small, variable delays. It only matters if Network Doctor shows jitter on a strong signal.",
			Fix:    &Fix{"net.wifi-powersave", map[string]string{"powersave": "off"}}})
	}
	for _, i := range n.Interfaces {
		if i.Kind != "ethernet" && i.Kind != "wifi" {
			continue
		}
		if errs := i.RxErrors + i.TxErrors; errs >= 100 {
			add(Finding{ID: "net.errors:" + i.Name, Area: "Network", Sev: SevNotice,
				Title:  fmt.Sprintf("%s has %d packet errors since boot", i.Name, errs),
				Detail: "Errors on a physical link point at cabling, a failing port or a driver problem (Wi-Fi: weak signal)."})
		}
		if i.Kind == "ethernet" && i.SpeedMbps != nil && *i.SpeedMbps == 100 {
			add(Finding{ID: "net.slow-ethernet:" + i.Name, Area: "Network", Sev: SevNotice, Title: i.Name + " negotiated only 100 Mbit/s",
				Detail: "Gigabit links often fall back to 100 Mbit/s with a damaged cable or a bad port."})
		}
	}
	if n.DefaultIface != "" && len(n.Resolvers) == 0 {
		add(Finding{ID: "net.no-dns", Area: "Network", Sev: SevWarn, Title: "No DNS resolver configured",
			Detail: "Name lookups will fail even though the network is up."})
	}

	// ---- gaming readiness ----
	if !r.HasTool("gamemoderun") {
		add(Finding{ID: "gaming.gamemode", Area: "Gaming", Sev: SevInfo, Title: "GameMode is not installed (optional)",
			Detail: "GameMode applies temporary, game-only tweaks and is the recommended way to get them. Install the gamemode package to use it with TurboShift."})
	} else {
		add(Finding{ID: "gaming.gamemode", Area: "Gaming", Sev: SevOK, Title: "GameMode is installed"})
	}
	if !r.HasTool("mangohud") {
		add(Finding{ID: "gaming.mangohud", Area: "Gaming", Sev: SevInfo, Title: "MangoHud is not installed (optional)",
			Detail: "Benchmark Lab uses MangoHud's local logs to measure FPS and frame times. Without it you can still use the system and network tools."})
	} else {
		add(Finding{ID: "gaming.mangohud", Area: "Gaming", Sev: SevOK, Title: "MangoHud is installed (needed for game benchmarks)"})
	}

	sort.SliceStable(f, func(i, j int) bool { return f[i].Sev > f[j].Sev })
	return f
}

func hasPrefix(f []Finding, p string) bool {
	for _, x := range f {
		if strings.HasPrefix(x.ID, p) {
			return true
		}
	}
	return false
}

func gpuLabel(g detect.GPU) string {
	if g.Name != "" {
		return g.Name
	}
	return strings.ToUpper(g.Vendor[:1]) + g.Vendor[1:] + " " + g.PCIID
}

// Counts summarises findings by severity for dashboards.
func Counts(f []Finding) (warn, notice int) {
	for _, x := range f {
		switch x.Sev {
		case SevWarn:
			warn++
		case SevNotice:
			notice++
		}
	}
	return
}
