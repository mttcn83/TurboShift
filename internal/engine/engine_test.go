package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mttcn83/turboshift/internal/detect"
	"github.com/mttcn83/turboshift/internal/privilege"
	"github.com/mttcn83/turboshift/internal/simtest"
	"github.com/mttcn83/turboshift/internal/snapshot"
	"github.com/mttcn83/turboshift/internal/tweaks"
)

func init() { tweaks.PollInterval = time.Millisecond }

type rig struct {
	m    *simtest.Machine
	priv *privilege.Fake
	eng  *Engine
}

func newRig(t *testing.T, mut ...func(*simtest.Machine)) *rig {
	t.Helper()
	m := simtest.New()
	for _, f := range mut {
		f(m)
	}
	return rigFor(t, m, tweaks.Default())
}

func rigFor(t *testing.T, m *simtest.Machine, reg *tweaks.Registry) *rig {
	t.Helper()
	priv := &privilege.Fake{FS: m.FS}
	rep := detect.Probe(context.Background(), detect.Env{FS: m.FS, Run: m, GOARCH: "amd64", Getenv: func(string) string { return "" }})
	return &rig{m: m, priv: priv, eng: &Engine{
		Reg:   reg,
		Store: &snapshot.Store{Dir: filepath.Join(t.TempDir(), "snaps")},
		Env:   tweaks.Env{FS: m.FS, Run: m, Priv: priv, Report: rep},
	}}
}

var ctx = context.Background()

func ppd(p string) Request { return Request{"power.profile", map[string]string{"profile": p}} }
func wifi(p string) Request {
	return Request{"net.wifi-powersave", map[string]string{"powersave": p}}
}

