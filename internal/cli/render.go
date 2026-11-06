package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/mttcn83/turboshift/internal/diagnostics"
)

// out wraps stdout/stderr with optional ANSI colour. Colour is used only on a
// real terminal, never with NO_COLOR or TERM=dumb, and never in --json.
type out struct {
	w, err io.Writer
	color  bool
}

func newOut(stdout, stderr io.Writer) *out {
	o := &out{w: stdout, err: stderr}
	if f, ok := stdout.(*os.File); ok {
		if fi, err := f.Stat(); err == nil && fi.Mode()&os.ModeCharDevice != 0 {
			o.color = os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
		}
	}
	return o
}

func (o *out) paint(code, s string) string {
	if !o.color {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func (o *out) accent(s string) string { return o.paint("36;1", s) }
func (o *out) dim(s string) string    { return o.paint("2", s) }
func (o *out) bold(s string) string   { return o.paint("1", s) }
func (o *out) green(s string) string  { return o.paint("32", s) }
func (o *out) yellow(s string) string { return o.paint("33", s) }
func (o *out) red(s string) string    { return o.paint("31;1", s) }

func (o *out) printf(format string, a ...any) { fmt.Fprintf(o.w, format, a...) }
func (o *out) println(a ...any)               { fmt.Fprintln(o.w, a...) }

func (o *out) heading(title string) {
	o.printf("\n  %s  %s\n  %s\n", o.accent("TURBOSHIFT"), o.dim("/ "+title), o.dim(strings.Repeat("-", 54)))
}

// sev renders a severity as a fixed-width, colour-independent tag.
func (o *out) sev(s diagnostics.Severity) string {
	switch s {
	case diagnostics.SevWarn:
		return o.red("WARN  ")
	case diagnostics.SevNotice:
		return o.yellow("NOTICE")
	case diagnostics.SevInfo:
		return o.dim("INFO  ")
	}
	return o.green("OK    ")
}

func (o *out) json(v any) int {
	enc := json.NewEncoder(o.w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Fprintf(o.err, "turbo: %v\n", err)
		return ExitError
	}
	return ExitOK
}

// wrap word-wraps text to width with a hanging indent.
func wrap(text string, width int, indent string) string {
	var lines []string
	line := ""
	for _, w := range strings.Fields(text) {
		if line != "" && len(line)+1+len(w) > width {
			lines = append(lines, line)
			line = ""
		}
		if line != "" {
			line += " "
		}
		line += w
	}
	if line != "" {
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n"+indent)
}
