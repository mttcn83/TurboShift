// Package engine runs tweak transactions: plan → journal → apply → verify →
// commit, with automatic rollback, drift-aware restore and crash recovery.
package engine

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mttcn83/turboshift/internal/snapshot"
	"github.com/mttcn83/turboshift/internal/tweaks"
)

// Request asks for one tweak with parameters.
type Request struct {
	TweakID string
	Params  map[string]string
}

// Problem explains why one request could not be planned.
type Problem struct {
	TweakID string
	Err     error
}

func (p Problem) Error() string { return p.TweakID + ": " + p.Err.Error() }

// Preview is a side-effect-free description of what Apply would do.
type Preview struct {
	Plans    []tweaks.Plan
	Problems []Problem
}

// Event is progress reported to the UI/CLI.
type Event struct {
	Phase   string // plan, journal, apply, verify, rollback, commit, restore, recover
	TweakID string
	Message string
	Err     error
}

type Engine struct {
	Reg   *tweaks.Registry
	Store *snapshot.Store
	Env   tweaks.Env
	Now   func() time.Time
}

func (e *Engine) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}

func noop(Event) {}

// TransactionError is returned when Apply failed and was rolled back.
type TransactionError struct {
	SnapshotID  string
	TweakID     string
	Cause       error
	RolledBack  bool  // every applied change was reverted
	RollbackErr error // what could not be reverted
}

func (t *TransactionError) Error() string {
	s := fmt.Sprintf("%s failed: %v", t.TweakID, t.Cause)
	switch {
	case t.RolledBack:
		s += " — all changes were rolled back"
	case t.RollbackErr != nil:
		s += fmt.Sprintf(" — ROLLBACK INCOMPLETE (%v); run `turbo snapshots recover` (restore point %s)", t.RollbackErr, t.SnapshotID)
	}
	return s
}
func (t *TransactionError) Unwrap() error { return t.Cause }

// Preview plans every request against the live system without modifying anything.
func (e *Engine) Preview(ctx context.Context, reqs []Request) Preview {
	var pv Preview
	seen := map[string]bool{}
	for _, r := range reqs {
		t, ok := e.Reg.Get(r.TweakID)
		if !ok {
			pv.Problems = append(pv.Problems, Problem{r.TweakID, fmt.Errorf("unknown tweak")})
			continue
		}
		if seen[r.TweakID] {
			pv.Problems = append(pv.Problems, Problem{r.TweakID, errors.New("requested twice in one transaction")})
			continue
		}
		seen[r.TweakID] = true
		pl, err := t.Plan(ctx, e.Env, r.Params)
		if err != nil {
			pv.Problems = append(pv.Problems, Problem{r.TweakID, err})
			continue
		}
		pv.Plans = append(pv.Plans, pl)
	}
	// Declared conflicts between tweaks chosen together.
	chosen := map[string]bool{}
	for _, p := range pv.Plans {
		chosen[p.TweakID] = true
	}
	var keep []tweaks.Plan
	for _, p := range pv.Plans {
		t, _ := e.Reg.Get(p.TweakID)
		bad := ""
		for _, c := range t.Meta().Conflicts {
			if chosen[c] {
				bad = c
			}
		}
		if bad != "" {
			pv.Problems = append(pv.Problems, Problem{p.TweakID, fmt.Errorf("conflicts with %s selected in the same transaction", bad)})
			continue
		}
		keep = append(keep, p)
	}
	pv.Plans = keep
	return pv
}

