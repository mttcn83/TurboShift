package tweaks

import (
	"context"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/mttcn83/turboshift/internal/platform"
	"github.com/mttcn83/turboshift/internal/privilege"
)

// fileTweak is a tweak whose whole state is a set of kernel attribute files
// (sysfs / procfs) that are written as root through the allowlisted helper.
// Each tweak below only supplies its facts (where the files are, which values
// exist, when it is worth recommending); the safety logic — plan, one-prompt
// batched write, read-back verification, idempotent restore — is shared.
//
// State keys name an attribute ("cpu3", "nvme0n1", ...); values are what the
// tweak shows the user (already normalised by fromFile).
type fileTweak struct {
	meta Meta
	// gate returns why the tweak must not be offered here, or "".
	gate func(env Env) string
	// targets maps every managed key to its attribute path. Empty = unsupported.
	targets func(env Env) map[string]string
	// options lists the values this machine accepts, given the targets.
	options func(env Env, targets map[string]string) []Option
	// recommend may suggest a change from live state; nil means never.
	recommend func(env Env, cur State) *Recommendation
	// toFile/fromFile convert between the shown value and the file contents.
	// nil means identical. fromFile receives the already-trimmed contents.
	toFile   func(key, v string) string
	fromFile func(key, raw string) string
	// presets, when set, makes each option a named bundle: option value ->
	// per-key values (all targets must be present in every bundle).
	presets map[string]map[string]string
	// warnings are shown in every preview.
	warnings []string
	// summary names what is being changed, for the preview ("CPU energy preference").
	what string
}

func (f fileTweak) Meta() Meta { return f.meta }

func (f fileTweak) to(key, v string) string {
	if f.toFile != nil {
		return f.toFile(key, v)
	}
	return v
}

func (f fileTweak) from(key, raw string) string {
	if f.fromFile != nil {
		return f.fromFile(key, raw)
	}
	return raw
}

func (f fileTweak) readAll(env Env, targets map[string]string) (State, error) {
	cur := State{}
	for k, p := range targets {
		raw, ok := platform.ReadTrim(env.FS, p)
		if !ok {
			return nil, fmt.Errorf("%s is unreadable (hardware went away?)", p)
		}
		cur[k] = f.from(k, raw)
	}
	return cur, nil
}

func (f fileTweak) Assess(_ context.Context, env Env) Assessment {
	if why := f.gate(env); why != "" {
		return Assessment{Reason: why}
	}
	t := f.targets(env)
	if len(t) == 0 {
		return Assessment{Reason: "This system does not expose the setting (" + f.what + ")."}
	}
	if env.Priv == nil {
		return Assessment{Reason: "Privileged actions are unavailable."}
	}
	if ok, why := env.Priv.Available(); !ok {
		return Assessment{Reason: why}
	}
	cur, err := f.readAll(env, t)
	if err != nil {
		return Assessment{Reason: err.Error()}
	}
	opts := f.options(env, t)
	if len(opts) == 0 {
		return Assessment{Reason: "No alternative value is available on this system."}
	}
	a := Assessment{Available: true, Current: cur, Options: opts}
	if f.recommend != nil {
		a.Recommend = f.recommend(env, cur)
	}
	return a
}

func (f fileTweak) Read(_ context.Context, env Env, want State) (State, error) {
	t := f.targets(env)
	sub := map[string]string{}
	for k := range want {
		p, ok := t[k]
		if !ok {
			return nil, fmt.Errorf("%s is no longer present", k)
		}
		sub[k] = p
	}
	return f.readAll(env, sub)
}

