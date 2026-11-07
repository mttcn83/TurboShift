package tweaks

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/mttcn83/turboshift/internal/platform"
)

// Tweaks added in 1.1, plus the live-evidence rules that decide when the
// catalogue suggests a setting. A suggestion is a claim about this PC, so each
// rule below names the reading it is based on and says nothing when that
// reading is missing.

// ---- sys.watchers -------------------------------------------------------------

const inotifyDir = "/proc/sys/fs/inotify/"

// Watchers raises how many files one user may have watched for changes.
func Watchers() Tweak {
	const w, i = inotifyDir + "max_user_watches", inotifyDir + "max_user_instances"
	presets := map[string]map[string]string{
		"some": {"watches": "262144", "instances": "512"},
		"more": {"watches": "524288", "instances": "512"},
		"most": {"watches": "1048576", "instances": "1024"},
	}
	opts := []Option{
		{"some", "262144 files", "Room for several big projects. Needs at most about a quarter of a gigabyte of system memory, and only if every slot is used."},
		{"more", "524288 files", "What editors and sync tools usually ask for. Up to about half a gigabyte of system memory if every slot is used."},
		{"most", "1048576 files", "For very large source trees or many projects at once."},
	}
	m := Meta{
		ID: "sys.watchers", Name: "Let editors and sync tools watch more files",
		Summary:  "Raise the limit on how many files programs can watch for changes, which fixes \"too many open files\" and \"file watchers reached\" errors.",
		Details:  "Raises fs.inotify.max_user_watches and max_user_instances. Editors (VS Code, JetBrains), dev servers, Syncthing, Dropbox and Docker tools ask the kernel to tell them when files change, and each watched folder uses one slot. When the limit runs out they fail with ENOSPC \"System limit for number of file watchers reached\".",
		Helps:    "Anyone who sees that error, or who works in big source trees or syncs many folders. Newer kernels size the limit by RAM, so this mostly matters on older kernels or small machines.",
		Tradeoff: "The limit is only a ceiling: each slot that is actually used costs about 1 KB of system memory that no program can swap out, so a runaway tool could now use up to half a gigabyte before it is stopped. Nothing changes for programs that do not hit the limit.",
		Evidence: "The error message itself is the evidence. `cat /proc/sys/fs/inotify/max_user_watches` shows the current limit; the error should stop after the change.",
		Category: CatStorage, Tier: TierRecommended, Risk: RiskLow, Scope: ScopeUntilBoot,
		NeedsAuth: true, ParamName: "limit", Options: opts,
	}
	return fileTweak{
		meta: m, what: "file-watch limits", presets: presets,
		gate: func(Env) string { return "" },
		targets: func(env Env) map[string]string {
			_, a := platform.ReadTrim(env.FS, w)
			_, b := platform.ReadTrim(env.FS, i)
			if a && b {
				return map[string]string{"watches": w, "instances": i}
			}
			return nil
		},
		options:  func(Env, map[string]string) []Option { return opts },
		warnings: []string{resets},
	}
}

// ---- desktop.hypr-vrr ---------------------------------------------------------

// HyprVRR switches variable refresh rate (FreeSync / G-Sync / Adaptive-Sync).
func HyprVRR() Tweak {
	k := "misc:vrr"
	opts := []Option{
		{"off", "Off", "Fixed refresh rate."},
		{"fullscreen", "Full-screen only", "Variable refresh rate while a full-screen window is up, fixed on the desktop."},
		{"on", "Always on", "Variable refresh rate all the time, including on the desktop."},
	}
	return cmdTweak{
		meta: Meta{
			ID: "desktop.hypr-vrr", Name: "Smoother games on adaptive-sync monitors (VRR)",
			Summary:  "Let the monitor follow the game's frame rate instead of a fixed refresh, which removes tearing and judder when the frame rate wobbles.",
			Details:  "Sets misc:vrr in Hyprland (0 off, 1 on, 2 full-screen only). With VRR the monitor refreshes when a frame is ready, so a game running at 87 fps does not stutter between 60 and 120.",
			Helps:    "Gamers with a FreeSync, G-Sync Compatible or Adaptive-Sync monitor whose frame rate is not locked to the refresh rate.",
			Tradeoff: "Only works if the monitor and cable support it; otherwise nothing changes. Some monitors flicker on dark scenes with VRR on, which is why 'full-screen only' keeps it off for the desktop.",
			Evidence: "A game with an uneven frame rate looks smoother and shows no tear lines; if the monitor has an on-screen refresh readout it will move with the frame rate.",
			Category: CatDesktop, Tier: TierSituational, Risk: RiskLow, Scope: ScopeSession,
			ParamName: "vrr", Options: opts,
		},
		what: "Hyprland variable refresh rate", keys: []string{k},
		presets: map[string]map[string]string{"off": {k: "0"}, "on": {k: "1"}, "fullscreen": {k: "2"}},
		opts:    opts,
		gate:    hyprGate(k), read: hyprRead, write: hyprWrite(k),
		warn: []string{"Lasts until Hyprland reloads its config or you log out."},
	}
}

