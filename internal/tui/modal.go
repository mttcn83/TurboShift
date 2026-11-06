package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mttcn83/turboshift/internal/engine"
)

type modalKind int

const (
	modalNone modalKind = iota
	modalPreview
	modalProgress
	modalResult
	modalConfirm
	modalHelp
)

// mline is one logical paragraph of modal text; wrapping happens at render
// time so it always fits the current terminal width.
type mline struct {
	text   string
	style  string // "" | dim | warn | err | ok | bold | head
	indent int
}

type mbtn struct{ label, id, key string }

type modal struct {
	kind    modalKind
	title   string
	lines   []mline
	buttons []mbtn // buttons[0] is the primary one (Enter)
	scroll  int

	reqs   []engine.Request // preview: what Apply will run
	snapID string           // result/confirm: the restore point concerned
	yes    string           // confirm: what happens on yes (restore | force | recover | delete)
	arg    string
}

func (m *Model) style(name, s string) string {
	switch name {
	case "dim":
		return m.th.Dim(s)
	case "warn":
		return m.th.Warn(s)
	case "err":
		return m.th.Err(s)
	case "ok":
		return m.th.OK(s)
	case "bold":
		return m.th.Bold(s)
	case "head":
		return m.th.Accent(s)
	}
	return s
}

func (md *modal) add(style, text string, indent ...int) {
	in := 0
	if len(indent) > 0 {
		in = indent[0]
	}
	md.lines = append(md.lines, mline{text, style, in})
}

func (md *modal) gap() { md.lines = append(md.lines, mline{}) }

// modalLines wraps and styles the modal's text for a given inner width.
func (m *Model) modalLines(inner int) []string {
	var out []string
	for _, l := range m.modal.lines {
		if l.text == "" {
			out = append(out, "")
			continue
		}
		pad := strings.Repeat(" ", l.indent)
		for _, w := range wrapText(l.text, inner-l.indent) {
			out = append(out, pad+m.style(l.style, w))
		}
	}
	return out
}

func btnText(b mbtn) string { return "[ " + b.label + " ]" }

// overlayModal draws the active modal centred over body and registers its
// button hit boxes (later hits win in View.OnMouse, so they sit on top).
func (m *Model) overlayModal(body []string, w, h, yOff int) []string {
	md := m.modal
	mw := min(w-4, 78)
	if md.kind == modalHelp {
		mw = min(w-4, 66)
	}
	inner := mw - 4
	all := m.modalLines(inner)

	btnRows := 0
	if len(md.buttons) > 0 {
		btnRows = 2
	}
	maxContent := max(3, h-4-btnRows)
	var view []string
	switch {
	case md.kind == modalProgress: // follow the tail
		view = all
		if len(view) > maxContent {
			view = view[len(view)-maxContent:]
		}
	default:
		maxScroll := max(0, len(all)-maxContent)
		md.scroll = clampI(md.scroll, 0, maxScroll)
		view = all[md.scroll:min(len(all), md.scroll+maxContent)]
	}
	rows := append([]string(nil), view...)
	if btnRows > 0 {
		var bl []string
		for i, b := range md.buttons {
			if i == 0 {
				bl = append(bl, m.th.Sel(btnText(b)))
			} else {
				bl = append(bl, m.th.Bold(btnText(b)))
			}
		}
		rows = append(rows, "", strings.Join(bl, "  "))
	}
	mh := len(rows) + 2
	box := m.box(md.title, rows, mw, mh, true)
	x0, y0 := (w-mw)/2, max(0, (h-mh)/2)

	out := append([]string(nil), body...)
	for len(out) < h {
		out = append(out, "")
	}
	// A two-cell clear margin keeps the screen behind from poking its borders
	// into the dialog's edges.
	pad := min(2, x0)
	gap := strings.Repeat(" ", pad)
	for i := -1; i <= len(box); i++ {
		y := y0 + i
		if y < 0 || y >= len(out) {
			continue
		}
		line := strings.Repeat(" ", mw)
		if i >= 0 && i < len(box) {
			line = box[i]
		}
		out[y] = splice(out[y], gap+line+gap, x0-pad, mw+2*pad)
	}
	if btnRows > 0 {
		by, bx := yOff+y0+len(rows), x0+2 // last content row; box inner starts 2 cells in
		for _, b := range md.buttons {
			t := btnText(b)
			m.addHit(bx, by, vw(t), 1, "modal:"+b.id)
			bx += vw(t) + 2
		}
	}
	return out
}

