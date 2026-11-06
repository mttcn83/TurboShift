package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/mttcn83/turboshift/internal/detect"
	"github.com/mttcn83/turboshift/internal/diagnostics"
)

// bodyY is the first screen row of the content area (below the tab bar).
const bodyY = 2

func (m *Model) keyDashboard(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case "o":
		return m.optimizeRecommended(), true
	case "enter":
		if !m.scanned {
			return m.startScan(false), true
		}
		return m.optimizeRecommended(), true
	}
	return nil, false
}

// kv renders "label  value" with a dim fixed-width label.
func (m *Model) kv(label, value string, lw int) string {
	return m.th.Dim(fit(label, lw)) + " " + value
}

func (m *Model) viewDashboard(w, h int) []string {
	var out []string
	if w >= bannerWidth+2 && h >= 30 && m.gl.Full != "#" {
		for _, l := range banner {
			out = append(out, " "+m.th.Accent(l))
		}
		out = append(out, " "+m.th.Dim("Free · open source · local only · evidence-based, reversible Linux optimization"))
	} else {
		out = append(out, " "+m.th.Accent("TurboShift")+m.th.Dim("  evidence-based, reversible Linux optimization · local only"))
	}

	// Action buttons first: they must stay visible on small terminals.
	y := bodyY + len(out)
	b1, b2 := "[ Scan My PC (s) ]", "[ Optimize My PC (o) ]"
	if m.busy != nil {
		out = append(out, " "+m.th.Dim(b1+"  "+b2+"   working…"))
	} else {
		line := " " + m.th.Sel(b1) + "  " + m.th.Bold(b2)
		if !m.scanned {
			line += m.th.Dim("   first time? press Enter")
		}
		out = append(out, line)
		m.addHit(1, y, vw(b1), 1, "scan")
		m.addHit(1+vw(b1)+2, y, vw(b2), 1, "optimize")
	}
	out = append(out, "")

	// Health is the most important panel, then live gauges, then the hardware card.
	health := m.healthBox(w - 4)
	live, mach := m.liveBox(true), m.machineBox()
	side := w >= 100
	height := func(live, mach []string) int {
		lh, mh := 0, 0
		if live != nil {
			lh = len(live) + 2
		}
		if mach != nil {
			mh = len(mach) + 2
		}
		if side {
			return max(lh, mh)
		}
		return lh + mh
	}
	budget := h - len(out) - (len(health) + 2)
	if height(live, mach) > budget {
		live = m.liveBox(false)
	}
	if height(live, mach) > budget {
		mach = nil
	}
	if height(live, mach) > budget {
		live = nil
	}
	switch {
	case live != nil && mach != nil && side:
		lw := (w - 1) / 2
		out = append(out, joinCols(1, m.box("Live", live, lw, 0, false), m.box("This machine", mach, w-1-lw, 0, false))...)
	case live != nil && mach != nil:
		out = append(out, m.box("Live", live, w, 0, false)...)
		out = append(out, m.box("This machine", mach, w, 0, false)...)
	case live != nil:
		out = append(out, m.box("Live", live, w, 0, false)...)
	}
	out = append(out, m.box("Health", health, w, 0, false)...)
	return out
}

func (m *Model) liveBox(sparks bool) []string {
	if !m.haveRep || m.sampler == nil {
		return []string{m.th.Dim("Reading your system…")}
	}
	mt := m.met
	bw := 16
	var l []string
	cpu := fmt.Sprintf("%4.0f%%", mt.CPUPct)
	extra := ""
	if mt.CPUMHz > 0 {
		extra += fmt.Sprintf(" %4.2f GHz", mt.CPUMHz/1000)
	}
	if mt.TempC > 0 {
		extra += fmt.Sprintf(" %3.0f°C", mt.TempC)
	}
	if !mt.HasRates {
		cpu = "  …"
	}
	spark := func(data []float64, hi float64) {
		if sparks {
			l = append(l, m.kv("", m.spark(data, bw, hi), 4))
		}
	}
	l = append(l, m.kv("CPU", m.bar(mt.CPUPct, bw)+" "+cpu+m.th.Dim(extra), 4))
	spark(m.hist.cpu, 100)
	swap := ""
	if mt.SwapUsedPct > 0 {
		swap = fmt.Sprintf(" swap %.0f%%", mt.SwapUsedPct)
	}
	l = append(l, m.kv("MEM", m.bar(mt.MemUsedPct, bw)+fmt.Sprintf(" %3.0f%%", mt.MemUsedPct)+m.th.Dim(swap), 4))
	if len(mt.GPUs) == 0 {
		l = append(l, m.kv("GPU", m.th.Dim("none detected"), 4))
	}
	for i, g := range mt.GPUs {
		if !g.Available {
			l = append(l, m.kv("GPU", m.th.Dim(g.Note), 4))
			continue
		}
		l = append(l, m.kv("GPU", m.bar(g.LoadPct, bw)+fmt.Sprintf(" %3.0f%%", g.LoadPct)+m.th.Dim(fmt.Sprintf(" %3.0f°C %3.0f/%.0f MiB", g.TempC, g.VRAMUsed, g.VRAMTotal)), 4))
		if i == 0 {
			spark(m.hist.gpu, 100)
		}
	}
	if mt.NetIface != "" {
		l = append(l, m.kv("NET", m.th.Dim(mt.NetIface)+"  ↓ "+rate(mt.NetRxBps)+"  ↑ "+rate(mt.NetTxBps), 4))
		spark(m.hist.rx, 0)
	} else {
		l = append(l, m.kv("NET", m.th.Dim("no active connection"), 4))
	}
	return l
}

