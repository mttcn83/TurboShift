package tweaks

import (
	"fmt"
	"strings"

	"github.com/mttcn83/turboshift/internal/platform"
	"github.com/mttcn83/turboshift/internal/privilege"
)

// More kernel tunables, all built on fileTweak. Every entry states a real
// mechanism, who it helps, what it costs and how to measure it; the shipped
// default is always "no change" until the user picks a value, and most are
// never recommended automatically because the right value depends on the
// workload.

// one is the common single-file shape: one attribute, a fixed option list.
func one(m Meta, what, key, p string, opts []Option, warn string) fileTweak {
	m.NeedsAuth = true
	m.Options = opts
	if m.ParamName == "" {
		m.ParamName = "value"
	}
	return fileTweak{
		meta: m, what: what,
		gate: func(Env) string { return "" },
		targets: func(env Env) map[string]string {
			if _, ok := platform.ReadTrim(env.FS, p); ok {
				return map[string]string{key: p}
			}
			return nil
		},
		options:  func(Env, map[string]string) []Option { return opts },
		warnings: []string{warn},
	}
}

const resets = "Resets at reboot."

var qdiscOptions = []Option{
	{"fq_codel", "fq_codel", "Fair queueing with active queue management: keeps latency low when the link is busy. The default on most distributions."},
	{"cake", "cake", "Like fq_codel with per-host fairness and shaping; the usual choice against bufferbloat. Needs the sch_cake module."},
	{"fq", "fq", "Fair queueing without AQM; pairs with BBR on servers."},
	{"pfifo_fast", "pfifo_fast", "Plain FIFO: no latency control (legacy)."},
}

// DefaultQdisc selects the queueing discipline new network interfaces get.
func DefaultQdisc() Tweak {
	t := one(Meta{
		ID: "net.qdisc", Name: "Default packet scheduler (qdisc)",
		Summary:  "Pick how queued packets are scheduled on network interfaces, to keep latency low when the link is busy.",
		Details:  "Writes net.core.default_qdisc. fq_codel and cake fight bufferbloat: the delay that builds up in queues while a download or upload saturates the link. It applies to interfaces created or re-activated afterwards (reconnect to apply it to the current one).",
		Tradeoff: "A scheduler cannot fix bufferbloat that lives in your router's queue, only your own machine's. cake needs a little more CPU on very fast links.",
		Evidence: "Network Doctor with a load test: compare latency under load before and after (and reconnect the interface first).",
		Category: CatNetwork, Tier: TierAdvanced, Risk: RiskLow, Scope: ScopeUntilBoot,
	}, "default packet scheduler", "default_qdisc", "/proc/sys/net/core/default_qdisc", qdiscOptions, "Applies to interfaces brought up afterwards; resets at reboot.")
	return t
}

var fastOpenOptions = []Option{
	{"0", "Off", "Never use TCP Fast Open."},
	{"1", "Client", "Send data in the first packet when connecting out (saves one round trip on repeat visits to servers that support it)."},
	{"3", "Client and server", "Client behaviour plus accepting Fast Open connections."},
}

// TCPFastOpen allows data in the SYN packet for repeat connections.
func TCPFastOpen() Tweak {
	return one(Meta{
		ID: "net.tcp-fastopen", Name: "TCP Fast Open",
		Summary:  "Skip one network round trip when reconnecting to servers you have visited before.",
		Details:  "Writes net.ipv4.tcp_fastopen. With client support the first data goes out in the connection-opening packet once a cookie from the server is cached, which shaves one round-trip time off each new connection to a server that supports it.",
		Tradeoff: "Only helps connections to servers that support it, and some middleboxes drop such packets (the kernel falls back automatically). Gains are one round trip per connection, not bandwidth.",
		Evidence: "Time many short connections (page loads, API calls) before and after; on a fast LAN the difference is below measurement noise.",
		Category: CatNetwork, Tier: TierAdvanced, Risk: RiskLow, Scope: ScopeUntilBoot,
	}, "TCP Fast Open", "tcp_fastopen", "/proc/sys/net/ipv4/tcp_fastopen", fastOpenOptions, resets)
}

var slowStartOptions = []Option{
	{"0", "Keep speed after idle", "A connection that pauses keeps its learned speed (good for bursty traffic such as interactive streams)."},
	{"1", "Reset after idle", "The kernel default: speed is relearned after a pause."},
}

