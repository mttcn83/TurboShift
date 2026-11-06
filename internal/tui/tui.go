// Package tui is TurboShift's interactive terminal application (Bubble Tea v2).
//
// Long operations (scans, applies, network tests) run in goroutines and report
// back through messages, so the interface never blocks. Every clickable thing
// is also reachable by keyboard, and vice versa.
package tui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/mttcn83/turboshift/internal/app"
	"github.com/mttcn83/turboshift/internal/config"
	"github.com/mttcn83/turboshift/internal/detect"
	"github.com/mttcn83/turboshift/internal/diagnostics"
	"github.com/mttcn83/turboshift/internal/snapshot"
	"github.com/mttcn83/turboshift/internal/tweaks"
	"github.com/mttcn83/turboshift/internal/version"
)

type Screen int

const (
	ScrDashboard Screen = iota
	ScrDoctor
	ScrOptimizer
	ScrGaming
	ScrNetwork
	ScrBench
	ScrVault
	ScrSettings
	numScreens
)

var screenNames = [numScreens]string{"Dashboard", "Doctor", "Optimizer", "Gaming", "Network", "Benchmark", "Vault", "Settings"}

// hit is a clickable rectangle (content-area coordinates) and what it does.
type hit struct {
	x, y, w, h int
	id         string
}

// actMsg triggers a named UI action (from a click or a key).
type actMsg struct{ id string }

type (
	tickMsg    time.Time
	animMsg    time.Time
	metricsMsg struct {
		m     diagnostics.Metrics
		stamp time.Time
	}
	initMsg struct {
		rep         detect.Report
		interrupted []*snapshot.Snapshot
	}
	// streamMsg carries one message from a goroutine and the channel to keep listening on.
	streamMsg struct {
		ch  <-chan tea.Msg
		msg tea.Msg
	}
)

// listen waits for the next message on ch (nil when the channel closes).
func listen(ch <-chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return streamMsg{ch: ch, msg: msg}
	}
}

// busyState describes the one long-running operation allowed at a time.
type busyState struct {
	kind   string // scan | apply | restore | recover | net | import
	label  string
	steps  []string
	cancel context.CancelFunc
}

type textInput struct {
	val    string
	active bool
}

func (t *textInput) handle(msg tea.KeyPressMsg) (done, cancel bool) {
	switch k := msg.String(); k {
	case "enter":
		return true, false
	case "esc":
		return false, true
	case "backspace":
		if r := []rune(t.val); len(r) > 0 {
			t.val = string(r[:len(r)-1])
		}
	case "ctrl+u":
		t.val = ""
	default:
		if msg.Key().Text != "" && !strings.Contains(k, "ctrl+") {
			t.val += msg.Key().Text
		}
	}
	return false, false
}

// Model is the whole application state.
type Model struct {
	app *app.App
	ctx context.Context
	th  Theme
	gl  Glyphs

	w, h   int
	screen Screen

	// system knowledge
	rep      detect.Report
	haveRep  bool
	findings []diagnostics.Finding
	assess   map[string]tweaks.Assessment
	top      []diagnostics.Proc
	scanned  bool
	scanAt   time.Time

	// live metrics
	sampler *diagnostics.Sampler
	met     diagnostics.Metrics
	hist    struct{ cpu, mem, rx, tx, gpu []float64 }
	tickN   int
	// topWindow is how long the scan samples per-process CPU use.
	topWindow time.Duration
	// noTimers disables the clock ticks (tests drive the model synchronously).
	noTimers bool
	// sampling is true while a metrics sample is in flight (the Sampler is not concurrency-safe).
	sampling bool

	busy    *busyState
	frame   int
	toast   string
	toastOK bool
	toastAt time.Time

	modal         *modal
	pendingPreset string // preset to start once the running scan finishes
	lockedNote    string // set when admin tweaks are locked because turbo is not installed system-wide
	interrupted   []*snapshot.Snapshot
	hits          []hit // from the last render, used by mouse handling in View.OnMouse

	opt optState
	doc docState
	gm  gameState
	net netState
	bn  benchState
	vt  vaultState
	st  settingsState
}

// NewModel builds the initial model (exported for tests).
func NewModel(ctx context.Context, a *app.App) *Model {
	m := &Model{app: a, ctx: ctx, w: 100, h: 32, topWindow: 2 * time.Second}
	m.applyPrefs()
	m.initScreens()
	return m
}

