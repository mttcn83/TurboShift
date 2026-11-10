package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/mttcn83/turboshift/internal/app"
	"github.com/mttcn83/turboshift/internal/engine"
)

// cmdCheck reports whether the settings TurboShift applied are still in
// effect, and with --reapply puts back the ones a restart reset.
func cmdCheck(ctx context.Context, a *app.App, args []string, o *out) int {
	fs := newFlags("check", o)
	asJSON := fs.Bool("json", false, "machine-readable output")
	reapply := fs.Bool("reapply", false, "re-apply settings that were reset (e.g. by a restart)")
	yes := fs.Bool("yes", false, "with --reapply: skip the confirmation prompt")
	if pos, err := parse(fs, args); err != nil {
		return ExitUsage
	} else if len(pos) > 0 {
		return usageErr(o, "usage: turbo check [--json] [--reapply [--yes]]")
	}
	eng := a.Engine(a.Detect(ctx))
	res, err := eng.Check(ctx)
	if err != nil {
		fmt.Fprintf(o.err, "turbo: %v\n", err)
		return ExitError
	}
	var reset []engine.CheckResult
	notHolding := 0
	for _, r := range res {
		if r.State != engine.CheckHolding {
			notHolding++
		}
		if r.State == engine.CheckReset {
			reset = append(reset, r)
		}
	}
	if *asJSON {
		items := []map[string]any{}
		for _, r := range res {
			p := r.Params
			if p == nil {
				p = map[string]string{}
			}
			items = append(items, map[string]any{"tweak_id": r.TweakID, "name": r.Name, "snapshot_id": r.SnapshotID, "params": p, "state": string(r.State), "note": r.Note})
		}
		if code := o.json(map[string]any{"schema_version": schemaVersion, "settings": items, "not_holding": notHolding}); code != ExitOK {
			return code
		}
		if notHolding > 0 && !*reapply {
			return ExitPartial
		}
		if !*reapply || len(reset) == 0 {
			return ExitOK
		}
	} else {
		o.heading("check")
		if len(res) == 0 {
			o.println("  Nothing applied by TurboShift is in effect, so there is nothing to check.")
			o.println()
			return ExitOK
		}
		for _, r := range res {
			mark := o.green("OK     ")
			switch r.State {
			case engine.CheckReset:
				mark = o.yellow("RESET  ")
			case engine.CheckChanged:
				mark = o.red("CHANGED")
			case engine.CheckUnknown:
				mark = o.dim("UNKNOWN")
			}
			line := fmt.Sprintf("  %s %-22s %s", mark, r.TweakID, r.Name)
			if r.Note != "" {
				line += "  " + o.dim(r.Note)
			}
			o.println(line)
		}
		o.println()
		if notHolding == 0 {
			o.printf("  %s All %d applied setting(s) are still in effect.\n\n", o.green("OK"), len(res))
			return ExitOK
		}
		if len(reset) > 0 && !*reapply {
			o.printf("  %d setting(s) were reset. Put them back with: turbo check --reapply\n\n", len(reset))
		}
	}
	if !*reapply {
		return ExitPartial
	}
	if len(reset) == 0 {
		return ExitPartial
	}
	reqs := make([]engine.Request, 0, len(reset))
	var ids []string
	for _, r := range reset {
		reqs = append(reqs, engine.Request{TweakID: r.TweakID, Params: r.Params})
		ids = append(ids, r.TweakID)
	}
	if !*yes && !*asJSON {
		o.printf("  Re-apply %s and create a restore point? [y/N] ", strings.Join(ids, ", "))
		var ans string
		_, _ = fmt.Fscanln(stdinReader(), &ans)
		if !strings.EqualFold(strings.TrimSpace(ans), "y") && !strings.EqualFold(strings.TrimSpace(ans), "yes") {
			o.println("  Cancelled. Nothing was changed.")
			return ExitOK
		}
	} else if !*yes {
		fmt.Fprintln(o.err, "turbo: --reapply with --json needs --yes")
		return ExitUsage
	}
	snap, err := eng.Apply(ctx, reqs, "reapply", func(e engine.Event) {
		if !*asJSON {
			o.printf("  %s %s\n", o.dim(fmt.Sprintf("%-8s", e.Phase)), e.Message)
		}
	})
	if err != nil {
		fmt.Fprintf(o.err, "turbo: re-apply failed: %v\n", err)
		return ExitPartial
	}
	if !*asJSON {
		o.printf("\n  %s Restore point %s\n\n", o.green("DONE"), snap.ID)
	}
	if notHolding > len(reset) {
		return ExitPartial
	}
	return ExitOK
}