func (f fileTweak) Plan(ctx context.Context, env Env, params map[string]string) (Plan, error) {
	a := f.Assess(ctx, env)
	if !a.Available {
		return Plan{}, fmt.Errorf("%w: %s", ErrUnavailable, a.Reason)
	}
	v := params[f.meta.ParamName]
	if err := CheckParam(f.meta, a.Options, v); err != nil {
		return Plan{}, err
	}
	after := State{}
	groups := map[string]int{}
	changed := 0
	for k, cur := range a.Current {
		want := v
		if f.presets != nil {
			want = f.presets[v][k]
		}
		after[k] = want
		if cur != want {
			changed++
			groups[cur]++
		}
	}
	if changed == 0 {
		return Plan{}, ErrNoChange
	}
	var froms []string
	for val, n := range groups {
		if len(a.Current) > 1 {
			froms = append(froms, fmt.Sprintf("%s ×%d", val, n))
		} else {
			froms = append(froms, val)
		}
	}
	sort.Strings(froms)
	label := f.what
	if len(after) > 1 {
		label = fmt.Sprintf("%s (%d devices)", f.what, len(after))
	}
	warn := append([]string{"Administrator permission is required."}, f.warnings...)
	return Plan{
		TweakID: f.meta.ID, Params: map[string]string{f.meta.ParamName: v},
		Before: a.Current.Clone(), After: after, NeedsAuth: true,
		Changes:  []Change{{label, strings.Join(froms, ", "), shownTo(after, v)}},
		Warnings: warn,
	}, nil
}

