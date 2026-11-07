package tweaks

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/mttcn83/turboshift/internal/platform"
	"github.com/mttcn83/turboshift/internal/privilege"
)

// The second wave of kernel tunables: latency, input, memory behaviour, disk
// queues and GPU power. All of them are plain sysfs/procfs attributes written
// through the same allowlisted, read-back-verified path as everything else.

const (
	kb   = "/proc/sys/vm/"
	netp = "/proc/sys/net/"
)

// ---- network ---------------------------------------------------------------

var notsentOptions = []Option{
	{"131072", "128 KiB", "Moderate cap on unsent data per connection: less lag in interactive streams."},
	{"16384", "16 KiB", "Tight cap: lowest queueing delay for chatty or real-time TCP traffic; may lower bulk throughput."},
	{"4294967295", "No limit", "The kernel default."},
}

// TCPNotsentLowat limits how much unsent data a TCP connection may queue.
func TCPNotsentLowat() Tweak {
	return one(Meta{
		ID: "net.notsent-lowat", Name: "Less lag on busy TCP connections",
		Summary:  "Stop programs from stuffing the send queue, so interactive traffic (remote desktop, SSH, streaming) reacts sooner.",
		Details:  "Sets net.ipv4.tcp_notsent_lowat. Normally a program can queue as much data as the socket buffer holds, and anything new waits behind it. With a cap, the kernel tells the program to hold back, so fresh data is sent sooner.",
		Helps:    "Remote desktop, SSH, game streaming, video calls over TCP, anything where a late frame is worse than a lower peak speed.",
		Tradeoff: "Very low values can reduce top speed of bulk uploads. It only affects programs that use the select/poll/epoll wake-up pattern.",
		Evidence: "Run Network Doctor's load test and compare latency under load before and after, over several runs.",
		Category: CatNetwork, Tier: TierAdvanced, Risk: RiskLow, Scope: ScopeUntilBoot,
	}, "TCP unsent-data cap", "tcp_notsent_lowat", netp+"ipv4/tcp_notsent_lowat", notsentOptions, resets)
}

// TCPBuffers raises the ceiling for socket buffers.
func TCPBuffers() Tweak {
	const rm, wm = netp + "core/rmem_max", netp + "core/wmem_max"
	const trm, twm = netp + "ipv4/tcp_rmem", netp + "ipv4/tcp_wmem"
	// A normal program's TCP buffer is sized automatically up to the third
	// number of tcp_rmem / tcp_wmem; core/rmem_max only caps programs that ask
	// for a size themselves. Raising only the latter changes nothing for most
	// downloads, so the large option raises all four.
	presets := map[string]map[string]string{
		"default": {"rmem": "212992", "wmem": "212992", "tcp_rmem": "4096 131072 6291456", "tcp_wmem": "4096 16384 4194304"},
		"fast":    {"rmem": "16777216", "wmem": "16777216", "tcp_rmem": "4096 131072 16777216", "tcp_wmem": "4096 16384 16777216"},
	}
	opts := []Option{
		{"default", "Usual limits", "Up to about 6 MiB per connection when receiving and 4 MiB when sending. Fine for most home connections."},
		{"fast", "Large (16 MiB)", "Up to 16 MiB per connection, so one download can fill a fast or far-away link."},
	}
	m := Meta{
		ID: "net.buffers", Name: "Faster downloads on fast or far links",
		Summary:  "Allow bigger network buffers so a single download can fill a gigabit or long-distance connection.",
		Details:  "Raises the ceilings of the automatic TCP receive and send buffers (net.ipv4.tcp_rmem and tcp_wmem) and net.core.rmem_max / wmem_max to 16 MiB. TCP can only keep as much data in flight as its buffer allows (speed is at most buffer / round-trip time), so on a fast link to a distant server a small ceiling caps the speed of one connection: about 6 MiB limits a 100 ms path to roughly 500 Mbit/s.",
		Helps:    "Gigabit and faster connections, VPNs, downloads from distant servers, large game or ISO downloads.",
		Tradeoff: "Each connection can now hold up to 16 MiB, so many large transfers at once can use a lot of memory (the buffers only grow as far as the link needs). Nothing changes on a slow link, a nearby server or game traffic, and it can add delay on a congested path if the buffers fill.",
		Evidence: "Compare the throughput of one big download (or Network Doctor's load test) before and after, several runs.",
		Category: CatNetwork, Tier: TierAdvanced, Risk: RiskLow, Scope: ScopeUntilBoot,
		NeedsAuth: true, ParamName: "size", Options: opts,
	}
	return fileTweak{
		meta: m, what: "network buffer ceiling", presets: presets,
		gate: func(Env) string { return "" },
		targets: func(env Env) map[string]string {
			_, a := platform.ReadTrim(env.FS, rm)
			_, b := platform.ReadTrim(env.FS, wm)
			_, c := platform.ReadTrim(env.FS, trm)
			_, d := platform.ReadTrim(env.FS, twm)
			if a && b && c && d {
				return map[string]string{"rmem": rm, "wmem": wm, "tcp_rmem": trm, "tcp_wmem": twm}
			}
			return nil
		},
		options:  func(Env, map[string]string) []Option { return opts },
		fromFile: func(_, raw string) string { return privilege.SpaceSeparated(raw) },
		warnings: []string{resets},
		recommend: func(env Env, cur State) *Recommendation {
			// Evidence: a wired link fast enough that the usual ceiling is the limit.
			f := strings.Fields(cur["tcp_rmem"])
			if len(f) != 3 {
				return nil
			}
			ceil, err := strconv.ParseUint(f[2], 10, 64)
			if err != nil || ceil >= 8<<20 {
				return nil
			}
			for _, i := range env.Report.Network.Interfaces {
				if i.Name == env.Report.Network.DefaultIface && i.Kind == "ethernet" && i.SpeedMbps != nil && *i.SpeedMbps >= 2500 {
					return &Recommendation{Params: map[string]string{"size": "fast"},
						Why: fmt.Sprintf("Your main connection (%s) runs at %d Mbit/s, but one download can use at most %d MiB of receive buffer, which limits a 100 ms path to roughly %d Mbit/s.", i.Name, *i.SpeedMbps, ceil>>20, ceil*8*10/1_000_000)}
				}
			}
			return nil
		},
	}
}

