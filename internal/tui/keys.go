package tui

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
)

func (m *Model) initScreens() {
	m.opt = optState{checked: map[string]bool{}, choice: map[string]int{}}
	m.doc = docState{}
	// Preselect only the tools that are actually installed.
	m.gm = gameState{gamemode: m.app.Run.Has("gamemoderun"), mangohud: m.app.Run.Has("mangohud")}
	m.vt = vaultState{}
	m.bn = benchState{selected: map[string]bool{}}
	p := m.app.Prefs.Defaults()
	m.net.target.val, m.net.lookup.val = p.PingTarget, p.LookupName
}

// screenHints are the footer hints specific to the current screen.
func (m *Model) screenHints() []string {
	switch m.screen {
	case ScrDashboard:
		return []string{m.hint("s", "scan my PC"), m.hint("o", "optimize"), m.hint("Tab", "next screen")}
	case ScrDoctor:
		return []string{m.hint("r", "rescan"), m.hint("Enter", "details/fix"), m.hint("↑↓", "move")}
	case ScrOptimizer:
		return []string{m.hint("Space", "select"), m.hint("←→", "option"), m.hint("a", "apply selected"), m.hint("/", "search")}
	case ScrGaming:
		return []string{m.hint("Space", "toggle"), m.hint("c", "copy launch options"), m.hint("↑↓", "move")}
	case ScrNetwork:
		return []string{m.hint("r", "run test"), m.hint("e", "allow external"), m.hint("t/l/u", "edit target/lookup/URL")}
	case ScrBench:
		return []string{m.hint("i", "import logs"), m.hint("c", "compare"), m.hint("Space", "select"), m.hint("d", "delete run")}
	case ScrVault:
		return []string{m.hint("Enter", "restore"), m.hint("f", "force restore"), m.hint("R", "recover interrupted")}
	case ScrSettings:
		return []string{m.hint("Enter", "change"), m.hint("↑↓", "move")}
	}
	return nil
}

// inputActive reports whether a text field currently owns the keyboard.
func (m *Model) inputActive() *textInput {
	switch {
	case m.opt.search.active:
		return &m.opt.search
	case m.net.editing != nil && m.net.editing.active:
		return m.net.editing
	case m.bn.label.active:
		return &m.bn.label
	case m.st.editing != nil && m.st.editing.active:
		return m.st.editing
	}
	return nil
}

func (m *Model) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	k := msg.String()

	if k == "ctrl+c" {
		if m.busy != nil && m.busy.cancel != nil {
			m.busy.cancel()
			m.notify(true, "Cancelling… anything half-applied is rolled back automatically.")
			return nil
		}
		return tea.Quit
	}
	if m.modal != nil {
		return m.modalKey(msg)
	}
	if in := m.inputActive(); in != nil {
		done, cancel := in.handle(msg)
		if done || cancel {
			in.active = false
			return m.inputFinished(done)
		}
		m.inputChanged()
		return nil
	}

	// Screen-specific keys first (they may use arrows/letters).
	if cmd, ok := m.screenKey(msg); ok {
		return cmd
	}
	switch k {
	case "q":
		if m.busy != nil {
			m.notify(false, "An operation is running. Press Ctrl+C to cancel it first.")
			return nil
		}
		return tea.Quit
	case "?", "f1":
		m.openHelp()
	case "tab", "right", "l", "]":
		return m.goScreen((m.screen + 1) % numScreens)
	case "shift+tab", "left", "h", "[":
		return m.goScreen((m.screen + numScreens - 1) % numScreens)
	case "1", "2", "3", "4", "5", "6", "7", "8":
		n, _ := strconv.Atoi(k)
		return m.goScreen(Screen(n - 1))
	case "s":
		if m.screen == ScrDashboard {
			return m.startScan(false)
		}
	case "/":
		cmd := m.goScreen(ScrOptimizer)
		m.opt.search.active = true
		return cmd
	}
	return nil
}

func (m *Model) goScreen(s Screen) tea.Cmd {
	if s < 0 || s >= numScreens {
		return nil
	}
	m.screen = s
	switch s {
	case ScrVault:
		return m.loadVault()
	case ScrBench:
		return m.loadBench()
	case ScrOptimizer:
		if !m.scanned && m.haveRep && m.busy == nil {
			return m.startScan(false)
		}
	}
	return nil
}

// screenKey routes a key to the active screen; ok=false means "not mine".
func (m *Model) screenKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	switch m.screen {
	case ScrDashboard:
		return m.keyDashboard(msg)
	case ScrDoctor:
		return m.keyDoctor(msg)
	case ScrOptimizer:
		return m.keyOptimizer(msg)
	case ScrGaming:
		return m.keyGaming(msg)
	case ScrNetwork:
		return m.keyNetwork(msg)
	case ScrBench:
		return m.keyBench(msg)
	case ScrVault:
		return m.keyVault(msg)
	case ScrSettings:
		return m.keySettings(msg)
	}
	return nil, false
}

func (m *Model) inputFinished(accepted bool) tea.Cmd {
	switch {
	case m.screen == ScrNetwork:
		return m.netInputDone(accepted)
	case m.screen == ScrBench:
		return m.benchLabelDone(accepted)
	case m.screen == ScrSettings:
		m.settingsInputDone(accepted)
	case m.screen == ScrOptimizer:
		if !accepted {
			m.opt.search.val = ""
		}
		m.opt.cursor, m.opt.top = 0, 0
	}
	return nil
}

func (m *Model) inputChanged() {
	if m.screen == ScrOptimizer {
		m.opt.cursor, m.opt.top = 0, 0
	}
}

// act executes a named action (clicks and some keys land here).
func (m *Model) act(id string) tea.Cmd {
	if m.modal != nil && !strings.HasPrefix(id, "modal:") {
		return nil // modals swallow clicks outside themselves
	}
	kind, arg, _ := strings.Cut(id, ":")
	switch kind {
	case "tab":
		n, _ := strconv.Atoi(arg)
		return m.goScreen(Screen(n))
	case "scan":
		return m.startScan(false)
	case "optimize":
		return m.optimizeRecommended()
	case "modal":
		return m.modalAct(arg)
	case "opt":
		return m.optAct(arg)
	case "doc":
		return m.docAct(arg)
	case "game":
		return m.gameAct(arg)
	case "net":
		return m.netAct(arg)
	case "bench":
		return m.benchAct(arg)
	case "vault":
		return m.vaultAct(arg)
	case "set":
		return m.settingsAct(arg)
	}
	return nil
}

// scrollBy moves the cursor of the active list (mouse wheel).
func (m *Model) scrollBy(d int) tea.Cmd {
	if m.modal != nil {
		m.modal.scroll = max(0, m.modal.scroll+d*2)
		return nil
	}
	switch m.screen {
	case ScrOptimizer:
		m.opt.cursor += d
	case ScrDoctor:
		m.doc.cursor += d
	case ScrVault:
		m.vt.cursor += d
	case ScrBench:
		m.bn.cursor += d
	case ScrSettings:
		m.st.cursor += d
	case ScrGaming:
		m.gm.cursor += d
	}
	return nil
}

// moveKey interprets list-navigation keys; returns the new cursor and whether handled.
func moveKey(k string, cursor, n, page int) (int, bool) {
	switch k {
	case "up", "k":
		cursor--
	case "down", "j":
		cursor++
	case "pgup":
		cursor -= page
	case "pgdown":
		cursor += page
	case "home", "g":
		cursor = 0
	case "end", "G":
		cursor = n - 1
	default:
		return cursor, false
	}
	return clampI(cursor, 0, max(0, n-1)), true
}