// Apply executes the requests as one transaction. On any failure, every change
// made so far is reverted in reverse order.
func (e *Engine) Apply(ctx context.Context, reqs []Request, label string, on func(Event)) (*snapshot.Snapshot, error) {
	if on == nil {
		on = noop
	}
	lock, err := e.Store.Acquire()
	if err != nil {
		return nil, err
	}
	defer lock.Release()

	// A previously interrupted transaction must be resolved first, otherwise
	// its Before-state could be overwritten by ours.
	if pend, _ := e.Store.Interrupted(); len(pend) > 0 {
		return nil, fmt.Errorf("an earlier transaction (%s) was interrupted; recover it first", pend[0].ID)
	}

	on(Event{Phase: "plan", Message: "Reading current system state"})
	pv := e.Preview(ctx, reqs)
	if len(pv.Problems) > 0 {
		return nil, &PlanError{Problems: pv.Problems}
	}
	if len(pv.Plans) == 0 {
		return nil, errors.New("nothing to apply")
	}

	snap := e.Store.New(label)
	snap.Kernel = e.Env.Report.System.Kernel
	for _, p := range pv.Plans {
		t, _ := e.Reg.Get(p.TweakID)
		snap.Items = append(snap.Items, snapshot.Item{
			TweakID: p.TweakID, Name: t.Meta().Name, Params: p.Params,
			Before: p.Before, After: p.After, Status: snapshot.ItemPlanned,
		})
	}
	snap.Log(e.now(), "Restore point created before any change")
	on(Event{Phase: "journal", Message: "Saving restore point " + snap.ID})
	if err := e.Store.Save(snap); err != nil {
		return nil, fmt.Errorf("could not save the restore point, so nothing was changed: %w", err)
	}

	snap.Status = snapshot.StatusApplying
	if err := e.Store.Save(snap); err != nil {
		return nil, err
	}

	for i := range snap.Items {
		it := &snap.Items[i]
		pl := pv.Plans[i]
		t, _ := e.Reg.Get(it.TweakID)

		it.Status = snapshot.ItemApplying
		_ = e.Store.Save(snap)
		on(Event{Phase: "apply", TweakID: it.TweakID, Message: "Applying " + it.Name})

		if err := e.applyOne(ctx, t, pl, on); err != nil {
			it.Status, it.Error = snapshot.ItemFailed, err.Error()
			on(Event{Phase: "apply", TweakID: it.TweakID, Message: "Failed: " + err.Error(), Err: err})
			return snap, e.rollback(snap, i, it.TweakID, err, on)
		}
		it.Status = snapshot.ItemVerified
		if err := e.Store.Save(snap); err != nil {
			return snap, e.rollback(snap, i, it.TweakID, fmt.Errorf("could not update restore point: %w", err), on)
		}
		on(Event{Phase: "verify", TweakID: it.TweakID, Message: it.Name + " verified on the live system"})
	}

	snap.Status = snapshot.StatusCommitted
	snap.Log(e.now(), "All changes applied and verified")
	if err := e.Store.Save(snap); err != nil {
		return snap, err
	}
	on(Event{Phase: "commit", Message: "Done. Restore point " + snap.ID + " saved"})
	return snap, nil
}

// applyOne applies and verifies a single plan, with an optional safety guard.
func (e *Engine) applyOne(ctx context.Context, t tweaks.Tweak, pl tweaks.Plan, on func(Event)) (err error) {
	var guard tweaks.Guard
	if g, ok := t.(tweaks.Guarded); ok && pl.Disruptive {
		gd, gerr := g.BeginGuard(ctx, e.Env, pl)
		if gerr != nil {
			on(Event{Phase: "apply", TweakID: pl.TweakID, Message: "No automatic safety net available (" + gerr.Error() + "); relying on the restore point"})
		} else {
			guard = gd
			on(Event{Phase: "apply", TweakID: pl.TweakID, Message: "Safety checkpoint created"})
		}
	}
	defer func() {
		if guard == nil {
			return
		}
		bg := context.WithoutCancel(ctx)
		if err != nil {
			if aerr := guard.Abort(bg); aerr != nil {
				on(Event{Phase: "rollback", TweakID: pl.TweakID, Message: aerr.Error()})
			}
		} else if cerr := guard.Commit(bg); cerr != nil {
			on(Event{Phase: "commit", TweakID: pl.TweakID, Message: "Could not release the safety checkpoint (it will expire on its own): " + cerr.Error()})
		}
	}()

	if err := t.Apply(ctx, e.Env, pl); err != nil {
		return err
	}
	on(Event{Phase: "verify", TweakID: pl.TweakID, Message: "Checking the live system"})
	return tweaks.Verify(ctx, t, e.Env, pl)
}