var backlogOptions = []Option{
	{"1000", "1000", "The kernel default."},
	{"5000", "5000", "Absorb larger bursts of incoming packets before dropping any."},
	{"10000", "10000", "For 10 Gb/s links and heavy traffic."},
}

// NetdevBacklog sets the per-CPU receive queue length.
func NetdevBacklog() Tweak {
	return one(Meta{
		ID: "net.backlog", Name: "Stop dropping packets in bursts",
		Summary:  "Make the incoming-packet queue longer so short bursts are not thrown away.",
		Details:  "Sets net.core.netdev_max_backlog. When packets arrive faster than the CPU can process them, they wait in this queue; once it is full they are dropped and have to be re-sent.",
		Helps:    "Fast links (2.5/10 Gb/s), busy servers, and machines where `/proc/net/softnet_stat` shows dropped packets (second column).",
		Tradeoff: "A longer queue can add a little delay under overload. No benefit on an ordinary home connection.",
		Evidence: "Only useful if the dropped-packets counter in /proc/net/softnet_stat is growing under load.",
		Category: CatNetwork, Tier: TierAdvanced, Risk: RiskLow, Scope: ScopeUntilBoot,
	}, "receive queue length", "backlog", netp+"core/netdev_max_backlog", backlogOptions, resets)
}

var mtuOptions = []Option{
	{"0", "Off", "The kernel default: trust the path MTU the network reports."},
	{"1", "When needed", "Probe for a working packet size only after a black-holed connection is detected."},
	{"2", "Always", "Always probe for the best packet size."},
}

// TCPMTUProbing lets TCP recover from broken path-MTU discovery.
func TCPMTUProbing() Tweak {
	return one(Meta{
		ID: "net.mtu-probing", Name: "Recover from stuck connections on odd networks",
		Summary:  "Let TCP find a working packet size by itself when a VPN, tunnel or hotel network silently drops big packets.",
		Details:  "Sets net.ipv4.tcp_mtu_probing. Some networks drop large packets without telling anyone, so a connection works for small requests (the page starts loading) and then hangs. With probing on, TCP notices and shrinks its packets.",
		Helps:    "VPN, PPPoE, mobile tethering and public Wi-Fi users who see sites that half-load or SSH sessions that hang on large output.",
		Tradeoff: "Small. On a healthy network it normally never triggers. If it misjudges a limit it can shrink packets and lower speed; 'Always' sends a few extra test packets.",
		Evidence: "Only matters if you have such hangs. If everything already works there is nothing to measure.",
		Category: CatNetwork, Tier: TierSituational, Risk: RiskLow, Scope: ScopeUntilBoot,
	}, "TCP MTU probing", "tcp_mtu_probing", netp+"ipv4/tcp_mtu_probing", mtuOptions, resets)
}

var ecnOptions = []Option{
	{"0", "Off", "Never use ECN."},
	{"1", "Request and accept", "Ask for congestion marks on all outgoing connections."},
	{"2", "Only when the server asks", "The kernel default."},
}

// TCPECN toggles explicit congestion notification.
func TCPECN() Tweak {
	return one(Meta{
		ID: "net.ecn", Name: "Smoother traffic with modern routers (ECN)",
		Summary:  "Let routers tell your connection to slow down by marking packets instead of dropping them.",
		Details:  "Sets net.ipv4.tcp_ecn. With ECN, a congested router sets a flag on packets instead of discarding them, so TCP backs off without having to re-send anything. It works best together with fq_codel or cake.",
		Helps:    "Bufferbloat-fighting setups; busy home networks that use a modern router.",
		Tradeoff: "A few old routers and firewalls mishandle ECN packets. Connections to some sites can then be slow to start or fail until the system retries without it. If a site will not load after enabling this, turn it off.",
		Evidence: "Network Doctor's load test: compare latency under load and packet retransmissions with ECN on and off.",
		Category: CatNetwork, Tier: TierAdvanced, Risk: RiskLow, Scope: ScopeUntilBoot,
	}, "ECN", "tcp_ecn", netp+"ipv4/tcp_ecn", ecnOptions, resets)
}