// applyPrefs derives theme and glyph set from preferences and the environment.
func (m *Model) applyPrefs() {
	name := m.app.Prefs.Theme
	ascii := m.app.Prefs.ASCII
	env := m.app.Getenv
	if env == nil {
		env = os.Getenv
	}
	if name == "" || name == "auto" {
		name = "dark"
		if env("NO_COLOR") != "" || env("TERM") == "dumb" {
			name = "mono"
		}
	}
	if env("TERM") == "dumb" || env("TERM") == "linux" {
		ascii = true // the Linux console font lacks most box/braille glyphs
	}
	loc := strings.ToLower(env("LC_ALL") + env("LC_CTYPE") + env("LANG"))
	if loc != "" && !strings.Contains(loc, "utf-8") && !strings.Contains(loc, "utf8") {
		ascii = true
	}
	m.th = NewTheme(name)
	m.gl = unicodeGlyphs
	if ascii {
		m.gl = asciiGlyphs
	}
}

// Run starts the interactive application.
func Run(ctx context.Context, a *app.App) error {
	p := tea.NewProgram(NewModel(ctx, a), tea.WithContext(ctx))
	_, err := p.Run()
	return err
}

func (m *Model) Init() tea.Cmd {
	a := m.app
	detect := func() tea.Msg {
		rep := a.Detect(m.ctx)
		pend, _ := a.Store.Interrupted()
		return initMsg{rep, pend}
	}
	if m.noTimers {
		return detect
	}
	return tea.Batch(detect, tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) }))
}

// notify shows a transient status line.
func (m *Model) notify(ok bool, s string) {
	m.toast, m.toastOK, m.toastAt = s, ok, time.Now()
}

func (m *Model) isBusy() bool { return m.busy != nil }

func (m *Model) anim() tea.Cmd {
	if m.noTimers {
		return nil
	}
	return tea.Tick(110*time.Millisecond, func(t time.Time) tea.Msg { return animMsg(t) })
}

// startBusy marks an operation running and starts the animation clock.
func (m *Model) startBusy(kind, label string, cancel context.CancelFunc) tea.Cmd {
	m.busy = &busyState{kind: kind, label: label, cancel: cancel}
	return m.anim()
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		return m, nil

	case initMsg:
		m.rep, m.haveRep = msg.rep, true
		m.interrupted = msg.interrupted
		m.sampler = &diagnostics.Sampler{FS: m.app.FS, Run: m.app.Run, GPUs: msg.rep.GPUs, NetIface: msg.rep.Network.DefaultIface}
		m.findings = diagnostics.Diagnose(msg.rep)
		if len(msg.interrupted) > 0 {
			m.openRecoverModal()
		}
		return m, nil

	case tickMsg:
		next := tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
		if m.noTimers {
			next = nil
		}
		if m.sampler == nil || m.sampling {
			return m, next
		}
		m.sampling = true
		m.tickN++
		sm := m.sampler
		withGPU := m.screen == ScrDashboard && m.tickN%3 == 1
		if !withGPU {
			saved := sm.GPUs
			// GPU polling only on the dashboard and only every third tick.
			return m, tea.Batch(next, func() tea.Msg {
				sm.GPUs = nil
				met := sm.Sample(m.ctx, time.Now())
				sm.GPUs = saved
				return metricsMsg{met, time.Now()}
			})
		}
		return m, tea.Batch(next, func() tea.Msg { return metricsMsg{sm.Sample(m.ctx, time.Now()), time.Now()} })

	case metricsMsg:
		m.sampling = false
		keepGPU := m.met.GPUs
		m.met = msg.m
		if len(m.met.GPUs) == 0 {
			m.met.GPUs = keepGPU
		}
		push := func(s *[]float64, v float64) {
			*s = append(*s, v)
			if len(*s) > 120 {
				*s = (*s)[len(*s)-120:]
			}
		}
		if msg.m.HasRates {
			push(&m.hist.cpu, msg.m.CPUPct)
			push(&m.hist.rx, msg.m.NetRxBps)
			push(&m.hist.tx, msg.m.NetTxBps)
		}
		push(&m.hist.mem, msg.m.MemUsedPct)
		if len(msg.m.GPUs) > 0 && msg.m.GPUs[0].Available {
			push(&m.hist.gpu, msg.m.GPUs[0].LoadPct)
		}
		return m, nil

	case animMsg:
		m.frame++
		if m.isBusy() {
			return m, m.anim()
		}
		return m, nil

	case streamMsg:
		cmd := m.handleStream(msg.msg)
		return m, tea.Batch(cmd, listen(msg.ch))

	case actMsg:
		return m, m.act(msg.id)

	case tea.KeyPressMsg:
		return m, m.handleKey(msg)

	case tea.MouseWheelMsg:
		return m, m.scrollBy(wheelDelta(msg))
	}
	// Results delivered directly (not through a stream).
	return m, m.handleStream(msg)
}