// rollback reverts items [0..upTo] in reverse order, including the failed one
// (Restore is idempotent, so a partial Apply is handled too).
func (e *Engine) rollback(snap *snapshot.Snapshot, upTo int, failedID string, cause error, on func(Event)) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var errs []error
	for i := upTo; i >= 0; i-- {
		it := &snap.Items[i]
		t, _ := e.Reg.Get(it.TweakID)
		on(Event{Phase: "rollback", TweakID: it.TweakID, Message: "Reverting " + it.Name})
		if err := e.restoreItem(ctx, t, it); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", it.Name, err))
			on(Event{Phase: "rollback", TweakID: it.TweakID, Message: "Could not revert: " + err.Error(), Err: err})
			continue
		}
		if it.Status != snapshot.ItemFailed {
			it.Status = snapshot.ItemReverted
		}
	}
	te := &TransactionError{SnapshotID: snap.ID, TweakID: failedID, Cause: cause}
	if len(errs) == 0 {
		snap.Status, te.RolledBack = snapshot.StatusRolledBack, true
		snap.Log(e.now(), "Failed and rolled back: "+cause.Error())
	} else {
		te.RollbackErr = errors.Join(errs...)
		snap.Status = snapshot.StatusRollbackFailed
		snap.Log(e.now(), "Failed; rollback incomplete: "+te.RollbackErr.Error())
	}
	_ = e.Store.Save(snap)
	return te
}

// restoreItem puts an item's Before state back and verifies it.
func (e *Engine) restoreItem(ctx context.Context, t tweaks.Tweak, it *snapshot.Item) error {
	before := tweaks.State(it.Before)
	// Nothing to undo (e.g. the failed step never changed anything): do not
	// rewrite values or ask for administrator permission a second time.
	if cur, err := t.Read(ctx, e.Env, before); err == nil && cur.Covers(before) {
		return nil
	}
	pl := tweaks.Plan{TweakID: it.TweakID, Params: it.Params, Before: it.Before, After: it.After}
	if err := t.Restore(ctx, e.Env, pl); err != nil {
		return err
	}
	return tweaks.VerifyState(ctx, t, e.Env, before)
}

// PlanError carries every reason a transaction could not be planned.
type PlanError struct{ Problems []Problem }

func (p *PlanError) Error() string {
	var s []string
	for _, pr := range p.Problems {
		s = append(s, pr.Error())
	}
	return "cannot apply: " + strings.Join(s, "; ")
}

// ---- restore & recovery ------------------------------------------------

// RestoreOptions controls Restore.
type RestoreOptions struct {
	// Force restores items even when another program changed the setting
	// since TurboShift applied it (drift).
	Force bool
}

// ItemResult is the outcome for one item of a restore.
type ItemResult struct {
	TweakID string
	Name    string
	Status  snapshot.ItemStatus
	Note    string
}