// ---- CPU ---------------------------------------------------------------------

const pstateDir = "/sys/devices/system/cpu/intel_pstate/"

var minPerfOptions = []Option{
	{"50", "50 %", "The CPU never idles below half of its range: quicker response, more idle power."},
	{"75", "75 %", "High floor; noticeably warmer and hungrier when idle."},
	{"100", "100 %", "Run at full speed all the time (like the performance governor)."},
}

// CPUMinPerf raises the Intel P-state floor.
func CPUMinPerf() Tweak {
	t := one(Meta{
		ID: "cpu.min-perf", Name: "Keep the CPU from idling low",
		Summary:  "Raise the lowest speed the CPU is allowed to drop to, so it reacts sooner instead of ramping up.",
		Details:  "Writes intel_pstate/min_perf_pct. By default an idle CPU parks at a tiny fraction of its speed and has to ramp up when work arrives. A higher floor removes that ramp-up delay.",
		Helps:    "Latency-sensitive desktop and gaming use on a desktop PC or a plugged-in laptop with the Intel P-state driver.",
		Tradeoff: "Higher idle power, heat and fan noise. The effect is small on modern CPUs, which already ramp in milliseconds.",
		Evidence: "Compare input-to-frame latency or 1% low frame times in Benchmark Lab; revert if the difference is not clearly visible.",
		Category: CatCPU, Tier: TierSituational, Risk: RiskLow, Scope: ScopeUntilBoot,
	}, "Intel P-state floor", "min_perf_pct", pstateDir+"min_perf_pct", minPerfOptions, "Resets at reboot or when a power manager starts.")
	t.gate = func(env Env) string { return powerOwner(env, "the CPU speed floor") }
	return t
}

var hwpBoostOptions = []Option{
	{"1", "On", "Jump to a high speed as soon as a thread wakes up after waiting on I/O."},
	{"0", "Off", "The kernel default."},
}

// CPUHWPBoost toggles dynamic HWP boost.
func CPUHWPBoost() Tweak {
	t := one(Meta{
		ID: "cpu.hwp-boost", Name: "Faster CPU boost after waiting",
		Summary:  "Make the CPU jump to full speed the moment a program resumes after waiting on disk or network.",
		Details:  "Writes intel_pstate/hwp_dynamic_boost. When a thread that was blocked on I/O wakes up, the driver temporarily raises the speed limit instead of waiting for the CPU to notice the load.",
		Helps:    "Responsiveness of builds, game loading and desktop use on Intel CPUs with HWP.",
		Tradeoff: "Slightly higher power use during short bursts; no effect when the CPU is already busy.",
		Evidence: "Time a short, I/O-heavy task such as launching a large application, several runs each.",
		Category: CatCPU, Tier: TierSituational, Risk: RiskLow, Scope: ScopeUntilBoot,
	}, "HWP dynamic boost", "hwp_dynamic_boost", pstateDir+"hwp_dynamic_boost", hwpBoostOptions, resets)
	t.gate = func(env Env) string { return powerOwner(env, "HWP boost") }
	return t
}

var watchdogOptions = []Option{
	{"0", "Off", "No periodic lock-up check: fewer CPU wake-ups, less jitter, slightly less power."},
	{"1", "On", "The kernel default on many systems."},
}

// NMIWatchdog toggles the hard-lockup detector.
func NMIWatchdog() Tweak {
	return one(Meta{
		ID: "sys.watchdog", Name: "Turn off the CPU lock-up detector",
		Summary:  "Stop the kernel from waking every CPU once every few seconds just to check that it has not frozen.",
		Details:  "Sets kernel.nmi_watchdog to 0. The watchdog fires a non-maskable interrupt on each core at a regular interval. That is useful for kernel debugging but costs a tiny amount of power and adds rare, very small latency blips.",
		Helps:    "Laptops (longer idle sleep states) and latency-sensitive audio or gaming setups.",
		Tradeoff: "The system can no longer report a hard processor lock-up in its logs; the machine would simply stay frozen with no clue why. You lose a diagnostic you may want when reporting a freeze.",
		Evidence: "Powertop's wake-ups per second drop slightly; frame-time effect is too small to measure on most systems.",
		Category: CatCPU, Tier: TierSituational, Risk: RiskLow, Scope: ScopeUntilBoot,
	}, "NMI watchdog", "nmi_watchdog", "/proc/sys/kernel/nmi_watchdog", watchdogOptions, resets)
}

// ---- input -------------------------------------------------------------------

var pollOptions = []Option{
	{"0", "Device default", "Let each device choose its own report rate."},
	{"1", "1000 Hz (1 ms)", "Fastest USB rate: lowest input delay."},
	{"2", "500 Hz (2 ms)", "Half the rate; useful when a device misbehaves at 1000 Hz."},
	{"4", "250 Hz (4 ms)", "Quarter rate."},
	{"8", "125 Hz (8 ms)", "The USB full-speed default."},
}

