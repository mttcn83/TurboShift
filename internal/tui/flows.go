package tui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/mttcn83/turboshift/internal/benchmark"
	"github.com/mttcn83/turboshift/internal/detect"
	"github.com/mttcn83/turboshift/internal/diagnostics"
	"github.com/mttcn83/turboshift/internal/engine"
	"github.com/mttcn83/turboshift/internal/snapshot"
	"github.com/mttcn83/turboshift/internal/tweaks"
)

// Messages produced by background work.
type (
	stageMsg    string
	scanDoneMsg struct {
		rep         detect.Report
		findings    []diagnostics.Finding
		assess      map[string]tweaks.Assessment
		top         []diagnostics.Proc
		interrupted []*snapshot.Snapshot
		optimize    bool
		cancelled   bool
	}
	previewMsg struct {
		reqs []engine.Request
		pv   engine.Preview
	}
	applyEventMsg struct{ ev engine.Event }
	applyDoneMsg  struct {
		snap *snapshot.Snapshot
		err  error
	}
	restoreDoneMsg struct {
		kind string // restore | recover
		id   string
		res  []engine.ItemResult
		err  error
		done []string // recover: IDs handled
	}
	netDoneMsg struct {
		res       diagnostics.NetReport
		cancelled bool
	}
	vaultLoadedMsg struct {
		snaps   []*snapshot.Snapshot
		corrupt []string
		pend    []*snapshot.Snapshot
	}
	benchLoadedMsg struct {
		runs []benchmark.Run
		logs []string
		err  error
	}
	importDoneMsg struct {
		run  benchmark.Run
		src  string
		err  error
		last bool
	}
)

// noGraphicalAuth reports whether a privilege prompt could not be shown
// without corrupting the terminal UI (no graphical session for polkit's agent).
func (m *Model) noGraphicalAuth() bool {
	if os.Geteuid() == 0 {
		return false
	}
	g := m.app.Getenv
	if g == nil {
		g = os.Getenv
	}
	return g("DISPLAY") == "" && g("WAYLAND_DISPLAY") == ""
}

// ---- scan ----

func (m *Model) startScan(thenOptimize bool) tea.Cmd {
	if m.busy != nil {
		m.notify(false, "Another operation is running; wait for it to finish.")
		return nil
	}
	a, window := m.app, m.topWindow
	ctx, cancel := context.WithCancel(m.ctx)
	ch := make(chan tea.Msg, 16)
	cmd := m.startBusy("scan", "Scanning your PC…", cancel)
	go func() {
		defer close(ch)
		defer cancel()
		ch <- stageMsg("Reading hardware and system settings…")
		rep := a.Detect(ctx)
		ch <- stageMsg("Looking for problems…")
		findings := diagnostics.Diagnose(rep)
		ch <- stageMsg("Checking which optimizations fit this machine…")
		env := a.Env(rep)
		assess := map[string]tweaks.Assessment{}
		for _, t := range a.Reg.All() {
			if ctx.Err() != nil {
				break
			}
			assess[t.Meta().ID] = t.Assess(ctx, env)
		}
		ch <- stageMsg("Sampling the busiest processes (2 s)…")
		top := diagnostics.TopProcesses(ctx, a.FS, 5, window)
		pend, _ := a.Store.Interrupted()
		ch <- scanDoneMsg{rep, findings, assess, top, pend, thenOptimize, ctx.Err() != nil}
	}()
	return tea.Batch(cmd, listen(ch))
}

