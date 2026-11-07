package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/mttcn83/turboshift/internal/integrations"
)

type gameState struct {
	gamemode, mangohud bool
	logSecs            int // 0 = no MangoHud log
	gpu                int // 0 auto, 1 nvidia, 2 mesa
	cursor             int
}

var gpuChoices = []integrations.GPU{integrations.GPUAuto, integrations.GPUNvidia, integrations.GPUMesa}

const gameRows = 4

func (m *Model) gameSpec() integrations.LaunchSpec {
	s := integrations.LaunchSpec{GameMode: m.gm.gamemode, MangoHud: m.gm.mangohud || m.gm.logSecs > 0, GPU: gpuChoices[m.gm.gpu]}
	if m.gm.logSecs > 0 {
		s.LogSeconds, s.LogDir = m.gm.logSecs, m.app.MangoLogDir()
	}
	return s
}

func (m *Model) keyGaming(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	k := msg.String()
	if c, ok := moveKey(k, m.gm.cursor, gameRows, 2); ok {
		m.gm.cursor = c
		return nil, true
	}
	switch k {
	case " ", "space", "enter", "right":
		return m.gameAct(fmt.Sprintf("tog:%d", m.gm.cursor)), true
	case "left":
		return m.gameAct(fmt.Sprintf("tog:%d", m.gm.cursor)), true
	case "c":
		return m.gameAct("copy"), true
	}
	return nil, false
}

func (m *Model) gameAct(arg string) tea.Cmd {
	parts := strings.Split(arg, ":")
	switch parts[0] {
	case "tog":
		var i int
		if len(parts) > 1 {
			fmt.Sscanf(parts[1], "%d", &i)
		}
		m.gm.cursor = clampI(i, 0, gameRows-1)
		switch m.gm.cursor {
		case 0:
			m.gm.gamemode = !m.gm.gamemode
		case 1:
			m.gm.mangohud = !m.gm.mangohud
			if !m.gm.mangohud {
				m.gm.logSecs = 0
			}
		case 2:
			if m.gm.logSecs == 0 {
				m.gm.logSecs, m.gm.mangohud = 60, true
			} else {
				m.gm.logSecs = 0
			}
		case 3:
			m.gm.gpu = (m.gm.gpu + 1) % len(gpuChoices)
		}
	case "copy":
		spec := m.gameSpec()
		if err := spec.Validate(); err != nil {
			m.notify(false, err.Error())
			return nil
		}
		m.notify(true, "Launch options copied to your clipboard (if your terminal allows OSC 52); also shown below.")
		return tea.SetClipboard(spec.Steam())
	}
	return nil
}

func (m *Model) viewGaming(w, h int) []string {
	var out []string
	out = append(out, " "+m.th.Bold("Gaming")+m.th.Dim("   launch options for your games; nothing here changes system settings"), "")

	// Readiness: only facts we can observe.
	var ready []string
	tick := func(ok bool, yes, no string) string {
		if ok {
			return m.th.OK(m.gl.Check) + " " + yes
		}
		return m.th.Warn(m.gl.Warn) + " " + m.th.Dim(no)
	}
	ready = append(ready, tick(m.app.Run.Has("gamemoderun"), "GameMode installed", "GameMode not installed (optional; package \"gamemode\" on most distributions)"))
	ready = append(ready, tick(m.app.Run.Has("mangohud"), "MangoHud installed", "MangoHud not installed (needed for frame-time logs and the Benchmark Lab)"))
	if m.haveRep {
		if p := m.rep.Power.PPDProfile; p != "" {
			ready = append(ready, tick(p == "performance" || p == "balanced", "Power profile: "+p, "Power profile is "+p+": expect lower performance (see the Optimizer)"))
		}
		hybrid := len(m.rep.GPUs) > 1
		if hybrid {
			ready = append(ready, m.th.OK(m.gl.Info)+" Hybrid graphics: pick the GPU below if a game runs on the wrong one")
		}
		if m.rep.System.SessionType != "" {
			ready = append(ready, m.th.OK(m.gl.Info)+" "+m.rep.System.SessionType+" session")
		}
	}
	out = append(out, m.box("Readiness", ready, w, 0, false)...)

	// Builder.
	spec := m.gameSpec()
	gpuName := map[integrations.GPU]string{integrations.GPUAuto: "automatic (system default)", integrations.GPUNvidia: "NVIDIA (PRIME render offload)", integrations.GPUMesa: "AMD/Intel via Mesa (DRI_PRIME)"}[spec.GPU]
	opts := []struct{ label, val string }{
		{"Run through GameMode", onOff(m.gm.gamemode)},
		{"Show MangoHud overlay", onOff(m.gm.mangohud || m.gm.logSecs > 0)},
		{"Record a MangoHud log for the Benchmark Lab", logText(m.gm.logSecs)},
		{"Render on", gpuName},
	}
	var b []string
	for i, o := range opts {
		line := fmt.Sprintf("%s %-46s %s", m.gl.Off, o.label, o.val)
		if o.val == "on" || (i == 2 && m.gm.logSecs > 0) {
			line = strings.Replace(line, m.gl.Off, m.gl.On, 1)
		}
		if i == 3 {
			line = "    " + fmt.Sprintf("%-46s %s", o.label, o.val)
		}
		if i == m.gm.cursor {
			line = m.th.Sel(fit(line, w-4))
		}
		m.addHit(0, bodyY+len(out)+1+len(b), w, 1, fmt.Sprintf("game:tog:%d", i))
		b = append(b, line)
	}
	out = append(out, m.box("Launch options builder", b, w, 0, true)...)

	cmd := spec.Steam()
	if err := spec.Validate(); err != nil {
		cmd = m.th.Err(err.Error())
	}
	res := []string{m.th.Dim("Steam: paste into a game's Properties " + m.gl.Arrow + " Launch Options")}
	res = append(res, wrapText(cmd, w-6)...)
	res = append(res, "", m.th.Dim("Outside Steam: ")+"turbo game run"+flagString(spec)+" -- <your command>")
	if spec.GameMode || spec.MangoHud {
		res = append(res, "")
		for _, l := range wrapText(integrations.OverlayNotice, w-6) {
			res = append(res, m.th.Dim(l))
		}
	}
	out = append(out, m.box("Result", res, w, 0, false)...)
	by := bodyY + len(out)
	btn := "[ Copy launch options (c) ]"
	out = append(out, " "+m.th.Sel(btn)+m.th.Dim("   per-game profiles are not implemented yet"))
	m.addHit(1, by, vw(btn), 1, "game:copy")
	return out
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

func logText(s int) string {
	if s == 0 {
		return "off"
	}
	return fmt.Sprintf("on (%d s, starts 10 s after launch)", s)
}

func flagString(s integrations.LaunchSpec) string {
	var f []string
	if s.GameMode {
		f = append(f, "--gamemode")
	}
	if s.MangoHud && s.LogSeconds == 0 {
		f = append(f, "--mangohud")
	}
	if s.LogSeconds > 0 {
		f = append(f, fmt.Sprintf("--log %d", s.LogSeconds))
	}
	if s.GPU != integrations.GPUAuto {
		f = append(f, "--gpu "+string(s.GPU))
	}
	if len(f) == 0 {
		return ""
	}
	return " " + strings.Join(f, " ")
}
