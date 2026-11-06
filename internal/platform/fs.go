// Package platform holds the thin OS boundary TurboShift talks through:
// a filesystem (procfs/sysfs/state files) and a process runner. Everything
// above this package takes these interfaces so it can be tested against
// in-memory fakes instead of a real machine.
package platform

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// FS is the subset of filesystem access TurboShift needs. Paths are absolute
// (e.g. "/sys/devices/system/cpu/cpu0/cpufreq/scaling_governor").
type FS interface {
	ReadFile(path string) ([]byte, error)
	WriteFile(path string, data []byte, perm os.FileMode) error
	ReadDir(path string) ([]os.DirEntry, error)
	Stat(path string) (os.FileInfo, error)
	Readlink(path string) (string, error)
	// Statfs returns total and available bytes for the filesystem holding path.
	Statfs(path string) (total, avail uint64, err error)
}

// OSFS is the real filesystem, optionally re-rooted (Root "" or "/" means the
// real root). A re-rooted OSFS lets integration tests use a temp directory as
// a fake sysroot.
type OSFS struct{ Root string }

func (o OSFS) p(path string) string {
	if o.Root == "" || o.Root == "/" {
		return path
	}
	return filepath.Join(o.Root, path)
}

func (o OSFS) ReadFile(path string) ([]byte, error) { return os.ReadFile(o.p(path)) }

func (o OSFS) WriteFile(path string, data []byte, perm os.FileMode) error {
	return os.WriteFile(o.p(path), data, perm)
}

func (o OSFS) ReadDir(path string) ([]os.DirEntry, error) { return os.ReadDir(o.p(path)) }
func (o OSFS) Stat(path string) (os.FileInfo, error)      { return os.Stat(o.p(path)) }
func (o OSFS) Readlink(path string) (string, error)       { return os.Readlink(o.p(path)) }

func (o OSFS) Statfs(path string) (uint64, uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(o.p(path), &st); err != nil {
		return 0, 0, err
	}
	bs := uint64(st.Bsize)
	return st.Blocks * bs, st.Bavail * bs, nil
}

// ReadTrim reads a small text file and trims whitespace. ok is false when the
// file is missing or unreadable, which for sysfs usually just means
// "this capability does not exist on this machine".
func ReadTrim(fs FS, path string) (string, bool) {
	b, err := fs.ReadFile(path)
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(b)), true
}
