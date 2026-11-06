// Package snapshot persists transaction journals ("restore points").
//
// A snapshot records, for every tweak applied in one transaction, the exact
// state captured *before* the change and the state we expect *after*. It is
// written to disk before the OS is touched, updated after every step, and is
// therefore also the crash-recovery journal.
//
// Scope honesty: a snapshot captures only what TurboShift's own tweaks manage.
// It is not a full-system backup.
package snapshot

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"syscall"
	"time"
)

type Status string

const (
	StatusPending        Status = "pending"         // journal written, nothing applied yet
	StatusApplying       Status = "applying"        // in progress (or interrupted)
	StatusCommitted      Status = "committed"       // all changes applied and verified
	StatusRolledBack     Status = "rolled_back"     // failed; everything reverted
	StatusRollbackFailed Status = "rollback_failed" // failed; something could not be reverted
	StatusRestored       Status = "restored"        // user restored it
	StatusRestorePartial Status = "restore_partial" // user restore skipped/failed some items
)

type ItemStatus string

const (
	ItemPlanned  ItemStatus = "planned"
	ItemApplying ItemStatus = "applying"
	ItemVerified ItemStatus = "verified"
	ItemFailed   ItemStatus = "failed"
	ItemReverted ItemStatus = "reverted"
	ItemDrifted  ItemStatus = "drifted" // changed by someone else; left alone on restore
)

// Item is one tweak's part of a transaction.
type Item struct {
	TweakID string            `json:"tweak_id"`
	Name    string            `json:"name"`
	Params  map[string]string `json:"params,omitempty"`
	Before  map[string]string `json:"before"`
	After   map[string]string `json:"after"`
	Status  ItemStatus        `json:"status"`
	Error   string            `json:"error,omitempty"`
}

// Event is a human-readable history entry.
type Event struct {
	Time time.Time `json:"time"`
	Text string    `json:"text"`
}

// FormatVersion is the layout of a restore-point file. It only changes with a
// layout change older versions cannot read; files from before the field
// existed have no "format" key and are layout 1.
const FormatVersion = 1

type Snapshot struct {
	Format    int       `json:"format,omitempty"`
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Label     string    `json:"label"`
	Status    Status    `json:"status"`
	Kernel    string    `json:"kernel,omitempty"`
	Items     []Item    `json:"items"`
	Events    []Event   `json:"events,omitempty"`
	// Checksum is SHA-256 over the snapshot with this field empty.
	Checksum string `json:"checksum"`
}

// Log appends a history event.
func (s *Snapshot) Log(now time.Time, text string) {
	s.Events = append(s.Events, Event{Time: now, Text: text})
}

var (
	ErrNotFound = errors.New("snapshot not found")
	ErrCorrupt  = errors.New("snapshot failed its integrity check")
	ErrTooNew   = errors.New("snapshot was written by a newer TurboShift")
	ErrLocked   = errors.New("another TurboShift operation is already running")
	idRe        = regexp.MustCompile(`^[0-9A-Za-z_-]{1,64}$`)
)

// Store keeps snapshots as one JSON file each under Dir.
type Store struct {
	Dir string
	Now func() time.Time
}

func (st *Store) now() time.Time {
	if st.Now != nil {
		return st.Now()
	}
	return time.Now()
}

func (st *Store) file(id string) string { return filepath.Join(st.Dir, id+".json") }

// New creates (but does not persist) an empty snapshot with a fresh ID.
func (st *Store) New(label string) *Snapshot {
	var r [3]byte
	_, _ = rand.Read(r[:])
	t := st.now().UTC()
	return &Snapshot{
		ID:        t.Format("20060102T150405Z") + "-" + hex.EncodeToString(r[:]),
		CreatedAt: t, UpdatedAt: t, Label: label, Status: StatusPending,
	}
}

