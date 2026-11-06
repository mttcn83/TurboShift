package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/mttcn83/turboshift/internal/version"
)

type settingsState struct {
	cursor  int
	ping    textInput
	lookup  textInput
	editing *textInput
	backup  string
}

const settingRows = 4

var themeCycle = append([]string{"auto"}, ThemeNames...)

func (m *Model) keySettings(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	k := msg.String()
	if c, ok := moveKey(k, m.st.cursor, settingRows, 2); ok {
		m.st.cursor = c
		return nil, true
	}
	switch k {
	case "enter", " ", "space":
		return m.settingsAct(fmt.Sprintf("do:%d", m.st.cursor)), true
	}
	return nil, false
}

func (m *Model) settingsAct(arg string) tea.Cmd {
	kind, rest, _ := strings.Cut(arg, ":")
	var i int
	fmt.Sscanf(rest, "%d", &i)
	if kind == "row" || kind == "do" {
		m.st.cursor = clampI(i, 0, settingRows-1)
	}
	if kind != "do" {
		return nil
	}
	switch m.st.cursor {
	case 0:
		cur := m.app.Prefs.Theme
		if cur == "" {
			cur = "auto"
		}
		next := themeCycle[0]
		for j, n := range themeCycle {
			if n == cur {
				next = themeCycle[(j+1)%len(themeCycle)]
			}
		}
		m.app.Prefs.Theme = next
		m.applyPrefs()
		m.savePrefs()
	case 1:
		m.app.Prefs.ASCII = !m.app.Prefs.ASCII
		m.applyPrefs()
		m.savePrefs()
	case 2:
		m.st.ping.val = m.app.Prefs.PingTarget
		m.st.editing, m.st.backup = &m.st.ping, m.st.ping.val
		m.st.ping.active = true
	case 3:
		m.st.lookup.val = m.app.Prefs.LookupName
		m.st.editing, m.st.backup = &m.st.lookup, m.st.lookup.val
		m.st.lookup.active = true
	}
	return nil
}

func (m *Model) settingsInputDone(accepted bool) {
	in := m.st.editing
	if in == nil || !accepted {
		return
	}
	v := strings.TrimSpace(in.val)
	if v == "" || strings.ContainsAny(v, " \t") {
		m.notify(false, "Enter a host name or address without spaces.")
		return
	}
	if in == &m.st.ping {
		m.app.Prefs.PingTarget = v
		m.net.target.val = v
	} else {
		m.app.Prefs.LookupName = v
		m.net.lookup.val = v
	}
	m.savePrefs()
}

func (m *Model) viewSettings(w, h int) []string {
	var out []string
	out = append(out, " "+m.th.Bold("Settings")+m.th.Dim("   stored in "+m.app.Paths.Config), "")

	theme := m.app.Prefs.Theme
	if theme == "" {
		theme = "auto"
	}
	val := func(in *textInput, cur string) string {
		if in.active {
			return in.val + m.th.Accent("_")
		}
		return cur
	}
	rows := []struct{ label, value, note string }{
		{"Theme", theme, "auto · dark · light · colorblind · mono (no colour at all)"},
		{"ASCII-only drawing", onOff(m.app.Prefs.ASCII), "for terminals without box-drawing or braille glyphs"},
		{"Ping target", val(&m.st.ping, m.app.Prefs.PingTarget), "contacted only when you allow external network tests"},
		{"DNS lookup name", val(&m.st.lookup, m.app.Prefs.LookupName), "resolved only when you allow external network tests"},
	}
	var b []string
	for i, r := range rows {
		var line string
		if i == m.st.cursor && !m.st.ping.active && !m.st.lookup.active {
			line = m.th.Sel(fit(" "+fmt.Sprintf("%-20s %-18s", r.label, r.value), w-4))
		} else {
			line = " " + fmt.Sprintf("%-20s %-18s", r.label, r.value)
		}
		line += " " + m.th.Dim(r.note)
		m.addHit(0, bodyY+len(out)+1+len(b), w, 1, fmt.Sprintf("set:do:%d", i))
		b = append(b, line)
	}
	out = append(out, m.box("Preferences (Enter changes the highlighted one)", b, w, 0, true)...)

	var info []string
	info = append(info, m.kv("Version", "TurboShift "+version.Version+" · MIT licence · free, no accounts, no telemetry", 14))
	info = append(info, wrapText("Privacy: TurboShift reads /proc and /sys on your machine, writes only to the folders below, and contacts nothing unless you press run in the Network Doctor with external tests allowed. There is no telemetry, no account, no cloud and no AI.", w-6)...)
	info = append(info, "")
	info = append(info, m.kv("Config", m.app.Paths.Config, 14))
	info = append(info, m.kv("Restore points", m.app.Paths.State, 14))
	info = append(info, m.kv("Benchmarks", m.app.Paths.Data, 14))
	if m.haveRep {
		var have, miss []string
		for _, t := range m.rep.Tools {
			if t.Present {
				have = append(have, t.Name)
			} else {
				miss = append(miss, t.Name)
			}
		}
		info = append(info, m.kv("Tools found", strings.Join(have, ", "), 14))
		if len(miss) > 0 {
			info = append(info, m.kv("Not installed", m.th.Dim(strings.Join(miss, ", ")+" (optional)"), 14))
		}
	}
	out = append(out, m.box("About & privacy", info, w, 0, false)...)
	return out
}
