package snapshot

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	return &Store{Dir: filepath.Join(t.TempDir(), "snaps"), Now: func() time.Time { return time.Date(2026, 11, 6, 12, 0, 0, 0, time.UTC) }}
}

func sample(st *Store) *Snapshot {
	s := st.New("test")
	s.Items = []Item{{TweakID: "power.profile", Before: map[string]string{"profile": "balanced"}, After: map[string]string{"profile": "performance"}, Status: ItemPlanned}}
	return s
}

func TestSaveLoadRoundTripAndPermissions(t *testing.T) {
	st := newStore(t)
	s := sample(st)
	if err := st.Save(s); err != nil {
		t.Fatal(err)
	}
	got, err := st.Load(s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Items[0].Before["profile"] != "balanced" || got.Label != "test" {
		t.Errorf("round trip lost data: %+v", got)
	}
	fi, _ := os.Stat(st.file(s.ID))
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("snapshot perms = %v, want 0600", fi.Mode().Perm())
	}
	di, _ := os.Stat(st.Dir)
	if di.Mode().Perm() != 0o700 {
		t.Errorf("dir perms = %v, want 0700", di.Mode().Perm())
	}
	ents, _ := os.ReadDir(st.Dir)
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
}

func TestTamperedSnapshotIsRejected(t *testing.T) {
	st := newStore(t)
	s := sample(st)
	_ = st.Save(s)
	b, _ := os.ReadFile(st.file(s.ID))
	// An attacker/bug flips the "before" value to make a restore write something else.
	b = []byte(strings.Replace(string(b), `"balanced"`, `"power-saver"`, 1))
	_ = os.WriteFile(st.file(s.ID), b, 0o600)
	if _, err := st.Load(s.ID); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("want ErrCorrupt, got %v", err)
	}
}

func TestTruncatedJSONIsCorruptNotPanic(t *testing.T) {
	st := newStore(t)
	_ = os.MkdirAll(st.Dir, 0o700)
	_ = os.WriteFile(filepath.Join(st.Dir, "abc.json"), []byte(`{"id":"abc","items":[`), 0o600)
	if _, err := st.Load("abc"); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("want ErrCorrupt, got %v", err)
	}
	snaps, corrupt, err := st.List()
	if err != nil || len(snaps) != 0 || len(corrupt) != 1 {
		t.Fatalf("list: %v %v %v", snaps, corrupt, err)
	}
}

func TestIDValidationBlocksPathTraversal(t *testing.T) {
	st := newStore(t)
	for _, id := range []string{"../etc/passwd", "a/b", "", "x y", strings.Repeat("a", 65)} {
		if _, err := st.Load(id); !errors.Is(err, ErrNotFound) {
			t.Errorf("Load(%q) = %v, want ErrNotFound", id, err)
		}
	}
	s := sample(st)
	s.ID = "../evil"
	if err := st.Save(s); err == nil {
		t.Error("Save accepted a path-traversal ID")
	}
}

func TestListNewestFirstAndInterrupted(t *testing.T) {
	st := newStore(t)
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, status := range []Status{StatusCommitted, StatusApplying, StatusRolledBack} {
		st.Now = func() time.Time { return t0.Add(time.Duration(i) * time.Hour) }
		s := sample(st)
		s.Status = status
		if err := st.Save(s); err != nil {
			t.Fatal(err)
		}
	}
	all, _, _ := st.List()
	if len(all) != 3 || !all[0].CreatedAt.After(all[2].CreatedAt) {
		t.Fatalf("not newest-first: %v", all)
	}
	in, _ := st.Interrupted()
	if len(in) != 1 || in[0].Status != StatusApplying {
		t.Errorf("interrupted = %+v", in)
	}
}

func TestListMissingDirIsEmptyNotError(t *testing.T) {
	st := newStore(t)
	s, c, err := st.List()
	if err != nil || len(s) != 0 || len(c) != 0 {
		t.Errorf("%v %v %v", s, c, err)
	}
}

func TestLockIsExclusive(t *testing.T) {
	st := newStore(t)
	l1, err := st.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Acquire(); !errors.Is(err, ErrLocked) {
		t.Fatalf("second Acquire: want ErrLocked, got %v", err)
	}
	l1.Release()
	l2, err := st.Acquire()
	if err != nil {
		t.Fatalf("lock not released: %v", err)
	}
	l2.Release()
	l2.Release() // idempotent
}

func TestSaveFailsCleanlyWhenDirUnwritable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	base := t.TempDir()
	ro := filepath.Join(base, "ro")
	_ = os.Mkdir(ro, 0o500)
	st := &Store{Dir: filepath.Join(ro, "snaps")}
	if err := st.Save(sample(st)); err == nil {
		t.Error("expected error writing into read-only directory")
	}
}

func TestRestorePointFormatVersioning(t *testing.T) {
	st := &Store{Dir: t.TempDir()}
	s := st.New("x")
	if err := st.Save(s); err != nil {
		t.Fatal(err)
	}
	if got, err := st.Load(s.ID); err != nil || got.Format != FormatVersion {
		t.Fatalf("a saved restore point carries format %d: %+v %v", FormatVersion, got, err)
	}

	// A file written before the field existed (no "format" key, checksum over
	// the old layout) must still load.
	legacy := st.New("old")
	legacy.UpdatedAt = legacy.CreatedAt
	legacy.Checksum = checksum(*legacy) // Format 0 is omitted from the JSON, as in old files
	b, _ := json.MarshalIndent(legacy, "", "  ")
	if strings.Contains(string(b), `"format"`) {
		t.Fatal("a zero format must not be written")
	}
	if err := os.WriteFile(st.file(legacy.ID), b, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Load(legacy.ID); err != nil {
		t.Errorf("legacy restore point no longer loads: %v", err)
	}

	// A file from a future layout is refused with a clear reason, not misread.
	future := st.New("new")
	future.Format = FormatVersion + 1
	future.Checksum = checksum(*future)
	b, _ = json.MarshalIndent(future, "", "  ")
	os.WriteFile(st.file(future.ID), b, 0o600)
	if _, err := st.Load(future.ID); !errors.Is(err, ErrTooNew) {
		t.Errorf("want ErrTooNew, got %v", err)
	}
}