// TCPSlowStartAfterIdle keeps connection speed across pauses.
func TCPSlowStartAfterIdle() Tweak {
	return one(Meta{
		ID: "net.tcp-idle", Name: "TCP speed after idle",
		Summary:  "Stop long-lived connections from slowing down after a short pause.",
		Details:  "Writes net.ipv4.tcp_slow_start_after_idle. By default a TCP connection idle for a moment restarts at a small window, so the next burst (a chat message, the next video chunk, a remote-desktop frame) starts slowly. Disabling this keeps the learned window.",
		Tradeoff: "On a congested path the first burst after a pause can overshoot and cause brief loss. Only long-lived, bursty TCP connections are affected.",
		Evidence: "Compare the time to deliver a burst after a pause (for example an interactive SSH or remote desktop session) over several runs.",
		Category: CatNetwork, Tier: TierAdvanced, Risk: RiskLow, Scope: ScopeUntilBoot,
	}, "TCP idle behaviour", "slow_start_after_idle", "/proc/sys/net/ipv4/tcp_slow_start_after_idle", slowStartOptions, resets)
}

var budgetOptions = []Option{
	{"300", "300", "The kernel default."},
	{"600", "600", "Process more packets per network interrupt: less overhead on fast links and busy servers."},
	{"1000", "1000", "Higher still; only for very high packet rates."},
}

// NetdevBudget raises the packets handled per softirq poll.
func NetdevBudget() Tweak {
	return one(Meta{
		ID: "net.budget", Name: "Network receive budget",
		Summary:  "Let the kernel process more incoming packets per interrupt on very busy links.",
		Details:  "Writes net.core.netdev_budget. When a fast NIC receives packets faster than the default budget can drain, the kernel counts 'time squeezes' and defers work, which adds jitter. A larger budget lets one poll cycle drain more.",
		Tradeoff: "A longer poll cycle can delay other work on the same CPU by a little. Irrelevant on ordinary home links.",
		Evidence: "Only worth trying if /proc/net/softnet_stat shows a growing third column (time squeezes) under load.",
		Category: CatNetwork, Tier: TierAdvanced, Risk: RiskLow, Scope: ScopeUntilBoot,
	}, "network receive budget", "netdev_budget", "/proc/sys/net/core/netdev_budget", budgetOptions, resets)
}

var vfsOptions = []Option{
	{"50", "50", "Keep directory and file-name caches longer: faster repeated file lookups (project trees, game libraries)."},
	{"100", "100", "The kernel default."},
	{"200", "200", "Reclaim those caches sooner, freeing memory for programs."},
}

// VFSCachePressure biases reclaim between file-name caches and program memory.
func VFSCachePressure() Tweak {
	return one(Meta{
		ID: "mem.vfs-cache", Name: "File-name cache retention",
		Summary:  "Keep directory and file lookup caches in memory longer.",
		Details:  "Writes vm.vfs_cache_pressure. Lower values keep the dentry and inode caches longer, which speeds up repeated directory scans, large game or source trees and file managers when you have enough RAM.",
		Tradeoff: "Those caches then hold memory that programs could use. On a machine that is already short of RAM, lower values make things worse.",
		Evidence: "Time a repeated directory scan (for example `find` over your game library) before and after; check memory pressure in Doctor.",
		Category: CatMemory, Tier: TierAdvanced, Risk: RiskLow, Scope: ScopeUntilBoot,
	}, "file-name cache retention", "vfs_cache_pressure", "/proc/sys/vm/vfs_cache_pressure", vfsOptions, resets)
}

