// Command turbo is TurboShift: a local-only, reversible Linux optimizer.
// With no arguments it opens the interactive TUI.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/mttcn83/turboshift/internal/app"
	"github.com/mttcn83/turboshift/internal/cli"
	"github.com/mttcn83/turboshift/internal/tui"
)

func main() { os.Exit(run()) }

func run() int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	a, err := app.New()
	if err != nil {
		fmt.Fprintln(os.Stderr, "turbo:", err)
		return 1
	}
	args := os.Args[1:]
	if len(args) == 0 {
		if !isTerminal(os.Stdin) || !isTerminal(os.Stdout) {
			// Piped or redirected: the TUI cannot run, so show help instead.
			return cli.Run(ctx, a, nil, os.Stdout, os.Stderr)
		}
		if err := tui.Run(ctx, a); err != nil {
			fmt.Fprintln(os.Stderr, "turbo:", err)
			return 1
		}
		return 0
	}
	return cli.Run(ctx, a, args, os.Stdout, os.Stderr)
}

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}
