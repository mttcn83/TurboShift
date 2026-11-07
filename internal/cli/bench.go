package cli

import (
	"context"
	"fmt"
	"math"
	"os/exec"
	"strings"
	"syscall"

	"github.com/mttcn83/turboshift/internal/app"
	"github.com/mttcn83/turboshift/internal/benchmark"
	"github.com/mttcn83/turboshift/internal/integrations"
)

func cmdBenchmark(ctx context.Context, a *app.App, args []string, o *out) int {
	if len(args) == 0 {
		return usageErr(o, "benchmark needs a subcommand: import | list | compare | find")
	}
	switch args[0] {
	case "import":
		fs := newFlags("benchmark import", o)
		label := fs.String("label", "", "configuration this run belongs to, e.g. \"balanced\" or \"performance\" (required)")
		ts := fs.Float64("trim-start", 0, "seconds of warm-up to drop from the start")
		te := fs.Float64("trim-end", 0, "seconds to drop from the end")
		pos, err := parse(fs, args[1:])
		if err != nil || len(pos) == 0 || *label == "" {
			return usageErr(o, "usage: turbo benchmark import <log.csv>... --label NAME [--trim-start S] [--trim-end S]")
		}
		code := ExitOK
		for _, p := range pos {
			r, err := a.Bench.Import(a.FS, p, *label, benchmark.ImportOptions{TrimStartS: *ts, TrimEndS: *te})
			if err != nil {
				o.printf("  %s %v\n", o.red("FAILED"), err)
				code = ExitError
				continue
			}
			o.printf("  %s %-30s %7.1f avg fps · %6.1f 1%% low · %5.2f ms p99 · %d frames\n", o.green("OK"), r.Source, r.AvgFPS, r.Low1FPS, r.FrameP99Ms, r.Frames)
			if r.Bottleneck != "" {
				o.printf("     %s\n", o.dim(r.Bottleneck))
			}
		}
		return code

	case "list":
		fs := newFlags("benchmark list", o)
		asJSON := fs.Bool("json", false, "machine-readable output")
		if _, err := parse(fs, args[1:]); err != nil {
			return ExitUsage
		}
		runs, err := a.Bench.List()
		if err != nil {
			return fail(o, err)
		}
		if *asJSON {
			if runs == nil {
				runs = []benchmark.Run{}
			}
			return o.json(jsonDoc("runs", runs))
		}
		o.heading("benchmark runs")
		if len(runs) == 0 {
			o.println("  No runs yet. Record with MangoHud, then `turbo benchmark import <log.csv> --label NAME`.")
		}
		by := benchmark.ByLabel(runs)
		for _, l := range benchmark.Labels(runs) {
			o.printf("  %s  (%d runs)\n", o.bold(l), len(by[l]))
			for _, r := range by[l] {
				o.printf("    %s  %-28s %7.1f avg · %6.1f 1%% low\n", r.StartedAt.Local().Format("01-02 15:04"), r.Source, r.AvgFPS, r.Low1FPS)
			}
		}
		o.println()
		return ExitOK

	case "find":
		fs := a.FS
		logs := benchmark.FindLogs(fs, a.LogDirs())
		if len(logs) == 0 {
			o.println("  No MangoHud logs found. MangoHud only writes logs when `output_folder` is set (see `turbo game options --log 60`).")
		}
		for _, l := range logs {
			o.println(" ", l)
		}
		return ExitOK

	case "compare":
		fs := newFlags("benchmark compare", o)
		asJSON := fs.Bool("json", false, "machine-readable output")
		pos, err := parse(fs, args[1:])
		if err != nil || len(pos) != 2 {
			return usageErr(o, "usage: turbo benchmark compare <labelA> <labelB> [--json]")
		}
		runs, err := a.Bench.List()
		if err != nil {
			return fail(o, err)
		}
		by := benchmark.ByLabel(runs)
		for _, l := range pos {
			if len(by[l]) == 0 {
				return usageErr(o, "no runs labelled %q (known: %s)", l, strings.Join(benchmark.Labels(runs), ", "))
			}
		}
		c := benchmark.Compare(pos[0], by[pos[0]], pos[1], by[pos[1]])
		if *asJSON {
			type mr struct {
				Metric   string   `json:"metric"`
				Unit     string   `json:"unit"`
				Verdict  string   `json:"verdict"`
				Reason   string   `json:"reason"`
				NA       int      `json:"n_a"`
				NB       int      `json:"n_b"`
				MedianA  float64  `json:"median_a"`
				MedianB  float64  `json:"median_b"`
				DeltaPct float64  `json:"delta_pct"`
				P        *float64 `json:"p"`
			}
			var rs []mr
			for _, r := range c.Results {
				var p *float64
				if !math.IsNaN(r.P) {
					p = &r.P
				}
				rs = append(rs, mr{r.Metric.Name, r.Metric.Unit, string(r.Verdict), r.Reason, r.NA, r.NB, r.MedianA, r.MedianB, r.DeltaPct, p})
			}
			v, why := c.Overall()
			return o.json(map[string]any{"schema_version": schemaVersion, "a": pos[0], "b": pos[1], "overall": string(v), "summary": why, "metrics": rs, "warnings": c.Warnings})
		}
		o.heading(fmt.Sprintf("%s vs %s", pos[0], pos[1]))
		for _, r := range c.Results {
			o.printf("  %-22s %s %.1f → %s %.1f %s   %+.1f%%\n", r.Metric.Name, pos[0], r.MedianA, pos[1], r.MedianB, r.Metric.Unit, r.DeltaPct)
			o.printf("      runs %d vs %d · spread A %.1f–%.1f, B %.1f–%.1f · run-to-run CV %.1f%% / %.1f%%\n",
				r.NA, r.NB, r.MinA, r.MaxA, r.MinB, r.MaxB, r.CVA*100, r.CVB*100)
			tag := o.yellow
			switch r.Verdict {
			case benchmark.VerdictImproved:
				tag = o.green
			case benchmark.VerdictRegressed:
				tag = o.red
			}
			o.printf("      %s — %s\n", tag(string(r.Verdict)), wrap(r.Reason, 72, "        "))
		}
		v, _ := c.Overall()
		o.printf("\n  %s %s\n", o.bold("Overall:"), string(v))
		for _, w := range c.Warnings {
			o.printf("  %s %s\n", o.yellow("!"), wrap(w, 74, "    "))
		}
		o.println()
		return ExitOK
	}
	return usageErr(o, "unknown benchmark subcommand %q", args[0])
}