// splice replaces cells [x0, x0+w) of line with box, preserving the rest.
func splice(line, box string, x0, w int) string {
	left := ansi.Truncate(line, x0, "")
	if n := vw(left); n < x0 {
		left += strings.Repeat(" ", x0-n)
	}
	right := ansi.TruncateLeft(line, x0+w, "")
	return left + "\x1b[m" + box + "\x1b[m" + right
}

func (m *Model) modalKey(msg tea.KeyPressMsg) tea.Cmd {
	md := m.modal
	k := msg.String()
	switch k {
	case "up", "k":
		md.scroll = max(0, md.scroll-1)
		return nil
	case "down", "j":
		md.scroll++
		return nil
	case "pgup":
		md.scroll = max(0, md.scroll-8)
		return nil
	case "pgdown":
		md.scroll += 8
		return nil
	}
	switch md.kind {
	case modalProgress:
		if k == "esc" {
			m.notify(false, "Working… press Ctrl+C to cancel; changes are rolled back safely.")
		}
		return nil
	case modalHelp:
		if k == "esc" || k == "enter" || k == "q" || k == "?" || k == "f1" {
			m.modal = nil
		}
		return nil
	}
	if k == "esc" {
		m.modal = nil
		return nil
	}
	if k == "enter" && len(md.buttons) > 0 {
		return m.modalAct(md.buttons[0].id)
	}
	for _, b := range md.buttons {
		if b.key != "" && b.key == k {
			return m.modalAct(b.id)
		}
	}
	return nil
}

func (m *Model) modalAct(id string) tea.Cmd {
	md := m.modal
	if md == nil {
		return nil
	}
	switch id {
	case "close":
		m.modal = nil
	case "optimizer":
		m.modal = nil
		return m.goScreen(ScrOptimizer)
	case "apply":
		return m.runApply(md.reqs)
	case "undo":
		return m.runRestore(md.snapID, false)
	case "force":
		return m.runRestore(md.snapID, true)
	case "yes":
		switch md.yes {
		case "restore":
			return m.runRestore(md.snapID, false)
		case "force":
			return m.runRestore(md.snapID, true)
		case "recover":
			return m.runRecover()
		case "delete":
			id := md.arg
			m.modal = nil
			return m.deleteRun(id)
		}
	}
	return nil
}

func (m *Model) openHelp() {
	md := &modal{kind: modalHelp, title: "Keyboard & mouse"}
	for _, r := range [][2]string{
		{"1-8, Tab, ←/→", "switch screen (or click a tab)"},
		{"↑/↓, PgUp/PgDn", "move in lists (mouse wheel works too)"},
		{"Space / Enter", "select / open or confirm"},
		{"/", "search the optimization catalog"},
		{"s", "scan my PC (Dashboard)"},
		{"o", "optimize my PC: preview first, nothing changes until you confirm"},
		{"Esc", "close a dialog or cancel typing"},
		{"Ctrl+C", "cancel a running operation (changes are rolled back safely)"},
		{"q", "quit"},
	} {
		md.add("bold", r[0])
		md.add("", r[1], 2)
	}
	md.gap()
	md.add("dim", "Every change is previewed, journaled before it happens, verified afterwards, and can be undone from the Vault. TurboShift never runs as root; a permission prompt appears only for the specific change that needs it.")
	m.modal = md
}

// openRecoverModal asks whether to undo a transaction that never finished.
func (m *Model) openRecoverModal() {
	md := &modal{kind: modalConfirm, title: "Unfinished changes found", yes: "recover"}
	md.add("warn", "A previous TurboShift run did not finish (crash, power loss or a forced exit).")
	md.gap()
	md.add("", "Recovery undoes whatever that run had already changed, using the journal written before it started. It only touches settings TurboShift itself changed.")
	md.gap()
	for _, s := range m.interrupted {
		md.add("dim", s.ID+"  "+s.Label+"  ("+string(s.Status)+")", 2)
	}
	md.buttons = []mbtn{{"Recover now (Enter)", "yes", "y"}, {"Not now (Esc)", "close", "n"}}
	m.modal = md
}
