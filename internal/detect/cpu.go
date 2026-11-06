package detect

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/mttcn83/turboshift/internal/platform"
)

func probeCPU(e Env) CPU {
	var c CPU
	if b, err := e.FS.ReadFile("/proc/cpuinfo"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			k, v, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			switch strings.TrimSpace(k) {
			case "model name":
				if c.Model == "" {
					c.Model = strings.TrimSpace(v)
				}
			case "processor":
				c.Threads++
			}
		}
	}
	const pol = "/sys/devices/system/cpu/cpu0/cpufreq/"
	c.Driver, _ = platform.ReadTrim(e.FS, pol+"scaling_driver")
	c.Governor, _ = platform.ReadTrim(e.FS, pol+"scaling_governor")
	if v, ok := platform.ReadTrim(e.FS, pol+"scaling_available_governors"); ok {
		c.Governors = strings.Fields(v)
	}
	c.EPP, _ = platform.ReadTrim(e.FS, pol+"energy_performance_preference")
	if v, ok := platform.ReadTrim(e.FS, pol+"energy_performance_available_preferences"); ok {
		c.EPPOptions = strings.Fields(v)
	}
	c.MinMHz = khzToMHz(e, pol+"cpuinfo_min_freq")
	c.MaxMHz = khzToMHz(e, pol+"cpuinfo_max_freq")

	// Average current frequency over all online CPUs.
	var sum float64
	var n int
	for i := 0; i < max(c.Threads, 1); i++ {
		if m := khzToMHz(e, fmt.Sprintf("/sys/devices/system/cpu/cpu%d/cpufreq/scaling_cur_freq", i)); m != nil {
			sum += *m
			n++
		}
	}
	if n > 0 {
		avg := sum / float64(n)
		c.CurMHz = &avg
	}

	// intel_pstate: no_turbo=0 means turbo is available. cpufreq boost is the generic knob.
	if v, ok := platform.ReadTrim(e.FS, "/sys/devices/system/cpu/intel_pstate/no_turbo"); ok {
		t := v == "0"
		c.TurboEnabled = &t
	} else if v, ok := platform.ReadTrim(e.FS, "/sys/devices/system/cpu/cpufreq/boost"); ok {
		t := v == "1"
		c.TurboEnabled = &t
	}

	c.ThrottleEvents, c.ThrottleSeconds = throttleStats(e, c.Threads)
	c.TempC = cpuTemp(e)
	return c
}

func khzToMHz(e Env, p string) *float64 {
	v, ok := platform.ReadTrim(e.FS, p)
	if !ok {
		return nil
	}
	k, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return nil
	}
	m := k / 1000
	return &m
}

// throttleStats reads the kernel's thermal-throttle counters. The package
// files hold one package-wide value that every CPU directory repeats, so the
// maximum across CPUs is used (summing would multiply it by the thread count).
// Per-core counters are the fallback on kernels without package files.
func throttleStats(e Env, threads int) (events *uint64, seconds *float64) {
	maxOf := func(file string) (uint64, bool) {
		var best uint64
		found := false
		for i := 0; i < max(threads, 1); i++ {
			p := fmt.Sprintf("/sys/devices/system/cpu/cpu%d/thermal_throttle/%s", i, file)
			if v, ok := platform.ReadTrim(e.FS, p); ok {
				if x, err := strconv.ParseUint(v, 10, 64); err == nil {
					found = true
					best = max(best, x)
				}
			}
		}
		return best, found
	}
	if n, ok := maxOf("package_throttle_count"); ok {
		events = &n
	} else if n, ok := maxOf("core_throttle_count"); ok {
		events = &n
	}
	if ms, ok := maxOf("package_throttle_total_time_ms"); ok {
		s := float64(ms) / 1000
		seconds = &s
	}
	return events, seconds
}

