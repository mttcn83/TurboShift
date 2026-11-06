package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/mttcn83/turboshift/internal/tweaks"
)

type optState struct {
	checked map[string]bool
	choice  map[string]int // index into the tweak's options
	cursor  int
	top     int
	search  textInput
	cat     tweaks.Category // "" = all categories
	dscroll int             // scroll offset of the details pane
}

func tierTag(t tweaks.Tier) string {
	switch t {
	case tweaks.TierRecommended:
		return "REC"
	case tweaks.TierSituational:
		return "SIT"
	}
	return "ADV"
}

// stateSummary condenses a tweak's state map into a short value ("balanced",
// or "powersave/performance" when per-core values differ).
func stateSummary(s tweaks.State) string {
	seen := map[string]bool{}
	var vals []string
	for _, v := range s {
		if !seen[v] {
			seen[v] = true
			vals = append(vals, v)
		}
	}
	sort.Strings(vals)
	if len(vals) == 0 {
		return "?"
	}
	return strings.Join(vals, "/")
}

// summaryOf describes a tweak's current state in a few words.
func summaryOf(t tweaks.Tweak, s tweaks.State) string {
	if sm, ok := t.(tweaks.Summarizer); ok {
		return sm.Summary(s)
	}
	return stateSummary(s)
}

// humanState is summaryOf with a raw value swapped for its option label
// ("-1" becomes "Never suspend") when the tweak is in a single known state.
func humanState(t tweaks.Tweak, as tweaks.Assessment) string {
	s := summaryOf(t, as.Current)
	for _, o := range optionsOf(t.Meta(), as) {
		if o.Value == s {
			return o.Label
		}
	}
	return s
}

// alreadyAt reports whether every part of the tweak already has the value.
func alreadyAt(as tweaks.Assessment, v string) bool {
	if len(as.Current) == 0 {
		return false
	}
	for _, c := range as.Current {
		if c != v {
			return false
		}
	}
	return true
}

// tickPreset replaces the current selection with the preset's items that
// apply to this machine and are not already set. It changes nothing on the
// system: the user still reviews the exact preview before anything is applied.
func (m *Model) tickPreset(p tweaks.Preset) (ticked int) {
	if !m.scanned {
		m.notify(false, "Scan first (press r) so TurboShift knows which settings fit this PC.")
		return 0
	}
	m.opt.checked = map[string]bool{}
	m.opt.search.val = ""
	skipped := 0
	first := ""
	for _, it := range p.Items {
		t, ok := m.app.Reg.Get(it.TweakID)
		as := m.assess[it.TweakID]
		if !ok || !as.Available {
			skipped++
			continue
		}
		mt := t.Meta()
		opts := optionsOf(mt, as)
		idx := optionIndex(opts, it.Value)
		if len(opts) == 0 || opts[idx].Value != it.Value || alreadyAt(as, it.Value) {
			skipped++
			continue
		}
		m.opt.checked[it.TweakID] = true
		m.opt.choice[it.TweakID] = idx
		if first == "" {
			first = it.TweakID
		}
		ticked++
	}
	for i, v := range m.visibleTweaks() {
		if v.Meta().ID == first {
			m.opt.cursor = i
			break
		}
	}
	if ticked == 0 {
		m.notify(false, fmt.Sprintf("%s: nothing to change, all %d settings are already set or unavailable on this PC.", p.Name, len(p.Items)))
		return 0
	}
	m.notify(true, fmt.Sprintf("%s: %d setting(s) selected, %d not applicable here.", p.Name, ticked, skipped))
	return ticked
}

// presetFit counts the preset's settings that would change something here.
func (m *Model) presetFit(p tweaks.Preset) int {
	n := 0
	for _, it := range p.Items {
		as := m.assess[it.TweakID]
		if !as.Available || alreadyAt(as, it.Value) {
			continue
		}
		if opts := optionsOf(m.metaOf(it.TweakID), as); len(opts) > 0 && opts[optionIndex(opts, it.Value)].Value == it.Value {
			n++
		}
	}
	return n
}