// DirtyWriteback shapes how much unwritten data may pile up in RAM.
func DirtyWriteback() Tweak {
	const bg, hard = "/proc/sys/vm/dirty_background_ratio", "/proc/sys/vm/dirty_ratio"
	presets := map[string]map[string]string{
		"responsive": {"background": "5", "hard": "10"},
		"default":    {"background": "10", "hard": "20"},
		"throughput": {"background": "20", "hard": "40"},
	}
	opts := []Option{
		{"responsive", "Responsive", "Start writing sooner (5% / 10%): fewer long stalls when a big write finishes, especially to slow or USB disks."},
		{"default", "Kernel default", "10% / 20% of RAM."},
		{"throughput", "Throughput", "Buffer more (20% / 40%): fewer, larger writes; stalls can be longer when the buffer finally flushes."},
	}
	m := Meta{
		ID: "mem.dirty", Name: "Write-back buffering",
		Summary:  "Control how much unwritten data may collect in RAM before the kernel flushes it to disk.",
		Details:  "Writes vm.dirty_background_ratio and vm.dirty_ratio together. With lots of RAM and a slow disk, the defaults let gigabytes pile up and then freeze programs while they flush. Lower values spread the writing out.",
		Tradeoff: "More frequent writes use the disk more steadily and can lower peak copy speed; higher values risk more data loss on a power cut.",
		Evidence: "Copy a large file to a slow drive and watch whether the desktop stalls at the end; Doctor shows disk stall pressure.",
		Category: CatStorage, Tier: TierAdvanced, Risk: RiskLow, Scope: ScopeUntilBoot,
		NeedsAuth: true, ParamName: "mode", Options: opts,
	}
	return fileTweak{
		meta: m, what: "write-back buffering", presets: presets,
		gate: func(Env) string { return "" },
		targets: func(env Env) map[string]string {
			_, a := platform.ReadTrim(env.FS, bg)
			_, b := platform.ReadTrim(env.FS, hard)
			if a && b {
				return map[string]string{"background": bg, "hard": hard}
			}
			return nil
		},
		options:  func(Env, map[string]string) []Option { return opts },
		warnings: []string{resets},
	}
}

var pageClusterOptions = []Option{
	{"0", "0", "Read back only the page asked for: lowest swap-in latency (best for zram/fast swap)."},
	{"1", "1", "Read 2 pages."},
	{"3", "3", "The kernel default: read 8 pages (helps spinning disks)."},
}

// PageCluster sets swap read-ahead.
func PageCluster() Tweak {
	t := one(Meta{
		ID: "mem.page-cluster", Name: "Swap read-ahead",
		Summary:  "Reduce the delay when memory is swapped back in from fast swap such as zram.",
		Details:  "Writes vm.page-cluster, the number of pages (as a power of two) read per swap-in. The default of 8 pages suits spinning disks; for zram or an SSD swap, reading a single page avoids wasted decompression and shortens stalls.",
		Tradeoff: "On a hard-disk swap, reading a single page is slower. Irrelevant if you never swap.",
		Evidence: "Watch memory pressure (Doctor) while the machine is swapping; the gain only exists when it is.",
		Category: CatMemory, Tier: TierAdvanced, Risk: RiskLow, Scope: ScopeUntilBoot,
	}, "swap read-ahead", "page_cluster", "/proc/sys/vm/page-cluster", pageClusterOptions, resets)
	t.gate = func(env Env) string {
		if env.Report.Memory.SwapTotalKB == 0 {
			return "No swap or zram is configured, so swap read-ahead has no effect."
		}
		return ""
	}
	t.recommend = func(env Env, cur State) *Recommendation {
		if env.Report.Memory.ZramDevices > 0 && cur["page_cluster"] == "3" {
			return &Recommendation{Params: map[string]string{"value": "0"},
				Why: "Swap is on compressed RAM (zram) but swap-in still reads 8 pages at a time, which only makes sense for spinning disks."}
		}
		return nil
	}
	return t
}

var compactionOptions = []Option{
	{"0", "Off", "No background memory defragmentation: avoids periodic CPU work and stutter."},
	{"20", "20", "The kernel default."},
	{"50", "50", "More eager defragmentation, which helps large-page allocations."},
}

// CompactionProactiveness controls background memory defragmentation.
func CompactionProactiveness() Tweak {
	return one(Meta{
		ID: "mem.compaction", Name: "Background memory compaction",
		Summary:  "Stop the kernel from defragmenting memory in the background while you are working.",
		Details:  "Writes vm.compaction_proactiveness. Proactive compaction moves pages around to keep large free blocks available; on some desktops and games that background work shows up as occasional frame-time spikes. Setting it to 0 leaves compaction to when it is actually needed.",
		Tradeoff: "Large contiguous allocations (huge pages, some drivers) can occasionally take longer to satisfy.",
		Evidence: "Compare the 1% low and 0.1% low frame times in Benchmark Lab over several runs; the effect is small and only for spike-prone setups.",
		Category: CatMemory, Tier: TierAdvanced, Risk: RiskLow, Scope: ScopeUntilBoot,
	}, "memory compaction", "compaction_proactiveness", "/proc/sys/vm/compaction_proactiveness", compactionOptions, resets)
}