func shorten(s string, n int) string { return strings.TrimRight(fit(s, n), " ") }

func (m *Model) machineBox() []string {
	if !m.haveRep {
		return []string{m.th.Dim("Reading your system…")}
	}
	r := m.rep
	var l []string
	add := func(k, v string) {
		if v != "" {
			l = append(l, m.kv(k, v, 7))
		}
	}
	add("OS", joinNonEmpty(" · ", r.System.Distro, r.System.Kernel))
	add("Session", joinNonEmpty(" · ", r.System.SessionType, r.System.Desktop))
	add("CPU", fmt.Sprintf("%s (%d threads)", shorten(r.CPU.Model, 34), r.CPU.Threads))
	add("Memory", human(r.Memory.TotalKB*1024)+" RAM"+zramNote(r.Memory))
	for _, g := range r.GPUs {
		add("GPU", joinNonEmpty(" · ", shorten(g.Name, 30), g.Driver, g.RuntimeStatus))
	}
	add("Power", powerLine(r))
	add("Network", netLine(r))
	return l
}

func zramNote(mem detect.Memory) string {
	if mem.ZramDevices > 0 {
		return " + zram"
	}
	return ""
}

func powerLine(r detect.Report) string {
	var p []string
	if r.Power.PPDProfile != "" {
		p = append(p, "profile "+r.Power.PPDProfile)
	}
	if r.CPU.Governor != "" {
		p = append(p, "governor "+r.CPU.Governor)
	}
	if r.Power.OnAC != nil {
		if *r.Power.OnAC {
			p = append(p, "on AC")
		} else {
			p = append(p, "on battery")
		}
	}
	return strings.Join(p, " · ")
}

func netLine(r detect.Report) string {
	if r.Network.DefaultIface == "" {
		return "no default route"
	}
	kind := ""
	for _, i := range r.Network.Interfaces {
		if i.Name == r.Network.DefaultIface {
			kind = i.Kind
		}
	}
	return joinNonEmpty(" · ", r.Network.DefaultIface, kind, r.Network.ResolverMode)
}

func (m *Model) healthBox(inner int) []string {
	if !m.haveRep {
		return []string{m.th.Dim("Checking…")}
	}
	warn, note := diagnostics.Counts(m.findings)
	var l []string
	sum := fmt.Sprintf("%s  %s", m.countBadge(warn, "warning", m.th.Warn), m.countBadge(note, "notice", m.th.Warn))
	scan := m.th.Dim("not scanned yet: press s for a full scan")
	if m.scanned {
		scan = m.th.Dim("full scan " + m.scanAt.Format("15:04"))
		rec := len(m.recommended())
		if rec == 0 {
			sum += "   " + m.th.OK(m.gl.Check+" no optimization recommended")
		} else {
			sum += "   " + m.th.Accent(fmt.Sprintf("%d optimization(s) recommended", rec))
		}
	}
	l = append(l, sum+"   "+scan)
	fs := append([]diagnostics.Finding(nil), m.findings...)
	sort.SliceStable(fs, func(i, j int) bool { return fs[i].Sev > fs[j].Sev })
	shown := 0
	for _, f := range fs {
		if f.Sev < diagnostics.SevNotice || shown >= 3 {
			continue
		}
		l = append(l, m.sevTag(f.Sev)+" "+f.Title)
		shown++
	}
	if shown == 0 {
		l = append(l, m.th.OK(m.gl.Check)+" Nothing needs your attention.")
	}
	if len(m.interrupted) > 0 {
		l = append(l, m.th.Err("!")+" An unfinished change was found: open the Vault (7) to recover it.")
	}
	return l
}

func (m *Model) countBadge(n int, word string, style func(string) string) string {
	s := fmt.Sprintf("%d %s", n, word)
	if n != 1 {
		s += "s"
	}
	if n == 0 {
		return m.th.OK(s)
	}
	return style(s)
}

// sevTag is a fixed-width text label (colour is never the only signal).
func (m *Model) sevTag(s diagnostics.Severity) string {
	switch s {
	case diagnostics.SevWarn:
		return m.th.Err("WARN  ")
	case diagnostics.SevNotice:
		return m.th.Warn("NOTICE")
	case diagnostics.SevInfo:
		return m.th.Dim("INFO  ")
	}
	return m.th.OK("OK    ")
}