func (m *Model) metaOf(id string) tweaks.Meta {
	if t, ok := m.app.Reg.Get(id); ok {
		return t.Meta()
	}
	return tweaks.Meta{}
}

// startPreset is the one-step path from a keypress or click to the preview:
// scan first if needed, select what fits, then show the exact changes.
func (m *Model) startPreset(id string) tea.Cmd {
	p, ok := tweaks.PresetByID(id)
	if !ok {
		return nil
	}
	if !m.scanned {
		m.pendingPreset = id
		return m.startScan(false)
	}
	if m.tickPreset(p) == 0 {
		return nil
	}
	return m.previewCmd(m.selected())
}

// openPresets lists the ready-made bundles.
func (m *Model) openPresets() {
	md := &modal{kind: modalConfirm, title: "Presets: one-step bundles"}
	md.add("dim", "Pick a goal. You will see the exact changes first; nothing happens until you confirm.")
	md.gap()
	for _, p := range tweaks.Presets() {
		n := m.presetFit(p)
		fit := fmt.Sprintf("%d of %d settings apply here", n, len(p.Items))
		if !m.scanned {
			fit = "scan first to see what applies"
		}
		md.add("head", p.Headline+"  ("+p.Key+")  "+fit)
		md.add("", p.Summary, 2)
		md.add("dim", "Best for: "+p.Best, 2)
		md.add("dim", "Cost: "+p.Cost, 2)
		md.gap()
		md.buttons = append(md.buttons, mbtn{p.Name, "preset:" + p.ID, p.Key})
	}
	md.buttons = append(md.buttons, mbtn{"Cancel (Esc)", "close", ""})
	m.modal = md
}

// visibleTweaks is the catalog filtered by the search box.
func (m *Model) visibleTweaks() []tweaks.Tweak {
	q := strings.ToLower(strings.TrimSpace(m.opt.search.val))
	var out []tweaks.Tweak
	for _, t := range m.app.Reg.All() {
		mt := t.Meta()
		hay := strings.ToLower(mt.ID + " " + mt.Name + " " + mt.Summary + " " + string(mt.Category) + " " + string(mt.Tier) + " " + mt.Risk.String())
		if m.opt.cat != "" && mt.Category != m.opt.cat {
			continue
		}
		if q == "" || strings.Contains(hay, q) {
			out = append(out, t)
		}
	}
	return out
}

// selectTweak ticks a tweak and chooses the value a Doctor finding suggested.
func (m *Model) selectTweak(id string, params map[string]string) {
	t, ok := m.app.Reg.Get(id)
	if !ok {
		return
	}
	mt := t.Meta()
	m.opt.search.val = ""
	if as, ok := m.assess[id]; ok && as.Available {
		m.opt.checked[id] = true
		m.opt.choice[id] = optionIndex(optionsOf(mt, as), params[mt.ParamName])
	}
	for i, v := range m.visibleTweaks() {
		if v.Meta().ID == id {
			m.opt.cursor = i
		}
	}
}

func (m *Model) keyOptimizer(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	k := msg.String()
	vis := m.visibleTweaks()
	switch k {
	case "pgup":
		m.opt.dscroll = max(0, m.opt.dscroll-6)
		return nil, true
	case "pgdown":
		m.opt.dscroll += 6
		return nil, true
	}
	if c, ok := moveKey(k, m.opt.cursor, len(vis), 6); ok {
		if c != m.opt.cursor {
			m.opt.dscroll = 0
		}
		m.opt.cursor = c
		return nil, true
	}
	switch k {
	case "/":
		m.opt.search.active = true
		return nil, true
	case "c":
		m.cycleCategory()
		return nil, true
	case "esc":
		if m.opt.cat != "" && m.opt.search.val == "" {
			m.opt.cat, m.opt.cursor, m.opt.top = "", 0, 0
			return nil, true
		}
		if m.opt.search.val != "" {
			m.opt.search.val = ""
			m.opt.cursor = 0
			return nil, true
		}
	case " ", "space":
		return m.optAct(fmt.Sprintf("tog:%d", m.opt.cursor)), true
	case "left", "right":
		d := 1
		if k == "left" {
			d = -1
		}
		return m.optAct(fmt.Sprintf("cyc:%d:%d", m.opt.cursor, d)), true
	case "a", "enter":
		return m.optAct("apply"), true
	case "r":
		return m.startScan(false), true
	case "p":
		m.openPresets()
		return nil, true
	}
	return nil, false
}

