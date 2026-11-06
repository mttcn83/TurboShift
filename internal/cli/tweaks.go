package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/mttcn83/turboshift/internal/app"
	"github.com/mttcn83/turboshift/internal/engine"
	"github.com/mttcn83/turboshift/internal/snapshot"
	"github.com/mttcn83/turboshift/internal/tweaks"
)

func cmdTweaks(ctx context.Context, a *app.App, args []string, o *out) int {
	if len(args) == 0 {
		return usageErr(o, "tweaks needs a subcommand: list | show <id>")
	}
	rep := a.Detect(ctx)
	env := a.Env(rep)
	switch args[0] {
	case "list":
		fs := newFlags("tweaks list", o)
		asJSON := fs.Bool("json", false, "machine-readable output (always includes everything)")
		all := fs.Bool("all", false, "also list tweaks that are not available on this PC, with the reason")
		if _, err := parse(fs, args[1:]); err != nil {
			return ExitUsage
		}
		type row struct {
			ID, Name, Category, Tier, Risk, Scope string
			Available                             bool
			Current                               tweaks.State
			Reason                                string
			Recommended                           *tweaks.Recommendation
		}
		var rows []row
		for _, t := range a.Reg.All() {
			m, as := t.Meta(), t.Assess(ctx, env)
			rows = append(rows, row{m.ID, m.Name, string(m.Category), string(m.Tier), m.Risk.String(), string(m.Scope), as.Available, as.Current, as.Reason, as.Recommend})
		}
		if *asJSON {
			return o.json(rows)
		}
		o.heading("tweaks")
		cat, hidden := "", 0
		for _, r := range rows {
			if !r.Available && !*all {
				hidden++
				continue
			}
			if r.Category != cat {
				cat = r.Category
				o.printf("\n  %s\n", o.bold(cat))
			}
			state := o.green("available  ")
			if !r.Available {
				state = o.dim("unavailable")
			}
			o.printf("  %s  %-27s %-12s risk %-6s %s\n", state, o.bold(r.ID), r.Tier, r.Risk, r.Name)
			if !r.Available {
				o.printf("               %s\n", o.dim(wrap(r.Reason, 70, "               ")))
			} else if r.Recommended != nil {
				o.printf("               %s\n", o.yellow("suggested: "+wrap(r.Recommended.Why, 62, "               ")))
			}
		}
		if hidden > 0 {
			o.printf("\n  %s\n", o.dim(fmt.Sprintf("%d more are not available on this PC (hardware missing, a power manager owns the setting, or turbo is not installed system-wide). `turbo tweaks list --all` shows why.", hidden)))
		}
		o.println()
		return ExitOK

	case "show":
		if len(args) != 2 {
			return usageErr(o, "usage: turbo tweaks show <id>")
		}
		t, ok := a.Reg.Get(args[1])
		if !ok {
			return usageErr(o, "unknown tweak %q (see `turbo tweaks list`)", args[1])
		}
		m, as := t.Meta(), t.Assess(ctx, env)
		o.heading("tweak " + m.ID)
		f := func(k, v string) { o.printf("  %-11s %s\n", o.dim(k), wrap(v, 66, "              ")) }
		f("Name", m.Name)
		f("What", m.Summary)
		f("Details", m.Details)
		f("Helps", m.Helps)
		f("Trade-off", m.Tradeoff)
		f("Measure", m.Evidence)
		f("Tier/Risk", fmt.Sprintf("%s · risk %s · %s · %s", m.Tier, m.Risk, m.Scope, map[bool]string{true: "needs administrator permission", false: "no root needed"}[m.NeedsAuth]))
		if as.Available {
			f("Now", fmt.Sprint(as.Current))
			opts := as.Options
			if len(opts) == 0 {
				opts = m.Options
			}
			for _, op := range opts {
				o.printf("              %s=%s  %s\n", m.ParamName, o.bold(op.Value), o.dim(op.Note))
			}
		} else {
			f("Unavailable", as.Reason)
		}
		o.println()
		return ExitOK
	}
	return usageErr(o, "unknown tweaks subcommand %q", args[0])
}