func wheelDelta(msg tea.MouseWheelMsg) int {
	if msg.Mouse().Button == tea.MouseWheelUp {
		return -1
	}
	return 1
}

// handleStream processes results of background work.
func (m *Model) handleStream(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case stageMsg:
		if m.busy != nil {
			m.busy.steps = append(m.busy.steps, string(msg))
			m.busy.label = string(msg)
		}
	case scanDoneMsg:
		return m.onScanDone(msg)
	case previewMsg:
		m.onPreview(msg)
	case applyEventMsg:
		m.onApplyEvent(msg)
	case applyDoneMsg:
		return m.onApplyDone(msg)
	case restoreDoneMsg:
		return m.onRestoreDone(msg)
	case netDoneMsg:
		m.onNetDone(msg)
	case vaultLoadedMsg:
		m.vt.snaps, m.vt.corrupt, m.interrupted = msg.snaps, msg.corrupt, msg.pend
		m.vt.cursor = clampI(m.vt.cursor, 0, max(0, len(msg.snaps)-1))
	case benchLoadedMsg:
		m.onBenchLoaded(msg)
	case importDoneMsg:
		return m.onImportDone(msg)
	}
	return nil
}

// ---- layout & rendering ----

const (
	minW = 64
	minH = 18
)

// tooSmall is the notice for a terminal under minW x minH. It wraps to the
// width it has and never draws more rows than the terminal offers.
func (m *Model) tooSmall() string {
	width := max(m.w-2, 8)
	msg := fmt.Sprintf("Terminal too small (%d×%d). TurboShift needs at least %d×%d: enlarge the window, or press q to quit.", m.w, m.h, minW, minH)
	lines := strings.Split(ansi.Wrap(msg, width, ""), "\n")
	if m.h > 0 && len(lines) > m.h {
		lines = lines[:m.h]
	}
	for i, l := range lines {
		lines[i] = " " + l
	}
	return m.th.Warn(strings.Join(lines, "\n"))
}

func (m *Model) View() tea.View {
	var content string
	m.hits = nil
	if m.w < minW || m.h < minH {
		content = m.tooSmall()
	} else {
		content = m.render()
	}
	v := tea.NewView(content)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = "TurboShift"
	hits := append([]hit(nil), m.hits...)
	v.OnMouse = func(msg tea.MouseMsg) tea.Cmd {
		click, ok := msg.(tea.MouseClickMsg)
		if !ok || click.Mouse().Button != tea.MouseLeft {
			return nil
		}
		mm := click.Mouse()
		for i := len(hits) - 1; i >= 0; i-- { // later (modal) hits win
			h := hits[i]
			if mm.X >= h.x && mm.X < h.x+h.w && mm.Y >= h.y && mm.Y < h.y+h.h {
				id := h.id
				return func() tea.Msg { return actMsg{id} }
			}
		}
		return nil
	}
	return v
}

func (m *Model) addHit(x, y, w, h int, id string) { m.hits = append(m.hits, hit{x, y, w, h, id}) }

// render composes tab bar, content, status line and footer into a w×h frame.
func (m *Model) render() string {
	var lines []string
	lines = append(lines, m.renderTabs()...)
	bodyH := m.h - len(lines) - 2
	var body []string
	if m.modal != nil && m.modal.kind != modalHelp {
		body = m.renderScreen(m.w, bodyH)
		body = m.overlayModal(body, m.w, bodyH, len(lines))
	} else if m.modal != nil {
		body = m.overlayModal(m.renderScreen(m.w, bodyH), m.w, bodyH, len(lines))
	} else {
		body = m.renderScreen(m.w, bodyH)
	}
	for len(body) < bodyH {
		body = append(body, "")
	}
	lines = append(lines, body[:bodyH]...)
	lines = append(lines, m.renderStatus(), m.renderFooter())
	for i := range lines {
		lines[i] = fit(lines[i], m.w)
		if m.gl.Full == "#" {
			lines[i] = asciiize(lines[i])
		}
	}
	return strings.Join(lines, "\n")
}