var maxMapOptions = []Option{
	{"65530", "65530", "The old kernel default."},
	{"262144", "262144", "A comfortable value for most games."},
	{"1048576", "1048576", "Commonly used by current distributions."},
	{"2147483642", "2147483642", "Effectively unlimited; the maximum."},
}

// MaxMapCount raises the per-process memory mapping limit.
func MaxMapCount() Tweak {
	return one(Meta{
		ID: "game.max-map-count", Name: "Memory mappings per process",
		Summary:  "Raise the limit on memory regions a single program may map (some games crash or stutter at the old limit).",
		Details:  "Writes vm.max_map_count. Large games, engines running under Wine/Proton and some memory-hungry tools create hundreds of thousands of mappings; at the old limit of 65530 they fail to allocate and crash or hitch. Most current distributions already raise it.",
		Tradeoff: "Essentially none: it is only a ceiling. Each mapping costs a little kernel memory when it is used.",
		Evidence: "Needed only if a game crashes with out-of-memory errors while RAM is free; there is no frame-rate gain when the limit was not being hit.",
		Category: CatGaming, Tier: TierSituational, Risk: RiskLow, Scope: ScopeUntilBoot,
	}, "memory mapping limit", "max_map_count", "/proc/sys/vm/max_map_count", maxMapOptions, resets)
}

var splitLockOptions = []Option{
	{"0", "Off", "Do not throttle programs that use split-lock instructions."},
	{"1", "On", "The kernel default: such programs are deliberately slowed (10 ms stalls)."},
}

// SplitLockMitigate stops the kernel from penalising split-lock instructions.
func SplitLockMitigate() Tweak {
	return one(Meta{
		ID: "game.split-lock", Name: "Split-lock penalty",
		Summary:  "Stop the kernel from slowing programs that use split-lock memory operations (affects some Windows games under Wine/Proton).",
		Details:  "Writes kernel.split_lock_mitigate (Linux 6.2+). A few games and anti-cheat or DRM layers use misaligned atomic instructions; the kernel penalises them with a ~10 ms stall per occurrence, which shows up as severe stutter. Turning the penalty off removes it.",
		Tradeoff: "A program using split locks can then stall other CPU cores' memory access (a local denial-of-service nuisance on shared servers, not a privilege issue). Fine on a personal desktop.",
		Evidence: "Look for 'x86/split lock detection' messages in `dmesg`; if there are none, this changes nothing.",
		Category: CatGaming, Tier: TierSituational, Risk: RiskLow, Scope: ScopeUntilBoot,
	}, "split-lock penalty", "split_lock_mitigate", "/proc/sys/kernel/split_lock_mitigate", splitLockOptions, resets)
}

var autogroupOptions = []Option{
	{"1", "On", "Group processes per terminal/session so a heavy build cannot starve interactive programs (kernel default)."},
	{"0", "Off", "Treat all processes individually."},
}

// SchedAutogroup toggles per-session scheduler grouping.
func SchedAutogroup() Tweak {
	return one(Meta{
		ID: "cpu.autogroup", Name: "Per-session CPU scheduling groups",
		Summary:  "Keep the desktop responsive while something heavy compiles or renders.",
		Details:  "Writes kernel.sched_autogroup_enabled. With autogroup on, each terminal/session gets a fair share of CPU, so `make -j64` in one window cannot make the rest of the desktop sluggish. It is already on by default on most kernels; this tweak mainly lets you check or restore it.",
		Tradeoff: "Turning it off can raise throughput for one big multi-process job running alongside others at the cost of interactivity.",
		Evidence: "Run a long parallel build and watch UI responsiveness and frame times of a game running alongside it.",
		Category: CatCPU, Tier: TierAdvanced, Risk: RiskLow, Scope: ScopeUntilBoot,
	}, "scheduler autogroup", "autogroup", "/proc/sys/kernel/sched_autogroup_enabled", autogroupOptions, resets)
}

