package engine

import (
	"testing"

	"github.com/mttcn83/turboshift/internal/snapshot"
)

// A setting applied by an older restore point stays in effect when a newer
// point that changed it again is only partly restored: reverting the newer
// item puts back exactly what the older point applied. Check must keep
// reporting it, and must report it once however often it was re-applied.
func TestCheckAfterPartialRestoreKeepsOlderPointAndNeverDuplicates(t *testing.T) {
	r := newRig(t)
	if _, err := r.eng.Apply(ctx, []Request{ppd("performance")}, "a", nil); err != nil {
		t.Fatal(err)
	}
	b, err := r.eng.Apply(ctx, []Request{ppd("power-saver"), wifi("off")}, "b", nil)
	if err != nil {
		t.Fatal(err)
	}
	r.m.NMPowersave = "ignore" // someone else changed Wi-Fi, so restoring b is partial
	if _, err := r.eng.Restore(ctx, b.ID, RestoreOptions{}, nil); err != nil {
		t.Fatal(err)
	}
	if s, _ := r.eng.Store.Load(b.ID); s.Status != snapshot.StatusRestorePartial || r.m.PPDProfile != "performance" {
		t.Fatalf("setup: status=%s profile=%s", s.Status, r.m.PPDProfile)
	}

	count := func(res []CheckResult, id string) (n int, last CheckResult) {
		for _, c := range res {
			if c.TweakID == id {
				n, last = n+1, c
			}
		}
		return
	}
	res, err := r.eng.Check(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n, c := count(res, "power.profile"); n != 1 || c.State != CheckHolding || c.Params["profile"] != "performance" {
		t.Fatalf("the older point's setting is still in effect and must be checked: %+v", res)
	}
	r.m.PPDProfile = "balanced" // a restart reset it
	res, _ = r.eng.Check(ctx)
	if n, c := count(res, "power.profile"); n != 1 || c.State != CheckReset {
		t.Fatalf("reset of the older point's setting went unnoticed: %+v", res)
	}

	r.m.PPDProfile = "performance"
	if _, err := r.eng.Apply(ctx, []Request{ppd("power-saver")}, "c", nil); err != nil {
		t.Fatal(err)
	}
	res, _ = r.eng.Check(ctx)
	if n, c := count(res, "power.profile"); n != 1 || c.Params["profile"] != "power-saver" || c.State != CheckHolding {
		t.Fatalf("want power.profile once, from the newest point: %+v", res)
	}
}
