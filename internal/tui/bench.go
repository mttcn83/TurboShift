package tui

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/mttcn83/turboshift/internal/benchmark"
)

type benchState struct {
	runs       []benchmark.Run
	logs       []string
	loaded     bool
	err        error
	cursor     int
	top        int
	selected   map[string]bool // labels chosen for A/B
	selOrder   []string
	label      textInput
	pendingLog string
	lastLabel  string
	cmp        *benchmark.Comparison
}

type brow struct {
	kind  string // label | run | log
	label string
	run   benchmark.Run
	log   string
}

func (m *Model) benchRows() []brow {
	var rows []brow
	by := benchmark.ByLabel(m.bn.runs)
	for _, l := range benchmark.Labels(m.bn.runs) {
		rows = append(rows, brow{kind: "label", label: l})
		for _, r := range by[l] {
			rows = append(rows, brow{kind: "run", label: l, run: r})
		}
	}
	for _, p := range m.bn.logs {
		rows = append(rows, brow{kind: "log", log: p})
	}
	return rows
}

func (m *Model) onBenchLoaded(msg benchLoadedMsg) {
	m.bn.loaded, m.bn.err = true, msg.err
	if msg.err == nil || msg.runs != nil {
		m.bn.runs, m.bn.logs = msg.runs, msg.logs
	}
	have := map[string]bool{}
	for _, l := range benchmark.Labels(m.bn.runs) {
		have[l] = true
	}
	var order []string
	for _, l := range m.bn.selOrder {
		if have[l] {
			order = append(order, l)
		} else {
			delete(m.bn.selected, l)
		}
	}
	m.bn.selOrder = order
	if len(order) < 2 {
		m.bn.cmp = nil
	}
	m.bn.cursor = clampI(m.bn.cursor, 0, max(0, len(m.benchRows())-1))
}

func (m *Model) keyBench(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	k := msg.String()
	rows := m.benchRows()
	if c, ok := moveKey(k, m.bn.cursor, len(rows), 8); ok {
		m.bn.cursor = c
		return nil, true
	}
	switch k {
	case " ", "space":
		return m.benchAct(fmt.Sprintf("tog:%d", m.bn.cursor)), true
	case "enter", "i":
		return m.benchAct(fmt.Sprintf("open:%d", m.bn.cursor)), true
	case "c":
		return m.benchAct("compare"), true
	case "d":
		return m.benchAct(fmt.Sprintf("del:%d", m.bn.cursor)), true
	case "r":
		return m.loadBench(), true
	}
	return nil, false
}

func (m *Model) benchAct(arg string) tea.Cmd {
	parts := strings.Split(arg, ":")
	rows := m.benchRows()
	idx := -1
	if len(parts) > 1 {
		fmt.Sscanf(parts[1], "%d", &idx)
	}
	var row brow
	if idx >= 0 && idx < len(rows) {
		row = rows[idx]
		m.bn.cursor = idx
	}
	switch parts[0] {
	case "sel":
	case "tog":
		if row.kind != "label" {
			m.notify(false, "Select a configuration row (a label) to include it in a comparison.")
			return nil
		}
		m.toggleLabel(row.label)
	case "open":
		switch row.kind {
		case "log":
			m.bn.pendingLog = row.log
			m.bn.label.val = m.bn.lastLabel
			m.bn.label.active = true
		case "label":
			m.toggleLabel(row.label)
		}
	case "compare":
		if len(m.bn.selOrder) != 2 {
			m.notify(false, "Pick exactly two configurations with Space, then press c.")
			return nil
		}
		runs := benchmark.ByLabel(m.bn.runs)
		a, b := m.bn.selOrder[0], m.bn.selOrder[1]
		c := benchmark.Compare(a, runs[a], b, runs[b])
		m.bn.cmp = &c
	case "del":
		if row.kind != "run" {
			m.notify(false, "Move to an imported run to delete it. Original log files are never touched.")
			return nil
		}
		md := &modal{kind: modalConfirm, title: "Delete imported run?", yes: "delete", arg: row.run.ID}
		md.add("", fmt.Sprintf("Remove the imported summary of %q from configuration %q.", row.run.Source, row.run.Label))
		md.add("dim", "Your original MangoHud log file is not deleted.")
		md.buttons = []mbtn{{"Delete (Enter)", "yes", "y"}, {"Keep (Esc)", "close", "n"}}
		m.modal = md
	case "reload":
		return m.loadBench()
	}
	return nil
}

