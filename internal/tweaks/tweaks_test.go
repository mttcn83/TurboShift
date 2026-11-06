package tweaks

import (
	"context"
	"errors"
	"testing"

	"github.com/mttcn83/turboshift/internal/platform"
)

// A restore point is plain JSON on disk. Values read back from it must be
// validated before reaching a program's argument list.
func TestRestoreRejectsOptionLikeValuesFromJournal(t *testing.T) {
	run := platform.NewFakeRunner().Install("powerprofilesctl").Install("nmcli")
	env := Env{FS: platform.NewMemFS(), Run: run}
	for _, v := range []string{"--help", "-x", "", "a b", "x;reboot", "Balanced", "$(id)"} {
		err := PowerProfile{}.Restore(context.Background(), env, Plan{Before: State{"profile": v}})
		if !errors.Is(err, ErrInvalidParam) {
			t.Errorf("profile %q: err=%v", v, err)
		}
		err = WifiPowerSave{}.Restore(context.Background(), env, Plan{Before: State{"nm_powersave": v}})
		if !errors.Is(err, ErrInvalidParam) {
			t.Errorf("wifi %q: err=%v", v, err)
		}
	}
	if len(run.Calls) != 0 {
		t.Errorf("invalid journal values reached a program: %v", run.Calls)
	}
}