func pollTweak(id, name, what, param, device string) Tweak {
	return one(Meta{
		ID: id, Name: name,
		Summary:  "Ask USB " + device + " to report as often as possible, so your input reaches the game sooner.",
		Details:  "Sets the usbhid " + param + " module parameter (poll interval in milliseconds). A lower interval means more frequent reports and less delay before the system sees your input. Most gaming mice already poll at 1000 Hz on their own; this sets the same limit for devices that default to 125 Hz.",
		Helps:    "Gaming mice, keyboards and controllers plugged in over USB (not Bluetooth, not 2.4 GHz dongles that manage their own rate).",
		Tradeoff: "Applies only to devices connected after the change (unplug and replug). A few cheap devices glitch above 125 Hz; set it back if that happens.",
		Evidence: "Use a mouse polling-rate test (e.g. `evhz`) to see the rate your device actually delivers before and after replugging.",
		Category: CatInput, Tier: TierSituational, Risk: RiskLow, Scope: ScopeUntilBoot,
	}, what, "poll", "/sys/module/usbhid/parameters/"+param, pollOptions, "Takes effect for devices you plug in afterwards; resets at reboot.")
}

func MousePoll() Tweak {
	return pollTweak("input.mouse-poll", "Faster USB mouse reports", "mouse polling", "mousepoll", "mice")
}
func KeyboardPoll() Tweak {
	return pollTweak("input.kb-poll", "Faster USB keyboard reports", "keyboard polling", "kbpoll", "keyboards")
}
func GamepadPoll() Tweak {
	return pollTweak("input.pad-poll", "Faster USB controller reports", "controller polling", "jspoll", "controllers and joysticks")
}

// ---- memory ------------------------------------------------------------------

// zswapPool reports why zswap cannot be offered (zram swap, or no swap at all).
func zswapGate(env Env) string {
	if env.Report.Memory.SwapTotalKB == 0 {
		return "No swap is configured; zswap compresses pages on their way to a swap device."
	}
	if env.Report.Memory.ZramDevices > 0 {
		return "Swap is on zram. Compressing twice (zswap on top of zram) only wastes CPU."
	}
	return ""
}

var zswapOptions = []Option{
	{"1", "On", "Compress pages in RAM before they go to disk swap: far fewer slow disk writes when memory is full."},
	{"0", "Off", "Send pages straight to the swap device."},
}

// Zswap enables the compressed swap cache.
func Zswap() Tweak {
	t := one(Meta{
		ID: "mem.zswap", Name: "Compress memory before swapping to disk",
		Summary:  "Keep swapped-out pages compressed in RAM so a nearly full machine stalls far less.",
		Details:  "Sets zswap.enabled. Instead of writing pages to a slow disk swap, the kernel compresses them into a pool in RAM and only writes the overflow. Reading a page back from that pool is orders of magnitude faster than from disk.",
		Helps:    "Machines with disk swap that sometimes run out of RAM (browsers, VMs, builds, games with big assets).",
		Tradeoff: "Uses some RAM and CPU to compress. Not useful if you never swap, or if swap already lives in zram.",
		Evidence: "Watch memory pressure in Doctor while the machine is swapping; compare how long the desktop freezes under load.",
		Category: CatMemory, Tier: TierAdvanced, Risk: RiskLow, Scope: ScopeUntilBoot,
	}, "zswap", "zswap", "/sys/module/zswap/parameters/enabled", zswapOptions, resets)
	t.gate = zswapGate
	t.fromFile = func(_, raw string) string { return privilege.BoolChoice(raw) }
	return t
}

var zswapCompressors = []Option{
	{"lz4", "lz4", "Very fast, moderate compression: the best match for latency."},
	{"zstd", "zstd", "Better compression, more CPU: the best match for fitting more in RAM."},
	{"lzo", "lzo", "Fast and light; a bit weaker than lz4."},
	{"lzo-rle", "lzo-rle", "Like lzo, tuned for memory pages that are mostly zeroes."},
}

// cryptoNames lists algorithm names the kernel has registered.
func cryptoNames(env Env) []string {
	b, err := env.FS.ReadFile("/proc/crypto")
	if err != nil {
		return nil
	}
	var out []string
	for _, l := range strings.Split(string(b), "\n") {
		if k, v, ok := strings.Cut(l, ":"); ok && strings.TrimSpace(k) == "name" {
			out = append(out, strings.TrimSpace(v))
		}
	}
	return out
}