func cmdApply(ctx context.Context, a *app.App, args []string, o *out) int {
	fs := newFlags("apply", o)
	dry := fs.Bool("dry-run", false, "show the exact changes without applying")
	yes := fs.Bool("yes", false, "skip the confirmation prompt")
	label := fs.String("label", "cli", "name for the restore point")
	pos, err := parse(fs, args)
	if err != nil {
		return ExitUsage
	}
	if len(pos) == 0 {
		return usageErr(o, "usage: turbo apply <tweak-id>=<value>... [--dry-run] [--yes]  (e.g. power.profile=performance)")
	}
	kv, order, err := kvArgs(pos)
	if err != nil {
		return usageErr(o, "%v", err)
	}
	var reqs []engine.Request
	for _, id := range order {
		t, ok := a.Reg.Get(id)
		if !ok {
			return usageErr(o, "unknown tweak %q", id)
		}
		reqs = append(reqs, engine.Request{TweakID: id, Params: map[string]string{t.Meta().ParamName: kv[id]}})
	}
	rep := a.Detect(ctx)
	eng := a.Engine(rep)
	pv := eng.Preview(ctx, reqs)

	o.heading("apply")
	for _, p := range pv.Problems {
		if errors.Is(p.Err, tweaks.ErrNoChange) {
			o.printf("  %s %s: already set — nothing to change\n", o.green("OK"), p.TweakID)
		} else {
			o.printf("  %s %s: %v\n", o.red("SKIP"), p.TweakID, p.Err)
		}
	}
	for _, p := range pv.Plans {
		t, _ := a.Reg.Get(p.TweakID)
		o.printf("  %s  %s\n", o.accent(p.TweakID), t.Meta().Name)
		for _, c := range p.Changes {
			o.printf("      %s: %s → %s\n", c.What, o.dim(c.From), o.bold(c.To))
		}
		for _, w := range p.Warnings {
			o.printf("      %s\n", o.yellow("! "+w))
		}
	}
	if len(pv.Plans) == 0 {
		o.println()
		for _, p := range pv.Problems {
			if !errors.Is(p.Err, tweaks.ErrNoChange) {
				return ExitError
			}
		}
		return ExitOK
	}
	if *dry {
		o.printf("\n  %s\n\n", o.dim("Dry run: nothing was changed."))
		return ExitOK
	}
	if len(pv.Problems) > 0 {
		for _, p := range pv.Problems {
			if !errors.Is(p.Err, tweaks.ErrNoChange) {
				o.printf("\n  Not applying: the request has problems (above). Fix them or drop them from the command.\n\n")
				return ExitError
			}
		}
	}
	if !*yes {
		o.printf("\n  Apply %d change(s) and create a restore point? [y/N] ", len(pv.Plans))
		var ans string
		_, _ = fmt.Fscanln(stdinReader(), &ans)
		if !strings.EqualFold(strings.TrimSpace(ans), "y") && !strings.EqualFold(strings.TrimSpace(ans), "yes") {
			o.println("  Cancelled. Nothing was changed.")
			return ExitOK
		}
	}
	snap, err := eng.Apply(ctx, reqs, *label, func(e engine.Event) { o.printf("  %s %s\n", o.dim(fmt.Sprintf("%-8s", e.Phase)), e.Message) })
	if err != nil {
		o.printf("\n  %s %v\n\n", o.red("FAILED"), err)
		var te *engine.TransactionError
		if errors.As(err, &te) && te.RolledBack {
			return ExitPartial
		}
		return ExitError
	}
	o.printf("\n  %s Restore point %s\n  Undo any time: turbo snapshots restore %s\n\n", o.green("DONE"), snap.ID, snap.ID)
	return ExitOK
}

