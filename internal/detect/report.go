// Package detect performs read-only hardware and system detection. It never
// writes anything, works as an ordinary user, and treats every missing or
// unreadable source as "unknown" rather than an error.
package detect

import (
	"context"

	"github.com/mttcn83/turboshift/internal/platform"
)

// SchemaVersion is bumped on any breaking change to the JSON layout.
const SchemaVersion = 1

// Report is the complete, JSON-serialisable system description. Pointer
// fields are nil when the value could not be determined (unknown is not zero).
type Report struct {
	SchemaVersion int      `json:"schema_version"`
	System        System   `json:"system"`
	CPU           CPU      `json:"cpu"`
	Power         Power    `json:"power"`
	Memory        Memory   `json:"memory"`
	GPUs          []GPU    `json:"gpus"`
	Storage       []Mount  `json:"storage"`
	Network       Network  `json:"network"`
	Tools         []Tool   `json:"tools"`
	Warnings      []string `json:"warnings,omitempty"`
}

type System struct {
	Distro      string `json:"distro"`
	DistroID    string `json:"distro_id"`
	DistroLike  string `json:"distro_like,omitempty"`
	Kernel      string `json:"kernel"`
	Arch        string `json:"arch"`
	UptimeSec   int64  `json:"uptime_sec"`
	SessionType string `json:"session_type,omitempty"` // wayland | x11 | tty
	Desktop     string `json:"desktop,omitempty"`
}

type CPU struct {
	Model        string   `json:"model"`
	Threads      int      `json:"threads"`
	Driver       string   `json:"scaling_driver,omitempty"`
	Governor     string   `json:"governor,omitempty"`
	Governors    []string `json:"available_governors,omitempty"`
	EPP          string   `json:"energy_performance_preference,omitempty"`
	EPPOptions   []string `json:"available_epp,omitempty"`
	MinMHz       *float64 `json:"min_mhz,omitempty"`
	MaxMHz       *float64 `json:"max_mhz,omitempty"`
	CurMHz       *float64 `json:"cur_mhz_avg,omitempty"`
	TurboEnabled *bool    `json:"turbo_enabled,omitempty"`
	// ThrottleEvents is the kernel's package throttle counter (one value per
	// package, replicated in every CPU's sysfs directory, so it is not summed).
	ThrottleEvents *uint64 `json:"thermal_throttle_events,omitempty"`
	// ThrottleSeconds is total time the package spent throttled since boot.
	ThrottleSeconds *float64 `json:"thermal_throttle_seconds,omitempty"`
	TempC           *float64 `json:"package_temp_c,omitempty"`
}

type Manager struct {
	Name   string `json:"name"`
	Unit   string `json:"unit"`
	Active bool   `json:"active"`
}

type Battery struct {
	Percent int    `json:"percent"`
	Status  string `json:"status"`
}

type Power struct {
	OnAC        *bool     `json:"on_ac,omitempty"`
	Battery     *Battery  `json:"battery,omitempty"`
	Managers    []Manager `json:"managers"`
	PPDProfile  string    `json:"ppd_profile,omitempty"`
	PPDProfiles []string  `json:"ppd_profiles,omitempty"`
	Unknown     bool      `json:"managers_unknown,omitempty"` // systemctl unavailable
}

type Memory struct {
	TotalKB     uint64   `json:"total_kb"`
	AvailableKB uint64   `json:"available_kb"`
	SwapTotalKB uint64   `json:"swap_total_kb"`
	SwapFreeKB  uint64   `json:"swap_free_kb"`
	ZramDevices int      `json:"zram_devices"`
	PSIMemSome  *float64 `json:"psi_memory_some_avg10,omitempty"`
	PSIMemFull  *float64 `json:"psi_memory_full_avg10,omitempty"`
	PSIIOSome   *float64 `json:"psi_io_some_avg10,omitempty"`
	PSICPUSome  *float64 `json:"psi_cpu_some_avg10,omitempty"`
}

type GPU struct {
	Vendor        string `json:"vendor"` // nvidia | amd | intel | other
	Name          string `json:"name,omitempty"`
	PCIID         string `json:"pci_id"`
	Slot          string `json:"slot"`
	Driver        string `json:"driver,omitempty"`
	BootVGA       bool   `json:"boot_vga"`
	RuntimeStatus string `json:"runtime_pm_status,omitempty"` // active | suspended
}

type Mount struct {
	Path       string `json:"path"`
	Device     string `json:"device"`
	FSType     string `json:"fstype"`
	TotalBytes uint64 `json:"total_bytes"`
	FreeBytes  uint64 `json:"free_bytes"`
}

type Iface struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"` // wifi | ethernet | loopback | virtual
	State     string `json:"state"`
	Carrier   *bool  `json:"carrier,omitempty"`
	SpeedMbps *int   `json:"speed_mbps,omitempty"`
	Driver    string `json:"driver,omitempty"`
	RxErrors  uint64 `json:"rx_errors"`
	TxErrors  uint64 `json:"tx_errors"`
	RxDropped uint64 `json:"rx_dropped"`
	TxDropped uint64 `json:"tx_dropped"`
}

type Network struct {
	Interfaces     []Iface  `json:"interfaces"`
	DefaultIface   string   `json:"default_iface,omitempty"`
	Gateway        string   `json:"default_gateway,omitempty"`
	Resolvers      []string `json:"resolvers,omitempty"`
	ResolverMode   string   `json:"resolver_mode,omitempty"` // systemd-resolved | static | unknown
	NetworkManager bool     `json:"network_manager"`
	WifiPowerSave  string   `json:"wifi_power_save,omitempty"` // on | off (from iw)
}

type Tool struct {
	Name    string `json:"name"`
	Present bool   `json:"present"`
	Purpose string `json:"purpose"`
}

// Env is everything Probe needs from the outside world.
type Env struct {
	FS     platform.FS
	Run    platform.Runner
	Getenv func(string) string
	GOARCH string
}

// Probe gathers a full Report. It is bounded by ctx and never fails as a
// whole: individual probe problems are recorded in Report.Warnings.
func Probe(ctx context.Context, e Env) Report {
	r := Report{SchemaVersion: SchemaVersion}
	warn := func(s string) { r.Warnings = append(r.Warnings, s) }

	r.System = probeSystem(e)
	r.CPU = probeCPU(e)
	r.Power = probePower(ctx, e, warn)
	r.Memory = probeMemory(e)
	r.GPUs = probeGPUs(ctx, e)
	r.Storage = probeStorage(e)
	r.Network = probeNetwork(ctx, e)
	r.Tools = probeTools(e)
	if r.CPU.Model == "" {
		warn("could not read /proc/cpuinfo")
	}
	if r.CPU.Driver == "" {
		warn("no cpufreq interface found (virtual machine, or frequency scaling is managed by firmware)")
	}
	return r
}