func checksum(s Snapshot) string {
	s.Checksum = ""
	b, _ := json.Marshal(s)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// Save atomically and durably writes the snapshot (temp file + fsync +
// rename + directory fsync), so a crash leaves either the old or the new
// version, never a torn file.
func (st *Store) Save(s *Snapshot) error {
	if !idRe.MatchString(s.ID) {
		return fmt.Errorf("invalid snapshot id %q", s.ID)
	}
	if err := os.MkdirAll(st.Dir, 0o700); err != nil {
		return fmt.Errorf("create snapshot directory: %w", err)
	}
	s.UpdatedAt = st.now().UTC()
	s.Format = FormatVersion
	s.Checksum = checksum(*s)
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(st.Dir, ".tmp-"+s.ID+"-*")
	if err != nil {
		return fmt.Errorf("write snapshot: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		cleanup()
		return fmt.Errorf("write snapshot: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		cleanup()
		return fmt.Errorf("sync snapshot: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmpName, st.file(s.ID)); err != nil {
		cleanup()
		return fmt.Errorf("commit snapshot: %w", err)
	}
	if d, err := os.Open(st.Dir); err == nil {
		_ = d.Sync()
		d.Close()
	}
	return nil
}

// Load reads and verifies one snapshot.
func (st *Store) Load(id string) (*Snapshot, error) {
	if !idRe.MatchString(id) {
		return nil, fmt.Errorf("%w: %q", ErrNotFound, id)
	}
	b, err := os.ReadFile(st.file(id))
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	if err != nil {
		return nil, err
	}
	var s Snapshot
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, fmt.Errorf("%w: %s (%v)", ErrCorrupt, id, err)
	}
	if s.ID != id || checksum(s) != s.Checksum {
		return nil, fmt.Errorf("%w: %s", ErrCorrupt, id)
	}
	if s.Format > FormatVersion {
		return nil, fmt.Errorf("%w: %s uses layout %d, this version reads up to %d (upgrade TurboShift to restore it)", ErrTooNew, id, s.Format, FormatVersion)
	}
	return &s, nil
}

// List returns all readable snapshots, newest first. Corrupt files are
// reported separately instead of hiding the rest.
func (st *Store) List() (snaps []*Snapshot, corrupt []string, err error) {
	ents, err := os.ReadDir(st.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	for _, e := range ents {
		n := e.Name()
		if e.IsDir() || filepath.Ext(n) != ".json" {
			continue
		}
		id := n[:len(n)-len(".json")]
		s, err := st.Load(id)
		if err != nil {
			corrupt = append(corrupt, id)
			continue
		}
		snaps = append(snaps, s)
	}
	sort.Slice(snaps, func(i, j int) bool { return snaps[i].CreatedAt.After(snaps[j].CreatedAt) })
	return snaps, corrupt, nil
}

// Interrupted returns snapshots that still need recovery: "applying" (a
// previous run was killed mid-transaction) and "rollback_failed" (an automatic
// rollback could not revert everything, so settings may still be changed).
func (st *Store) Interrupted() ([]*Snapshot, error) {
	all, _, err := st.List()
	var out []*Snapshot
	for _, s := range all {
		if s.Status == StatusApplying || s.Status == StatusRollbackFailed {
			out = append(out, s)
		}
	}
	return out, err
}

// Lock is an exclusive advisory lock (flock) held for the duration of a
// mutating operation. It is released automatically if the process dies.
type Lock struct{ f *os.File }

// Acquire takes the store's exclusive lock without blocking.
func (st *Store) Acquire() (*Lock, error) {
	if err := os.MkdirAll(st.Dir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(st.Dir, "lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrLocked
		}
		return nil, err
	}
	_ = f.Truncate(0)
	_, _ = f.WriteString(strconv.Itoa(os.Getpid()) + "\n")
	return &Lock{f: f}, nil
}

// Release drops the lock.
func (l *Lock) Release() {
	if l == nil || l.f == nil {
		return
	}
	_ = syscall.Flock(int(l.f.Fd()), syscall.LOCK_UN)
	l.f.Close()
	l.f = nil
}