// ---- audio.quantum ------------------------------------------------------------

var (
	quantumVal  = regexp.MustCompile(`^[0-9]{1,4}$`)
	quantumLine = regexp.MustCompile(`key:'clock\.force-quantum' value:'([0-9]+)'`)
)

const quantumKey = "clock.force-quantum"

func quantumRead(ctx context.Context, env Env, _ string) (string, error) {
	res, err := env.Run.Run(ctx, "pw-metadata", "-n", "settings", "0", quantumKey)
	if err != nil {
		return "", err
	}
	m := quantumLine.FindStringSubmatch(res.Stdout)
	if m == nil {
		return "", fmt.Errorf("unexpected pw-metadata reply")
	}
	return m[1], nil
}

// PipeWireQuantum sets the audio buffer size PipeWire uses for every program.
func PipeWireQuantum() Tweak {
	opts := []Option{
		{"0", "Automatic", "Let each program ask for the buffer size it wants. The usual setting."},
		{"128", "Small (128)", "Low delay for music making or voice chat. Needs more processor time and crackles sooner if the computer is busy."},
		{"512", "Medium (512)", "A balance for gaming with voice chat."},
		{"1024", "Large (1024)", "Fewer crackles and dropouts on a busy or slow computer, at the cost of more delay."},
	}
	presets := map[string]map[string]string{}
	for _, o := range opts {
		presets[o.Value] = map[string]string{quantumKey: o.Value}
	}
	return cmdTweak{
		meta: Meta{
			ID: "audio.quantum", Name: "Fix crackling audio or lower audio delay",
			Summary:  "Choose how big PipeWire's audio buffer is: larger stops crackles and pops, smaller cuts the delay between an action and the sound.",
			Details:  "Sets PipeWire's clock.force-quantum (frames per audio cycle) for all programs at once with pw-metadata. A larger quantum gives the computer more time to deliver each slice of audio, which removes xruns (crackles and pops) under load; a smaller one lowers latency.",
			Helps:    "People who hear crackling, popping or dropouts while gaming, compiling or streaming, and people who need low delay for instruments, voice chat or rhythm games.",
			Tradeoff: "Latency and reliability pull in opposite directions: at 48 kHz, 128 frames is about 2.7 ms of buffer and 1024 about 21 ms. A forced value overrides what each program asks for, and a too-small one makes crackling worse. It lasts until PipeWire restarts.",
			Evidence: "Listen under the load that caused the problem, or watch the ERR column of `pw-top` (it counts dropouts) before and after.",
			Category: CatInput, Tier: TierSituational, Risk: RiskLow, Scope: ScopeSession,
			ParamName: "frames", Options: opts,
		},
		what: "PipeWire audio buffer", keys: []string{quantumKey},
		presets: presets, opts: opts,
		gate: func(ctx context.Context, env Env) string {
			if !env.Run.Has("pw-metadata") {
				return "PipeWire's pw-metadata is not installed."
			}
			if _, err := quantumRead(ctx, env, quantumKey); err != nil {
				return "PipeWire is not running for this user, or this version does not expose the audio buffer setting."
			}
			return ""
		},
		read: quantumRead,
		write: func(ctx context.Context, env Env, key, val string) error {
			if key != quantumKey || !quantumVal.MatchString(val) {
				return fmt.Errorf("%w: refusing to set %q to %q", ErrInvalidParam, key, val)
			}
			if _, err := env.Run.Run(ctx, "pw-metadata", "-n", "settings", "0", quantumKey, val); err != nil {
				return fmt.Errorf("pw-metadata: %w", err)
			}
			return nil
		},
		warn: []string{"Applies to all audio programs at once; lasts until PipeWire restarts."},
	}
}

// ---- suggestions --------------------------------------------------------------

func atoi(s string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	return n, err == nil
}

func gib(kb uint64) float64 { return float64(kb) / (1 << 20) }

