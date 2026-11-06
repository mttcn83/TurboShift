package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/mttcn83/turboshift/internal/diagnostics"
)

type docState struct{ cursor, top int }

// sortedFindings orders findings worst-first, stable within a severity.
func (m *Model) sortedFindings() []diagnostics.Finding {
	fs := append([]diagnostics.Finding(nil), m.findings...)
	sort.SliceStable(fs, func(i, j int) bool { return fs[i].Sev > fs[j].Sev })
	return fs
}

func (m *Model) keyDoctor(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	k := msg.String()
	n := len(m.findings)
	if c, ok := moveKey(k, m.doc.cursor, n, 8); ok {
		m.doc.cursor = c
		return nil, true
	}
	switch k {
	case "r":
		return m.startScan(false), true
	case "enter", " ":
		return m.docAct("fix"), true
	}
	return nil, false
}

func (m *Model) docAct(arg string) tea.Cmd {
	fs := m.sortedFindings()
	switch {
	case strings.HasPrefix(arg, "row"):
		var i int
		fmt.Sscanf(strings.TrimPrefix(arg, "row"), "%d", &i)
		m.doc.cursor = clampI(i, 0, max(0, len(fs)-1))
	case arg == "rescan":
		return m.startScan(false)
	case arg == "fix":
		if len(fs) == 0 {
			return nil
		}
		f := fs[clampI(m.doc.cursor, 0, len(fs)-1)]
		if f.Fix == nil {
			m.notify(true, "This finding is informational: there is no safe automatic fix to offer.")
			return nil
		}
		m.selectTweak(f.Fix.TweakID, f.Fix.Params)
		return m.goScreen(ScrOptimizer)
	}
	return nil
}

func (m *Model) viewDoctor(w, h int) []string {
	fs := m.sortedFindings()
	m.doc.cursor = clampI(m.doc.cursor, 0, max(0, len(fs)-1))
	var out []string
	warn, note := diagnostics.Counts(m.findings)
	out = append(out, " "+m.th.Bold("System Doctor")+m.th.Dim(fmt.Sprintf("   %d finding(s) · %d warning(s) · %d notice(s) · every finding names the measurement behind it", len(fs), warn, note)))
	out = append(out, "")
	if len(fs) == 0 {
		return append(out, " "+m.th.Dim("Still detecting your system…"))
	}
	listH := max(4, min(len(fs), (h-3)/2))
	m.doc.top = scrollWindow(m.doc.cursor, m.doc.top, listH, len(fs))
	for i := m.doc.top; i < min(len(fs), m.doc.top+listH); i++ {
		f := fs[i]
		line := m.sevTag(f.Sev) + " " + m.th.Dim(fit(f.Area, 8)) + " " + f.Title
		if f.Fix != nil {
			line += " " + m.th.Accent("[fix]")
		}
		if i == m.doc.cursor {
			line = m.th.Sel(fit(fmt.Sprintf(" %s %-6s %-8s %s", m.gl.Arrow, strings.ToUpper(f.Sev.String()), f.Area, f.Title), w-2))
		} else {
			line = "   " + line
		}
		m.addHit(0, bodyY+len(out), w, 1, fmt.Sprintf("doc:row%d", i))
		out = append(out, line)
	}
	out = append(out, "")
	f := fs[m.doc.cursor]
	var d []string
	d = append(d, m.th.Bold(f.Title))
	if f.Detail != "" {
		d = append(d, wrapText(f.Detail, w-6)...)
	}
	if f.Fix != nil {
		name := f.Fix.TweakID
		if t, ok := m.app.Reg.Get(f.Fix.TweakID); ok {
			name = t.Meta().Name
		}
		d = append(d, "", m.th.Accent("Fix available: ")+name+" "+m.th.Dim("→ press Enter to review it in the Optimizer (nothing is applied yet)"))
	}
	if len(m.top) > 0 {
		d = append(d, "", m.th.Dim("Busiest processes during the last scan:"))
		for _, p := range m.top {
			d = append(d, m.th.Dim(fmt.Sprintf("  %-24s %5.0f%% CPU  %s", shorten(p.Name, 24), p.CPUPct, human(uint64(p.RSSMiB*1024*1024)))))
		}
	}
	if len(m.rep.Warnings) > 0 {
		d = append(d, "", m.th.Dim("Detection limits on this machine:"))
		for _, wn := range m.rep.Warnings {
			d = append(d, m.th.Dim("  "+m.gl.Dot+" "+wn))
		}
	}
	return append(out, m.box("Details", d, w, max(5, h-len(out)), false)...)
}