// write sets every attribute under ONE administrator authorisation.
func (f fileTweak) write(ctx context.Context, env Env, target State) error {
	if env.Priv == nil {
		return errors.New("no privilege executor configured")
	}
	paths := f.targets(env)
	var keys []string
	for k := range target {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var as []privilege.Action
	for _, k := range keys {
		p, ok := paths[k]
		if !ok {
			return fmt.Errorf("%s is no longer present on this system", k)
		}
		as = append(as, privilege.Action{Op: privilege.OpWriteSysfs, Path: p, Value: f.to(k, target[k])})
	}
	return privilege.DoAll(ctx, env.Priv, as)
}

func (f fileTweak) Apply(ctx context.Context, env Env, p Plan) error {
	return f.write(ctx, env, p.After)
}

// Restore writes the original values back (idempotent, safe after a partial Apply).
func (f fileTweak) Restore(ctx context.Context, env Env, p Plan) error {
	return f.write(ctx, env, p.Before)
}

// ---- shared helpers -------------------------------------------------------

func readWords(env Env, p string) []string {
	s, _ := platform.ReadTrim(env.FS, p)
	return strings.Fields(strings.NewReplacer("[", "", "]", "").Replace(s))
}

func onAC(env Env) bool { v := env.Report.Power.OnAC; return v != nil && *v }

// powerOwner is the gate for settings a power manager (PPD, TLP, TuneD,
// auto-cpufreq) rewrites: changing them behind its back would be undone or
// would fight it.
func powerOwner(env Env, what string) string {
	if m := activeManagers(env.Report, ""); len(m) > 0 {
		return fmt.Sprintf("%s is managing power policy and would override or fight a direct change to %s.", strings.Join(m, ", "), what)
	}
	if env.Report.Power.Unknown {
		return "Could not determine whether a power manager is running, so changing " + what + " directly is not safe to offer."
	}
	return ""
}

func pick(all []Option, have []string) []Option {
	var out []Option
	for _, o := range all {
		for _, h := range have {
			if h == o.Value {
				out = append(out, o)
				break
			}
		}
	}
	return out
}

func cpuAttr(env Env, file string) map[string]string {
	out := map[string]string{}
	for _, n := range cpuIndexes(env) {
		p := path.Join(cpuRoot, fmt.Sprintf("cpu%d", n), "cpufreq", file)
		if _, ok := platform.ReadTrim(env.FS, p); ok {
			out[fmt.Sprintf("cpu%d", n)] = p
		}
	}
	return out
}

// ---- cpu.epp ----------------------------------------------------------------

var eppOptions = []Option{
	{"performance", "Performance", "Always favour speed; highest power and heat."},
	{"balance_performance", "Balance performance", "Favour speed but let idle cores save power (a common AC default)."},
	{"default", "Firmware default", "Whatever the platform firmware chose."},
	{"balance_power", "Balance power", "Favour efficiency; slower ramp-up."},
	{"power", "Power", "Strongly favour efficiency; the CPU is slow to boost."},
}

// CPUEnergyPreference is the Energy Performance Preference hint used by
// intel_pstate (HWP) and amd-pstate in active mode.
func CPUEnergyPreference() Tweak {
	return fileTweak{
		what: "CPU energy preference",
		meta: Meta{
			ID: "cpu.epp", Name: "Make the CPU favour speed over saving power",
			Summary:  "Tell the CPU's own speed controller to ramp up faster, or to save more power.",
			Details:  "Writes the energy preference of every CPU thread. On modern Intel and AMD chips the processor chooses its own clock speed; this hint biases that choice. A power-saving preference left on while you are plugged in makes the CPU slow to speed up, which can feel like lag.",
			Helps:    "Anyone plugged in whose CPU still leans towards saving power; laptops and desktops with intel_pstate or amd-pstate.",
			Tradeoff: "Favouring performance raises power draw, heat and fan noise, and shortens battery life. It does nothing for GPU- or network-bound work.",
			Evidence: "Compare frame times (Benchmark Lab, ≥4 runs each). If a game is GPU-bound the result will not move; then revert.",
			Category: CatCPU, Tier: TierSituational, Risk: RiskLow, Scope: ScopeUntilBoot,
			NeedsAuth: true, ParamName: "preference", Options: eppOptions,
		},
		gate: func(env Env) string {
			if len(env.Report.CPU.EPPOptions) == 0 {
				return "This CPU/driver does not expose an energy preference (needs intel_pstate or amd-pstate in active mode)."
			}
			return powerOwner(env, "the CPU energy preference")
		},
		targets: func(env Env) map[string]string { return cpuAttr(env, "energy_performance_preference") },
		options: func(env Env, _ map[string]string) []Option { return pick(eppOptions, env.Report.CPU.EPPOptions) },
		recommend: func(env Env, cur State) *Recommendation {
			if !onAC(env) {
				return nil
			}
			for _, v := range cur {
				if v == "power" || v == "balance_power" {
					return &Recommendation{Params: map[string]string{"preference": "balance_performance"},
						Why: "You are plugged in but the CPU energy preference favours power saving (" + v + "), so it ramps up slowly for no battery benefit."}
				}
			}
			return nil
		},
		warnings: []string{"Resets at reboot or when a power manager is started."},
	}
}

// ---- cpu.boost --------------------------------------------------------------

const (
	pstateNoTurbo = "/sys/devices/system/cpu/intel_pstate/no_turbo"
	cpufreqBoost  = "/sys/devices/system/cpu/cpufreq/boost"
)

var boostOptions = []Option{
	{"on", "On", "Let the CPU exceed its base clock when there is thermal and power headroom."},
	{"off", "Off", "Cap the CPU at its base clock: cooler and quieter, but slower single-thread work."},
}

// CPUBoost toggles turbo/boost where the kernel exposes the switch.
func CPUBoost() Tweak {
	return fileTweak{
		what: "CPU turbo/boost",
		meta: Meta{
			ID: "cpu.boost", Name: "Let the CPU run faster than its base speed (turbo)",
			Summary:  "Allow or forbid the CPU to run above its base clock.",
			Details:  "Flips the kernel's turbo switch. With boost off the CPU never exceeds its base clock, which can cost a large share of single-thread speed in games and compilers; with it on it uses spare thermal headroom to go faster.",
			Helps:    "People whose turbo got switched off (by a script or a power tool); also anyone who wants to turn it off to cool a hot laptop.",
			Tradeoff: "Boost raises temperature, fan noise and power draw. Turning it off is a legitimate way to cool a hot laptop; turning it on only helps if something turned it off.",
			Evidence: "Watch clock speed and temperature on the Dashboard, and compare frame times in Benchmark Lab.",
			Category: CatCPU, Tier: TierSituational, Risk: RiskLow, Scope: ScopeUntilBoot,
			NeedsAuth: true, ParamName: "boost", Options: boostOptions,
		},
		gate: func(env Env) string { return powerOwner(env, "CPU boost") },
		targets: func(env Env) map[string]string {
			if _, ok := platform.ReadTrim(env.FS, pstateNoTurbo); ok {
				return map[string]string{"intel_pstate": pstateNoTurbo}
			}
			if _, ok := platform.ReadTrim(env.FS, cpufreqBoost); ok {
				return map[string]string{"cpufreq": cpufreqBoost}
			}
			return nil
		},
		options: func(Env, map[string]string) []Option { return boostOptions },
		recommend: func(env Env, cur State) *Recommendation {
			if onAC(env) && cur["intel_pstate"] == "off" || onAC(env) && cur["cpufreq"] == "off" {
				return &Recommendation{Params: map[string]string{"boost": "on"},
					Why: "CPU boost is switched off while you are plugged in, so the CPU is capped at its base clock."}
			}
			return nil
		},
		// intel_pstate/no_turbo is inverted: 0 means boost is ON.
		fromFile: func(key, raw string) string {
			on := raw == "1"
			if key == "intel_pstate" {
				on = raw == "0"
			}
			if on {
				return "on"
			}
			return "off"
		},
		toFile: func(key, v string) string {
			on := v == "on"
			if key == "intel_pstate" {
				on = !on
			}
			if on {
				return "1"
			}
			return "0"
		},
		warnings: []string{"Resets at reboot."},
	}
}

// ---- platform.profile -------------------------------------------------------

const platformProfile = "/sys/firmware/acpi/platform_profile"

var platformOptions = []Option{
	{"low-power", "Low power", "Quietest and coolest; lowest performance."},
	{"quiet", "Quiet", "Reduced fan noise."},
	{"cool", "Cool", "Favour lower temperatures."},
	{"balanced", "Balanced", "The firmware default."},
	{"balanced-performance", "Balanced performance", "More performance, a little more heat and noise."},
	{"performance", "Performance", "Highest sustained power limits the firmware allows; loudest."},
}

// PlatformProfile sets the ACPI platform profile (laptop firmware power/thermal mode).
func PlatformProfile() Tweak {
	return fileTweak{
		what: "firmware platform profile",
		meta: Meta{
			ID: "platform.profile", Name: "Laptop fan and power mode",
			Summary:  "Switch the laptop firmware between quiet, balanced and performance modes.",
			Details:  "Writes the ACPI platform profile that many laptops (ASUS, Lenovo, HP, Dell, Framework...) map to fan curves and CPU/GPU power limits. With power-profiles-daemon running the daemon owns this setting, so TurboShift only offers it when no power manager is active.",
			Helps:    "Gaming laptops that need a performance profile for sustained load, or a quiet profile for the library.",
			Tradeoff: "Performance profiles raise fan noise, heat and battery drain. The firmware decides what each name really does.",
			Evidence: "Compare sustained frame times and CPU temperature over a long session (short runs hide thermal limits).",
			Category: CatCPU, Tier: TierAdvanced, Risk: RiskLow, Scope: ScopeUntilBoot,
			NeedsAuth: true, ParamName: "profile", Options: platformOptions,
		},
		gate: func(env Env) string {
			if _, ok := platform.ReadTrim(env.FS, platformProfile); !ok {
				return "This laptop's firmware does not expose a platform profile."
			}
			return powerOwner(env, "the platform profile")
		},
		targets: func(env Env) map[string]string {
			if _, ok := platform.ReadTrim(env.FS, platformProfile); ok {
				return map[string]string{"platform_profile": platformProfile}
			}
			return nil
		},
		options: func(env Env, _ map[string]string) []Option {
			return pick(platformOptions, readWords(env, platformProfile+"_choices"))
		},
		warnings: []string{"Resets at reboot."},
	}
}

// ---- net.tcp-cc -------------------------------------------------------------

const tcpCC = "/proc/sys/net/ipv4/tcp_congestion_control"

var tcpOptions = []Option{
	{"bbr", "BBR", "Model-based; often higher throughput on lossy or long links and less queueing. TCP only."},
	{"cubic", "CUBIC", "The Linux default for most distributions."},
	{"reno", "Reno", "Classic, conservative."},
	{"westwood", "Westwood", "Tuned for wireless loss."},
	{"htcp", "H-TCP", "Aimed at high-bandwidth, high-latency links."},
	{"vegas", "Vegas", "Delay-based; yields to competing flows."},
}

// TCPCongestion switches the TCP congestion-control algorithm.
func TCPCongestion() Tweak {
	return fileTweak{
		what: "TCP congestion control",
		meta: Meta{
			ID: "net.tcp-cc", Name: "Better download speed on lossy or long links (BBR)",
			Summary:  "Choose how downloads and web traffic react to lost packets and delay.",
			Details:  "Sets the TCP congestion-control algorithm for new connections. BBR models the link instead of reacting only to loss, which often gives higher throughput on lossy or long-distance paths. Online games use UDP, which this does not touch.",
			Helps:    "Wi-Fi, mobile, VPN and far-away servers; game launchers, large downloads and streaming.",
			Tradeoff: "Only algorithms already loaded in your kernel are offered (load tcp_bbr first if BBR is missing). A change can make some links faster and others worse.",
			Evidence: "Network Doctor's download load test: compare throughput and latency under load before and after, over several runs.",
			Category: CatNetwork, Tier: TierAdvanced, Risk: RiskLow, Scope: ScopeUntilBoot,
			NeedsAuth: true, ParamName: "algorithm", Options: tcpOptions,
		},
		gate: func(Env) string { return "" },
		targets: func(env Env) map[string]string {
			if _, ok := platform.ReadTrim(env.FS, tcpCC); ok {
				return map[string]string{"tcp_congestion_control": tcpCC}
			}
			return nil
		},
		options: func(env Env, _ map[string]string) []Option {
			return pick(tcpOptions, readWords(env, "/proc/sys/net/ipv4/tcp_available_congestion_control"))
		},
		warnings: []string{"Affects new TCP connections only, and resets at reboot."},
	}
}

// ---- mem.swappiness -----------------------------------------------------------

var swappinessOptions = []Option{
	{"10", "10", "Strongly prefer keeping program memory in RAM; swap only under pressure."},
	{"30", "30", "Lean towards keeping program memory in RAM."},
	{"60", "60", "The kernel default."},
	{"100", "100", "Treat file cache and swap equally; sensible with zram."},
	{"180", "180", "Prefer swapping out idle pages; designed for fast swap such as zram."},
}

// Swappiness tunes how eagerly the kernel swaps.
func Swappiness() Tweak {
	return fileTweak{
		what: "vm.swappiness",
		meta: Meta{
			ID: "mem.swappiness", Name: "Keep your programs in RAM longer (swappiness)",
			Summary:  "Set how eagerly the kernel moves idle memory out to swap.",
			Details:  "Sets vm.swappiness (0-200). Lower values keep program memory in RAM longer; high values (100-200) make sense when swap is fast compressed RAM (zram), because swapping is cheaper than dropping file cache. There is no best number for everyone, so nothing is recommended automatically.",
			Helps:    "Desktops that hold many programs open; machines with zram or fast swap.",
			Tradeoff: "Too low can cause stalls when memory finally runs short; too high on a slow disk makes the system swap needlessly.",
			Evidence: "Watch memory pressure (Doctor shows the kernel's own stall figures) during your real workload before and after.",
			Category: CatMemory, Tier: TierAdvanced, Risk: RiskLow, Scope: ScopeUntilBoot,
			NeedsAuth: true, ParamName: "swappiness", Options: swappinessOptions,
		},
		gate: func(env Env) string {
			if env.Report.Memory.SwapTotalKB == 0 {
				return "No swap or zram is configured, so swappiness has no effect."
			}
			return ""
		},
		targets: func(env Env) map[string]string {
			if _, ok := platform.ReadTrim(env.FS, "/proc/sys/vm/swappiness"); ok {
				return map[string]string{"swappiness": "/proc/sys/vm/swappiness"}
			}
			return nil
		},
		options:  func(Env, map[string]string) []Option { return swappinessOptions },
		warnings: []string{"Resets at reboot."},
	}
}

// ---- io.scheduler -------------------------------------------------------------

var schedOptions = []Option{
	{"none", "none", "No reordering; best for fast NVMe SSDs that schedule internally."},
	{"mq-deadline", "mq-deadline", "Simple and low overhead; a good all-round choice for SSDs and HDDs."},
	{"bfq", "bfq", "Fairness and desktop responsiveness under heavy disk load; more CPU overhead."},
	{"kyber", "kyber", "Latency-targeting scheduler for fast devices."},
}

var diskNameOK = func(n string) bool {
	return strings.HasPrefix(n, "nvme") && strings.Contains(n, "n") && !strings.Contains(n, "p") ||
		strings.HasPrefix(n, "sd") || strings.HasPrefix(n, "vd") || strings.HasPrefix(n, "mmcblk") && !strings.Contains(n, "p")
}

// IOScheduler chooses the block I/O scheduler of whole physical disks.
func IOScheduler() Tweak {
	return fileTweak{
		what: "I/O scheduler",
		meta: Meta{
			ID: "io.scheduler", Name: "Choose how the kernel orders disk requests",
			Summary:  "Pick the policy that decides which disk requests go first.",
			Details:  "Writes the I/O scheduler of each whole disk (NVMe, SATA/USB, virtio, SD). The kernel default suits most machines; 'bfq' can keep a desktop responsive during heavy disk activity, 'none' is right for fast NVMe.",
			Helps:    "Desktops that freeze during heavy disk use (bfq); fast NVMe drives (none).",
			Tradeoff: "Only choose differently if you see the disk starving interactive programs (Doctor shows disk stall pressure). Wrong choices can reduce throughput.",
			Evidence: "Compare disk pressure and your real task's duration before and after, over several runs.",
			Category: CatStorage, Tier: TierAdvanced, Risk: RiskLow, Scope: ScopeUntilBoot,
			NeedsAuth: true, ParamName: "scheduler", Options: schedOptions,
		},
		gate: func(Env) string { return "" },
		targets: func(env Env) map[string]string {
			out := map[string]string{}
			ents, err := env.FS.ReadDir("/sys/block")
			if err != nil {
				return nil
			}
			for _, e := range ents {
				n := e.Name()
				if !diskNameOK(n) {
					continue
				}
				p := "/sys/block/" + n + "/queue/scheduler"
				if _, ok := platform.ReadTrim(env.FS, p); ok {
					out[n] = p
				}
			}
			return out
		},
		options: func(env Env, t map[string]string) []Option {
			// Offer only what every selected disk supports.
			var common []string
			first := true
			for _, p := range t {
				have := readWords(env, p)
				if first {
					common, first = have, false
					continue
				}
				var keep []string
				for _, c := range common {
					for _, h := range have {
						if c == h {
							keep = append(keep, c)
						}
					}
				}
				common = keep
			}
			return pick(schedOptions, common)
		},
		fromFile: func(_, raw string) string { return privilege.ActiveChoice(raw) },
		warnings: []string{"Resets at reboot (udev rules or the kernel command line decide the default)."},
	}
}

// shownTo is the "to" column of a preview: the chosen option, plus the actual
// values when an option is a bundle of several settings.
func shownTo(after State, opt string) string {
	var vals []string
	seen := map[string]bool{}
	for _, v := range after {
		if !seen[v] {
			seen[v] = true
			vals = append(vals, v)
		}
	}
	sort.Strings(vals)
	if len(vals) == 1 && vals[0] == opt {
		return opt
	}
	return opt + " (" + strings.Join(vals, "/") + ")"
}