// suggestions maps a tweak ID to its evidence rule. fileTweak.Assess uses an
// entry here only when the tweak has no rule of its own.
var suggestions = map[string]func(env Env, cur State) *Recommendation{
	"mem.swappiness": func(env Env, cur State) *Recommendation {
		n, ok := atoi(cur["swappiness"])
		if env.Report.Memory.ZramDevices == 0 || !ok || n > 60 {
			return nil
		}
		return &Recommendation{Params: map[string]string{"swappiness": "180"},
			Why: fmt.Sprintf("Your swap is compressed RAM (zram), which is far faster than a disk, but the system still treats swapping as expensive (swappiness %d). Values of 100 and above are meant for exactly this setup.", n)}
	},
	"mem.dirty": func(env Env, cur State) *Recommendation {
		if cur["background"] != "10" || cur["hard"] != "20" {
			return nil
		}
		ram := gib(env.Report.Memory.TotalKB)
		io := env.Report.Memory.PSIIOSome
		switch {
		case ram >= 32:
			return &Recommendation{Params: map[string]string{"mode": "responsive"},
				Why: fmt.Sprintf("With %.0f GB of RAM the default lets up to %.1f GB of unsaved data pile up before programs are forced to wait for the disk, which freezes the desktop at the end of a big copy to a slow or USB drive.", ram, ram*0.2)}
		case ram >= 8 && io != nil && *io >= 5:
			return &Recommendation{Params: map[string]string{"mode": "responsive"},
				Why: fmt.Sprintf("Programs spent %.0f%% of the last 10 seconds waiting for the disk, and the defaults allow %.1f GB of unsaved data to pile up first.", *io, ram*0.2)}
		}
		return nil
	},
	"game.max-map-count": func(env Env, cur State) *Recommendation {
		n, ok := atoi(cur["max_map_count"])
		if !ok || n >= 1048576 {
			return nil
		}
		return &Recommendation{Params: map[string]string{"value": "1048576"},
			Why: fmt.Sprintf("The memory-map limit is %d. Large games under Proton and several engines (for example Unreal and Unity titles) need far more and crash or hitch when they run out. Current distributions and SteamOS use 1048576 or more.", n)}
	},
	"net.qdisc": func(env Env, cur State) *Recommendation {
		if cur["default_qdisc"] != "pfifo_fast" {
			return nil
		}
		return &Recommendation{Params: map[string]string{"value": "fq_codel"},
			Why: "New connections use the plain first-in-first-out queue (pfifo_fast), which lets one big download add seconds of delay to everything else on the link."}
	},
	"mem.zswap": func(env Env, cur State) *Recommendation {
		m := env.Report.Memory
		if cur["zswap"] != "0" || m.SwapTotalKB == 0 || m.ZramDevices > 0 {
			return nil
		}
		used := float64(m.SwapTotalKB-min(m.SwapFreeKB, m.SwapTotalKB)) / float64(m.SwapTotalKB) * 100
		if used >= 25 {
			return &Recommendation{Params: map[string]string{"value": "1"},
				Why: fmt.Sprintf("%.0f%% of your disk swap is in use and zswap is off, so every swapped page goes to the slow disk instead of being compressed in RAM first.", used)}
		}
		if p := m.PSIMemSome; p != nil && *p >= 2 {
			return &Recommendation{Params: map[string]string{"value": "1"},
				Why: fmt.Sprintf("Programs waited on memory for %.1f%% of the last 10 seconds and swap is on a disk; zswap would compress pages in RAM first.", *p)}
		}
		return nil
	},
	"sys.watchers": func(env Env, cur State) *Recommendation {
		n, ok := atoi(cur["watches"])
		if !ok || n >= 65536 {
			return nil
		}
		pick := "more"
		if gib(env.Report.Memory.TotalKB) < 8 {
			pick = "some"
		}
		return &Recommendation{Params: map[string]string{"limit": pick},
			Why: fmt.Sprintf("Programs may watch only %d files for changes. Editors, dev servers and sync tools run out of that quickly and fail with \"file watchers reached\" errors; current kernels allow far more.", n)}
	},
	"cpu.autogroup": func(env Env, cur State) *Recommendation {
		if cur["autogroup"] != "0" {
			return nil
		}
		return &Recommendation{Params: map[string]string{"value": "1"},
			Why: "Scheduler autogrouping is off (often a tuning script did it), so one heavy build or render can make the whole desktop sluggish."}
	},
	"platform.profile": func(env Env, cur State) *Recommendation {
		if !onAC(env) || cur["platform_profile"] != "low-power" {
			return nil
		}
		return &Recommendation{Params: map[string]string{"profile": "balanced"},
			Why: "The laptop's firmware is in its low-power mode while you are plugged in, which limits fan speed and power for no battery benefit."}
	},
}

// governorSuggestion fires for drivers where the 'powersave' governor really
// pins the lowest speed (with intel_pstate and amd-pstate in active mode it
// does not: there the hardware still scales).
func governorSuggestion(env Env, cur State) *Recommendation {
	switch env.Report.CPU.Driver {
	case "acpi-cpufreq", "intel_cpufreq", "amd-pstate", "cpufreq-dt":
	default:
		return nil
	}
	if !onAC(env) {
		return nil
	}
	stuck := false
	for _, v := range cur {
		stuck = stuck || v == "powersave"
	}
	if !stuck {
		return nil
	}
	for _, g := range []string{"schedutil", "ondemand"} {
		for _, have := range env.Report.CPU.Governors {
			if have == g {
				return &Recommendation{Params: map[string]string{"governor": g},
					Why: fmt.Sprintf("With the %s driver the 'powersave' governor holds the CPU at its lowest speed, and you are plugged in. '%s' lets it speed up when there is work.", env.Report.CPU.Driver, g)}
			}
		}
	}
	return nil
}
