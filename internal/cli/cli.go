// Package cli implements the scriptable subcommands. They are secondary to
// the interactive TUI and exist for automation, SSH one-liners and bug reports.
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mttcn83/turboshift/internal/app"
	"github.com/mttcn83/turboshift/internal/privilege"
	"github.com/mttcn83/turboshift/internal/version"
)

// Exit codes (stable, documented in the README).
const (
	ExitOK      = 0
	ExitError   = 1 // operation failed
	ExitUsage   = 2 // bad arguments
	ExitPartial = 3 // transaction rolled back, or restore left items untouched
)

const usage = `TurboShift — a free, local-only Linux performance optimizer.

Run "turbo" with no arguments for the interactive app (recommended).

Usage: turbo [command] [options]

Diagnose (read-only, no root needed)
  doctor [--json] [--top]        scan this PC and explain what limits it
  network [--external] [...]     layer-by-layer network diagnosis
  status                         one-screen summary and pending recovery

Optimize (reversible; every change is journaled first)
  tweaks list                    what exists and whether it applies here
  tweaks show <id>               details, risks, how to measure it
  apply <id>=<value>... [--dry-run] [--yes]
  snapshots list | show <id> | restore <id> [--force] | recover

Measure
  benchmark import <log.csv> --label NAME [--trim-start S] [--trim-end S]
  benchmark list | compare <labelA> <labelB> [--json]
  benchmark find                 list MangoHud logs found on this PC

Games
  game options [--gamemode] [--mangohud] [--log SECONDS] [--gpu nvidia|mesa]
  game run [same options] -- <command> [args...]

Other
  --version, --help

Exit codes: 0 ok · 1 error · 2 usage · 3 rolled back / partially restored
Nothing is sent anywhere. See PRIVACY.md.
`

// Run executes one CLI invocation and returns the process exit code.
func Run(ctx context.Context, a *app.App, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stdout, usage)
		return ExitOK
	}
	cmd, rest := args[0], args[1:]
	o := newOut(stdout, stderr)
	switch cmd {
	case "-h", "--help", "help":
		fmt.Fprint(stdout, usage)
		return ExitOK
	case "-v", "--version", "version":
		fmt.Fprintf(stdout, "turbo %s\n", version.Version)
		return ExitOK
	case privilege.HelperCommand:
		return runHelper(ctx, a, rest, stderr)
	case "doctor", "scan":
		return cmdDoctor(ctx, a, rest, o)
	case "status":
		return cmdStatus(ctx, a, rest, o)
	case "tweaks":
		return cmdTweaks(ctx, a, rest, o)
	case "apply":
		return cmdApply(ctx, a, rest, o)
	case "snapshots", "snapshot":
		return cmdSnapshots(ctx, a, rest, o)
	case "network":
		return cmdNetwork(ctx, a, rest, o)
	case "benchmark", "bench":
		return cmdBenchmark(ctx, a, rest, o)
	case "game":
		return cmdGame(ctx, a, rest, o)
	}
	fmt.Fprintf(stderr, "turbo: unknown command %q\n\n", cmd)
	fmt.Fprint(stderr, usage)
	return ExitUsage
}

// runHelper is the root-side half of the privilege boundary (`turbo __priv`).
func runHelper(ctx context.Context, a *app.App, args []string, stderr io.Writer) int {
	err := privilege.RunHelper(ctx, a.FS, os.Geteuid(), args, func(s string) { fmt.Println(s) })
	if err != nil {
		fmt.Fprintln(stderr, err)
		return ExitError
	}
	return ExitOK
}

// newFlags makes a FlagSet that reports errors to stderr and never exits.
func newFlags(name string, o *out) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(o.err)
	return fs
}

// parse parses flags allowing them before or after positional arguments.
func parse(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return pos, nil
		}
		if args[0] == "--" {
			return append(pos, args[1:]...), nil
		}
		pos = append(pos, args[0])
		args = args[1:]
	}
}

func usageErr(o *out, format string, a ...any) int {
	fmt.Fprintf(o.err, "turbo: "+format+"\n", a...)
	return ExitUsage
}

func fail(o *out, err error) int {
	fmt.Fprintf(o.err, "turbo: %v\n", err)
	return ExitError
}

// kvArgs parses id=value arguments.
func kvArgs(args []string) (map[string]string, []string, error) {
	m := map[string]string{}
	var order []string
	for _, a := range args {
		k, v, ok := strings.Cut(a, "=")
		if !ok || k == "" || v == "" {
			return nil, nil, fmt.Errorf("expected <id>=<value>, got %q", a)
		}
		if _, dup := m[k]; dup {
			return nil, nil, fmt.Errorf("%s given twice", k)
		}
		m[k] = v
		order = append(order, k)
	}
	return m, order, nil
}

// stdinReader is replaced in tests.
var stdinReader = func() io.Reader { return os.Stdin }

func osEnviron() []string  { return os.Environ() }
func mkdir(p string) error { return os.MkdirAll(p, 0o700) }