func gameFlags(a *app.App, o *out, name string, args []string) (integrations.LaunchSpec, []string, bool) {
	fs := newFlags(name, o)
	gm := fs.Bool("gamemode", false, "launch through gamemoderun")
	mh := fs.Bool("mangohud", false, "launch through mangohud")
	logS := fs.Int("log", 0, "have MangoHud record a log of this many seconds (starts 10 s after launch)")
	gpu := fs.String("gpu", "", "render on the discrete GPU: nvidia | mesa")
	pos, err := parse(fs, args)
	if err != nil {
		return integrations.LaunchSpec{}, nil, false
	}
	spec := integrations.LaunchSpec{GameMode: *gm, MangoHud: *mh || *logS > 0, LogSeconds: *logS, GPU: integrations.GPU(*gpu)}
	if *logS > 0 {
		spec.LogDir = a.MangoLogDir()
	}
	if err := spec.Validate(); err != nil {
		usageErr(o, "%v", err)
		return spec, nil, false
	}
	return spec, pos, true
}

func cmdGame(ctx context.Context, a *app.App, args []string, o *out) int {
	if len(args) == 0 {
		return usageErr(o, "game needs a subcommand: options | run")
	}
	spec, rest, ok := gameFlags(a, o, "game "+args[0], args[1:])
	if !ok {
		return ExitUsage
	}
	switch args[0] {
	case "options":
		o.println(spec.Steam())
		if spec.GameMode || spec.MangoHud {
			fmt.Fprintln(o.err, "\n"+wrap(integrations.OverlayNotice, 76, ""))
		}
		return ExitOK
	case "run":
		if len(rest) == 0 {
			return usageErr(o, "usage: turbo game run [options] -- <command> [args...]")
		}
		argv := append(spec.Wrappers(), rest...)
		for _, w := range spec.Wrappers() {
			if !a.Run.Has(w) {
				return fail(o, fmt.Errorf("%s is not installed; install it from your distribution or drop it from the options", w))
			}
		}
		bin, err := exec.LookPath(argv[0])
		if err != nil {
			return fail(o, fmt.Errorf("%s: command not found", argv[0]))
		}
		if spec.LogSeconds > 0 {
			_ = mkdir(spec.LogDir)
		}
		env := append(osEnviron(), spec.Env()...)
		// Replace this process: signals, exit status and the controlling
		// terminal go straight to the game, and TurboShift changes nothing system-wide.
		if err := syscall.Exec(bin, argv, env); err != nil {
			return fail(o, err)
		}
	}
	return usageErr(o, "unknown game subcommand %q", args[0])
}
