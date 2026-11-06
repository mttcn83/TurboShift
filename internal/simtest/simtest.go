// Package simtest provides a stateful simulated Linux machine for tests.
//
// It implements platform.Runner (powerprofilesctl, nmcli, iw, busctl,
// systemctl) and owns a platform.MemFS for sysfs/procfs. Unlike the static
// FakeRunner, commands change state, so apply/verify/restore can be tested
// end to end, with injectable failures. It is test-only and never linked into
// the turbo binary.
package simtest

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/mttcn83/turboshift/internal/platform"
)

type Machine struct {
	mu sync.Mutex
	FS *platform.MemFS

	Tools map[string]bool // installed executables

	// power-profiles-daemon
	PPD         bool // service active
	PPDProfile  string
	PPDProfiles []string
	OtherMgrs   map[string]bool // unit -> active (tuned.service, tlp.service...)

	// NetworkManager / Wi-Fi
	NM             bool
	WifiUUID       string
	Iface          string
	NMPowersave    string // nm enum name
	DNS            string // ipv4.dns, comma separated ("" = automatic)
	IgnoreAutoDNS  string // ipv4.ignore-auto-dns: "yes" | "no"
	Radio          string // "on" | "off" as iw reports
	ReapplyApplies bool   // does `device reapply` push the setting to the radio?

	// Hypr holds Hyprland's integer options; nil means Hyprland is not running.
	Hypr map[string]int
	// GSettings holds org.gnome.desktop.interface keys ("true"/"false").
	GSettings map[string]string

	Fail map[string]error // injected failures keyed by op name

	Checkpoints, Destroyed, RolledBack int
	// NMRollbackDenied simulates polkit refusing CheckpointRollback.
	NMRollbackDenied bool

	Calls []string
}

// New returns a hybrid-laptop-like machine on AC with PPD and NetworkManager.
func New() *Machine {
	m := &Machine{
		FS:    platform.NewMemFS(),
		Tools: map[string]bool{"systemctl": true, "powerprofilesctl": true, "nmcli": true, "iw": true, "busctl": true, "pkexec": true},
		PPD:   true, PPDProfile: "balanced", PPDProfiles: []string{"performance", "balanced", "power-saver"},
		OtherMgrs: map[string]bool{},
		NM:        true, WifiUUID: "049d7dd4-b361-44b1-8e51-70ac8f2e2a81", Iface: "wlp108s0",
		NMPowersave: "default", Radio: "on", ReapplyApplies: true, IgnoreAutoDNS: "no",
		Fail: map[string]error{},
	}
	f := m.FS
	f.Set("/etc/os-release", "PRETTY_NAME=\"Test Linux\"\nID=testos\n")
	f.Set("/proc/sys/kernel/osrelease", "7.3.1-test\n")
	f.Set("/proc/cpuinfo", "processor\t: 0\nmodel name\t: Test CPU\n\nprocessor\t: 1\nmodel name\t: Test CPU\n")
	for i := 0; i < 2; i++ {
		p := fmt.Sprintf("/sys/devices/system/cpu/cpu%d/cpufreq/", i)
		f.Set(p+"scaling_governor", "powersave\n").Set(p+"scaling_driver", "intel_pstate\n")
		f.Set(p+"scaling_available_governors", "performance powersave\n")
	}
	f.Set("/sys/class/power_supply/ADP0/type", "Mains\n").Set("/sys/class/power_supply/ADP0/online", "1\n")
	f.Set("/proc/net/route", "Iface\tDestination\tGateway\tFlags\tRefCnt\tUse\tMetric\tMask\nwlp108s0\t00000000\t0101A8C0\t0003\t0\t0\t600\t00000000\n")
	f.Set("/sys/class/net/wlp108s0/operstate", "up\n").Set("/sys/class/net/wlp108s0/wireless/x", "")
	f.Set("/etc/resolv.conf", "nameserver 192.168.1.1\n")
	f.Set("/proc/meminfo", "MemTotal: 16000000 kB\nMemAvailable: 8000000 kB\nSwapTotal: 0 kB\nSwapFree: 0 kB\n")
	return m
}

func (m *Machine) SetGovernor(cpu int, g string) {
	m.FS.Set(fmt.Sprintf("/sys/devices/system/cpu/cpu%d/cpufreq/scaling_governor", cpu), g+"\n")
}

// Governor returns the simulated governor of a CPU.
func (m *Machine) Governor(cpu int) string {
	v, _ := m.FS.Get(fmt.Sprintf("/sys/devices/system/cpu/cpu%d/cpufreq/scaling_governor", cpu))
	return strings.TrimSpace(v)
}

func (m *Machine) Has(name string) bool { m.mu.Lock(); defer m.mu.Unlock(); return m.Tools[name] }

// Called reports whether any recorded command line contains substr.
func (m *Machine) Called(substr string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.Calls {
		if strings.Contains(c, substr) {
			return true
		}
	}
	return false
}