// cycleCategory steps the list through All, then each category in catalogue order.
func (m *Model) cycleCategory() {
	var cats []tweaks.Category
	seen := map[tweaks.Category]bool{}
	for _, t := range m.app.Reg.All() {
		if c := t.Meta().Category; !seen[c] {
			seen[c] = true
			cats = append(cats, c)
		}
	}
	next := tweaks.Category("")
	if m.opt.cat == "" {
		next = cats[0]
	} else {
		for i, c := range cats {
			if c == m.opt.cat && i+1 < len(cats) {
				next = cats[i+1]
			}
		}
	}
	m.opt.cat, m.opt.cursor, m.opt.top, m.opt.dscroll = next, 0, 0, 0
}

func (m *Model) optAct(arg string) tea.Cmd {
	vis := m.visibleTweaks()
	parts := strings.Split(arg, ":")
	idx := -1
	if len(parts) > 1 {
		fmt.Sscanf(parts[1], "%d", &idx)
	}
	at := func() (tweaks.Tweak, bool) {
		if idx < 0 || idx >= len(vis) {
			return nil, false
		}
		return vis[idx], true
	}
	switch parts[0] {
	case "sel":
		if _, ok := at(); ok {
			if idx != m.opt.cursor {
				m.opt.dscroll = 0
			}
			m.opt.cursor = idx
		}
	case "tog":
		t, ok := at()
		if !ok {
			return nil
		}
		m.opt.cursor = idx
		mt := t.Meta()
		as := m.assess[mt.ID]
		if !as.Available {
			reason := as.Reason
			if !m.scanned {
				reason = "scan first (press r) so TurboShift can check this machine"
			}
			m.notify(false, mt.Name+" is not available: "+reason)
			return nil
		}
		m.opt.checked[mt.ID] = !m.opt.checked[mt.ID]
	case "cyc":
		t, ok := at()
		if !ok {
			return nil
		}
		m.opt.cursor = idx
		mt := t.Meta()
		opts := optionsOf(mt, m.assess[mt.ID])
		if len(opts) > 1 {
			d := 1
			if len(parts) > 2 && parts[2] == "-1" {
				d = -1
			}
			m.opt.choice[mt.ID] = (m.opt.choice[mt.ID] + d + len(opts)) % len(opts)
		}
	case "apply":
		reqs := m.selected()
		if len(reqs) == 0 {
			m.notify(false, "Nothing selected: move to an item and press Space to tick it.")
			return nil
		}
		return m.previewCmd(reqs)
	case "scan":
		return m.startScan(false)
	case "cat":
		m.cycleCategory()
	case "presets":
		m.openPresets()
	}
	return nil
}