func (m *Model) onScanDone(msg scanDoneMsg) tea.Cmd {
	m.busy = nil
	if msg.cancelled {
		m.notify(false, "Scan cancelled.")
		return nil
	}
	m.rep, m.haveRep = msg.rep, true
	m.findings, m.assess, m.top = msg.findings, msg.assess, msg.top
	m.interrupted = msg.interrupted
	m.scanned, m.scanAt = true, time.Now()
	// Pre-select only what is both Recommended-tier and recommended by live
	// evidence; anything else stays opt-in.
	for _, t := range m.app.Reg.All() {
		mt := t.Meta()
		as, ok := m.assess[mt.ID]
		if !ok || !as.Available {
			delete(m.opt.checked, mt.ID)
			continue
		}
		if as.Recommend != nil && mt.Tier == tweaks.TierRecommended {
			m.opt.checked[mt.ID] = true
		}
		if as.Recommend != nil {
			m.opt.choice[mt.ID] = optionIndex(optionsOf(mt, as), as.Recommend.Params[mt.ParamName])
		}
	}
	warn, note := diagnostics.Counts(m.findings)
	rec := len(m.recommended())
	if msg.optimize {
		return m.optimizeRecommended()
	}
	m.notify(true, fmt.Sprintf("Scan complete: %d warning(s), %d notice(s), %d recommended change(s).", warn, note, rec))
	if len(m.interrupted) > 0 && m.modal == nil {
		m.openRecoverModal()
	}
	return nil
}

// ---- optimizer plumbing ----

func optionsOf(mt tweaks.Meta, as tweaks.Assessment) []tweaks.Option {
	if len(as.Options) > 0 {
		return as.Options
	}
	return mt.Options
}

func optionIndex(opts []tweaks.Option, v string) int {
	for i, o := range opts {
		if o.Value == v {
			return i
		}
	}
	return 0
}

// requestFor builds the engine request for a tweak using the user's choice,
// falling back to the live recommendation.
func (m *Model) requestFor(t tweaks.Tweak) (engine.Request, bool) {
	mt := t.Meta()
	as, ok := m.assess[mt.ID]
	if !ok || !as.Available {
		return engine.Request{}, false
	}
	opts := optionsOf(mt, as)
	if len(opts) == 0 {
		return engine.Request{}, false
	}
	idx := clampI(m.opt.choice[mt.ID], 0, len(opts)-1)
	return engine.Request{TweakID: mt.ID, Params: map[string]string{mt.ParamName: opts[idx].Value}}, true
}

// recommended lists requests for everything both Recommended-tier and
// currently recommended by evidence.
func (m *Model) recommended() []engine.Request {
	var reqs []engine.Request
	for _, t := range m.app.Reg.All() {
		mt := t.Meta()
		as := m.assess[mt.ID]
		if !as.Available || as.Recommend == nil || mt.Tier != tweaks.TierRecommended {
			continue
		}
		reqs = append(reqs, engine.Request{TweakID: mt.ID, Params: as.Recommend.Params})
	}
	return reqs
}

func (m *Model) selected() []engine.Request {
	var reqs []engine.Request
	for _, t := range m.app.Reg.All() {
		if m.opt.checked[t.Meta().ID] {
			if r, ok := m.requestFor(t); ok {
				reqs = append(reqs, r)
			}
		}
	}
	return reqs
}

// optimizeRecommended is the "Optimize My PC" button: scan if needed, then
// preview what the evidence supports. Doing nothing is a valid answer.
func (m *Model) optimizeRecommended() tea.Cmd {
	if !m.scanned {
		return m.startScan(true)
	}
	reqs := m.recommended()
	if len(reqs) == 0 {
		md := &modal{kind: modalResult, title: "Nothing needs changing"}
		md.add("ok", m.gl.Check+" Your PC is already well configured for everything TurboShift can safely change.")
		md.gap()
		md.add("", "No setting was changed and no restore point was created. TurboShift does not invent tweaks to look busy.")
		optional := 0
		for _, as := range m.assess {
			if as.Available {
				optional++
			}
		}
		if optional > 0 {
			md.gap()
			md.add("dim", fmt.Sprintf("%d optional tweak(s) are available in the Optimizer (for specific situations such as gaming or battery life). They are never applied automatically.", optional))
		}
		md.buttons = []mbtn{{"Browse Optimizer", "optimizer", "o"}, {"Close (Esc)", "close", ""}}
		m.modal = md
		return nil
	}
	return m.previewCmd(reqs)
}

func (m *Model) previewCmd(reqs []engine.Request) tea.Cmd {
	eng, ctx := m.app.Engine(m.rep), m.ctx
	m.notify(true, "Preparing preview…")
	return func() tea.Msg { return previewMsg{reqs, eng.Preview(ctx, reqs)} }
}