// ZswapCompressor picks the zswap compression algorithm.
func ZswapCompressor() Tweak {
	t := one(Meta{
		ID: "mem.zswap-compressor", Name: "Pick the memory compression speed",
		Summary:  "Trade compression strength against CPU time for zswap.",
		Details:  "Sets zswap.compressor. lz4 compresses and decompresses fastest, so swapped pages come back with the least delay. zstd squeezes more pages into the same RAM but costs more CPU. Only algorithms your kernel has already loaded are offered.",
		Helps:    "Anyone who uses zswap and wants to tune it for responsiveness (lz4) or capacity (zstd).",
		Tradeoff: "Pages already compressed stay in their old format until they are swapped back in. Changing the algorithm while heavily swapping can cause a brief hiccup.",
		Evidence: "Compare memory pressure and desktop freezes when the machine is deliberately pushed into swap.",
		Category: CatMemory, Tier: TierAdvanced, Risk: RiskLow, Scope: ScopeUntilBoot,
	}, "zswap compressor", "compressor", "/sys/module/zswap/parameters/compressor", zswapCompressors, resets)
	t.gate = func(env Env) string {
		if v, ok := platform.ReadTrim(env.FS, "/sys/module/zswap/parameters/enabled"); !ok || privilege.BoolChoice(v) != "1" {
			return "zswap is switched off; enable it first (mem.zswap)."
		}
		return ""
	}
	t.options = func(env Env, _ map[string]string) []Option { return pick(zswapCompressors, cryptoNames(env)) }
	return t
}

var thpDefragOptions = []Option{
	{"madvise", "madvise", "Only stall to make huge pages for programs that ask (kernel default)."},
	{"defer+madvise", "defer+madvise", "Never stall: let a background thread make huge pages; stall only for programs that ask."},
	{"defer", "defer", "Never stall; always compact in the background."},
	{"never", "never", "Never compact for huge pages."},
	{"always", "always", "Stall any program until a huge page is ready (worst for latency)."},
}

// THPDefrag controls stalling for huge pages.
func THPDefrag() Tweak {
	t := one(Meta{
		ID: "mem.thp-defrag", Name: "Fewer stalls when memory gets large pages",
		Summary:  "Stop programs from freezing while the kernel tidies memory to make large pages.",
		Details:  "Writes the transparent_hugepage/defrag policy. With some settings, a program that asks for memory can be paused while the kernel defragments RAM. 'defer+madvise' moves that work to a background thread so allocations never wait.",
		Helps:    "Games, emulators and VMs that show random stutter when RAM has been in use for a long time.",
		Tradeoff: "Slightly fewer huge pages get created, so some memory-heavy programs run a tiny bit slower.",
		Evidence: "Check `grep compact_stall /proc/vmstat` over a long session; compare 0.1% low frame times.",
		Category: CatMemory, Tier: TierAdvanced, Risk: RiskLow, Scope: ScopeUntilBoot,
	}, "huge-page defrag policy", "defrag", "/sys/kernel/mm/transparent_hugepage/defrag", thpDefragOptions, resets)
	t.fromFile = func(_, raw string) string { return privilege.ActiveChoice(raw) }
	return t
}

var minFreeOptions = []Option{
	{"65536", "64 MiB", "A modest reserve that avoids most direct-reclaim stalls."},
	{"131072", "128 MiB", "A larger reserve for gaming or big-asset workloads."},
	{"262144", "256 MiB", "Large reserve; wastes RAM on small machines."},
}

// MinFree sets the emergency free-memory reserve.
func MinFree() Tweak {
	return one(Meta{
		ID: "mem.min-free", Name: "Keep some RAM always free",
		Summary:  "Hold back a bigger reserve of free memory so programs rarely have to wait for the kernel to clear space.",
		Details:  "Sets vm.min_free_kbytes. When free memory falls to this level the kernel starts reclaiming immediately. A larger reserve makes sure there is room for sudden allocations (loading a level, starting a game) without a visible pause.",
		Helps:    "Machines with 16 GB or more that stutter when RAM is nearly full of cache, and some 10 GbE setups.",
		Tradeoff: "That reserve is not usable by programs. Too large for your RAM and the system hits memory pressure and out-of-memory kills sooner, so a wrong value makes things slower rather than faster. The larger options are meant for 16 GB or more.",
		Evidence: "Doctor's memory pressure and `grep allocstall /proc/vmstat` growing under load show direct reclaim is happening.",
		Category: CatMemory, Tier: TierAdvanced, Risk: RiskMedium, Scope: ScopeUntilBoot,
	}, "free-memory reserve", "min_free_kbytes", kb+"min_free_kbytes", minFreeOptions, resets)
}

var watermarkOptions = []Option{
	{"10", "10", "The kernel default."},
	{"50", "50", "Start background reclaim earlier, so programs hit the shortage less often."},
	{"125", "125", "Reclaim very early; best for bursty allocation on big-memory machines."},
}

// WatermarkScale starts background reclaim earlier.
func WatermarkScale() Tweak {
	return one(Meta{
		ID: "mem.watermark-scale", Name: "Start freeing memory earlier",
		Summary:  "Have the kernel begin clearing memory in the background sooner, so programs rarely wait for it.",
		Details:  "Sets vm.watermark_scale_factor. The kernel's reclaim thread (kswapd) wakes when free memory reaches a watermark; a larger factor raises the watermark so reclaim runs earlier and in the background instead of blocking your program.",
		Helps:    "Systems where allocation stalls show up in `allocstall` counters or as brief freezes when launching big applications.",
		Tradeoff: "Slightly more background work and a little less file cache is kept.",
		Evidence: "Compare `grep -E 'allocstall|pgscan_direct' /proc/vmstat` before and after a workload.",
		Category: CatMemory, Tier: TierAdvanced, Risk: RiskLow, Scope: ScopeUntilBoot,
	}, "reclaim watermark", "watermark_scale_factor", kb+"watermark_scale_factor", watermarkOptions, resets)
}

