package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// All widgets work on slices of lines whose *visible* width is exact, so
// panes can be joined side by side without drifting. ANSI escapes are
// zero-width (ansi.StringWidth) and wide runes are counted correctly.

func vw(s string) int { return ansi.StringWidth(s) }

// fit truncates (with an ellipsis) or right-pads s to exactly w columns.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if n := vw(s); n > w {
		return ansi.Truncate(s, w, "…")
	} else if n < w {
		return s + strings.Repeat(" ", w-n)
	}
	return s
}

// wrapText word-wraps plain text to w columns.
func wrapText(s string, w int) []string {
	if w < 4 {
		w = 4
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		line := ""
		for _, word := range strings.Fields(para) {
			for vw(word) > w { // hard-split very long tokens (paths, URLs)
				if line != "" {
					out = append(out, line)
					line = ""
				}
				out = append(out, ansi.Truncate(word, w, ""))
				word = ansi.TruncateLeft(word, w, "")
			}
			if line != "" && vw(line)+1+vw(word) > w {
				out = append(out, line)
				line = ""
			}
			if line != "" {
				line += " "
			}
			line += word
		}
		out = append(out, line)
	}
	return out
}

// box draws a titled panel of exactly w×h cells (h<=0: as tall as content).
func (m *Model) box(title string, body []string, w, h int, focus bool) []string {
	g, th := m.gl, m.th
	if w < 6 {
		w = 6
	}
	inner := w - 4
	bc := th.Border
	if focus {
		bc = th.Accent
	}
	t := ""
	if title != "" {
		t = " " + th.Title(title) + " "
	}
	tw := vw(t)
	if tw > inner {
		t, tw = ansi.Truncate(t, inner, ""), inner
	}
	top := bc(g.TL+g.H) + t + bc(strings.Repeat(g.H, w-3-tw)+g.TR)
	rows := len(body)
	if h > 0 {
		rows = h - 2
	}
	out := make([]string, 0, rows+2)
	out = append(out, top)
	for i := 0; i < rows; i++ {
		line := ""
		if i < len(body) {
			line = body[i]
		}
		out = append(out, bc(g.V)+" "+fit(line, inner)+" "+bc(g.V))
	}
	out = append(out, bc(g.BL+strings.Repeat(g.H, w-2)+g.BR))
	return out
}

// joinCols places panes side by side with a gap, padding shorter panes.
func joinCols(gap int, cols ...[]string) []string {
	h := 0
	for _, c := range cols {
		h = max(h, len(c))
	}
	widths := make([]int, len(cols))
	for i, c := range cols {
		for _, l := range c {
			widths[i] = max(widths[i], vw(l))
		}
	}
	out := make([]string, h)
	for r := 0; r < h; r++ {
		var b strings.Builder
		for i, c := range cols {
			l := ""
			if r < len(c) {
				l = c[r]
			}
			b.WriteString(fit(l, widths[i]))
			if i < len(cols)-1 {
				b.WriteString(strings.Repeat(" ", gap))
			}
		}
		out[r] = b.String()
	}
	return out
}

// bar renders a horizontal gauge of width w for pct in 0..100.
func (m *Model) bar(pct float64, w int) string {
	if w < 2 {
		return ""
	}
	pct = max(0, min(100, pct))
	n := int(pct/100*float64(w) + 0.5)
	return m.th.BarFull(strings.Repeat(m.gl.Full, n)) + m.th.BarEmpty(strings.Repeat(m.gl.Empty, w-n))
}

// spark renders the last w samples scaled to [0,hi] (hi<=0: auto).
func (m *Model) spark(data []float64, w int, hi float64) string {
	if w < 1 {
		return ""
	}
	if len(data) > w {
		data = data[len(data)-w:]
	}
	if hi <= 0 {
		for _, v := range data {
			hi = max(hi, v)
		}
		if hi == 0 {
			hi = 1
		}
	}
	sp := m.gl.Spark
	var b strings.Builder
	b.WriteString(strings.Repeat(sp[0], w-len(data)))
	for _, v := range data {
		i := int(v / hi * float64(len(sp)-1))
		b.WriteString(sp[max(0, min(len(sp)-1, i))])
	}
	return m.th.Accent(b.String())
}

func (m *Model) key(k string) string { return m.th.Key(k) }

// hint builds a "key label" footer item.
func (m *Model) hint(k, label string) string { return m.key(k) + " " + m.th.Dim(label) }

// badge renders a short fixed label with severity semantics.
func (m *Model) risk(r string) string {
	switch r {
	case "high":
		return m.th.Err("HIGH")
	case "medium":
		return m.th.Warn("MED ")
	}
	return m.th.OK("LOW ")
}

func human(b uint64) string {
	const u = 1024.0
	f := float64(b)
	for _, s := range []string{"B", "KiB", "MiB", "GiB", "TiB"} {
		if f < u || s == "TiB" {
			if s == "B" {
				return fmt.Sprintf("%d B", b)
			}
			return fmt.Sprintf("%.1f %s", f, s)
		}
		f /= u
	}
	return ""
}

func rate(bps float64) string {
	switch {
	case bps >= 1<<20:
		return fmt.Sprintf("%.1f MiB/s", bps/(1<<20))
	case bps >= 1<<10:
		return fmt.Sprintf("%.0f KiB/s", bps/(1<<10))
	}
	return fmt.Sprintf("%.0f B/s", bps)
}

func clampI(v, lo, hi int) int { return max(lo, min(hi, v)) }

// scrollWindow returns the first visible index so that cursor stays visible.
func scrollWindow(cursor, top, visible, total int) int {
	if cursor < top {
		top = cursor
	}
	if cursor >= top+visible {
		top = cursor - visible + 1
	}
	return clampI(top, 0, max(0, total-visible))
}

// stripANSI removes escape sequences (used to restyle a whole row as selected).
func stripANSI(s string) string { return ansi.Strip(s) }

// fitLeft keeps the last w columns of s.
func fitLeft(s string, w int) string {
	if vw(s) <= w {
		return s
	}
	return ansi.TruncateLeft(s, vw(s)-w, "")
}

// asciiRepl maps the few non-ASCII punctuation marks used in text to ASCII.
// Every replacement is exactly one column wide, so layout is unaffected.
var asciiRepl = strings.NewReplacer(
	"·", "|", "…", "~", "→", ">", "▸", ">", "←", "<", "↑", "^", "↓", "v", "—", "-", "–", "-",
	"✓", "+", "✗", "x", "•", "*", "▲", "^", "⏎", "<", "▏", "_", "│", "|", "─", "-",
)

// asciiize makes a rendered line safe for terminals (Linux console, non-UTF-8
// locales) that cannot draw those marks.
func asciiize(s string) string { return asciiRepl.Replace(s) }