func (m *Model) onPreview(msg previewMsg) {
	md := &modal{kind: modalPreview, title: "Review before applying", reqs: msg.reqs}
	needAuth, blocked := false, false
	for _, p := range msg.pv.Plans {
		t, _ := m.app.Reg.Get(p.TweakID)
		mt := t.Meta()
		md.add("head", mt.Name+"  ("+string(mt.Tier)+", risk "+mt.Risk.String()+")")
		for _, c := range p.Changes {
			md.add("", c.What+":  "+c.From+" "+m.gl.Arrow+" "+c.To, 2)
		}
		for _, w := range p.Warnings {
			md.add("warn", m.gl.Warn+" "+w, 2)
		}
		if p.Disruptive && !mentions(p.Warnings, "interrupt", "hiccup") {
			md.add("warn", m.gl.Warn+" May briefly interrupt something while it applies.", 2)
		}
		if p.NeedsAuth {
			needAuth = true
			md.add("dim", "Needs administrator approval for this step only.", 2)
		}
		md.add("dim", "Lasts: "+string(mt.Scope)+".  Measure it: "+mt.Evidence, 2)
		md.gap()
	}
	hard := false
	for _, pr := range msg.pv.Problems {
		if errors.Is(pr.Err, tweaks.ErrNoChange) {
			md.add("ok", m.gl.Check+" "+pr.TweakID+": already set, nothing to change.")
			continue
		}
		hard = true
		md.add("err", m.gl.Cross+" "+pr.TweakID+": "+pr.Err.Error())
	}
	if needAuth && m.noGraphicalAuth() {
		blocked = true
		md.add("warn", m.gl.Warn+" This needs an administrator prompt, which cannot appear inside a terminal-only session. Run the same change from a normal shell:")
		for _, p := range msg.pv.Plans {
			if p.NeedsAuth {
				md.add("bold", "turbo apply "+p.TweakID+"="+firstParam(p.Params), 2)
			}
		}
	}
	switch {
	case len(msg.pv.Plans) == 0:
		if !hard {
			md.add("ok", "Nothing to do.")
		}
		md.buttons = []mbtn{{"Close (Enter)", "close", ""}}
	case hard:
		md.add("warn", "Not applying: fix or unselect the items marked "+m.gl.Cross+" first.")
		md.buttons = []mbtn{{"Close (Enter)", "close", ""}}
	case blocked:
		md.buttons = []mbtn{{"Close (Enter)", "close", ""}}
	default:
		md.add("dim", "A restore point is saved first. If any step fails, everything already changed is reverted automatically.")
		md.buttons = []mbtn{{"Apply (Enter)", "apply", ""}, {"Cancel (Esc)", "close", ""}}
	}
	m.modal = md
}

func firstParam(p map[string]string) string {
	keys := make([]string, 0, len(p))
	for k := range p {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return ""
	}
	return p[keys[0]]
}

// ---- apply ----

func (m *Model) runApply(reqs []engine.Request) tea.Cmd {
	if m.busy != nil || len(reqs) == 0 {
		return nil
	}
	ctx, cancel := context.WithCancel(m.ctx)
	ch := make(chan tea.Msg, 64)
	eng := m.app.Engine(m.rep)
	m.modal = &modal{kind: modalProgress, title: "Applying changes"}
	cmd := m.startBusy("apply", "Applying changes…", cancel)
	go func() {
		defer close(ch)
		defer cancel()
		snap, err := eng.Apply(ctx, reqs, "tui", func(e engine.Event) { ch <- applyEventMsg{e} })
		ch <- applyDoneMsg{snap, err}
	}()
	return tea.Batch(cmd, listen(ch))
}

func (m *Model) progressLine(e engine.Event) {
	if m.busy == nil {
		return
	}
	m.busy.label = e.Message
	m.busy.steps = append(m.busy.steps, e.Message)
	if m.modal != nil && m.modal.kind == modalProgress {
		m.modal.add("", m.gl.Dot+" "+e.Message)
	}
}

func (m *Model) onApplyEvent(msg applyEventMsg) { m.progressLine(msg.ev) }