func (m *Model) toggleLabel(l string) {
	if m.bn.selected[l] {
		delete(m.bn.selected, l)
		for i, x := range m.bn.selOrder {
			if x == l {
				m.bn.selOrder = append(m.bn.selOrder[:i], m.bn.selOrder[i+1:]...)
				break
			}
		}
		m.bn.cmp = nil
		return
	}
	if len(m.bn.selOrder) == 2 { // keep the two most recent picks
		delete(m.bn.selected, m.bn.selOrder[0])
		m.bn.selOrder = m.bn.selOrder[1:]
	}
	m.bn.selected[l] = true
	m.bn.selOrder = append(m.bn.selOrder, l)
	m.bn.cmp = nil
}

func (m *Model) benchLabelDone(accepted bool) tea.Cmd {
	path, label := m.bn.pendingLog, strings.TrimSpace(m.bn.label.val)
	m.bn.pendingLog = ""
	if !accepted || path == "" {
		return nil
	}
	if err := benchmark.ValidateLabel(label); err != nil {
		m.notify(false, err.Error())
		return nil
	}
	m.bn.lastLabel = label
	a := m.app
	m.notify(true, "Importing "+filepath.Base(path)+"…")
	return func() tea.Msg {
		r, err := a.Bench.Import(a.FS, path, label, benchmark.ImportOptions{})
		return importDoneMsg{run: r, src: path, err: err}
	}
}

func (m *Model) onImportDone(msg importDoneMsg) tea.Cmd {
	if msg.err != nil {
		m.notify(false, "Import failed: "+msg.err.Error())
		return nil
	}
	r := msg.run
	m.notify(true, fmt.Sprintf("Imported %s: %.1f avg fps, %.1f fps 1%% low, %d frames.", r.Source, r.AvgFPS, r.Low1FPS, r.Frames))
	return m.loadBench()
}

func (m *Model) viewBench(w, h int) []string {
	var out []string
	out = append(out, " "+m.th.Bold("Benchmark Lab")+m.th.Dim("   import MangoHud logs, compare two configurations with honest statistics"), "")
	if !m.bn.loaded {
		return append(out, " "+m.th.Dim("Loading…"))
	}
	if m.bn.err != nil {
		out = append(out, " "+m.th.Err("Could not read stored runs: "+m.bn.err.Error()))
	}
	rows := m.benchRows()
	m.bn.cursor = clampI(m.bn.cursor, 0, max(0, len(rows)-1))

	if m.bn.label.active {
		out = append(out, " "+m.th.Accent("Label for "+filepath.Base(m.bn.pendingLog)+" (the configuration it was recorded under, e.g. balanced):")+" "+m.bn.label.val+m.th.Accent("_"), "")
	}

	avail := h - len(out) - 1
	side := w >= 100
	listW, detW := w, w
	listH := max(6, avail/2)
	if side {
		listW = w * 50 / 100
		detW = w - listW - 1
		listH = avail
	}
	listH = min(listH, avail)
	vis := max(1, listH-2)
	m.bn.top = scrollWindow(m.bn.cursor, m.bn.top, vis, len(rows))

	var lb []string
	if len(rows) == 0 {
		lb = append(lb,
			m.th.Dim("No runs and no MangoHud logs found yet."),
			"",
			m.th.Dim("1. Gaming screen: build launch options with"),
			m.th.Dim("   \"Record a MangoHud log\" on."),
			m.th.Dim("2. Play a few minutes per configuration"),
			m.th.Dim("   (several runs each; same scene)."),
			m.th.Dim("3. Come back here and import the logs."))
	}
	inner := listW - 4
	for i := m.bn.top; i < min(len(rows), m.bn.top+vis); i++ {
		r := rows[i]
		var line string
		switch r.kind {
		case "label":
			mark := m.gl.Off
			if m.bn.selected[r.label] {
				mark = m.gl.On
			}
			n := 0
			for _, x := range m.bn.runs {
				if x.Label == r.label {
					n++
				}
			}
			line = fmt.Sprintf("%s %s  (%d run%s)", mark, m.th.Bold(r.label), n, plural(n))
		case "run":
			line = fmt.Sprintf("    %s  %-18s %6.1f avg %6.1f low", r.run.StartedAt.Local().Format("01-02 15:04"), shorten(r.run.Source, 18), r.run.AvgFPS, r.run.Low1FPS)
		case "log":
			line = m.th.Accent("+") + " import  " + m.th.Dim(shortPath(r.log, inner-10))
		}
		if i == m.bn.cursor {
			line = m.th.Sel(fit(stripANSI(line), inner))
		}
		m.addHit(0, bodyY+len(out)+1+len(lb), listW, 1, fmt.Sprintf("bench:sel:%d", i))
		lb = append(lb, line)
	}
	listBox := m.box(fmt.Sprintf("Runs & logs found (%d)", len(rows)), lb, listW, listH, true)

	db := m.benchDetail(rows, detW-4, max(3, avail-listH-2))
	detH := avail
	if !side {
		detH = avail - listH
	}
	if side {
		out = append(out, joinCols(1, listBox, m.box("Comparison", db, detW, max(5, detH), false))...)
	} else {
		out = append(out, listBox...)
		if detH >= 5 {
			out = append(out, m.box("Comparison", db, detW, detH, false)...)
		}
	}
	out = append(out, " "+m.hintsLine())
	return out
}