var ksmOptions = []Option{
	{"0", "Off", "Stop scanning memory for duplicate pages: frees a little CPU and avoids latency spikes."},
	{"1", "On", "Merge identical pages to save RAM (mostly useful for many similar VMs)."},
}

// KSM stops or starts same-page merging.
func KSM() Tweak {
	t := one(Meta{
		ID: "mem.ksm", Name: "Stop scanning memory for duplicates",
		Summary:  "Turn off a background thread that hunts for identical pages, which is wasted work outside of VM hosts.",
		Details:  "Writes /sys/kernel/mm/ksm/run. Kernel same-page merging scans memory for duplicates to save RAM; the scanning costs CPU and can add small stalls. Already-merged pages stay merged.",
		Helps:    "Desktops and gaming PCs where nothing registers memory for merging (the scanner would find nothing).",
		Tradeoff: "If you run many identical VMs, you lose their memory savings.",
		Evidence: "Check `ksmd` CPU use in `top` and `/sys/kernel/mm/ksm/pages_sharing`; if both are zero this changes nothing.",
		Category: CatMemory, Tier: TierSituational, Risk: RiskLow, Scope: ScopeUntilBoot,
	}, "KSM", "ksm", "/sys/kernel/mm/ksm/run", ksmOptions, resets)
	t.gate = func(env Env) string {
		if v, ok := platform.ReadTrim(env.FS, "/sys/kernel/mm/ksm/run"); ok && v != "0" && v != "1" {
			return "KSM is in a special state (" + v + "); leaving it alone."
		}
		return ""
	}
	return t
}

// WritebackTiming shapes how often dirty data is flushed.
func WritebackTiming() Tweak {
	const wb, ex = kb + "dirty_writeback_centisecs", kb + "dirty_expire_centisecs"
	presets := map[string]map[string]string{
		"smooth":  {"interval": "100", "age": "1500"},
		"default": {"interval": "500", "age": "3000"},
		"relaxed": {"interval": "1500", "age": "6000"},
	}
	opts := []Option{
		{"smooth", "Smooth", "Flush every 1 s, data older than 15 s: small steady writes, shorter risk window after a crash."},
		{"default", "Kernel default", "Flush every 5 s, data older than 30 s."},
		{"relaxed", "Relaxed", "Flush every 15 s, data older than 60 s: fewer disk wake-ups on a laptop."},
	}
	m := Meta{
		ID: "mem.writeback-timing", Name: "Smoother disk writes",
		Summary:  "Control how often unsaved data is pushed to disk, to avoid long pauses or save laptop battery.",
		Details:  "Sets vm.dirty_writeback_centisecs and vm.dirty_expire_centisecs. The kernel wakes up on the interval and writes data that has been waiting longer than the age. Frequent small writes avoid big flush storms; infrequent ones let the disk sleep longer.",
		Helps:    "Smooth: desktops that stall on big copies. Relaxed: battery-powered machines with SSDs.",
		Tradeoff: "Relaxed lets data wait up to 60 s before it is written, so a power cut, flat battery or crash can lose the last minute of saves and downloads (or leave a file cut short). Smooth wakes the disk more often and uses a little more power.",
		Evidence: "Smooth: watch disk stall pressure in Doctor during a large copy. Relaxed: compare idle wake-ups in powertop.",
		Category: CatStorage, Tier: TierAdvanced, Risk: RiskMedium, Scope: ScopeUntilBoot,
		NeedsAuth: true, ParamName: "mode", Options: opts,
	}
	return fileTweak{
		meta: m, what: "write-back timing", presets: presets,
		gate: func(Env) string { return "" },
		targets: func(env Env) map[string]string {
			_, a := platform.ReadTrim(env.FS, wb)
			_, b := platform.ReadTrim(env.FS, ex)
			if a && b {
				return map[string]string{"interval": wb, "age": ex}
			}
			return nil
		},
		options:  func(Env, map[string]string) []Option { return opts },
		warnings: []string{resets},
	}
}

var statIntervalOptions = []Option{
	{"1", "1 s", "The kernel default."},
	{"5", "5 s", "Refresh memory statistics less often."},
	{"10", "10 s", "Fewest wake-ups; statistics lag a little."},
}

