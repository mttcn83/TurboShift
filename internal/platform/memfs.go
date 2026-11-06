package platform

import (
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
	"sync"
	"time"
)

// MemFS is an in-memory FS for tests. It models files, symlinks and optional
// per-path write errors (to simulate EACCES / read-only filesystems).
type MemFS struct {
	mu       sync.Mutex
	files    map[string][]byte
	links    map[string]string
	statfs   map[string][2]uint64
	WriteErr map[string]error // path -> error returned by WriteFile
	Writes   []string         // log of successful WriteFile paths, in order
}

func NewMemFS() *MemFS {
	return &MemFS{
		files: map[string][]byte{}, links: map[string]string{},
		statfs: map[string][2]uint64{}, WriteErr: map[string]error{},
	}
}

// Set creates or replaces a file (test setup; not recorded in Writes).
func (m *MemFS) Set(p, content string) *MemFS {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[path.Clean(p)] = []byte(content)
	return m
}

func (m *MemFS) Remove(p string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.files, path.Clean(p))
	delete(m.links, path.Clean(p))
}

func (m *MemFS) Symlink(target, p string) *MemFS {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.links[path.Clean(p)] = target
	return m
}

func (m *MemFS) SetStatfs(p string, total, avail uint64) *MemFS {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.statfs[path.Clean(p)] = [2]uint64{total, avail}
	return m
}

// Get returns a file's current content for assertions.
func (m *MemFS) Get(p string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.files[path.Clean(p)]
	return string(b), ok
}

func (m *MemFS) ReadFile(p string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p = path.Clean(p)
	if b, ok := m.files[p]; ok {
		return append([]byte(nil), b...), nil
	}
	return nil, &fs.PathError{Op: "open", Path: p, Err: fs.ErrNotExist}
}

func (m *MemFS) WriteFile(p string, data []byte, _ os.FileMode) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	p = path.Clean(p)
	if err, ok := m.WriteErr[p]; ok {
		return &fs.PathError{Op: "write", Path: p, Err: err}
	}
	m.files[p] = append([]byte(nil), data...)
	m.Writes = append(m.Writes, p)
	return nil
}

func (m *MemFS) Readlink(p string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.links[path.Clean(p)]; ok {
		return t, nil
	}
	return "", &fs.PathError{Op: "readlink", Path: p, Err: fs.ErrNotExist}
}

func (m *MemFS) Statfs(p string) (uint64, uint64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if v, ok := m.statfs[path.Clean(p)]; ok {
		return v[0], v[1], nil
	}
	return 0, 0, &fs.PathError{Op: "statfs", Path: p, Err: fs.ErrNotExist}
}

func (m *MemFS) exists(p string) (isDir, ok bool) {
	if _, f := m.files[p]; f {
		return false, true
	}
	if _, l := m.links[p]; l {
		return false, true
	}
	prefix := p + "/"
	if p == "/" {
		prefix = "/"
	}
	for k := range m.files {
		if strings.HasPrefix(k, prefix) {
			return true, true
		}
	}
	for k := range m.links {
		if strings.HasPrefix(k, prefix) {
			return true, true
		}
	}
	return false, false
}

func (m *MemFS) Stat(p string) (os.FileInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p = path.Clean(p)
	isDir, ok := m.exists(p)
	if !ok {
		return nil, &fs.PathError{Op: "stat", Path: p, Err: fs.ErrNotExist}
	}
	return memInfo{name: path.Base(p), dir: isDir, size: int64(len(m.files[p]))}, nil
}

func (m *MemFS) ReadDir(p string) ([]os.DirEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p = path.Clean(p)
	if isDir, ok := m.exists(p); !ok || !isDir {
		return nil, &fs.PathError{Op: "readdir", Path: p, Err: fs.ErrNotExist}
	}
	prefix := p + "/"
	if p == "/" {
		prefix = "/"
	}
	seen := map[string]bool{}
	var out []os.DirEntry
	add := func(full string, link bool) {
		if !strings.HasPrefix(full, prefix) {
			return
		}
		rest := full[len(prefix):]
		name, _, nested := strings.Cut(rest, "/")
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, memEntry{memInfo{name: name, dir: nested, link: link && !nested}})
	}
	for k := range m.files {
		add(k, false)
	}
	for k := range m.links {
		add(k, true)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out, nil
}

type memInfo struct {
	name string
	dir  bool
	link bool
	size int64
}

func (i memInfo) Name() string { return i.name }
func (i memInfo) Size() int64  { return i.size }
func (i memInfo) Mode() os.FileMode {
	switch {
	case i.dir:
		return os.ModeDir | 0o755
	case i.link:
		return os.ModeSymlink | 0o777
	}
	return 0o644
}
func (i memInfo) ModTime() time.Time { return time.Time{} }
func (i memInfo) IsDir() bool        { return i.dir }
func (i memInfo) Sys() any           { return nil }

type memEntry struct{ i memInfo }

func (e memEntry) Name() string               { return e.i.name }
func (e memEntry) IsDir() bool                { return e.i.dir }
func (e memEntry) Type() os.FileMode          { return e.i.Mode().Type() }
func (e memEntry) Info() (os.FileInfo, error) { return e.i, nil }