func TestApplyHappyPathVerifiesAndJournals(t *testing.T) {
	r := newRig(t)
	snap, err := r.eng.Apply(ctx, []Request{ppd("performance")}, "gaming", nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.m.PPDProfile != "performance" {
		t.Fatalf("system not changed: %s", r.m.PPDProfile)
	}
	if snap.Status != snapshot.StatusCommitted || snap.Items[0].Status != snapshot.ItemVerified {
		t.Errorf("snapshot: %+v", snap)
	}
	if snap.Items[0].Before["profile"] != "balanced" || snap.Items[0].After["profile"] != "performance" {
		t.Errorf("before/after not captured: %+v", snap.Items[0])
	}
	loaded, err := r.eng.Store.Load(snap.ID)
	if err != nil || loaded.Status != snapshot.StatusCommitted {
		t.Errorf("persisted snapshot wrong: %v %+v", err, loaded)
	}
}

func TestPreviewDoesNotTouchTheSystem(t *testing.T) {
	r := newRig(t)
	pv := r.eng.Preview(ctx, []Request{ppd("performance"), wifi("off")})
	if len(pv.Plans) != 2 || len(pv.Problems) != 0 {
		t.Fatalf("preview: %+v", pv)
	}
	if r.m.Called("set ") || r.m.Called("modify") || r.m.Called("reapply") || r.m.Called("Checkpoint") {
		t.Errorf("preview issued a mutating command: %v", r.m.Calls)
	}
	if got, _, _ := r.eng.Store.List(); len(got) != 0 {
		t.Error("preview wrote a snapshot")
	}
}

func TestRestoreRevertsAndVerifies(t *testing.T) {
	r := newRig(t)
	snap, _ := r.eng.Apply(ctx, []Request{ppd("performance"), wifi("off")}, "x", nil)
	if r.m.Radio != "off" || r.m.PPDProfile != "performance" {
		t.Fatal("setup apply failed")
	}
	res, err := r.eng.Restore(ctx, snap.ID, RestoreOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.m.PPDProfile != "balanced" || r.m.NMPowersave != "default" {
		t.Errorf("not restored: ppd=%s nm=%s", r.m.PPDProfile, r.m.NMPowersave)
	}
	for _, it := range res {
		if it.Status != snapshot.ItemReverted {
			t.Errorf("%s: %s %s", it.TweakID, it.Status, it.Note)
		}
	}
	after, _ := r.eng.Store.Load(snap.ID)
	if after.Status != snapshot.StatusRestored {
		t.Errorf("status = %s", after.Status)
	}
	if _, err := r.eng.Restore(ctx, snap.ID, RestoreOptions{}, nil); err == nil {
		t.Error("second restore should be refused")
	}
}

func TestFailedApplyRollsBackEarlierChanges(t *testing.T) {
	r := newRig(t, func(m *simtest.Machine) { m.Fail["nm-reapply"] = errors.New("device busy") })
	_, err := r.eng.Apply(ctx, []Request{ppd("performance"), wifi("off")}, "x", nil)
	var te *TransactionError
	if !errors.As(err, &te) || !te.RolledBack || te.TweakID != "net.wifi-powersave" {
		t.Fatalf("want rolled-back TransactionError, got %v", err)
	}
	if r.m.PPDProfile != "balanced" {
		t.Errorf("first tweak not rolled back: %s", r.m.PPDProfile)
	}
	if r.m.NMPowersave != "default" {
		t.Errorf("partial wifi change not reverted: %s", r.m.NMPowersave)
	}
	snaps, _, _ := r.eng.Store.List()
	if len(snaps) != 1 || snaps[0].Status != snapshot.StatusRolledBack {
		t.Fatalf("journal: %+v", snaps)
	}
	if r.m.Checkpoints != 1 || r.m.RolledBack != 1 {
		t.Errorf("NM checkpoint not created/rolled back: created=%d rolledback=%d", r.m.Checkpoints, r.m.RolledBack)
	}
}

func TestIneffectiveChangeIsNotReportedAsSuccess(t *testing.T) {
	// Setting saved, but the radio never changes: exit status 0 is not success.
	r := newRig(t, func(m *simtest.Machine) { m.ReapplyApplies = false })
	_, err := r.eng.Apply(ctx, []Request{wifi("off")}, "x", nil)
	var te *TransactionError
	if !errors.As(err, &te) || !strings.Contains(err.Error(), "still reports power save") {
		t.Fatalf("want effective-state failure, got %v", err)
	}
	if r.m.NMPowersave != "default" {
		t.Errorf("setting left modified: %s", r.m.NMPowersave)
	}
}

func TestVerificationDetectsSilentNoOp(t *testing.T) {
	// powerprofilesctl "succeeds" but state does not change (daemon ignores us).
	r := newRig(t)
	reg := tweaks.NewRegistry()
	reg.Register(noopPPD{})
	r.eng.Reg = reg
	_, err := r.eng.Apply(ctx, []Request{ppd("performance")}, "x", nil)
	if err == nil || !strings.Contains(err.Error(), "did not take effect") {
		t.Fatalf("want verification failure, got %v", err)
	}
}

// noopPPD applies nothing but reports success.
type noopPPD struct{ tweaks.PowerProfile }

func (noopPPD) Apply(context.Context, tweaks.Env, tweaks.Plan) error { return nil }

func TestRollbackFailureIsReportedHonestly(t *testing.T) {
	r := newRig(t, func(m *simtest.Machine) { m.Fail["nm-reapply"] = errors.New("boom") })
	_, err := r.eng.Apply(ctx, []Request{ppd("performance"), wifi("off")}, "x", func(e Event) {
		if e.Phase == "rollback" {
			r.m.Fail["ppd-set"] = errors.New("daemon died") // rollback of the first item will fail
		}
	})
	var te *TransactionError
	if !errors.As(err, &te) || te.RolledBack || te.RollbackErr == nil {
		t.Fatalf("want incomplete rollback, got %v", err)
	}
	if !strings.Contains(err.Error(), "ROLLBACK INCOMPLETE") || !strings.Contains(err.Error(), te.SnapshotID) {
		t.Errorf("message should tell the user how to recover: %v", err)
	}
	s, _ := r.eng.Store.Load(te.SnapshotID)
	if s.Status != snapshot.StatusRollbackFailed {
		t.Errorf("status = %s", s.Status)
	}
}

// A rollback that failed halfway must stay recoverable: it blocks new
// transactions and `recover` finishes the job once the cause is gone.
func TestFailedRollbackCanBeRecoveredLater(t *testing.T) {
	r := newRig(t, func(m *simtest.Machine) { m.Fail["nm-reapply"] = errors.New("boom") })
	_, err := r.eng.Apply(ctx, []Request{ppd("performance"), wifi("off")}, "x", func(e Event) {
		if e.Phase == "rollback" {
			r.m.Fail["ppd-set"] = errors.New("daemon died")
		}
	})
	var te *TransactionError
	if !errors.As(err, &te) || te.RollbackErr == nil {
		t.Fatalf("setup: want incomplete rollback, got %v", err)
	}
	if r.m.PPDProfile != "performance" {
		t.Fatalf("setup: profile should be stuck on performance, is %s", r.m.PPDProfile)
	}
	pend, _ := r.eng.Interrupted()
	if len(pend) != 1 || pend[0].ID != te.SnapshotID {
		t.Fatalf("failed rollback not offered for recovery: %v", pend)
	}
	if _, err := r.eng.Apply(ctx, []Request{ppd("power-saver")}, "y", nil); err == nil || !strings.Contains(err.Error(), "recover") {
		t.Errorf("new transactions must wait, got %v", err)
	}
	delete(r.m.Fail, "ppd-set") // the daemon is back
	if err := r.eng.Recover(ctx, te.SnapshotID, nil); err != nil {
		t.Fatal(err)
	}
	if r.m.PPDProfile != "balanced" {
		t.Errorf("recovery did not restore the original profile: %s", r.m.PPDProfile)
	}
	if s, _ := r.eng.Store.Load(te.SnapshotID); s.Status != snapshot.StatusRolledBack {
		t.Errorf("status = %s", s.Status)
	}
}

func TestDriftIsDetectedAndNotOverwrittenUnlessForced(t *testing.T) {
	r := newRig(t)
	snap, _ := r.eng.Apply(ctx, []Request{ppd("performance")}, "x", nil)
	r.m.PPDProfile = "power-saver" // another program (the desktop applet) changed it

	res, err := r.eng.Restore(ctx, snap.ID, RestoreOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res[0].Status != snapshot.ItemDrifted || r.m.PPDProfile != "power-saver" {
		t.Fatalf("drift not respected: %+v profile=%s", res, r.m.PPDProfile)
	}
	if s, _ := r.eng.Store.Load(snap.ID); s.Status != snapshot.StatusRestorePartial {
		t.Errorf("status = %s", s.Status)
	}
	if _, err := r.eng.Restore(ctx, snap.ID, RestoreOptions{Force: true}, nil); err != nil {
		t.Fatal(err)
	}
	if r.m.PPDProfile != "balanced" {
		t.Errorf("force restore did not restore: %s", r.m.PPDProfile)
	}
}

func TestRestoreWhenAlreadyBackToOriginal(t *testing.T) {
	r := newRig(t)
	snap, _ := r.eng.Apply(ctx, []Request{ppd("performance")}, "x", nil)
	r.m.PPDProfile = "balanced" // user already undid it manually
	res, err := r.eng.Restore(ctx, snap.ID, RestoreOptions{}, nil)
	if err != nil || res[0].Status != snapshot.ItemReverted {
		t.Fatalf("%v %+v", err, res)
	}
	if r.m.Called("set balanced") {
		t.Error("should not rewrite a setting that is already correct")
	}
}

// crashy applies the change and then "kills the process" (panic) before
// TurboShift can verify or save, simulating SIGKILL/power loss mid-transaction.
type crashy struct{ tweaks.PowerProfile }

func (crashy) Apply(ctx context.Context, env tweaks.Env, p tweaks.Plan) error {
	_, _ = env.Run.Run(ctx, "powerprofilesctl", "set", p.After["profile"])
	panic("simulated crash")
}

func TestInterruptedTransactionIsDetectedAndRecovered(t *testing.T) {
	r := newRig(t)
	crashReg := tweaks.NewRegistry()
	crashReg.Register(crashy{})
	crashEng := *r.eng
	crashEng.Reg = crashReg

	func() {
		defer func() { _ = recover() }()
		_, _ = crashEng.Apply(ctx, []Request{ppd("performance")}, "x", nil)
	}()
	if r.m.PPDProfile != "performance" {
		t.Fatal("test setup: crash should have left the change applied")
	}

	// "Next launch": a fresh engine on the same state dir.
	pend, err := r.eng.Interrupted()
	if err != nil || len(pend) != 1 {
		t.Fatalf("interrupted = %v %v", pend, err)
	}
	if _, err := r.eng.Apply(ctx, []Request{ppd("power-saver")}, "y", nil); err == nil || !strings.Contains(err.Error(), "interrupted") {
		t.Errorf("new transactions must wait for recovery, got %v", err)
	}
	if err := r.eng.Recover(ctx, pend[0].ID, nil); err != nil {
		t.Fatal(err)
	}
	if r.m.PPDProfile != "balanced" {
		t.Errorf("recovery did not restore original: %s", r.m.PPDProfile)
	}
	if s, _ := r.eng.Store.Load(pend[0].ID); s.Status != snapshot.StatusRolledBack {
		t.Errorf("status = %s", s.Status)
	}
	if p, _ := r.eng.Interrupted(); len(p) != 0 {
		t.Error("still reported interrupted")
	}
}

func TestConcurrentOperationsAreRefused(t *testing.T) {
	r := newRig(t)
	lock, err := r.eng.Store.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	if _, err := r.eng.Apply(ctx, []Request{ppd("performance")}, "x", nil); !errors.Is(err, snapshot.ErrLocked) {
		t.Errorf("want ErrLocked, got %v", err)
	}
	if r.m.PPDProfile != "balanced" {
		t.Error("system changed despite the lock")
	}
}

func TestConcurrentApplyExactlyOneWins(t *testing.T) {
	r := newRig(t)
	var wg sync.WaitGroup
	errs := make([]error, 6)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = r.eng.Apply(ctx, []Request{ppd("performance")}, "x", nil)
		}()
	}
	wg.Wait()
	ok := 0
	for _, e := range errs {
		if e == nil {
			ok++
		} else if !errors.Is(e, snapshot.ErrLocked) && !errors.As(e, new(*PlanError)) {
			t.Errorf("unexpected error: %v", e)
		}
	}
	if ok != 1 {
		t.Errorf("%d appliers succeeded, want exactly 1 (others locked out or see 'already set')", ok)
	}
}

func TestInvalidParametersAreRejectedBeforeAnyChange(t *testing.T) {
	r := newRig(t)
	for _, req := range []Request{
		ppd("turbo; rm -rf /"), ppd(""), wifi("maybe"),
		{"cpu.governor", map[string]string{"governor": "../../x"}},
		{"nope.nope", nil},
	} {
		_, err := r.eng.Apply(ctx, []Request{req}, "x", nil)
		var pe *PlanError
		if !errors.As(err, &pe) {
			t.Errorf("%v: want PlanError, got %v", req, err)
		}
	}
	if len(r.m.Calls) > 0 && r.m.Called("set ") {
		t.Error("a mutating command ran for invalid input")
	}
	if s, _, _ := r.eng.Store.List(); len(s) != 0 {
		t.Error("a snapshot was written for rejected input")
	}
}

func TestAlreadyOptimalIsNotAnError(t *testing.T) {
	r := newRig(t)
	pv := r.eng.Preview(ctx, []Request{ppd("balanced")})
	if len(pv.Plans) != 0 || len(pv.Problems) != 1 || !errors.Is(pv.Problems[0].Err, tweaks.ErrNoChange) {
		t.Errorf("preview: %+v", pv)
	}
}

func TestDuplicateRequestRejected(t *testing.T) {
	r := newRig(t)
	pv := r.eng.Preview(ctx, []Request{ppd("performance"), ppd("power-saver")})
	if len(pv.Problems) != 1 {
		t.Errorf("%+v", pv)
	}
}

func TestPowerManagerConflictMakesTweakUnavailable(t *testing.T) {
	r := newRig(t, func(m *simtest.Machine) { m.OtherMgrs["tuned.service"] = true })
	_, err := r.eng.Apply(ctx, []Request{ppd("performance")}, "x", nil)
	if err == nil || !strings.Contains(err.Error(), "TuneD") {
		t.Fatalf("want conflict naming TuneD, got %v", err)
	}
	if r.m.PPDProfile != "balanced" {
		t.Error("changed despite conflict")
	}
	// Direct CPU policy must refuse as well while any manager is active.
	_, err = r.eng.Apply(ctx, []Request{{"cpu.governor", map[string]string{"governor": "performance"}}}, "x", nil)
	if err == nil || len(r.priv.Actions) != 0 {
		t.Errorf("cpu.governor must not write while managers are active: %v", err)
	}
}

func TestMissingDependenciesFailGracefully(t *testing.T) {
	r := newRig(t, func(m *simtest.Machine) { m.Tools["powerprofilesctl"] = false; m.Tools["nmcli"] = false })
	pv := r.eng.Preview(ctx, []Request{ppd("performance"), wifi("off")})
	if len(pv.Plans) != 0 || len(pv.Problems) != 2 {
		t.Fatalf("%+v", pv)
	}
	for _, p := range pv.Problems {
		if !errors.Is(p.Err, tweaks.ErrUnavailable) {
			t.Errorf("%s: %v", p.TweakID, p.Err)
		}
	}
}

func TestSnapshotSaveFailureMeansNothingChanged(t *testing.T) {
	r := newRig(t)
	blocker := filepath.Join(t.TempDir(), "file")
	_ = os.WriteFile(blocker, []byte("x"), 0o600)
	r.eng.Store = &snapshot.Store{Dir: filepath.Join(blocker, "snaps")} // parent is a file
	_, err := r.eng.Apply(ctx, []Request{ppd("performance")}, "x", nil)
	if err == nil {
		t.Fatal("expected failure")
	}
	if r.m.PPDProfile != "balanced" {
		t.Error("system modified even though the restore point could not be saved")
	}
}

func TestCancelledContextRollsBack(t *testing.T) {
	r := newRig(t)
	c, cancel := context.WithCancel(ctx)
	_, err := r.eng.Apply(c, []Request{ppd("performance"), wifi("off")}, "x", func(e Event) {
		if e.Phase == "verify" && e.TweakID == "power.profile" && strings.Contains(e.Message, "verified") {
			cancel() // user hits Ctrl-C after the first tweak
		}
	})
	if err == nil {
		// The wifi tweak may still have completed if cancellation raced; then state must be consistent.
		if r.m.PPDProfile != "performance" {
			t.Fatal("inconsistent state after success")
		}
		return
	}
	if r.m.PPDProfile != "balanced" || r.m.NMPowersave != "default" {
		t.Errorf("cancel left changes behind: ppd=%s nm=%s", r.m.PPDProfile, r.m.NMPowersave)
	}
}

func TestNMCheckpointCommittedOnSuccessAndRollbackFallback(t *testing.T) {
	r := newRig(t)
	if _, err := r.eng.Apply(ctx, []Request{wifi("off")}, "x", nil); err != nil {
		t.Fatal(err)
	}
	if r.m.Checkpoints != 1 || r.m.Destroyed != 1 || r.m.RolledBack != 0 {
		t.Errorf("success should destroy (commit) the checkpoint: %+v", r.m)
	}

	// polkit refuses CheckpointRollback -> journal restore must still revert.
	r2 := newRig(t, func(m *simtest.Machine) {
		m.ReapplyApplies = false
		m.NMRollbackDenied = true
	})
	_, err := r2.eng.Apply(ctx, []Request{wifi("off")}, "x", nil)
	if err == nil || r2.m.NMPowersave != "default" {
		t.Errorf("journal fallback failed: err=%v nm=%s", err, r2.m.NMPowersave)
	}
}

func TestGuardUnavailableDoesNotBlockLowRiskChange(t *testing.T) {
	r := newRig(t, func(m *simtest.Machine) { m.Tools["busctl"] = false })
	var msgs []string
	if _, err := r.eng.Apply(ctx, []Request{wifi("off")}, "x", func(e Event) { msgs = append(msgs, e.Message) }); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(msgs, "|"), "No automatic safety net") {
		t.Error("user was not told the safety net was missing")
	}
}

