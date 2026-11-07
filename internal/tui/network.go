package tui

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/mttcn83/turboshift/internal/diagnostics"
)

type netState struct {
	external                bool
	target, lookup, loadURL textInput
	editing                 *textInput
	backup                  string
	res                     *diagnostics.NetReport
	scroll                  int
}

func (m *Model) netOptions() diagnostics.NetOptions {
	o := diagnostics.NetOptions{External: m.net.external, PingTarget: m.net.target.val, LookupName: m.net.lookup.val, LoadSeconds: 10}
	if m.net.external {
		o.LoadDownURL = strings.TrimSpace(m.net.loadURL.val)
	}
	return o
}

func (m *Model) keyNetwork(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	switch k := msg.String(); k {
	case "r", "enter":
		return m.netAct("run"), true
	case "e":
		return m.netAct("ext"), true
	case "t":
		return m.netAct("edit:target"), true
	case "l":
		return m.netAct("edit:lookup"), true
	case "u":
		return m.netAct("edit:url"), true
	case "up", "k":
		m.net.scroll = max(0, m.net.scroll-1)
		return nil, true
	case "down", "j":
		m.net.scroll++
		return nil, true
	case "pgup":
		m.net.scroll = max(0, m.net.scroll-8)
		return nil, true
	case "pgdown":
		m.net.scroll += 8
		return nil, true
	}
	return nil, false
}

func (m *Model) netAct(arg string) tea.Cmd {
	kind, which, _ := strings.Cut(arg, ":")
	switch kind {
	case "ext":
		m.net.external = !m.net.external
	case "edit":
		in := &m.net.target
		switch which {
		case "lookup":
			in = &m.net.lookup
		case "url":
			in = &m.net.loadURL
		}
		m.net.editing, m.net.backup = in, in.val
		in.active = true
	case "run":
		return m.startNet()
	}
	return nil
}

func (m *Model) netInputDone(accepted bool) tea.Cmd {
	in := m.net.editing
	if in == nil {
		return nil
	}
	if !accepted {
		in.val = m.net.backup
		return nil
	}
	in.val = strings.TrimSpace(in.val)
	if strings.ContainsAny(in.val, " \t\n") {
		in.val = m.net.backup
		m.notify(false, "That value may not contain spaces.")
		return nil
	}
	if in == &m.net.loadURL && in.val != "" && !strings.HasPrefix(in.val, "http://") && !strings.HasPrefix(in.val, "https://") {
		in.val = m.net.backup
		m.notify(false, "The download URL must start with http:// or https://")
		return nil
	}
	switch in {
	case &m.net.target:
		m.app.Prefs.PingTarget = in.val
		m.savePrefs()
	case &m.net.lookup:
		m.app.Prefs.LookupName = in.val
		m.savePrefs()
	}
	return nil
}

func (m *Model) startNet() tea.Cmd {
	if m.busy != nil {
		m.notify(false, "Another operation is running; wait for it to finish.")
		return nil
	}
	if !m.haveRep {
		m.notify(false, "Still detecting your system; try again in a moment.")
		return nil
	}
	if !m.net.external && strings.TrimSpace(m.net.loadURL.val) != "" {
		m.notify(false, "A download URL contacts an outside server: press e to allow external tests first.")
		return nil
	}
	opt, rep, a := m.netOptions(), m.rep, m.app
	ctx, cancel := context.WithCancel(m.ctx)
	ch := make(chan tea.Msg, 16)
	cmd := m.startBusy("net", "Testing your network…", cancel)
	m.net.scroll = 0
	go func() {
		defer close(ch)
		defer cancel()
		res := diagnostics.CollectNetwork(ctx, rep, opt, netDeps(a.FS, a.Run), func(s string) { ch <- stageMsg(s) })
		ch <- netDoneMsg{res, ctx.Err() != nil}
	}()
	return tea.Batch(cmd, listen(ch))
}

func (m *Model) onNetDone(msg netDoneMsg) {
	m.busy = nil
	if msg.cancelled {
		m.notify(false, "Network test cancelled.")
		return
	}
	res := msg.res
	m.net.res = &res
	m.notify(true, "Network test finished.")
}

func (m *Model) field(label string, in *textInput, key, placeholder string) string {
	v := in.val
	if v == "" {
		v = m.th.Dim(placeholder)
	}
	if in.active {
		v = in.val + m.th.Accent("_")
	}
	return m.th.Dim(fit(label, 16)) + " " + v + "  " + m.hint(key, "edit")
}

