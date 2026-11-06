package tweaks

import (
	"context"
	"fmt"
	"strings"
)

// PowerProfile switches the desktop power profile through its owner,
// power-profiles-daemon. TurboShift never writes CPU sysfs directly here:
// the daemon owns that, and two writers would fight.
type PowerProfile struct{}

const ppdName = "power-profiles-daemon"

var ppdOptions = []Option{
	{"performance", "Performance", "Highest clocks and fastest response; more heat, fan noise and battery drain."},
	{"balanced", "Balanced", "The distribution default for most work."},
	{"power-saver", "Power saver", "Lowest power; can noticeably limit performance."},
}

func (PowerProfile) Meta() Meta {
	return Meta{
		ID:       "power.profile",
		Name:     "Performance / balanced / power-saver mode",
		Summary:  "Switch the whole system between speed and battery saving with one setting.",
		Details:  "Changes the active profile in power-profiles-daemon, the service behind your desktop's power menu. The daemon then adjusts how eagerly the CPU speeds up and, on supported laptops, the firmware's fan and power limits. TurboShift only asks the daemon; it never writes CPU files itself.",
		Helps:    "Laptops and PCs that were left in Power saver while plugged in, or that you want in Performance for a gaming session.",
		Tradeoff: "Performance raises power draw, temperature and fan noise, and drains the battery faster. It only helps when the CPU, not the GPU or network, is the limit — a GPU-bound game may show no FPS change at all.",
		Evidence: "Compare frame times with MangoHud logs in Benchmark Lab (≥4 runs per profile). Expect little change in GPU-bound games.",
		Category: CatCPU, Tier: TierRecommended, Risk: RiskLow, Scope: ScopePersistent,
		ParamName: "profile", Options: ppdOptions,
	}
}

func (p PowerProfile) Assess(ctx context.Context, env Env) Assessment {
	if !env.Run.Has("powerprofilesctl") {
		return Assessment{Reason: "power-profiles-daemon is not installed (powerprofilesctl not found)."}
	}
	if other := activeManagers(env.Report, ppdName); len(other) > 0 {
		return Assessment{Reason: fmt.Sprintf("%s is also managing power policy. Using two managers makes them overwrite each other, so TurboShift leaves this alone.", strings.Join(other, ", "))}
	}
	cur, err := p.Read(ctx, env, State{"profile": ""})
	if err != nil {
		return Assessment{Reason: "power-profiles-daemon is not responding: " + err.Error()}
	}
	a := Assessment{Available: true, Current: cur, Options: p.options(env)}
	if on := env.Report.Power.OnAC; on != nil && *on && cur["profile"] == "power-saver" {
		a.Recommend = &Recommendation{
			Params: map[string]string{"profile": "balanced"},
			Why:    "You are plugged in but the power-saver profile is active, which caps performance for no battery benefit.",
		}
	}
	return a
}

// options narrows the choices to the profiles this machine actually offers
// (some laptops have no "performance").
func (PowerProfile) options(env Env) []Option {
	have := env.Report.Power.PPDProfiles
	if len(have) == 0 {
		return ppdOptions
	}
	var out []Option
	for _, o := range ppdOptions {
		for _, h := range have {
			if h == o.Value {
				out = append(out, o)
			}
		}
	}
	return out
}

func (PowerProfile) Read(ctx context.Context, env Env, _ State) (State, error) {
	res, err := env.Run.Run(ctx, "powerprofilesctl", "get")
	if err != nil {
		return nil, err
	}
	return State{"profile": strings.TrimSpace(res.Stdout)}, nil
}

func (p PowerProfile) Plan(ctx context.Context, env Env, params map[string]string) (Plan, error) {
	m := p.Meta()
	want := params["profile"]
	if err := CheckParam(m, p.options(env), want); err != nil {
		return Plan{}, err
	}
	a := p.Assess(ctx, env)
	if !a.Available {
		return Plan{}, fmt.Errorf("%w: %s", ErrUnavailable, a.Reason)
	}
	if a.Current["profile"] == want {
		return Plan{}, ErrNoChange
	}
	pl := Plan{
		TweakID: m.ID, Params: map[string]string{"profile": want},
		Before: a.Current.Clone(), After: State{"profile": want},
		Changes: []Change{{"Power profile", a.Current["profile"], want}},
	}
	if want == "performance" {
		pl.Warnings = append(pl.Warnings, "Higher power draw and fan noise. If you are on battery it will drain faster.")
		if on := env.Report.Power.OnAC; on != nil && !*on {
			pl.Warnings = append(pl.Warnings, "You are currently on battery.")
		}
	}
	return pl, nil
}

func (PowerProfile) Apply(ctx context.Context, env Env, p Plan) error {
	if err := safeToken("power profile", p.After["profile"]); err != nil {
		return err
	}
	_, err := env.Run.Run(ctx, "powerprofilesctl", "set", p.After["profile"])
	return err
}

func (PowerProfile) Restore(ctx context.Context, env Env, p Plan) error {
	if err := safeToken("power profile", p.Before["profile"]); err != nil {
		return err
	}
	_, err := env.Run.Run(ctx, "powerprofilesctl", "set", p.Before["profile"])
	return err
}
