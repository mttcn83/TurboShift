package tweaks

// A preset is a named bundle of tweak settings for one goal. It is only a
// list of requests: every item still goes through the normal preview, the
// restore point, apply and verify steps, and items that do not apply to this
// machine are skipped (and reported) rather than forced.

type PresetItem struct {
	TweakID string `json:"tweak_id"`
	Value   string `json:"value"` // the option value for the tweak's single parameter
}

type Preset struct {
	ID       string
	Key      string // single-key shortcut in the app
	Headline string // what it does, in the user's words
	Blurb    string // one short line for lists
	Name     string
	Summary  string // what you get
	Best     string // who it is for
	Cost     string // what it costs
	Items    []PresetItem
}

func items(kv ...string) []PresetItem {
	var out []PresetItem
	for i := 0; i+1 < len(kv); i += 2 {
		out = append(out, PresetItem{kv[i], kv[i+1]})
	}
	return out
}

var gamingItems = items(
	"power.profile", "performance",
	"cpu.governor", "performance",
	"cpu.epp", "performance",
	"cpu.boost", "on",
	"platform.profile", "performance",
	"pcie.aspm", "performance",
	"usb.autosuspend", "-1",
	"input.mouse-poll", "1",
	"input.kb-poll", "1",
	"input.pad-poll", "1",
	"game.max-map-count", "1048576",
	"mem.compaction", "0",
	"mem.thp-defrag", "defer+madvise",
	"mem.stat-interval", "10",
	"gpu.amd-level", "high",
	"desktop.hypr-direct-scanout", "on",
)

// Presets returns the built-in bundles, in the order they are shown.
func Presets() []Preset {
	return []Preset{
		{
			ID: "gaming", Key: "g", Headline: "Boost for gaming", Blurb: "Fast CPU/GPU, lower input delay, fewer stutters", Name: "Gaming",
			Summary: "Keep the CPU and GPU fast, reduce input delay and avoid kernel behaviours that can cause game stutter.",
			Best:    "A gaming session on a desktop or a plugged-in laptop.",
			Cost:    "More heat, fan noise and power use while it is set, and a laptop on battery drains faster. No speed-up is promised: check with Benchmark Lab. Everything resets at reboot, and `turbo undo` reverses it sooner. The split-lock setting is left out on purpose: it removes a protection, so apply it yourself only if you need it.",
			Items:   gamingItems,
		},
		{
			ID: "desktop", Key: "d", Headline: "Make the desktop snappier", Blurb: "Quicker file and window actions, fewer freezes", Name: "Desktop",
			Summary: "Make everyday use feel more responsive: quicker window and file operations, fewer stalls when copying files or using swap.",
			Best:    "Everyday desktop use, development and multitasking.",
			Cost:    "The disk is written a little more often, a little more memory is held back and the USB devices stay powered, so a laptop on battery may last somewhat less (not measured). No speed-up is promised, and everything resets at reboot.",
			Items: items(
				"cpu.epp", "balance_performance",
				"cpu.autogroup", "1",
				"usb.autosuspend", "-1",
				"mem.vfs-cache", "50",
				"mem.dirty", "responsive",
				"mem.writeback-timing", "smooth",
				"mem.page-cluster", "0",
				"mem.watermark-scale", "50",
				"mem.thp-defrag", "defer+madvise",
				"mem.stat-interval", "10",
				"io.add-random", "0",
			),
		},
		{
			ID: "network", Key: "n", Headline: "Cut network lag", Blurb: "Lower ping while downloading, quicker connections", Name: "Network",
			Summary: "Keep ping low while the connection is busy and make TCP traffic start and recover faster.",
			Best:    "Online gaming and calls on a connection that is also used for downloads; Wi-Fi laptops.",
			Cost:    "Wi-Fi power saving off uses more battery on laptops. BBR can take more than its fair share of a link other devices use, and the fast-open shortcut has a small privacy cost. Game traffic itself (UDP) is not changed, and any of this can make a particular network worse; measure with Network Doctor.",
			Items: items(
				"net.qdisc", "fq_codel",
				"net.tcp-cc", "bbr",
				"net.tcp-idle", "0",
				"net.tcp-fastopen", "1",
				"net.notsent-lowat", "131072",
				"net.mtu-probing", "1",
				"net.ecn", "1",
				"net.buffers", "fast",
				"net.wifi-powersave", "off",
			),
		},
		{
			ID: "maximum", Key: "m", Headline: "Maximum performance", Blurb: "Gaming plus an always-ready CPU. Hot and loud", Name: "Maximum",
			Summary: "Everything in Gaming, plus pinning the CPU floor high and removing background interruptions.",
			Best:    "Short, demanding sessions while plugged in: rendering, competitive play, benchmarks.",
			Cost:    "The most heat, noise and idle power of any preset. Keep your laptop plugged in and revert when you are done. It also turns off the processor freeze check, so a hard lock-up leaves no message in the logs.",
			Items: append(append([]PresetItem{}, gamingItems...), items(
				"cpu.min-perf", "100",
				"cpu.hwp-boost", "1",
				"sys.watchdog", "0",
				"mem.ksm", "0",
				"io.rq-affinity", "2",
				"gpu.dgpu-awake", "on",
			)...),
		},
	}
}

// PresetByID finds a preset.
func PresetByID(id string) (Preset, bool) {
	for _, p := range Presets() {
		if p.ID == id {
			return p, true
		}
	}
	return Preset{}, false
}