var shortNames = [numScreens]string{"Dash", "Doctor", "Optim", "Game", "Net", "Bench", "Vault", "Set"}

// renderTabs draws the tab bar, shrinking names then dropping them to numbers
// so every tab stays visible and clickable on narrow terminals.
func (m *Model) renderTabs() []string {
	mark := "▲ "
	if m.gl.Full == "#" {
		mark = ""
	}
	brands := []string{" " + mark + "TURBOSHIFT ", " " + mark, " " + mark}
	label := func(mode int, i Screen) string {
		switch mode {
		case 0:
			return " " + itoa(int(i)+1) + " " + screenNames[i] + " "
		case 1:
			return " " + itoa(int(i)+1) + " " + shortNames[i] + " "
		}
		return " " + itoa(int(i)+1) + " "
	}
	mode := 2
	for md := 0; md < 3; md++ {
		total := vw(brands[md])
		for i := Screen(0); i < numScreens; i++ {
			total += vw(label(md, i))
		}
		if total <= m.w {
			mode = md
			break
		}
	}
	var b strings.Builder
	brand := brands[mode]
	b.WriteString(m.th.Accent(brand))
	x := vw(brand)
	for i := Screen(0); i < numScreens; i++ {
		l := label(mode, i)
		if i == m.screen {
			b.WriteString(m.th.Sel(l))
		} else {
			b.WriteString(m.th.Dim(l))
		}
		m.addHit(x, 0, vw(l), 1, "tab:"+itoa(int(i)))
		x += vw(l)
	}
	return []string{b.String(), m.th.Border(strings.Repeat(m.gl.H, m.w))}
}

func (m *Model) renderScreen(w, h int) []string {
	var lines []string
	switch m.screen {
	case ScrDashboard:
		lines = m.viewDashboard(w, h)
	case ScrDoctor:
		lines = m.viewDoctor(w, h)
	case ScrOptimizer:
		lines = m.viewOptimizer(w, h)
	case ScrGaming:
		lines = m.viewGaming(w, h)
	case ScrNetwork:
		lines = m.viewNetwork(w, h)
	case ScrBench:
		lines = m.viewBench(w, h)
	case ScrVault:
		lines = m.viewVault(w, h)
	case ScrSettings:
		lines = m.viewSettings(w, h)
	}
	return lines
}

func (m *Model) renderStatus() string {
	if m.busy != nil {
		sp := m.gl.Spin[m.frame%len(m.gl.Spin)]
		return " " + m.th.Accent(sp) + " " + m.busy.label
	}
	if m.toast != "" && time.Since(m.toastAt) < 8*time.Second {
		if m.toastOK {
			return " " + m.th.OK(m.gl.Check) + " " + m.toast
		}
		return " " + m.th.Err(m.gl.Cross) + " " + m.toast
	}
	if os.Geteuid() == 0 {
		return " " + m.th.Warn("Running as root is not recommended: TurboShift asks for permission only when a change needs it.")
	}
	return " " + m.th.Dim("Local only · no telemetry · every change is reversible · v"+version.Version)
}

func (m *Model) renderFooter() string {
	always := []string{m.hint("?", "help"), m.hint("q", "quit")}
	var hs []string
	switch {
	case m.modal != nil:
		hs = []string{m.hint("Enter", "confirm"), m.hint("Esc", "close"), m.hint("↑↓", "scroll")}
	case m.screenHints() != nil:
		hs = m.screenHints()
	default:
		hs = []string{m.hint("←→/1-8", "screens"), m.hint("↑↓", "move"), m.hint("Enter", "open")}
	}
	// Drop the least important screen hints until the line fits; ? and q stay.
	for len(hs) > 0 && vw(" "+strings.Join(append(append([]string(nil), hs...), always...), "  ")) > m.w {
		hs = hs[:len(hs)-1]
	}
	if m.modal != nil {
		return " " + strings.Join(hs, "  ")
	}
	return " " + strings.Join(append(hs, always...), "  ")
}

// ---- helpers ----

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b []byte
	for ; i > 0; i /= 10 {
		b = append([]byte{byte('0' + i%10)}, b...)
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

func (m *Model) savePrefs() {
	if err := config.Save(m.app.Paths, m.app.Prefs); err != nil {
		m.notify(false, "Could not save settings: "+err.Error())
	}
}