// Restore reverts a committed snapshot. It will not blindly overwrite a
// setting someone else changed afterwards: such items are reported as drifted
// and left alone unless Force is set.
func (e *Engine) Restore(ctx context.Context, id string, opt RestoreOptions, on func(Event)) ([]ItemResult, error) {
	if on == nil {
		on = noop
	}
	lock, err := e.Store.Acquire()
	if err != nil {
		return nil, err
	}
	defer lock.Release()

	snap, err := e.Store.Load(id)
	if err != nil {
		return nil, err
	}
	switch snap.Status {
	case snapshot.StatusCommitted, snapshot.StatusRestorePartial:
	case snapshot.StatusRestored:
		return nil, errors.New("this restore point was already restored")
	default:
		return nil, fmt.Errorf("restore point is %q; only committed restore points can be restored (interrupted ones are recovered automatically)", snap.Status)
	}

	var results []ItemResult
	failed := false
	for i := len(snap.Items) - 1; i >= 0; i-- {
		it := &snap.Items[i]
		res := ItemResult{TweakID: it.TweakID, Name: it.Name}
		if it.Status == snapshot.ItemReverted {
			res.Status, res.Note = snapshot.ItemReverted, "already restored"
			results = append(results, res)
			continue
		}
		t, ok := e.Reg.Get(it.TweakID)
		if !ok {
			res.Status, res.Note = snapshot.ItemFailed, "this version of TurboShift no longer knows this tweak"
			it.Error, it.Status = res.Note, snapshot.ItemFailed
			failed = true
			results = append(results, res)
			continue
		}
		on(Event{Phase: "restore", TweakID: it.TweakID, Message: "Checking " + it.Name})

		cur, rerr := t.Read(ctx, e.Env, tweaks.State(it.After))
		curBefore, _ := t.Read(ctx, e.Env, tweaks.State(it.Before))
		switch {
		case rerr == nil && curBefore != nil && curBefore.Covers(tweaks.State(it.Before)):
			it.Status = snapshot.ItemReverted
			res.Status, res.Note = snapshot.ItemReverted, "already back to the original value"
		case rerr == nil && !cur.Covers(tweaks.State(it.After)) && !opt.Force:
			it.Status = snapshot.ItemDrifted
			res.Status, res.Note = snapshot.ItemDrifted, "changed by something else since TurboShift applied it ("+diff(tweaks.State(it.After), cur)+"); left untouched"
			failed = true
		default:
			if err := e.restoreItem(ctx, t, it); err != nil {
				it.Status, it.Error = snapshot.ItemFailed, err.Error()
				res.Status, res.Note = snapshot.ItemFailed, err.Error()
				failed = true
				on(Event{Phase: "restore", TweakID: it.TweakID, Message: "Failed: " + err.Error(), Err: err})
			} else {
				it.Status = snapshot.ItemReverted
				res.Status, res.Note = snapshot.ItemReverted, "restored and verified"
				on(Event{Phase: "restore", TweakID: it.TweakID, Message: it.Name + " restored"})
			}
		}
		results = append(results, res)
	}
	if failed {
		snap.Status = snapshot.StatusRestorePartial
		snap.Log(e.now(), "Restore finished with items skipped or failed")
	} else {
		snap.Status = snapshot.StatusRestored
		snap.Log(e.now(), "Restored")
	}
	if err := e.Store.Save(snap); err != nil {
		return results, err
	}
	return results, nil
}

func diff(want, got tweaks.State) string {
	var out []string
	for k, v := range want {
		if got[k] != v {
			out = append(out, fmt.Sprintf("%s: expected %q, now %q", k, v, got[k]))
		}
	}
	sort.Strings(out)
	if len(out) > 2 {
		out = append(out[:2], fmt.Sprintf("+%d more", len(out)-2))
	}
	return strings.Join(out, "; ")
}

// Interrupted lists transactions that were cut off (crash, power loss, kill).
func (e *Engine) Interrupted() ([]*snapshot.Snapshot, error) { return e.Store.Interrupted() }

// Recover reverts everything an interrupted transaction may have touched.
// Restore is idempotent, so items that were never applied are harmless.
func (e *Engine) Recover(ctx context.Context, id string, on func(Event)) error {
	if on == nil {
		on = noop
	}
	lock, err := e.Store.Acquire()
	if err != nil {
		return err
	}
	defer lock.Release()
	snap, err := e.Store.Load(id)
	if err != nil {
		return err
	}
	if snap.Status != snapshot.StatusApplying && snap.Status != snapshot.StatusRollbackFailed {
		return fmt.Errorf("restore point %s does not need recovery (%s)", id, snap.Status)
	}
	var errs []error
	for i := len(snap.Items) - 1; i >= 0; i-- {
		it := &snap.Items[i]
		if it.Status == snapshot.ItemPlanned || it.Status == snapshot.ItemReverted {
			continue
		}
		t, ok := e.Reg.Get(it.TweakID)
		if !ok {
			errs = append(errs, fmt.Errorf("%s: unknown tweak", it.TweakID))
			continue
		}
		on(Event{Phase: "recover", TweakID: it.TweakID, Message: "Reverting " + it.Name})
		if err := e.restoreItem(ctx, t, it); err != nil {
			it.Error = err.Error()
			errs = append(errs, fmt.Errorf("%s: %w", it.Name, err))
			continue
		}
		it.Status = snapshot.ItemReverted
	}
	if len(errs) > 0 {
		snap.Status = snapshot.StatusRollbackFailed
		snap.Log(e.now(), "Recovery incomplete: "+errors.Join(errs...).Error())
		_ = e.Store.Save(snap)
		return errors.Join(errs...)
	}
	snap.Status = snapshot.StatusRolledBack
	snap.Log(e.now(), "Recovered after interruption: all changes reverted")
	return e.Store.Save(snap)
}