var thpOptions = []Option{
	{"always", "always", "Use transparent huge pages for everything: best raw throughput for some workloads, but background compaction can cause latency spikes."},
	{"madvise", "madvise", "Only for programs that ask for them: the safe middle ground (Arch and Ubuntu default)."},
	{"never", "never", "Never use them: most predictable latency, slightly lower throughput in memory-heavy programs."},
}

// TransparentHugepages selects the transparent huge page policy.
func TransparentHugepages() Tweak {
	t := one(Meta{
		ID: "mem.thp", Name: "Transparent huge pages",
		Summary:  "Choose when the kernel backs memory with large pages.",
		Details:  "Writes /sys/kernel/mm/transparent_hugepage/enabled. Huge pages reduce address-translation overhead for memory-hungry programs (databases, JVMs, emulators, some games) but the kernel sometimes has to defragment memory to create them, which can cause latency spikes.",
		Tradeoff: "'always' can help throughput and hurt latency; 'never' is the reverse. Neither is universally better, which is why nothing here is recommended automatically.",
		Evidence: "Compare frame-time spikes (Benchmark Lab) or your real workload's runtime, several runs each, with `grep thp /proc/vmstat` for activity.",
		Category: CatMemory, Tier: TierAdvanced, Risk: RiskLow, Scope: ScopeUntilBoot,
	}, "transparent huge pages", "thp", "/sys/kernel/mm/transparent_hugepage/enabled", thpOptions, resets)
	t.fromFile = func(_, raw string) string { return privilege.ActiveChoice(raw) }
	return t
}

var readAheadOptions = []Option{
	{"128", "128 KiB", "The kernel default."},
	{"512", "512 KiB", "Prefetch more on sequential reads."},
	{"1024", "1 MiB", "Good for large sequential files on spinning disks and NAS-like workloads."},
	{"2048", "2 MiB", "Aggressive; wastes cache on random access."},
}

// ReadAhead sets how far ahead the kernel prefetches sequential reads.
func ReadAhead() Tweak {
	t := IOScheduler().(fileTweak)
	t.what = "disk read-ahead"
	t.meta = Meta{
		ID: "io.readahead", Name: "Disk read-ahead",
		Summary:  "Prefetch more data when you read large files sequentially (game loading, video, backups).",
		Details:  "Writes /sys/block/<disk>/queue/read_ahead_kb for each whole disk. Larger values let the kernel read bigger chunks ahead of a sequential read, which raises throughput on hard disks and some network-attached storage.",
		Tradeoff: "On SSDs and for random access it mostly wastes page cache and bandwidth. Do not raise it on a machine short of RAM.",
		Evidence: "Time a large sequential read of a file that is not already cached (for example a freshly rebooted machine loading a big game) before and after.",
		Category: CatStorage, Tier: TierAdvanced, Risk: RiskLow, Scope: ScopeUntilBoot,
		NeedsAuth: true, ParamName: "kib", Options: readAheadOptions,
	}
	base := IOScheduler().(fileTweak).targets
	t.targets = func(env Env) map[string]string {
		out := map[string]string{}
		for n := range base(env) {
			p := "/sys/block/" + n + "/queue/read_ahead_kb"
			if _, ok := platform.ReadTrim(env.FS, p); ok {
				out[n] = p
			}
		}
		return out
	}
	t.options = func(Env, map[string]string) []Option { return readAheadOptions }
	t.fromFile = nil
	return t
}

var usbOptions = []Option{
	{"-1", "Never suspend", "USB devices (mice, keyboards, headsets) never power down: no wake-up delay."},
	{"2", "After 2 s", "The kernel default: idle devices suspend after 2 seconds."},
	{"10", "After 10 s", "Suspend only after 10 idle seconds."},
}

// USBAutosuspend sets the idle time before USB devices power down.
func USBAutosuspend() Tweak {
	return one(Meta{
		ID: "usb.autosuspend", Name: "USB power saving",
		Summary:  "Stop mice, keyboards and headsets from going to sleep between uses, removing the wake-up delay.",
		Details:  "Writes the usbcore autosuspend delay (seconds; -1 disables). Some USB devices need a moment to wake up after being idle, which can feel like a dropped first click or a stuttering headset. Applies to devices connected afterwards.",
		Tradeoff: "Devices stay powered, so a laptop on battery uses a little more energy.",
		Evidence: "Only useful if you notice a first-input delay after idling; there is no frame-rate effect.",
		Category: CatCPU, Tier: TierSituational, Risk: RiskLow, Scope: ScopeUntilBoot,
	}, "USB autosuspend", "autosuspend", "/sys/module/usbcore/parameters/autosuspend", usbOptions, "Applies to devices plugged in afterwards; resets at reboot.")
}