func (m *Model) viewOptimizer(w, h int) []string {
	vis := m.visibleTweaks()
	m.opt.cursor = clampI(m.opt.cursor, 0, max(0, len(vis)-1))
	var out []string

	head := " " + m.th.Bold("Optimizer")
	switch {
	case m.opt.search.active:
		head += "   " + m.th.Accent("search:") + " " + m.opt.search.val + m.th.Accent("▏")
		if m.gl.Full == "#" {
			head = " " + m.th.Bold("Optimizer") + "   search: " + m.opt.search.val + "_"
		}
	case m.opt.search.val != "":
		head += "   " + m.th.Accent("filter:") + " " + m.opt.search.val + m.th.Dim("  (Esc clears)")
	case m.opt.cat != "":
		head += "   " + m.th.Accent("category: "+string(m.opt.cat)) + m.th.Dim("  (c next · Esc all)")
	default:
		head += m.th.Dim("   REC recommended · SIT situational · ADV advanced · every change is previewed")
	}
	out = append(out, head, "")
	if !m.scanned {
		out = append(out, " "+m.th.Warn("Not scanned yet.")+" TurboShift needs a scan to know which tweaks apply here. "+m.th.Sel("[ Scan now (r) ]"))
		m.addHit(1, bodyY+len(out)-1, w-1, 1, "opt:scan")
		out = append(out, "")
	}

	footer := 2
	avail := h - len(out) - footer
	sideBySide := w >= 100
	listW, detW := w, w
	listH := max(5, avail*55/100)
	if sideBySide {
		listW = w * 60 / 100
		detW = w - listW - 1
		listH = avail
	}
	listH = min(listH, avail)

	// ---- list ----
	rows := listH - 2
	m.opt.top = scrollWindow(m.opt.cursor, m.opt.top, max(1, rows), len(vis))
	var lb []string
	if len(vis) == 0 {
		lb = append(lb, m.th.Dim("No optimization matches \""+m.opt.search.val+"\"."))
	}
	inner := listW - 4
	for i := m.opt.top; i < min(len(vis), m.opt.top+rows); i++ {
		mt := vis[i].Meta()
		as := m.assess[mt.ID]
		box := m.gl.Off
		if m.opt.checked[mt.ID] {
			box = m.gl.On
		}
		state := m.th.Dim("unavailable")
		if !as.Available && !m.scanned {
			state = m.th.Dim("scan needed")
		} else if as.Available {
			opts := optionsOf(mt, as)
			choice := ""
			if len(opts) > 0 {
				choice = opts[clampI(m.opt.choice[mt.ID], 0, len(opts)-1)].Label
			}
			if inner >= 76 {
				state = m.th.Dim(fit(humanState(vis[i], as), 14)) + " " + m.gl.Arrow + " " + fit(choice, 14)
			} else if m.opt.checked[mt.ID] {
				state = m.gl.Arrow + " " + fit(choice, 14)
			} else {
				state = ""
			}
		}
		if inner < 76 && !as.Available {
			state = "" // the detail pane explains why; keep the name readable
		}
		star := " "
		if as.Available && as.Recommend != nil {
			star = m.th.Accent("*")
		}
		fixed := 3 + 1 + 3 + 1 + 4 + 1 + 1
		stateW := 0
		if inner >= 76 {
			stateW = 32
		} else if m.opt.checked[mt.ID] {
			stateW = 17
		}
		name := fit(mt.Name, max(10, inner-fixed-stateW-1))
		line := box + " " + tierTag(mt.Tier) + " " + m.risk(mt.Risk.String()) + " " + star + name + " " + state
		if i == m.opt.cursor {
			line = m.th.Sel(fit(stripANSI(line), inner))
		}
		m.addHit(0, bodyY+len(out)+1+len(lb), listW, 1, fmt.Sprintf("opt:sel:%d", i))
		m.addHit(1, bodyY+len(out)+1+len(lb), 4, 1, fmt.Sprintf("opt:tog:%d", i))
		lb = append(lb, line)
	}
	title := fmt.Sprintf("Catalog (%d)", len(vis))
	if len(vis) > 0 && m.opt.cat == "" {
		title += " · " + string(vis[m.opt.cursor].Meta().Category)
	}
	listBox := m.box(title, lb, listW, listH, true)

	// ---- detail ----
	var db []string
	if len(vis) > 0 {
		db = m.tweakDetail(vis[m.opt.cursor], detW-4)
	}
	detH := avail
	if !sideBySide {
		detH = avail - listH
	}
	detTitle := "Details"
	if visD := max(1, detH-2); len(db) > visD {
		m.opt.dscroll = clampI(m.opt.dscroll, 0, len(db)-visD)
		db = db[m.opt.dscroll:min(len(db), m.opt.dscroll+visD)]
		detTitle = "Details (PgUp/PgDn for more)"
	} else {
		m.opt.dscroll = 0
	}
	detBox := m.box(detTitle, db, detW, max(5, detH), false)

	if sideBySide {
		out = append(out, joinCols(1, listBox, detBox)...)
	} else {
		out = append(out, listBox...)
		if detH >= 5 {
			out = append(out, detBox...)
		}
	}

	n := len(m.selected())
	btn := fmt.Sprintf("[ Preview & apply %d selected (a) ]", n)
	by := bodyY + len(out)
	pb, cb := "[ Presets (p) ]", "[ Category (c) ]"
	if n == 0 {
		btn = "[ Preview & apply (a) ]"
		out = append(out, " "+m.th.Dim(btn)+" "+m.th.Bold(pb)+" "+m.th.Bold(cb)+m.th.Dim("  Space ticks an item · ←/→ changes its option"))
	} else {
		out = append(out, " "+m.th.Sel(btn)+" "+m.th.Bold(pb)+" "+m.th.Bold(cb)+m.th.Dim("  nothing changes until you confirm"))
	}
	m.addHit(1, by, vw(btn), 1, "opt:apply")
	m.addHit(2+vw(btn), by, vw(pb), 1, "opt:presets")
	m.addHit(3+vw(btn)+vw(pb), by, vw(cb), 1, "opt:cat")
	return out
}