func (m *Machine) Run(_ context.Context, name string, args ...string) (platform.Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	line := strings.TrimSpace(name + " " + strings.Join(args, " "))
	m.Calls = append(m.Calls, line)
	if !m.Tools[name] {
		return platform.Result{ExitCode: -1}, fmt.Errorf("%s: %w", name, platform.ErrNotInstalled)
	}
	ok := func(out string) (platform.Result, error) { return platform.Result{Stdout: out}, nil }
	bad := func(msg string) (platform.Result, error) {
		return platform.Result{Stderr: msg, ExitCode: 1}, errors.New(msg)
	}
	fail := func(op string) (platform.Result, error, bool) {
		if e, f := m.Fail[op]; f {
			return platform.Result{Stderr: e.Error(), ExitCode: 1}, e, true
		}
		return platform.Result{}, nil, false
	}

	switch name {
	case "systemctl":
		var out []string
		for _, u := range args[1:] {
			active := false
			switch u {
			case "power-profiles-daemon.service":
				active = m.PPD
			case "NetworkManager.service":
				active = m.NM
			default:
				active = m.OtherMgrs[u]
			}
			if active {
				out = append(out, "active")
			} else {
				out = append(out, "inactive")
			}
		}
		return ok(strings.Join(out, "\n") + "\n")

	case "powerprofilesctl":
		if !m.PPD {
			return bad("Couldn't connect to power-profiles-daemon")
		}
		switch args[0] {
		case "get":
			return ok(m.PPDProfile + "\n")
		case "list":
			var b strings.Builder
			for _, p := range m.PPDProfiles {
				pre := "  "
				if p == m.PPDProfile {
					pre = "* "
				}
				b.WriteString(pre + p + ":\n    CpuDriver:\tintel_pstate\n\n")
			}
			return ok(b.String())
		case "set":
			if r, e, f := fail("ppd-set"); f {
				return r, e
			}
			for _, p := range m.PPDProfiles {
				if p == args[1] {
					m.PPDProfile = p
					return ok("")
				}
			}
			return bad("Invalid profile name " + args[1])
		}

	case "nmcli":
		switch {
		case args[0] == "-t" && len(args) > 4 && args[2] == "UUID,DEVICE" && args[4] == "show": // connection show --active
			if m.WifiUUID == "" {
				return ok("")
			}
			return ok(m.WifiUUID + ":" + m.Iface + "\n")
		case args[0] == "-t" && len(args) > 4 && args[4] == "show": // connection show --active
			if m.WifiUUID == "" {
				return ok("1dc29a41-11d4-4efd-8199-9f35c8aeb581:loopback:lo\n")
			}
			return ok(m.WifiUUID + ":802-11-wireless:" + m.Iface + "\n1dc29a41-11d4-4efd-8199-9f35c8aeb581:loopback:lo\n")
		case args[0] == "-g" && args[1] == "ipv4.dns,ipv4.ignore-auto-dns":
			return ok(strings.ReplaceAll(m.DNS, ",", ", ") + "\n" + m.IgnoreAutoDNS + "\n")
		case args[0] == "-g": // -g 802-11-wireless.powersave connection show UUID
			return ok(m.NMPowersave + "\n")
		case args[0] == "connection" && args[1] == "modify" && args[3] == "ipv4.dns":
			if r, e, f := fail("nm-modify"); f {
				return r, e
			}
			m.DNS = strings.Join(strings.Fields(args[4]), ",")
			m.IgnoreAutoDNS = args[6]
			return ok("")
		case args[0] == "connection" && args[1] == "modify":
			if r, e, f := fail("nm-modify"); f {
				return r, e
			}
			m.NMPowersave = args[4]
			return ok("")
		case args[0] == "device" && args[1] == "reapply":
			if r, e, f := fail("nm-reapply"); f {
				return r, e
			}
			if m.ReapplyApplies {
				switch m.NMPowersave {
				case "disable":
					m.Radio = "off"
				case "enable":
					m.Radio = "on"
				}
			}
			return ok("")
		}

	case "iw":
		return ok("Power save: " + m.Radio + "\n")

	case "hyprctl":
		if m.Hypr == nil {
			return bad("Couldn't connect to Hyprland")
		}
		switch args[0] {
		case "getoption":
			v, found := m.Hypr[args[1]]
			if !found {
				return ok("no such option\n")
			}
			return ok(fmt.Sprintf(`{"option": %q, "int": %d, "float": %d.000000, "str": "[[EMPTY]]", "set": true}`+"\n", args[1], v, v))
		case "keyword":
			if r, e, f := fail("hypr-keyword"); f {
				return r, e
			}
			if _, found := m.Hypr[args[1]]; !found {
				return ok("config option <" + args[1] + "> does not exist\n")
			}
			var n int
			if _, err := fmt.Sscan(args[2], &n); err != nil {
				return ok("invalid value\n")
			}
			m.Hypr[args[1]] = n
			return ok("ok\n")
		}

	case "gsettings":
		if len(m.GSettings) == 0 {
			return bad("No such schema")
		}
		switch args[0] {
		case "get":
			if v, found := m.GSettings[args[2]]; found {
				return ok(v + "\n")
			}
			return bad("No such key")
		case "set":
			if r, e, f := fail("gsettings-set"); f {
				return r, e
			}
			m.GSettings[args[2]] = args[3]
			return ok("")
		}

	case "busctl":
		method := args[5]
		switch method {
		case "CheckpointCreate":
			if r, e, f := fail("checkpoint-create"); f {
				return r, e
			}
			m.Checkpoints++
			return ok(fmt.Sprintf("o \"/org/freedesktop/NetworkManager/Checkpoint/%d\"\n", m.Checkpoints))
		case "CheckpointDestroy":
			m.Destroyed++
			return ok("")
		case "CheckpointRollback":
			if m.NMRollbackDenied {
				return bad("Access denied")
			}
			m.RolledBack++
			return ok("a{su} 0\n")
		}
	}
	return platform.Result{ExitCode: 127, Stderr: "simtest: unsupported command: " + line}, fmt.Errorf("simtest: unsupported command: %s", line)
}