func (m *Model) viewNetwork(w, h int) []string {
	var out []string
	out = append(out, " "+m.th.Bold("Network Doctor")+m.th.Dim("   finds which layer is the problem: Wi-Fi/link, router, DNS, internet or load"), "")
	if !m.haveRep {
		return append(out, " "+m.th.Dim("Still detecting your system…"))
	}

	opt := m.netOptions()
	dests := diagnostics.PlanDestinations(m.rep, opt)
	var c []string
	c = append(c, m.th.Bold("This run will contact exactly:"))
	for _, d := range dests {
		tag := m.th.OK("local   ")
		if d.External {
			tag = m.th.Warn("external")
		}
		c = append(c, "  "+tag+" "+fit(d.Host, 24)+" "+m.th.Dim(d.Purpose))
	}
	ext := m.gl.Off
	if m.net.external {
		ext = m.gl.On
	}
	by := bodyY + len(out) + 1 + len(c)
	c = append(c, "")
	c = append(c, ext+" Allow tests beyond my router (DNS timing + internet ping)  "+m.hint("e", "toggle"))
	m.addHit(0, by+1, w, 1, "net:ext")
	c = append(c, m.field("Ping target", &m.net.target, "t", "(default)"))
	c = append(c, m.field("DNS lookup name", &m.net.lookup, "l", "(default)"))
	if m.net.external {
		c = append(c, m.field("Download URL", &m.net.loadURL, "u", "(optional: measures lag under load)"))
	}
	if m.net.external {
		for _, l := range wrapText(diagnostics.ExternalNotice, w-6) {
			c = append(c, m.th.Dim(l))
		}
	}
	c = append(c, m.th.Dim("Nothing is sent until you press run. No data leaves your machine except these probes."))
	out = append(out, m.box("Consent & settings", c, w, 0, !m.net.external)...)

	runY := bodyY + len(out)
	btn := "[ Run network test (r) ]"
	switch {
	case m.busy != nil && m.busy.kind == "net":
		out = append(out, " "+m.th.Dim(btn+"  running… Ctrl+C cancels"))
	default:
		out = append(out, " "+m.th.Sel(btn))
		m.addHit(1, runY, vw(btn), 1, "net:run")
	}

	// Results (scrollable).
	room := h - len(out)
	body := m.netResultLines(w - 4)
	vis := max(1, room-2)
	m.net.scroll = clampI(m.net.scroll, 0, max(0, len(body)-vis))
	end := min(len(body), m.net.scroll+vis)
	title := "Result"
	if len(body) > vis {
		title = fmt.Sprintf("Result (↑↓ scroll %d/%d)", m.net.scroll+1, len(body)-vis+1)
	}
	return append(out, m.box(title, body[m.net.scroll:end], w, max(4, room), false)...)
}

func (m *Model) netResultLines(w int) []string {
	if m.busy != nil && m.busy.kind == "net" {
		sp := m.gl.Spin[m.frame%len(m.gl.Spin)]
		l := []string{m.th.Accent(sp) + " " + m.busy.label}
		for _, s := range m.busy.steps {
			l = append(l, m.th.Dim("  "+m.gl.Check+" "+s))
		}
		return l
	}
	r := m.net.res
	if r == nil {
		return []string{m.th.Dim("No test yet. Without external tests only your link and router are checked, which already tells Wi-Fi trouble from internet trouble.")}
	}
	var l []string
	k := r.Link
	switch k.Kind {
	case "wifi":
		p := []string{"Wi-Fi " + k.Iface}
		if k.SignalDBm != nil {
			p = append(p, fmt.Sprintf("%d dBm", *k.SignalDBm))
		}
		if k.FreqMHz != nil {
			p = append(p, fmt.Sprintf("%.1f GHz", *k.FreqMHz/1000))
		}
		if k.BitrateMbps != nil {
			p = append(p, fmt.Sprintf("%.0f Mbit/s", *k.BitrateMbps))
		}
		if k.PowerSave != "" {
			p = append(p, "power save "+k.PowerSave)
		}
		l = append(l, m.kv("Link", strings.Join(p, " · "), 9))
	case "ethernet":
		sp := "speed unknown"
		if k.SpeedMbps != nil {
			sp = fmt.Sprintf("%d Mbit/s", *k.SpeedMbps)
		}
		l = append(l, m.kv("Link", "Ethernet "+k.Iface+" · "+sp, 9))
	default:
		l = append(l, m.kv("Link", "no active network link", 9))
	}
	ping := func(name string, s *diagnostics.PingStats) {
		if s != nil {
			l = append(l, m.kv(name, fmt.Sprintf("%s  loss %.1f%% · p50 %.1f ms · p95 %.1f ms · jitter %.1f ms", s.Target, s.LossPct, s.P50Ms, s.P95Ms, s.JitterMs), 9))
		}
	}
	ping("Router", r.Gateway)
	for _, d := range r.DNS {
		l = append(l, m.kv("DNS", fmt.Sprintf("%s  first %.0f ms · median %.0f ms · failures %d", d.Server, d.FirstMs, d.MedianMs, d.Failures), 9))
	}
	ping("Internet", r.Internet)
	for _, ls := range []*diagnostics.LoadStats{r.Download, r.Upload} {
		if ls != nil && ls.Error == "" {
			l = append(l, m.kv(strings.ToUpper(ls.Direction[:1])+ls.Direction[1:], fmt.Sprintf("%s  %.0f Mbit/s · latency under load p50 %.1f ms / p95 %.1f ms", ls.Host, ls.Mbps, ls.Under.P50Ms, ls.Under.P95Ms), 9))
		}
	}
	l = append(l, "", m.th.Bold("Diagnosis"))
	for _, d := range r.Diagnosis {
		l = append(l, m.sevTag(d.Sev)+" "+m.th.Bold(fit(d.Layer, 8))+" "+d.Title+m.th.Dim(" ("+d.Certainty+" certainty)"))
		if d.Sev >= diagnostics.SevNotice && d.Detail != "" {
			for _, x := range wrapText(d.Detail, w-9) {
				l = append(l, "         "+m.th.Dim(x))
			}
		}
	}
	for _, s := range r.Skipped {
		l = append(l, m.th.Dim("skipped: "+s))
	}
	l = append(l, "", m.th.Dim("ICMP can be rate-limited by some routers: treat a single short run as indicative, not proof."))
	return l
}

// netDeps builds the network probes; tests replace it so they never touch the network.
var netDeps = diagnostics.RealNetDeps