func cpuTemp(e Env) *float64 {
	ents, err := e.FS.ReadDir("/sys/class/hwmon")
	if err != nil {
		return nil
	}
	for _, d := range ents {
		base := "/sys/class/hwmon/" + d.Name()
		name, _ := platform.ReadTrim(e.FS, base+"/name")
		if name != "coretemp" && name != "k10temp" && name != "zenpower" {
			continue
		}
		// Prefer the package/Tctl sensor; fall back to temp1.
		for i := 1; i <= 8; i++ {
			label, _ := platform.ReadTrim(e.FS, fmt.Sprintf("%s/temp%d_label", base, i))
			if label == "Package id 0" || label == "Tctl" || label == "Tdie" {
				if t := milliC(e, fmt.Sprintf("%s/temp%d_input", base, i)); t != nil {
					return t
				}
			}
		}
		if t := milliC(e, base+"/temp1_input"); t != nil {
			return t
		}
	}
	return nil
}

func milliC(e Env, p string) *float64 {
	v, ok := platform.ReadTrim(e.FS, p)
	if !ok {
		return nil
	}
	x, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return nil
	}
	c := x / 1000
	return &c
}

// ---- power --------------------------------------------------------------

// managerUnits are services that claim ownership of CPU/power policy.
var managerUnits = []Manager{
	{Name: "power-profiles-daemon", Unit: "power-profiles-daemon.service"},
	{Name: "TuneD", Unit: "tuned.service"},
	{Name: "TLP", Unit: "tlp.service"},
	{Name: "auto-cpufreq", Unit: "auto-cpufreq.service"},
	{Name: "cpupower", Unit: "cpupower.service"},
}

func probePower(ctx context.Context, e Env, warn func(string)) Power {
	var p Power
	p.OnAC, p.Battery = probeSupply(e)

	units := make([]string, len(managerUnits))
	for i, m := range managerUnits {
		units[i] = m.Unit
	}
	p.Managers = append([]Manager(nil), managerUnits...)
	if e.Run.Has("systemctl") {
		// One call; prints one state per unit. Exit status is non-zero when any
		// unit is inactive, which is normal, so we parse stdout regardless.
		res, _ := e.Run.Run(ctx, "systemctl", append([]string{"is-active"}, units...)...)
		states := strings.Fields(res.Stdout)
		if len(states) == len(units) {
			for i, s := range states {
				p.Managers[i].Active = s == "active"
			}
		} else {
			p.Unknown = true
			warn("could not determine active power managers from systemctl")
		}
	} else {
		p.Unknown = true
	}

	if e.Run.Has("powerprofilesctl") {
		if res, err := e.Run.Run(ctx, "powerprofilesctl", "get"); err == nil {
			p.PPDProfile = strings.TrimSpace(res.Stdout)
		}
		if res, err := e.Run.Run(ctx, "powerprofilesctl", "list"); err == nil {
			p.PPDProfiles = ParsePPDProfiles(res.Stdout)
		}
	}
	return p
}

// ParsePPDProfiles extracts profile names from `powerprofilesctl list`.
func ParsePPDProfiles(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		t = strings.TrimPrefix(t, "* ")
		if strings.HasSuffix(t, ":") && !strings.Contains(t, " ") && !strings.HasPrefix(line, "    ") {
			out = append(out, strings.TrimSuffix(t, ":"))
		}
	}
	return out
}

func probeSupply(e Env) (*bool, *Battery) {
	ents, err := e.FS.ReadDir("/sys/class/power_supply")
	if err != nil {
		return nil, nil
	}
	var ac *bool
	var bat *Battery
	for _, d := range ents {
		base := "/sys/class/power_supply/" + d.Name()
		typ, _ := platform.ReadTrim(e.FS, base+"/type")
		switch typ {
		case "Mains", "USB_C", "USB_PD", "USB_PD_DRP":
			if v, ok := platform.ReadTrim(e.FS, base+"/online"); ok {
				on := v == "1"
				if ac == nil || on {
					ac = &on
				}
			}
		case "Battery":
			if bat != nil {
				continue
			}
			if scope, _ := platform.ReadTrim(e.FS, base+"/scope"); scope == "Device" {
				continue // peripheral battery (mouse, headset)
			}
			cap, _ := platform.ReadTrim(e.FS, base+"/capacity")
			pct, _ := strconv.Atoi(cap)
			st, _ := platform.ReadTrim(e.FS, base+"/status")
			bat = &Battery{Percent: pct, Status: st}
		}
	}
	return ac, bat
}