func (m *Model) hintsLine() string {
	return m.th.Dim("Space picks two configurations · c compares · Enter on \"+ import\" imports a log · d deletes an imported run")
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func shortPath(p string, w int) string {
	if vw(p) <= w {
		return p
	}
	return "…" + fitLeft(p, w-1)
}

func (m *Model) benchDetail(rows []brow, w, h int) []string {
	var d []string
	if m.bn.cmp == nil {
		if len(rows) > 0 && m.bn.cursor < len(rows) && rows[m.bn.cursor].kind == "run" {
			r := rows[m.bn.cursor].run
			d = append(d, m.th.Bold(r.Source), m.th.Dim("configuration: "+r.Label+" · "+benchmark.ProgramName(r.Source)), "")
			d = append(d,
				fmt.Sprintf("Average        %7.1f fps", r.AvgFPS),
				fmt.Sprintf("1%% low         %7.1f fps", r.Low1FPS),
				fmt.Sprintf("0.1%% low       %7.1f fps", r.Low01FPS),
				fmt.Sprintf("Frame time     p50 %.2f · p95 %.2f · p99 %.2f ms", r.FrameP50Ms, r.FrameP95Ms, r.FrameP99Ms),
				fmt.Sprintf("Duration       %.0f s, %d frames", r.DurationS, r.Frames))
			if r.Bottleneck != "" {
				d = append(d, "", m.th.Dim(r.Bottleneck))
			}
			return d
		}
		d = append(d, m.th.Dim("Tick two configurations with Space, then press c."), "",
			m.th.Dim("Verdicts come from an exact permutation test on whole runs:"),
			m.th.Dim("a result is called an improvement only if it is both"),
			m.th.Dim("statistically clear and at least 1% in size. With too few"),
			m.th.Dim("runs the answer is \"insufficient\", never a guess."))
		return d
	}
	c := m.bn.cmp
	v, why := c.Overall()
	d = append(d, m.th.Bold(c.LabelA+"  vs  "+c.LabelB), m.verdict(v)+" "+m.th.Dim(why), "")
	for _, r := range c.Results {
		d = append(d, m.th.Bold(r.Metric.Name))
		d = append(d, fmt.Sprintf("  %s %.1f %s %s %s %.1f %s  (%+.1f%%)", c.LabelA, r.MedianA, m.gl.Arrow, c.LabelB, "", r.MedianB, r.Metric.Unit, r.DeltaPct))
		d = append(d, m.th.Dim(fmt.Sprintf("  runs %d vs %d · spread %.1f–%.1f / %.1f–%.1f · run-to-run CV %.1f%% / %.1f%%", r.NA, r.NB, r.MinA, r.MaxA, r.MinB, r.MaxB, r.CVA*100, r.CVB*100)))
		p := "n/a"
		if !math.IsNaN(r.P) {
			p = fmt.Sprintf("%.3f", r.P)
		}
		d = append(d, "  "+m.verdict(r.Verdict)+m.th.Dim("  p="+p))
		for _, x := range wrapText(r.Reason, w-4) {
			d = append(d, m.th.Dim("    "+x))
		}
	}
	for _, wn := range c.Warnings {
		for i, x := range wrapText(wn, w-3) {
			pre := "  "
			if i == 0 {
				pre = m.th.Warn(m.gl.Warn) + " "
			}
			d = append(d, pre+x)
		}
	}
	return d
}

func (m *Model) verdict(v benchmark.Verdict) string {
	s := string(v)
	switch v {
	case benchmark.VerdictImproved:
		return m.th.OK(m.gl.Check + " " + s)
	case benchmark.VerdictRegressed:
		return m.th.Err(m.gl.Cross + " " + s)
	}
	return m.th.Warn(m.gl.Info + " " + s)
}
