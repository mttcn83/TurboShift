package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/mttcn83/turboshift/internal/snapshot"
)

type vaultState struct {
	snaps   []*snapshot.Snapshot
	corrupt []string
	cursor  int
	top     int
}

var statusText = map[snapshot.Status]string{
	snapshot.StatusCommitted:      "applied: can be undone",
	snapshot.StatusRolledBack:     "failed, everything reverted automatically",
	snapshot.StatusRollbackFailed: "failed and NOT fully reverted: see history below",
	snapshot.StatusRestored:       "already undone",
	snapshot.StatusRestorePartial: "partly undone (some items left alone)",
	snapshot.StatusPending:        "interrupted before anything changed",
	snapshot.StatusApplying:       "interrupted: use Recover (R)",
}

func (m *Model) currentSnap() *snapshot.Snapshot {
	if len(m.vt.snaps) == 0 {
		return nil
	}
	return m.vt.snaps[clampI(m.vt.cursor, 0, len(m.vt.snaps)-1)]
}

func (m *Model) keyVault(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	k := msg.String()
	if c, ok := moveKey(k, m.vt.cursor, len(m.vt.snaps), 6); ok {
		m.vt.cursor = c
		return nil, true
	}
	switch k {
	case "enter":
		return m.vaultAct("restore"), true
	case "f":
		return m.vaultAct("force"), true
	case "R":
		return m.vaultAct("recover"), true
	case "r":
		return m.loadVault(), true
	}
	return nil, false
}

func (m *Model) vaultAct(arg string) tea.Cmd {
	kind, rest, _ := strings.Cut(arg, ":")
	switch kind {
	case "row":
		var i int
		fmt.Sscanf(rest, "%d", &i)
		m.vt.cursor = clampI(i, 0, max(0, len(m.vt.snaps)-1))
	case "restore", "force":
		s := m.currentSnap()
		if s == nil {
			m.notify(false, "There are no restore points yet. One is created before every change.")
			return nil
		}
		if s.Status != snapshot.StatusCommitted && s.Status != snapshot.StatusRestorePartial {
			m.notify(false, "This restore point is "+statusText[s.Status]+": there is nothing to undo.")
			return nil
		}
		force := kind == "force"
		title := "Undo these changes?"
		if force {
			title = "Force restore?"
		}
		md := &modal{kind: modalConfirm, title: title, snapID: s.ID, yes: "restore"}
		if force {
			md.yes = "force"
		}
		for _, it := range s.Items {
			md.add("bold", it.Name)
			for k, v := range it.After {
				md.add("", k+":  "+v+" "+m.gl.Arrow+" "+it.Before[k], 2)
			}
		}
		md.gap()
		if force {
			md.add("warn", m.gl.Warn+" Force also overwrites settings another program changed after TurboShift did.")
		} else {
			md.add("dim", "Settings that something else changed since are left alone and reported, never overwritten.")
		}
		md.buttons = []mbtn{{"Restore (Enter)", "yes", "y"}, {"Cancel (Esc)", "close", "n"}}
		m.modal = md
	case "recover":
		if len(m.interrupted) == 0 {
			m.notify(true, "Nothing to recover: no interrupted changes.")
			return nil
		}
		m.openRecoverModal()
	}
	return nil
}

func (m *Model) viewVault(w, h int) []string {
	var out []string
	out = append(out, " "+m.th.Bold("Snapshot Vault")+m.th.Dim("   a restore point is saved before every change; it covers what TurboShift itself changed, not your files"), "")
	if len(m.interrupted) > 0 {
		out = append(out, " "+m.th.Err(m.gl.Warn+" "+fmt.Sprintf("%d interrupted change(s) found.", len(m.interrupted)))+" Press "+m.key("R")+" to undo what they changed.", "")
	}
	for _, c := range m.vt.corrupt {
		out = append(out, " "+m.th.Err("CORRUPT")+" "+c+m.th.Dim(" failed its integrity check and was ignored"))
	}
	avail := h - len(out)
	side := w >= 100
	listW, detW := w, w
	listH := max(5, avail/2)
	if side {
		listW = w * 45 / 100
		detW = w - listW - 1
		listH = avail
	}
	listH = min(listH, avail)
	vis := max(1, listH-2)
	m.vt.cursor = clampI(m.vt.cursor, 0, max(0, len(m.vt.snaps)-1))
	m.vt.top = scrollWindow(m.vt.cursor, m.vt.top, vis, len(m.vt.snaps))
	var lb []string
	if len(m.vt.snaps) == 0 {
		lb = append(lb, m.th.Dim("No restore points yet."), m.th.Dim("One is created automatically before every change."))
	}
	for i := m.vt.top; i < min(len(m.vt.snaps), m.vt.top+vis); i++ {
		s := m.vt.snaps[i]
		line := fmt.Sprintf("%s  %-15s %s", s.CreatedAt.Local().Format("01-02 15:04"), s.Status, itemNames(s))
		if i == m.vt.cursor {
			line = m.th.Sel(fit(" "+line, listW-4))
		} else {
			line = " " + line
		}
		m.addHit(0, bodyY+len(out)+1+len(lb), listW, 1, fmt.Sprintf("vault:row:%d", i))
		lb = append(lb, line)
	}
	listBox := m.box(fmt.Sprintf("Restore points (%d)", len(m.vt.snaps)), lb, listW, listH, true)

	var d []string
	if s := m.currentSnap(); s != nil {
		d = append(d, m.th.Bold(s.ID), m.th.Dim(s.Label+" · kernel "+s.Kernel), "", statusColor(m, s.Status)+" "+statusText[s.Status], "")
		for _, it := range s.Items {
			d = append(d, m.th.Bold(it.Name)+m.th.Dim("  ["+string(it.Status)+"]"))
			for k, v := range it.Before {
				d = append(d, "  "+k+":  "+m.th.Dim(v)+" "+m.gl.Arrow+" "+it.After[k])
			}
			if it.Error != "" {
				d = append(d, "  "+m.th.Err(it.Error))
			}
		}
		if len(s.Events) > 0 {
			d = append(d, "", m.th.Dim("History"))
			for _, e := range s.Events {
				d = append(d, m.th.Dim("  "+e.Time.Local().Format("15:04:05")+"  "+e.Text))
			}
		}
	}
	detH := avail
	if !side {
		detH = avail - listH
	}
	if side {
		out = append(out, joinCols(1, listBox, m.box("Details", d, detW, max(5, detH), false))...)
	} else {
		out = append(out, listBox...)
		if detH >= 5 {
			out = append(out, m.box("Details", d, detW, detH, false)...)
		}
	}
	return out
}

func statusColor(m *Model, s snapshot.Status) string {
	switch s {
	case snapshot.StatusCommitted, snapshot.StatusRestored:
		return m.th.OK(strings.ToUpper(string(s)))
	case snapshot.StatusRollbackFailed, snapshot.StatusApplying:
		return m.th.Err(strings.ToUpper(string(s)))
	}
	return m.th.Warn(strings.ToUpper(string(s)))
}

func itemNames(s *snapshot.Snapshot) string {
	var n []string
	for _, it := range s.Items {
		n = append(n, it.TweakID)
	}
	return strings.Join(n, ", ")
}