var aspmOptions = []Option{
	{"default", "default", "Use the firmware's PCIe power saving setup."},
	{"performance", "performance", "Disable PCIe link power saving: lowest latency for NVMe drives, GPUs and NICs."},
	{"powersave", "powersave", "Aggressive link power saving."},
	{"powersupersave", "powersupersave", "Maximum savings; may add latency."},
}

// PCIeASPM sets the PCI Express link power-management policy.
func PCIeASPM() Tweak {
	t := one(Meta{
		ID: "pcie.aspm", Name: "PCIe link power saving (ASPM)",
		Summary:  "Keep PCI Express links fully awake to avoid the wake-up latency of power-saving link states.",
		Details:  "Writes the pcie_aspm policy. Link power states save energy but each wake-up adds microseconds to milliseconds of latency; 'performance' keeps links active. Takes effect only where the firmware lets the OS control ASPM.",
		Tradeoff: "Higher idle power and heat, especially on laptops with an NVMe drive. The firmware can refuse changes (the kernel then reports the old value and TurboShift shows that).",
		Evidence: "Look for latency spikes after idle in storage or GPU workloads; compare with `lspci -vv` link states.",
		Category: CatCPU, Tier: TierAdvanced, Risk: RiskLow, Scope: ScopeUntilBoot,
	}, "PCIe ASPM policy", "aspm", "/sys/module/pcie_aspm/parameters/policy", aspmOptions, resets)
	t.fromFile = func(_, raw string) string { return privilege.ActiveChoice(raw) }
	return t
}

var amdDPMOptions = []Option{
	{"auto", "auto", "The driver adjusts clocks to load (default)."},
	{"high", "high", "Hold the GPU at its highest clocks: removes ramp-up lag, uses much more power and heat."},
	{"low", "low", "Hold the GPU at its lowest clocks: quiet and cool, slow."},
}

// AMDGPUPerformanceLevel pins AMD GPU clocks.
func AMDGPUPerformanceLevel() Tweak {
	m := Meta{
		ID: "gpu.amd-level", Name: "AMD GPU clock level",
		Summary:  "Hold an AMD graphics card at high clocks to avoid ramp-up lag in games.",
		Details:  "Writes power_dpm_force_performance_level for each amdgpu card. 'auto' lets the driver scale clocks with load, which can lag at the start of heavy frames; 'high' pins the top state.",
		Tradeoff: "Much higher idle power, heat and fan noise while it is set; it does not raise the card's maximum, only removes the wait to reach it. Revert it when you stop gaming.",
		Evidence: "Compare 1% low frame times in Benchmark Lab; a GPU already at full load in your game will not change.",
		Category: CatGaming, Tier: TierSituational, Risk: RiskMedium, Scope: ScopeUntilBoot,
		NeedsAuth: true, ParamName: "level", Options: amdDPMOptions,
	}
	files := func(env Env) map[string]string {
		out := map[string]string{}
		for _, g := range env.Report.GPUs {
			if g.Vendor != "amd" || g.Driver != "amdgpu" {
				continue
			}
			ents, err := env.FS.ReadDir("/sys/class/drm")
			if err != nil {
				continue
			}
			for _, e := range ents {
				n := e.Name()
				if len(n) < 5 || n[:4] != "card" || strings.Contains(n, "-") {
					continue
				}
				p := fmt.Sprintf("/sys/class/drm/%s/device/power_dpm_force_performance_level", n)
				if _, ok := platform.ReadTrim(env.FS, p); ok {
					out[n] = p
				}
			}
			break
		}
		return out
	}
	return fileTweak{
		meta: m, what: "AMD GPU clock level",
		gate:     func(Env) string { return "" },
		targets:  files,
		options:  func(Env, map[string]string) []Option { return amdDPMOptions },
		warnings: []string{"Revert after gaming: high clocks keep the card hot and loud. Resets at reboot."},
	}
}
