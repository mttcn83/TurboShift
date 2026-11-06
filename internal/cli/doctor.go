package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/mttcn83/turboshift/internal/app"
	"github.com/mttcn83/turboshift/internal/detect"
	"github.com/mttcn83/turboshift/internal/diagnostics"
)

type doctorJSON struct {
	SchemaVersion int                   `json:"schema_version"`
	Report        detect.Report         `json:"report"`
	Findings      []diagnostics.Finding `json:"findings"`
	Top           []diagnostics.Proc    `json:"top_processes,omitempty"`
}

func cmdDoctor(ctx context.Context, a *app.App, args []string, o *out) int {
	fs := newFlags("doctor", o)
	asJSON := fs.Bool("json", false, "machine-readable output")
	top := fs.Bool("top", false, "also sample the heaviest processes for ~1 s")
	if _, err := parse(fs, args); err != nil {
		return ExitUsage
	}
	rep := a.Detect(ctx)
	findings := diagnostics.Diagnose(rep)
	var procs []diagnostics.Proc
	if *top {
		procs = diagnostics.TopProcesses(ctx, a.FS, 5, time.Second)
	}
	if *asJSON {
		return o.json(doctorJSON{schemaVersion, rep, findings, procs})
	}

	o.heading("doctor")
	row := func(k, v string) {
		if v != "" {
			o.printf("  %-8s %s\n", o.dim(k), v)
		}
	}
	s := rep.System
	row("System", strings.TrimSpace(fmt.Sprintf("%s · kernel %s · %s", s.Distro, s.Kernel, s.SessionType)))
	row("CPU", cpuLine(rep.CPU))
	row("Memory", memLine(rep.Memory))
	for _, g := range rep.GPUs {
		n := g.Name
		if n == "" {
			n = g.Vendor + " " + g.PCIID
		}
		row("GPU", fmt.Sprintf("%s · %s", n, orDash(g.Driver)))
	}
	row("Power", powerLine(rep.Power))
	if rep.Network.DefaultIface != "" {
		row("Network", fmt.Sprintf("%s via %s", rep.Network.DefaultIface, rep.Network.Gateway))
	}

	o.println()
	for _, f := range findings {
		o.printf("  %s %-8s %s\n", o.sev(f.Sev), o.dim(f.Area), f.Title)
		if f.Sev >= diagnostics.SevNotice && f.Detail != "" {
			o.printf("%s\n", o.dim("            "+wrap(f.Detail, 72, "            ")))
		}
		if f.Fix != nil {
			o.printf("            fix: turbo apply %s=%s\n", f.Fix.TweakID, f.Fix.Params[paramOf(a, f.Fix.TweakID)])
		}
	}
	if len(procs) > 0 {
		o.printf("\n  %s\n", o.bold("Top processes (CPU % of one core, last 1 s)"))
		for _, p := range procs {
			o.printf("  %6.1f%%  %7.0f MiB  %s (pid %d)\n", p.CPUPct, p.RSSMiB, p.Name, p.PID)
		}
	}
	w, n := diagnostics.Counts(findings)
	o.printf("\n  %d warnings · %d notices", w, n)
	if len(rep.Warnings) > 0 {
		o.printf(" · %d detection limits (see --json)", len(rep.Warnings))
	}
	o.printf("\n  %s\n\n", o.dim("Read-only scan: nothing was changed. Run `turbo` for guided, reversible fixes."))
	return ExitOK
}

func paramOf(a *app.App, id string) string {
	if t, ok := a.Reg.Get(id); ok {
		return t.Meta().ParamName
	}
	return ""
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func cpuLine(c detect.CPU) string {
	s := fmt.Sprintf("%s · %d threads", c.Model, c.Threads)
	if c.Driver != "" {
		s += fmt.Sprintf(" · %s/%s", c.Driver, c.Governor)
	}
	if c.EPP != "" {
		s += " · EPP " + c.EPP
	}
	if c.TempC != nil {
		s += fmt.Sprintf(" · %.0f°C", *c.TempC)
	}
	return s
}

func memLine(m detect.Memory) string {
	if m.TotalKB == 0 {
		return ""
	}
	s := fmt.Sprintf("%.1f GiB, %.1f available", float64(m.TotalKB)/1048576, float64(m.AvailableKB)/1048576)
	if m.ZramDevices > 0 {
		s += " · zram"
	}
	return s
}

func powerLine(p detect.Power) string {
	var parts []string
	switch {
	case p.OnAC != nil && *p.OnAC:
		parts = append(parts, "AC")
	case p.OnAC != nil:
		parts = append(parts, "battery")
	}
	if p.Battery != nil {
		parts = append(parts, fmt.Sprintf("%d%% %s", p.Battery.Percent, strings.ToLower(p.Battery.Status)))
	}
	if p.PPDProfile != "" {
		parts = append(parts, "profile "+p.PPDProfile)
	}
	for _, m := range p.Managers {
		if m.Active {
			parts = append(parts, m.Name)
		}
	}
	return strings.Join(parts, " · ")
}

func cmdStatus(ctx context.Context, a *app.App, args []string, o *out) int {
	fs := newFlags("status", o)
	asJSON := fs.Bool("json", false, "machine-readable output")
	if _, err := parse(fs, args); err != nil {
		return ExitUsage
	}
	rep := a.Detect(ctx)
	snaps, corrupt, _ := a.Store.List()
	pend, _ := a.Store.Interrupted()
	findings := diagnostics.Diagnose(rep)
	w, n := diagnostics.Counts(findings)
	if *asJSON {
		return o.json(map[string]any{
			"schema_version": schemaVersion, "warnings": w, "notices": n, "snapshots": len(snaps), "interrupted": len(pend), "corrupt_snapshots": len(corrupt),
		})
	}
	o.heading("status")
	o.printf("  %-12s %d warnings · %d notices\n", "Health", w, n)
	o.printf("  %-12s %d restore points\n", "Vault", len(snaps))
	if len(snaps) > 0 {
		o.printf("  %-12s %s  (%s)\n", "Latest", snaps[0].ID, snaps[0].Status)
	}
	if len(pend) > 0 {
		o.printf("  %s\n", o.red(fmt.Sprintf("%d interrupted transaction(s): run `turbo snapshots recover`", len(pend))))
		return ExitPartial
	}
	if len(corrupt) > 0 {
		o.printf("  %s\n", o.yellow(fmt.Sprintf("%d restore point file(s) failed their integrity check", len(corrupt))))
	}
	o.println()
	return ExitOK
}