func (m *Model) tweakDetail(t tweaks.Tweak, w int) []string {
	mt := t.Meta()
	as := m.assess[mt.ID]
	var d []string
	d = append(d, m.th.Bold(mt.Name))
	d = append(d, m.th.Dim(fmt.Sprintf("%s · %s · risk %s · lasts: %s", mt.Category, mt.Tier, mt.Risk, mt.Scope)))
	if mt.NeedsAuth {
		d = append(d, m.th.Dim("Needs administrator approval when applied."))
	}
	d = append(d, "")
	d = append(d, wrapText(mt.Summary, w)...)
	if mt.Helps != "" {
		d = append(d, "", m.th.Bold("Who it helps"))
		d = append(d, wrapText(mt.Helps, w)...)
	}
	if !as.Available {
		reason := as.Reason
		if !m.scanned {
			reason = "not scanned yet"
		}
		d = append(d, "", m.th.Warn("Not available: ")+reason)
	} else {
		if as.Reason != "" {
			d = append(d, "", m.th.Warn("Note: ")+as.Reason)
		}
		d = append(d, "", m.kv("Now", humanState(t, as), 6))
		opts := optionsOf(mt, as)
		cur := clampI(m.opt.choice[mt.ID], 0, max(0, len(opts)-1))
		for i, o := range opts {
			mark := "  "
			if i == cur {
				mark = m.th.Accent(m.gl.Arrow) + " "
			}
			d = append(d, mark+m.th.Bold(o.Label))
			if o.Note != "" {
				d = append(d, "    "+m.th.Dim(o.Note))
			}
		}
		if as.Recommend != nil {
			d = append(d, "", m.th.Accent("Recommended now: ")+as.Recommend.Why)
		} else {
			d = append(d, "", m.th.Dim("No evidence on this machine suggests changing this right now."))
		}
	}
	d = append(d, "", m.th.Bold("What it does"))
	d = append(d, wrapText(mt.Details, w)...)
	d = append(d, "", m.th.Bold("Trade-off"))
	d = append(d, wrapText(mt.Tradeoff, w)...)
	d = append(d, "", m.th.Bold("How to check it helped"))
	d = append(d, wrapText(mt.Evidence, w)...)
	return d
}
