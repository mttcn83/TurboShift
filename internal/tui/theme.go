package tui

import (
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
)

// Theme turns semantic roles into styled text. The monochrome theme uses only
// text attributes (bold/underline/reverse), never colour, so meaning survives
// on any terminal; every severity also has a text label, so colour is never
// the only carrier of information.
type Theme struct {
	Name                                     string
	Accent, Dim, Bold, OK, Warn, Err, Border func(string) string
	Sel                                      func(string) string // selected row
	Key                                      func(string) string // key-cap
	Title                                    func(string) string // box title
	BarFull, BarEmpty                        func(string) string
}

type palette struct {
	accent, text, dim, border, ok, warn, err, selBG, selFG string
}

var palettes = map[string]palette{
	"dark":       {"#3DD6C6", "#D8DEE9", "#7A8499", "#3B4252", "#7BD88F", "#F5C451", "#FF6B6B", "#26324A", "#FFFFFF"},
	"light":      {"#0E7C7B", "#1F2937", "#6B7280", "#CBD5E1", "#15803D", "#B45309", "#B91C1C", "#D5EFEC", "#0B2E2D"},
	"colorblind": {"#56B4E9", "#D8DEE9", "#8A93A6", "#3B4252", "#009E73", "#E69F00", "#D55E00", "#24344D", "#FFFFFF"},
}

// ThemeNames lists the selectable themes in cycling order.
var ThemeNames = []string{"dark", "light", "colorblind", "mono"}

func fg(hex string) func(string) string {
	st := lipgloss.NewStyle().Foreground(color.Color(lipgloss.Color(hex)))
	return func(s string) string { return st.Render(s) }
}

func attr(f func(lipgloss.Style) lipgloss.Style) func(string) string {
	st := f(lipgloss.NewStyle())
	return func(s string) string { return st.Render(s) }
}

func plain(s string) string { return s }

// NewTheme builds a theme by name; unknown names fall back to dark.
func NewTheme(name string) Theme {
	if name == "mono" {
		bold := attr(func(s lipgloss.Style) lipgloss.Style { return s.Bold(true) })
		return Theme{Name: "mono",
			Accent: bold, Dim: attr(func(s lipgloss.Style) lipgloss.Style { return s.Faint(true) }), Bold: bold,
			OK: plain, Warn: bold, Err: attr(func(s lipgloss.Style) lipgloss.Style { return s.Bold(true).Underline(true) }),
			Border: plain, Sel: attr(func(s lipgloss.Style) lipgloss.Style { return s.Reverse(true) }),
			Key:   attr(func(s lipgloss.Style) lipgloss.Style { return s.Reverse(true) }),
			Title: bold, BarFull: plain, BarEmpty: plain,
		}
	}
	p, ok := palettes[name]
	if !ok {
		name, p = "dark", palettes["dark"]
	}
	sel := lipgloss.NewStyle().Background(color.Color(lipgloss.Color(p.selBG))).Foreground(color.Color(lipgloss.Color(p.selFG)))
	key := lipgloss.NewStyle().Background(color.Color(lipgloss.Color(p.border))).Foreground(color.Color(lipgloss.Color(p.text)))
	return Theme{Name: name,
		Accent: func() func(string) string {
			st := lipgloss.NewStyle().Foreground(color.Color(lipgloss.Color(p.accent))).Bold(true)
			return func(s string) string { return st.Render(s) }
		}(),
		Dim: fg(p.dim), Bold: attr(func(s lipgloss.Style) lipgloss.Style { return s.Bold(true) }),
		OK: fg(p.ok), Warn: fg(p.warn), Err: fg(p.err), Border: fg(p.border),
		Sel: func(s string) string { return sel.Render(s) },
		Key: func(s string) string { return key.Render(" " + s + " ") },
		Title: func() func(string) string {
			st := lipgloss.NewStyle().Foreground(color.Color(lipgloss.Color(p.accent)))
			return func(s string) string { return st.Render(s) }
		}(),
		BarFull: fg(p.accent), BarEmpty: fg(p.border),
	}
}

// Glyphs are the drawing characters; the ASCII set is used when requested or
// when the locale is not UTF-8.
type Glyphs struct {
	TL, TR, BL, BR, H, V     string
	Full, Empty              string
	Spark                    []string
	Spin                     []string
	Check, Cross, Dot, Arrow string
	On, Off                  string
	Warn, Info               string
}

var unicodeGlyphs = Glyphs{
	TL: "╭", TR: "╮", BL: "╰", BR: "╯", H: "─", V: "│",
	Full: "█", Empty: "░",
	Spark: strings.Split("▁▂▃▄▅▆▇█", ""),
	Spin:  strings.Split("⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏", ""),
	Check: "✓", Cross: "✗", Dot: "•", Arrow: "▸", On: "[x]", Off: "[ ]", Warn: "!", Info: "i",
}

var asciiGlyphs = Glyphs{
	TL: "+", TR: "+", BL: "+", BR: "+", H: "-", V: "|",
	Full: "#", Empty: ".",
	Spark: strings.Split(".:-=+*#%", ""),
	Spin:  strings.Split(`|/-\`, ""),
	Check: "+", Cross: "x", Dot: "*", Arrow: ">", On: "[x]", Off: "[ ]", Warn: "!", Info: "i",
}

// ANSI Shadow wordmark, hand-aligned (79 columns × 6 rows).
var banner = []string{
	`████████╗██╗   ██╗██████╗ ██████╗  ██████╗ ███████╗██╗  ██╗██╗███████╗████████╗`,
	`╚══██╔══╝██║   ██║██╔══██╗██╔══██╗██╔═══██╗██╔════╝██║  ██║██║██╔════╝╚══██╔══╝`,
	`   ██║   ██║   ██║██████╔╝██████╔╝██║   ██║███████╗███████║██║█████╗     ██║   `,
	`   ██║   ██║   ██║██╔══██╗██╔══██╗██║   ██║╚════██║██╔══██║██║██╔══╝     ██║   `,
	`   ██║   ╚██████╔╝██║  ██║██████╔╝╚██████╔╝███████║██║  ██║██║██║        ██║   `,
	`   ╚═╝    ╚═════╝ ╚═╝  ╚═╝╚═════╝  ╚═════╝ ╚══════╝╚═╝  ╚═╝╚═╝╚═╝        ╚═╝   `,
}

const bannerWidth = 80
