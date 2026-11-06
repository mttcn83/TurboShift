package cli

import (
	"context"
	"fmt"

	"github.com/mttcn83/turboshift/internal/app"
	"github.com/mttcn83/turboshift/internal/tweaks"
)

func cmdPreset(ctx context.Context, a *app.App, args []string, o *out) int {
	if len(args) == 0 {
		return usageErr(o, "usage: turbo preset list | show <id> | apply <id> [--dry-run] [--yes]")
	}
	switch args[0] {
	case "list":
		fs := newFlags("preset list", o)
		asJSON := fs.Bool("json", false, "machine-readable output")
		if _, err := parse(fs, args[1:]); err != nil {
			return ExitUsage
		}
		if *asJSON {
			type row struct {
				ID      string              `json:"id"`
				Name    string              `json:"name"`
				Summary string              `json:"summary"`
				Best    string              `json:"best"`
				Cost    string              `json:"cost"`
				Items   []tweaks.PresetItem `json:"items"`
			}
			rows := []row{}
			for _, p := range tweaks.Presets() {
				rows = append(rows, row{p.ID, p.Name, p.Summary, p.Best, p.Cost, p.Items})
			}
			return o.json(jsonDoc("presets", rows))
		}
		o.heading("presets")
		for _, p := range tweaks.Presets() {
			o.printf("  %s  %s\n", o.accent(fmt.Sprintf("%-8s", p.ID)), o.bold(p.Name))
			o.printf("            %s\n", wrap(p.Summary, 62, "            "))
		}
		o.printf("\n  %s\n\n", o.dim("turbo preset show <id> lists what each would change on this PC."))
		return ExitOK

	case "show", "apply":
		fs := newFlags("preset "+args[0], o)
		dry := fs.Bool("dry-run", false, "show the exact changes without applying")
		yes := fs.Bool("yes", false, "skip the confirmation prompt")
		pos, err := parse(fs, args[1:])
		if err != nil {
			return ExitUsage
		}
		if len(pos) != 1 {
			return usageErr(o, "usage: turbo preset %s <id>  (see `turbo preset list`)", args[0])
		}
		p, ok := tweaks.PresetByID(pos[0])
		if !ok {
			return usageErr(o, "unknown preset %q (see `turbo preset list`)", pos[0])
		}
		eng := a.Engine(a.Detect(ctx))
		reqs, skipped := eng.ResolvePreset(ctx, p)
		o.heading("preset " + p.ID)
		f := func(k, v string) { o.printf("  %-9s %s\n", o.dim(k), wrap(v, 66, "            ")) }
		f("Name", p.Name)
		f("What", p.Summary)
		f("Best for", p.Best)
		f("Cost", p.Cost)
		o.println()
		for _, r := range reqs {
			t, _ := a.Reg.Get(r.TweakID)
			o.printf("  %s %-26s %s\n", o.green("APPLY"), r.TweakID, o.dim(t.Meta().Name))
		}
		for _, s := range skipped {
			label := "SKIP "
			if s.Already {
				label = "OK   "
			}
			o.printf("  %s %-26s %s\n", o.dim(label), s.TweakID, o.dim(wrap(s.Reason, 40, "                                  ")))
		}
		o.println()
		if args[0] == "show" {
			o.printf("  %d of %d settings apply on this PC.\n\n", len(reqs), len(p.Items))
			return ExitOK
		}
		if len(reqs) == 0 {
			o.printf("  Nothing to do: every setting in this preset is already set or unavailable here.\n\n")
			return ExitOK
		}
		var applyArgs []string
		for _, r := range reqs {
			t, _ := a.Reg.Get(r.TweakID)
			applyArgs = append(applyArgs, r.TweakID+"="+r.Params[t.Meta().ParamName])
		}
		if *dry {
			applyArgs = append(applyArgs, "--dry-run")
		}
		if *yes {
			applyArgs = append(applyArgs, "--yes")
		}
		applyArgs = append(applyArgs, "--label", "preset:"+p.ID)
		return cmdApply(ctx, a, applyArgs, o)
	}
	return usageErr(o, "unknown preset subcommand %q", args[0])
}