// StatInterval slows the per-CPU vm statistics worker.
func StatInterval() Tweak {
	return one(Meta{
		ID: "mem.stat-interval", Name: "Fewer background interruptions",
		Summary:  "Make the kernel update its memory statistics less often, so it interrupts your CPU cores less.",
		Details:  "Sets vm.stat_interval. A small worker runs on every core once per interval to fold counters together. Lengthening it removes about nine in ten of those timer interruptions.",
		Helps:    "Latency-sensitive workloads (audio, real-time, competitive gaming) and laptops that want deeper idle sleep.",
		Tradeoff: "Memory statistics in /proc/vmstat update more slowly; nothing else changes.",
		Evidence: "The effect is a handful of microseconds per second; verify with a tool such as `cyclictest` if you need proof.",
		Category: CatCPU, Tier: TierSituational, Risk: RiskLow, Scope: ScopeUntilBoot,
	}, "memory statistics interval", "stat_interval", kb+"stat_interval", statIntervalOptions, resets)
}

// ---- storage -----------------------------------------------------------------

// diskOne is a per-disk queue attribute shared by every whole disk.
func diskOne(m Meta, what, file string, opts []Option, warn string) fileTweak {
	t := IOScheduler().(fileTweak)
	base := t.targets
	m.NeedsAuth = true
	m.Options = opts
	if m.ParamName == "" {
		m.ParamName = "value"
	}
	t.meta, t.what = m, what
	t.targets = func(env Env) map[string]string {
		out := map[string]string{}
		for n := range base(env) {
			p := "/sys/block/" + n + "/queue/" + file
			if _, ok := platform.ReadTrim(env.FS, p); ok {
				out[n] = p
			}
		}
		return out
	}
	t.options = func(Env, map[string]string) []Option { return opts }
	t.fromFile = nil
	t.warnings = []string{warn}
	return t
}

var rqOptions = []Option{
	{"0", "Any CPU", "Finish I/O on whichever CPU received the interrupt."},
	{"1", "Same CPU group", "The kernel default."},
	{"2", "Submitting CPU", "Always finish I/O on the CPU that issued it: better cache use and lower latency for fast NVMe."},
}

// RQAffinity pins I/O completion to the submitting CPU.
func RQAffinity() Tweak {
	return diskOne(Meta{
		ID: "io.rq-affinity", Name: "Finish disk requests on the same CPU",
		Summary:  "Complete each disk request on the core that asked for it, keeping its data in that core's fast cache.",
		Details:  "Writes /sys/block/<disk>/queue/rq_affinity = 2. Normally the completion of a disk request may run on a different core than the program that issued it, which forces data to cross between CPU caches.",
		Helps:    "Fast NVMe drives and workloads with many threads doing small random reads (game loading, databases, compiles).",
		Tradeoff: "Can unbalance CPU load slightly if one core issues most of the I/O.",
		Evidence: "Use `fio` with a small random-read job and compare IOPS and latency before and after.",
		Category: CatStorage, Tier: TierAdvanced, Risk: RiskLow, Scope: ScopeUntilBoot,
	}, "disk request affinity", "rq_affinity", rqOptions, resets)
}

var addRandomOptions = []Option{
	{"0", "Off", "Do not use disk timing as a source of randomness: less work per request."},
	{"1", "On", "Feed disk timing into the entropy pool (the kernel default for spinning disks)."},
}

// AddRandom stops disk events from feeding the entropy pool.
func AddRandom() Tweak {
	return diskOne(Meta{
		ID: "io.add-random", Name: "Skip wasted work on every disk request",
		Summary:  "Stop SSDs from contributing the timing of every request to the random-number pool, which is not needed.",
		Details:  "Writes /sys/block/<disk>/queue/add_random = 0. Modern kernels get plenty of randomness elsewhere, so measuring each I/O is a small amount of pointless extra work.",
		Helps:    "Fast SSDs and NVMe drives (the kernel already leaves it off for most of them, so this mainly checks and fixes it).",
		Tradeoff: "Negligible on a modern system with a normal source of randomness. Do not use it on very old systems that have none.",
		Evidence: "Gain is tiny (a few CPU cycles per request); you will not see it on a stopwatch.",
		Category: CatStorage, Tier: TierAdvanced, Risk: RiskLow, Scope: ScopeUntilBoot,
	}, "disk entropy contribution", "add_random", addRandomOptions, resets)
}

var noMergesOptions = []Option{
	{"0", "Merge all", "The kernel default: combine adjacent requests."},
	{"1", "Simple merges only", "Skip the expensive search for requests to merge."},
	{"2", "No merging", "Never combine requests: least CPU work, best for fast NVMe."},
}

// NoMerges skips request merging on fast drives.
func NoMerges() Tweak {
	return diskOne(Meta{
		ID: "io.nomerges", Name: "Skip merging on very fast drives",
		Summary:  "Let NVMe drives take small requests as they are, instead of spending CPU to merge them first.",
		Details:  "Writes /sys/block/<disk>/queue/nomerges. Merging adjacent requests helps hard disks a lot; on a fast NVMe SSD the CPU time spent looking for merges is more than the benefit.",
		Helps:    "High-end NVMe drives with random-access workloads.",
		Tradeoff: "On SATA SSDs and hard disks, turning merging off lowers sequential throughput.",
		Evidence: "Use `fio` with a random 4K job on the drive and compare IOPS and CPU use.",
		Category: CatStorage, Tier: TierAdvanced, Risk: RiskLow, Scope: ScopeUntilBoot,
	}, "request merging", "nomerges", noMergesOptions, resets)
}