func (m *Model) onApplyDone(msg applyDoneMsg) tea.Cmd {
	m.busy = nil
	md := &modal{kind: modalResult}
	if msg.err == nil {
		md.title = "Done"
		md.snapID = msg.snap.ID
		md.add("ok", m.gl.Check+" Applied and verified.")
		md.gap()
		for _, it := range msg.snap.Items {
			md.add("bold", it.Name)
			for k, v := range it.Before {
				md.add("", k+":  "+v+" "+m.gl.Arrow+" "+it.After[k], 2)
			}
		}
		md.gap()
		md.add("dim", "Restore point "+msg.snap.ID+". Undo from here, or any time from the Vault.")
		md.buttons = []mbtn{{"Close (Enter)", "close", ""}, {"Undo these changes (u)", "undo", "u"}}
	} else {
		var te *engine.TransactionError
		md.title = "Nothing was changed"
		switch {
		case errors.As(msg.err, &te) && te.RolledBack:
			md.add("warn", m.gl.Warn+" The change failed, so everything was reverted. Your system is as it was before.")
			md.gap()
			md.add("", "Why: "+te.Cause.Error())
		case errors.As(msg.err, &te):
			md.title = "Rollback incomplete"
			md.add("err", m.gl.Cross+" The change failed and TurboShift could not revert everything.")
			md.gap()
			md.add("", "Why: "+te.Cause.Error())
			if te.RollbackErr != nil {
				md.add("err", "Not reverted: "+te.RollbackErr.Error())
			}
			md.gap()
			md.add("", "Open the Vault (screen 7) and use Recover, or run: turbo snapshots recover")
		default:
			md.add("err", m.gl.Cross+" "+msg.err.Error())
		}
		md.buttons = []mbtn{{"Close (Enter)", "close", ""}}
	}
	m.modal = md
	m.opt.checked = map[string]bool{}
	return m.startScan(false)
}

// ---- restore / recover ----

// needsAuthFor reports whether restoring the snapshot would need administrator approval.
func (m *Model) needsAuthFor(s *snapshot.Snapshot) bool {
	for _, it := range s.Items {
		if t, ok := m.app.Reg.Get(it.TweakID); ok && t.Meta().NeedsAuth {
			return true
		}
	}
	return false
}

func (m *Model) blockedModal(title, cli string) {
	md := &modal{kind: modalResult, title: title}
	md.add("warn", m.gl.Warn+" This step needs an administrator prompt, which cannot appear inside a terminal-only session.")
	md.gap()
	md.add("", "Run it from a normal shell instead:")
	md.add("bold", cli, 2)
	md.buttons = []mbtn{{"Close (Enter)", "close", ""}}
	m.modal = md
}

func (m *Model) runRestore(id string, force bool) tea.Cmd {
	if m.busy != nil {
		return nil
	}
	snap, err := m.app.Store.Load(id)
	if err != nil {
		m.modal = nil
		m.notify(false, "Cannot load that restore point: "+err.Error())
		return nil
	}
	if m.needsAuthFor(snap) && m.noGraphicalAuth() {
		cli := "turbo snapshots restore " + id
		if force {
			cli += " --force"
		}
		m.blockedModal("Run this from a normal shell", cli)
		return nil
	}
	ctx, cancel := context.WithCancel(m.ctx)
	ch := make(chan tea.Msg, 64)
	eng := m.app.Engine(m.rep)
	m.modal = &modal{kind: modalProgress, title: "Undoing changes"}
	cmd := m.startBusy("restore", "Restoring "+id+"…", cancel)
	go func() {
		defer close(ch)
		defer cancel()
		res, err := eng.Restore(ctx, id, engine.RestoreOptions{Force: force}, func(e engine.Event) { ch <- applyEventMsg{e} })
		ch <- restoreDoneMsg{kind: "restore", id: id, res: res, err: err}
	}()
	return tea.Batch(cmd, listen(ch))
}