// ---- privileged CPU policy ---------------------------------------------

func noManagers(m *simtest.Machine) { m.PPD = false; m.Tools["powerprofilesctl"] = false }

func cpuReq(g string) Request { return Request{"cpu.governor", map[string]string{"governor": g}} }

func TestCPUPolicyAppliesToAllCoresAndRestores(t *testing.T) {
	r := newRig(t, noManagers)
	snap, err := r.eng.Apply(ctx, []Request{cpuReq("performance")}, "x", nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.m.Governor(0) != "performance" || r.m.Governor(1) != "performance" {
		t.Error("not applied to every CPU")
	}
	if len(r.priv.Actions) != 2 {
		t.Errorf("actions: %+v", r.priv.Actions)
	}
	if r.priv.Batches != 1 {
		t.Errorf("all CPUs must be written under one authorisation, got %d prompts", r.priv.Batches)
	}
	for _, a := range r.priv.Actions {
		if privilege.Validate(a) != nil {
			t.Errorf("tweak issued a non-allowlisted action: %+v", a)
		}
	}
	if _, err := r.eng.Restore(ctx, snap.ID, RestoreOptions{}, nil); err != nil {
		t.Fatal(err)
	}
	if r.m.Governor(0) != "powersave" || r.m.Governor(1) != "powersave" {
		t.Error("not restored")
	}
	if r.priv.Batches != 2 {
		t.Errorf("restore should add exactly one more authorisation, total %d", r.priv.Batches)
	}
}

func TestCPUPolicyRestoresPerCoreOriginalsWhenMixed(t *testing.T) {
	r := newRig(t, noManagers)
	r.m.SetGovernor(1, "performance") // cores differ before we start
	r.eng.Env.Report = detect.Probe(ctx, detect.Env{FS: r.m.FS, Run: r.m})
	snap, err := r.eng.Apply(ctx, []Request{cpuReq("performance")}, "x", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.eng.Restore(ctx, snap.ID, RestoreOptions{}, nil); err != nil {
		t.Fatal(err)
	}
	if r.m.Governor(0) != "powersave" || r.m.Governor(1) != "performance" {
		t.Errorf("per-core originals lost: cpu0=%s cpu1=%s", r.m.Governor(0), r.m.Governor(1))
	}
}

func TestCPUPolicyPermissionDeniedLeavesSystemUntouched(t *testing.T) {
	r := newRig(t, noManagers)
	r.priv.Err = errors.New("administrator authorisation was dismissed or denied")
	_, err := r.eng.Apply(ctx, []Request{cpuReq("performance")}, "x", nil)
	var te *TransactionError
	if !errors.As(err, &te) || !te.RolledBack {
		t.Fatalf("want clean rollback, got %v", err)
	}
	if r.m.Governor(0) != "powersave" {
		t.Error("governor changed despite denied authorisation")
	}
}

func TestCPUPolicyUnavailableWithoutPrivilegeMechanism(t *testing.T) {
	r := newRig(t, noManagers)
	r.priv.NoAuth = "pkexec (polkit) is not installed"
	_, err := r.eng.Apply(ctx, []Request{cpuReq("performance")}, "x", nil)
	if err == nil || !strings.Contains(err.Error(), "pkexec") {
		t.Errorf("got %v", err)
	}
}

func TestCPUPolicyRejectsGovernorNotOfferedByHardware(t *testing.T) {
	r := newRig(t, noManagers)
	_, err := r.eng.Apply(ctx, []Request{cpuReq("schedutil")}, "x", nil) // not in available governors
	var pe *PlanError
	if !errors.As(err, &pe) {
		t.Errorf("got %v", err)
	}
}

func TestRecommendations(t *testing.T) {
	// Plugged in on power-saver -> recommend balanced.
	r := newRig(t, func(m *simtest.Machine) { m.PPDProfile = "power-saver" })
	a, _ := r.eng.Reg.Get("power.profile")
	as := a.Assess(ctx, r.eng.Env)
	if as.Recommend == nil || as.Recommend.Params["profile"] != "balanced" {
		t.Errorf("want balanced recommendation: %+v", as)
	}
	// Already fine -> no recommendation (a valid, honest outcome).
	r2 := newRig(t)
	a2 := a.Assess(ctx, r2.eng.Env)
	if a2.Recommend != nil {
		t.Errorf("balanced on AC should not be 'fixed': %+v", a2.Recommend)
	}
	// Wi-Fi power save on while on AC -> situational recommendation.
	w, _ := r2.eng.Reg.Get("net.wifi-powersave")
	if as := w.Assess(ctx, r2.eng.Env); as.Recommend == nil || w.Meta().Tier != tweaks.TierSituational {
		t.Errorf("wifi: %+v", as)
	}
}
