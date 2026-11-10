package engine

import (
	"context"

	"github.com/mttcn83/turboshift/internal/snapshot"
	"github.com/mttcn83/turboshift/internal/tweaks"
)

// CheckState says whether a setting TurboShift applied is still in effect.
type CheckState string

const (
	CheckHolding CheckState = "holding" // live value still matches what was applied
	CheckReset   CheckState = "reset"   // back at the original value (typically a reboot reset it)
	CheckChanged CheckState = "changed" // something else set it to a third value
	CheckUnknown CheckState = "unknown" // could not be read, or the tweak is no longer known
)

// CheckResult is the live status of one applied setting.
type CheckResult struct {
	TweakID    string
	Name       string
	SnapshotID string
	Params     map[string]string
	State      CheckState
	Note       string
}

// Check compares what the restore points say is in effect with the live
// system. For each tweak only the most recent applied, not-yet-restored item
// counts. It is read-only.
func (e *Engine) Check(ctx context.Context) ([]CheckResult, error) {
	snaps, _, err := e.Store.List() // newest first
	if err != nil {
		return nil, err
	}
	type entry struct {
		snap string
		it   snapshot.Item
	}
	active := map[string]entry{}
	var order []string
	for i := len(snaps) - 1; i >= 0; i-- { // oldest first so later points win
		s := snaps[i]
		if s.Status != snapshot.StatusCommitted && s.Status != snapshot.StatusRestorePartial {
			continue
		}
		for _, it := range s.Items {
			switch it.Status {
			case snapshot.ItemVerified, snapshot.ItemDrifted:
				if _, ok := active[it.TweakID]; !ok {
					order = append(order, it.TweakID)
				}
				active[it.TweakID] = entry{s.ID, it}
			case snapshot.ItemReverted:
				delete(active, it.TweakID)
			}
		}
	}
	var out []CheckResult
	for _, id := range order {
		en, ok := active[id]
		if !ok {
			continue
		}
		it := en.it
		res := CheckResult{TweakID: id, Name: it.Name, SnapshotID: en.snap, Params: it.Params}
		t, known := e.Reg.Get(id)
		if !known {
			res.State, res.Note = CheckUnknown, "this version of TurboShift no longer knows this tweak"
			out = append(out, res)
			continue
		}
		cur, rerr := t.Read(ctx, e.Env, tweaks.State(it.After))
		switch {
		case rerr != nil:
			res.State, res.Note = CheckUnknown, rerr.Error()
		case cur.Covers(tweaks.State(it.After)):
			res.State = CheckHolding
		case cur.Covers(tweaks.State(it.Before)):
			res.State, res.Note = CheckReset, "back at its original value (a restart resets some settings)"
		default:
			res.State, res.Note = CheckChanged, "changed by something else ("+diff(tweaks.State(it.After), cur)+")"
		}
		out = append(out, res)
	}
	return out, nil
}