// ---- GPU ---------------------------------------------------------------------

var drmCardRe = regexp.MustCompile(`^card[0-9]{1,2}$`)

func intelClockFiles(env Env) map[string]string {
	out := map[string]string{}
	ents, err := env.FS.ReadDir("/sys/class/drm")
	if err != nil {
		return nil
	}
	for _, e := range ents {
		n := e.Name()
		if !drmCardRe.MatchString(n) {
			continue
		}
		p := "/sys/class/drm/" + n + "/gt_min_freq_mhz"
		if _, ok := platform.ReadTrim(env.FS, p); ok {
			out[n] = p
		}
	}
	return out
}

// IntelGPUMinClock raises the Intel iGPU's lowest clock.
func IntelGPUMinClock() Tweak {
	m := Meta{
		ID: "gpu.intel-min-clock", Name: "Wake the Intel graphics faster",
		Summary:  "Raise the lowest speed of Intel integrated graphics so it is ready the moment a frame arrives.",
		Details:  "Writes gt_min_freq_mhz for the i915 driver. The integrated GPU idles at its lowest clock and climbs when work shows up; a higher floor removes the climb.",
		Helps:    "Gaming and video on Intel integrated graphics, where the GPU ramp-up lags behind load.",
		Tradeoff: "More power and heat at idle. The maximum speed is unchanged.",
		Evidence: "Compare 1% low frame times in Benchmark Lab; a GPU already at full load will not change.",
		Category: CatGaming, Tier: TierSituational, Risk: RiskLow, Scope: ScopeUntilBoot,
		NeedsAuth: true, ParamName: "mhz",
	}
	return fileTweak{
		meta: m, what: "Intel GPU minimum clock",
		gate:    func(Env) string { return "" },
		targets: intelClockFiles,
		options: func(env Env, t map[string]string) []Option {
			var dir string
			for _, p := range t {
				dir = strings.TrimSuffix(p, "gt_min_freq_mhz")
				break
			}
			num := func(f string) int {
				s, _ := platform.ReadTrim(env.FS, dir+f)
				n, _ := strconv.Atoi(s)
				return n
			}
			lo, hi := num("gt_RPn_freq_mhz"), num("gt_max_freq_mhz")
			if lo <= 0 || hi <= lo {
				return nil
			}
			mid := (lo + hi) / 2
			return []Option{
				{strconv.Itoa(lo), fmt.Sprintf("%d MHz (lowest)", lo), "Let the GPU idle as low as it can."},
				{strconv.Itoa(mid), fmt.Sprintf("%d MHz (middle)", mid), "Half-way: quicker response for modest extra power."},
				{strconv.Itoa(hi), fmt.Sprintf("%d MHz (maximum)", hi), "Hold the GPU at its top speed."},
			}
		},
		warnings: []string{"Higher idle power and heat while set; resets at reboot."},
	}
}

var pciSlotRe = regexp.MustCompile(`^[0-9a-f]{4}:[0-9a-f]{2}:[0-9a-f]{2}\.[0-7]$`)

var dgpuOptions = []Option{
	{"on", "Always on", "Keep the graphics card powered: no wake-up delay when a game or app starts using it."},
	{"auto", "Allow sleep", "The kernel default: the card powers down when idle (saves battery)."},
}

// DGPUAwake keeps a discrete GPU out of runtime suspend.
func DGPUAwake() Tweak {
	m := Meta{
		ID: "gpu.dgpu-awake", Name: "Keep the dedicated graphics card awake",
		Summary:  "Stop the discrete GPU from powering down between uses, so games and apps start sooner.",
		Details:  "Writes the PCI power/control attribute of every non-boot graphics card. In 'auto' the GPU sleeps when idle and takes a noticeable moment (often a second or more) to wake when something asks for it.",
		Helps:    "Gaming laptops (hybrid graphics) and desktops that drive a second card; apps that open and close often.",
		Tradeoff: "A powered-on dedicated GPU draws several watts and can cut laptop battery life dramatically. Use it while plugged in.",
		Evidence: "Time how long a GPU-using app (e.g. `glxinfo -B` with DRI_PRIME=1) takes to start from idle, before and after.",
		Category: CatGaming, Tier: TierSituational, Risk: RiskMedium, Scope: ScopeUntilBoot,
		NeedsAuth: true, ParamName: "power", Options: dgpuOptions,
	}
	return fileTweak{
		meta: m, what: "discrete GPU power control",
		gate: func(Env) string { return "" },
		targets: func(env Env) map[string]string {
			out := map[string]string{}
			for _, g := range env.Report.GPUs {
				if g.BootVGA || !pciSlotRe.MatchString(g.Slot) {
					continue
				}
				p := "/sys/bus/pci/devices/" + g.Slot + "/power/control"
				if _, ok := platform.ReadTrim(env.FS, p); ok {
					out[g.Slot] = p
				}
			}
			return out
		},
		options:  func(Env, map[string]string) []Option { return dgpuOptions },
		warnings: []string{"Raises power use and heat while set; resets at reboot."},
	}
}