func (m *Model) runRecover() tea.Cmd {
	if m.busy != nil {
		return nil
	}
	pend := m.interrupted
	for _, s := range pend {
		if m.needsAuthFor(s) && m.noGraphicalAuth() {
			m.blockedModal("Run this from a normal shell", "turbo snapshots recover")
			return nil
		}
	}
	ctx, cancel := context.WithCancel(m.ctx)
	ch := make(chan tea.Msg, 64)
	eng := m.app.Engine(m.rep)
	m.modal = &modal{kind: modalProgress, title: "Recovering"}
	cmd := m.startBusy("recover", "Recovering unfinished changes…", cancel)
	go func() {
		defer close(ch)
		defer cancel()
		var handled []string
		var errs []error
		for _, s := range pend {
			if err := eng.Recover(ctx, s.ID, func(e engine.Event) { ch <- applyEventMsg{e} }); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", s.ID, err))
				continue
			}
			handled = append(handled, s.ID)
		}
		ch <- restoreDoneMsg{kind: "recover", err: errors.Join(errs...), done: handled}
	}()
	return tea.Batch(cmd, listen(ch))
}

func (m *Model) onRestoreDone(msg restoreDoneMsg) tea.Cmd {
	m.busy = nil
	md := &modal{kind: modalResult, buttons: []mbtn{{"Close (Enter)", "close", ""}}}
	drift := false
	switch {
	case msg.kind == "recover" && msg.err == nil:
		md.title = "Recovered"
		md.add("ok", fmt.Sprintf("%s %d unfinished change set(s) were undone.", m.gl.Check, len(msg.done)))
	case msg.kind == "recover":
		md.title = "Recovery incomplete"
		md.add("err", m.gl.Cross+" "+msg.err.Error())
		md.gap()
		md.add("", "Nothing was force-overwritten. See `turbo snapshots show <id>` for the journal.")
	case msg.err != nil:
		md.title = "Could not restore"
		md.add("err", m.gl.Cross+" "+msg.err.Error())
	default:
		md.title = "Restore finished"
		for _, r := range msg.res {
			switch r.Status {
			case snapshot.ItemDrifted:
				drift = true
				md.add("warn", m.gl.Warn+" "+r.Name+": left alone. "+r.Note)
			case snapshot.ItemFailed:
				md.add("err", m.gl.Cross+" "+r.Name+": "+r.Note)
			default:
				md.add("ok", m.gl.Check+" "+r.Name+": "+r.Note)
			}
		}
		if drift {
			md.gap()
			md.add("dim", "Another program changed these settings after TurboShift did, so they were not overwritten. Force restore sets them back anyway.")
			md.snapID = msg.id
			md.buttons = []mbtn{{"Close (Enter)", "close", ""}, {"Force restore (f)", "force", "f"}}
		}
	}
	m.modal = md
	return tea.Batch(m.startScan(false), m.loadVault())
}

// ---- vault / benchmark loading ----

func (m *Model) loadVault() tea.Cmd {
	st := m.app.Store
	return func() tea.Msg {
		snaps, corrupt, _ := st.List()
		sort.Slice(snaps, func(i, j int) bool { return snaps[i].CreatedAt.After(snaps[j].CreatedAt) })
		pend, _ := st.Interrupted()
		return vaultLoadedMsg{snaps, corrupt, pend}
	}
}

func (m *Model) loadBench() tea.Cmd {
	a := m.app
	return func() tea.Msg {
		runs, err := a.Bench.List()
		return benchLoadedMsg{runs, benchmark.FindLogs(a.FS, a.LogDirs()), err}
	}
}

func (m *Model) deleteRun(id string) tea.Cmd {
	a := m.app
	return func() tea.Msg {
		if err := a.Bench.Delete(id); err != nil {
			return benchLoadedMsg{err: err}
		}
		runs, err := a.Bench.List()
		return benchLoadedMsg{runs, benchmark.FindLogs(a.FS, a.LogDirs()), err}
	}
}

// joinNonEmpty is a tiny helper for status text.
func joinNonEmpty(sep string, parts ...string) string {
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, sep)
}

func mentions(lines []string, words ...string) bool {
	for _, l := range lines {
		for _, w := range words {
			if strings.Contains(strings.ToLower(l), w) {
				return true
			}
		}
	}
	return false
}
