package tweaks

import (
	"context"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/mttcn83/turboshift/internal/platform"
	"github.com/mttcn83/turboshift/internal/privilege"
)

// CPUPolicy sets the cpufreq governor directly through sysfs. It is the one
// tweak that needs root, and it is deliberately only offered when *no* power
// manager is active — otherwise the manager would overwrite it or fight it.
type CPUPolicy struct{}

func (CPUPolicy) Meta() Meta { return plained(CPUPolicy{}.rawMeta()) }

func (CPUPolicy) rawMeta() Meta {
	return Meta{
		ID:       "cpu.governor",
		Name:     "CPU speed policy (governor)",
		Summary:  "Tell every CPU core how eagerly to run fast. Offered only when no power manager is in charge.",
		Details:  "Sets the scaling governor on every core: 'performance' keeps clocks high all the time, 'powersave' lets them drop. The write goes through one allowlisted, validated helper that asks for administrator permission; TurboShift itself keeps running as your normal user.",
		Helps:    "Desktops and servers without power-profiles-daemon, TLP or TuneD that want steady high clocks during a game or a build.",
		Tradeoff: "The 'performance' governor keeps clocks high: more heat, noise and idle power, and often no FPS gain in GPU-bound games. The setting lasts until reboot and is not remembered across boots.",
		Evidence: "Compare MangoHud runs (≥4 per governor). With intel_pstate/amd-pstate in active mode the hardware already scales quickly, so differences are often within noise.",
		Category: CatCPU, Tier: TierAdvanced, Risk: RiskMedium, Scope: ScopeUntilBoot,
		NeedsAuth: true, ParamName: "governor",
		Options: []Option{
			{"performance", "performance", "Keep clocks high."},
			{"powersave", "powersave", "Favour low power."},
			{"schedutil", "schedutil", "Scheduler-driven scaling."},
			{"ondemand", "ondemand", "Scale with load."},
			{"conservative", "conservative", "Scale gradually."},
		},
	}
}

const cpuRoot = "/sys/devices/system/cpu"

var cpuDirRe = regexp.MustCompile(`^cpu([0-9]+)$`)

func cpuIndexes(env Env) []int {
	ents, err := env.FS.ReadDir(cpuRoot)
	if err != nil {
		return nil
	}
	var idx []int
	for _, e := range ents {
		if m := cpuDirRe.FindStringSubmatch(e.Name()); m != nil {
			n, _ := strconv.Atoi(m[1])
			if _, ok := platform.ReadTrim(env.FS, govPath(n)); ok {
				idx = append(idx, n)
			}
		}
	}
	sort.Ints(idx)
	return idx
}

func govPath(n int) string {
	return path.Join(cpuRoot, fmt.Sprintf("cpu%d", n), "cpufreq", "scaling_governor")
}

func govKey(n int) string { return fmt.Sprintf("cpu%d.governor", n) }

func (CPUPolicy) Assess(_ context.Context, env Env) Assessment {
	if len(env.Report.CPU.Governors) == 0 {
		return Assessment{Reason: "This system exposes no CPU frequency governors (virtual machine, or scaling is handled by firmware)."}
	}
	if m := activeManagers(env.Report, ""); len(m) > 0 {
		return Assessment{Reason: fmt.Sprintf("%s is managing CPU power policy and would override or fight a direct change. Use the Power profile option instead.", strings.Join(m, ", "))}
	}
	if env.Report.Power.Unknown {
		return Assessment{Reason: "Could not determine whether a power manager is running, so a direct CPU change is not safe to offer."}
	}
	if env.Priv == nil {
		return Assessment{Reason: "Privileged actions are unavailable."}
	}
	if ok, why := env.Priv.Available(); !ok {
		return Assessment{Reason: why}
	}
	idx := cpuIndexes(env)
	if len(idx) == 0 {
		return Assessment{Reason: "No per-CPU governor files found."}
	}
	cur := State{}
	for _, n := range idx {
		cur[govKey(n)], _ = platform.ReadTrim(env.FS, govPath(n))
	}
	var opts []Option
	for _, o := range (CPUPolicy{}).Meta().Options {
		for _, g := range env.Report.CPU.Governors {
			if g == o.Value {
				opts = append(opts, o)
			}
		}
	}
	return Assessment{Available: true, Current: cur, Options: plainOpts("cpu.governor", opts), Recommend: governorSuggestion(env, cur)}
}

func (CPUPolicy) Read(_ context.Context, env Env, want State) (State, error) {
	out := State{}
	for k := range want {
		var n int
		if _, err := fmt.Sscanf(k, "cpu%d.governor", &n); err != nil {
			return nil, fmt.Errorf("unexpected state key %q", k)
		}
		v, ok := platform.ReadTrim(env.FS, govPath(n))
		if !ok {
			return nil, fmt.Errorf("cpu%d governor is unreadable (CPU went offline?)", n)
		}
		out[k] = v
	}
	return out, nil
}

func (c CPUPolicy) Plan(ctx context.Context, env Env, params map[string]string) (Plan, error) {
	m := c.Meta()
	g := params["governor"]
	a := c.Assess(ctx, env)
	if !a.Available {
		return Plan{}, fmt.Errorf("%w: %s", ErrUnavailable, a.Reason)
	}
	if err := CheckParam(m, a.Options, g); err != nil {
		return Plan{}, err
	}
	after := State{}
	changed := 0
	groups := map[string]int{}
	for k, v := range a.Current {
		after[k] = g
		if v != g {
			changed++
			groups[v]++
		}
	}
	if changed == 0 {
		return Plan{}, ErrNoChange
	}
	var froms []string
	for v, n := range groups {
		froms = append(froms, fmt.Sprintf("%s ×%d", v, n))
	}
	sort.Strings(froms)
	return Plan{
		TweakID: m.ID, Params: map[string]string{"governor": g},
		Before: a.Current.Clone(), After: after, NeedsAuth: true,
		Changes: []Change{{fmt.Sprintf("CPU governor (%d threads)", len(after)), strings.Join(froms, ", "), g}},
		Warnings: []string{
			"Administrator permission is required.",
			"Resets at reboot. Higher heat/noise/idle power with 'performance'.",
		},
	}, nil
}

// write sets every CPU's governor under ONE administrator authorisation
// (a prompt per thread would mean dozens of password dialogs).
func (CPUPolicy) write(ctx context.Context, env Env, target State) error {
	if env.Priv == nil {
		return errors.New("no privilege executor configured")
	}
	var keys []string
	for k := range target {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var as []privilege.Action
	for _, k := range keys {
		var n int
		if _, err := fmt.Sscanf(k, "cpu%d.governor", &n); err != nil {
			return fmt.Errorf("unexpected state key %q", k)
		}
		as = append(as, privilege.Action{Op: privilege.OpWriteSysfs, Path: govPath(n), Value: target[k]})
	}
	return privilege.DoAll(ctx, env.Priv, as)
}

func (c CPUPolicy) Apply(ctx context.Context, env Env, p Plan) error {
	return c.write(ctx, env, p.After)
}

// Restore writes every CPU's original value (idempotent, safe after a partial Apply).
func (c CPUPolicy) Restore(ctx context.Context, env Env, p Plan) error {
	return c.write(ctx, env, p.Before)
}
