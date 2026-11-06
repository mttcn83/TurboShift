package engine

import (
	"context"
	"errors"
	"strings"

	"github.com/mttcn83/turboshift/internal/tweaks"
)

// SkippedItem is a preset entry that will not be applied, and why.
type SkippedItem struct {
	TweakID, Name, Reason string
	Already               bool // true when the setting already has the wanted value
}

// ResolvePreset turns a preset into the requests that make sense on this
// machine right now. Items that are unavailable, not offered with that value,
// or already set are returned as skipped; nothing is changed.
func (e *Engine) ResolvePreset(ctx context.Context, p tweaks.Preset) ([]Request, []SkippedItem) {
	var reqs []Request
	var skipped []SkippedItem
	for _, it := range p.Items {
		t, ok := e.Reg.Get(it.TweakID)
		if !ok {
			continue
		}
		m := t.Meta()
		req := Request{TweakID: it.TweakID, Params: map[string]string{m.ParamName: it.Value}}
		_, err := t.Plan(ctx, e.Env, req.Params)
		switch {
		case err == nil:
			reqs = append(reqs, req)
		case errors.Is(err, tweaks.ErrNoChange):
			skipped = append(skipped, SkippedItem{it.TweakID, m.Name, "already set", true})
		case errors.Is(err, tweaks.ErrUnavailable):
			skipped = append(skipped, SkippedItem{it.TweakID, m.Name, shortReason(strings.TrimPrefix(err.Error(), tweaks.ErrUnavailable.Error()+": ")), false})
		case errors.Is(err, tweaks.ErrInvalidParam):
			skipped = append(skipped, SkippedItem{it.TweakID, m.Name, "this system does not offer the value " + it.Value, false})
		default:
			skipped = append(skipped, SkippedItem{it.TweakID, m.Name, err.Error(), false})
		}
	}
	return reqs, skipped
}

// shortReason condenses the long explanations tweaks give for being
// unavailable into one clause that fits a list.
func shortReason(r string) string {
	if strings.HasPrefix(r, "administrator actions are disabled") {
		return "needs a system-wide install (sudo make install)"
	}
	if i := strings.Index(r, " and would"); i > 0 {
		r = r[:i] + " and owns this setting"
	}
	if i := strings.Index(r, ". "); i > 0 {
		r = r[:i]
	}
	return r
}