func cmdSnapshots(ctx context.Context, a *app.App, args []string, o *out) int {
	if len(args) == 0 {
		args = []string{"list"}
	}
	switch args[0] {
	case "list":
		fs := newFlags("snapshots list", o)
		asJSON := fs.Bool("json", false, "machine-readable output")
		if _, err := parse(fs, args[1:]); err != nil {
			return ExitUsage
		}
		snaps, corrupt, err := a.Store.List()
		if err != nil {
			return fail(o, err)
		}
		if *asJSON {
			return o.json(map[string]any{"snapshots": snaps, "corrupt": corrupt})
		}
		o.heading("snapshots")
		if len(snaps) == 0 {
			o.println("  No restore points yet. One is created automatically before every change.")
		}
		for _, s := range snaps {
			var names []string
			for _, it := range s.Items {
				names = append(names, it.TweakID)
			}
			o.printf("  %s  %-15s %s  %s\n", s.ID, s.Status, s.CreatedAt.Local().Format("2006-01-02 15:04"), strings.Join(names, ", "))
		}
		for _, c := range corrupt {
			o.printf("  %s %s failed its integrity check and was ignored\n", o.red("CORRUPT"), c)
		}
		o.println()
		return ExitOK

	case "show":
		if len(args) != 2 {
			return usageErr(o, "usage: turbo snapshots show <id>")
		}
		s, err := a.Store.Load(args[1])
		if err != nil {
			return fail(o, err)
		}
		o.heading("snapshot " + s.ID)
		o.printf("  status %s · created %s · kernel %s\n\n", s.Status, s.CreatedAt.Local().Format("2006-01-02 15:04:05"), s.Kernel)
		for _, it := range s.Items {
			o.printf("  %s  [%s]\n", o.bold(it.TweakID), it.Status)
			for k, v := range it.Before {
				o.printf("      %-24s %s → %s\n", k, o.dim(v), it.After[k])
			}
			if it.Error != "" {
				o.printf("      %s\n", o.red(it.Error))
			}
		}
		o.println()
		for _, e := range s.Events {
			o.printf("  %s  %s\n", o.dim(e.Time.Local().Format("15:04:05")), e.Text)
		}
		o.println()
		return ExitOK

	case "restore":
		fs := newFlags("snapshots restore", o)
		force := fs.Bool("force", false, "also undo settings another program changed since")
		pos, err := parse(fs, args[1:])
		if err != nil || len(pos) != 1 {
			return usageErr(o, "usage: turbo snapshots restore <id> [--force]")
		}
		rep := a.Detect(ctx)
		res, err := a.Engine(rep).Restore(ctx, pos[0], engine.RestoreOptions{Force: *force}, func(e engine.Event) { o.printf("  %s\n", e.Message) })
		if err != nil {
			return fail(o, err)
		}
		code := ExitOK
		o.println()
		for _, r := range res {
			mark := o.green("OK     ")
			if r.Status == snapshot.ItemDrifted {
				mark, code = o.yellow("SKIPPED"), ExitPartial
			} else if r.Status == snapshot.ItemFailed {
				mark, code = o.red("FAILED "), ExitPartial
			}
			o.printf("  %s %-22s %s\n", mark, r.TweakID, r.Note)
		}
		if code == ExitPartial {
			o.printf("\n  Some items were left as they are. Use --force to overwrite settings another program changed.\n")
		}
		o.println()
		return code

	case "recover":
		rep := a.Detect(ctx)
		eng := a.Engine(rep)
		pend, err := eng.Interrupted()
		if err != nil {
			return fail(o, err)
		}
		if len(pend) == 0 {
			o.println("  Nothing to recover: no interrupted transactions.")
			return ExitOK
		}
		code := ExitOK
		for _, s := range pend {
			o.printf("  Recovering %s\n", s.ID)
			if err := eng.Recover(ctx, s.ID, func(e engine.Event) { o.printf("    %s\n", e.Message) }); err != nil {
				o.printf("  %s %v\n", o.red("FAILED"), err)
				code = ExitError
			}
		}
		return code
	}
	return usageErr(o, "unknown snapshots subcommand %q", args[0])
}

// cmdUndo restores the newest restore point that still has changes to undo.
func cmdUndo(ctx context.Context, a *app.App, args []string, o *out) int {
	fs := newFlags("undo", o)
	yes := fs.Bool("yes", false, "skip the confirmation prompt")
	if _, err := parse(fs, args); err != nil {
		return ExitUsage
	}
	snaps, _, err := a.Store.List()
	if err != nil {
		return fail(o, err)
	}
	var s *snapshot.Snapshot
	for _, c := range snaps {
		if c.Status == snapshot.StatusCommitted || c.Status == snapshot.StatusRestorePartial {
			s = c
			break
		}
	}
	if s == nil {
		o.println("  Nothing to undo: TurboShift has not changed anything yet (or everything was already undone).")
		return ExitOK
	}
	o.heading("undo")
	o.printf("  Last change: %s (%s)\n", o.bold(s.Label), s.CreatedAt.Local().Format("2006-01-02 15:04"))
	for _, it := range s.Items {
		o.printf("    %s  %s\n", o.accent(it.TweakID), o.dim(it.Name))
	}
	if !*yes {
		o.printf("\n  Put these back as they were? [y/N] ")
		var ans string
		_, _ = fmt.Fscanln(stdinReader(), &ans)
		if !strings.EqualFold(strings.TrimSpace(ans), "y") && !strings.EqualFold(strings.TrimSpace(ans), "yes") {
			o.println("  Cancelled. Nothing was changed.")
			return ExitOK
		}
	}
	return cmdSnapshots(ctx, a, []string{"restore", s.ID}, o)
}
