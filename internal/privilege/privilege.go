// Package privilege is the only place TurboShift crosses into root.
//
// The TUI and CLI always run as the ordinary user. When a tweak needs a root
// write it asks an Executor to perform one *allowlisted* action. There is no
// "run this command as root" primitive anywhere: each Action names an
// operation and a fully validated target, and the root-side helper
// (`turbo __priv`, started through pkexec) re-validates everything itself
// before touching the system.
package privilege

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"github.com/mttcn83/turboshift/internal/platform"
)

// OpWriteSysfs writes a short token to an allowlisted sysfs attribute.
const OpWriteSysfs = "write-sysfs"

// HelperCommand is the hidden subcommand the privileged helper runs.
const HelperCommand = "__priv"

type Action struct {
	Op    string
	Path  string
	Value string
}

// Result reports the value that was in place before the write, so callers can
// cross-check their journal.
type Result struct{ Previous string }

type Executor interface {
	Do(ctx context.Context, a Action) (Result, error)
	// Available reports whether privileged actions can be attempted at all
	// (already root, or pkexec present) and, if not, why.
	Available() (bool, string)
}

var (
	ErrDenied  = errors.New("privileged action refused by validation")
	cpufreqRe  = regexp.MustCompile(`^/sys/devices/system/cpu/cpu[0-9]{1,4}/cpufreq/scaling_governor$`)
	sysfsValRe = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)
)

// Validate is the allowlist. Anything not explicitly permitted is refused.
func Validate(a Action) error {
	switch a.Op {
	case OpWriteSysfs:
		if !cpufreqRe.MatchString(a.Path) {
			return fmt.Errorf("%w: %q is not an allowlisted sysfs attribute", ErrDenied, a.Path)
		}
		if !sysfsValRe.MatchString(a.Value) {
			return fmt.Errorf("%w: invalid value %q", ErrDenied, a.Value)
		}
		return nil
	}
	return fmt.Errorf("%w: unknown operation %q", ErrDenied, a.Op)
}

// Direct performs actions in-process. It is used inside the root helper and
// in tests; running it as an unprivileged user simply yields permission errors.
type Direct struct{ FS platform.FS }

func (d Direct) Available() (bool, string) { return true, "" }

func (d Direct) Do(_ context.Context, a Action) (Result, error) {
	if err := Validate(a); err != nil {
		return Result{}, err
	}
	// Attributes are never created: a missing file means the capability does not
	// exist here, and creating one in the wrong place would be a bug or an attack.
	if _, err := d.FS.Stat(a.Path); err != nil {
		return Result{}, fmt.Errorf("%s does not exist on this system", a.Path)
	}
	prev, _ := platform.ReadTrim(d.FS, a.Path)
	if err := d.FS.WriteFile(a.Path, []byte(a.Value+"\n"), 0o644); err != nil {
		return Result{Previous: prev}, fmt.Errorf("write %s: %w", a.Path, err)
	}
	if now, ok := platform.ReadTrim(d.FS, a.Path); ok && now != a.Value {
		return Result{Previous: prev}, fmt.Errorf("kernel rejected %q for %s (value stayed %q)", a.Value, a.Path, now)
	}
	return Result{Previous: prev}, nil
}

// Pkexec asks polkit to run `turbo __priv <op> <path> <value> [...]` as root.
// Arguments travel as an argv array; no shell is involved.
type Pkexec struct {
	Run  platform.Runner
	Self string // absolute path of the turbo binary
	// Lstat is os.Lstat unless a test replaces it.
	Lstat func(string) (os.FileInfo, error)
}

// SafeHelperBinary reports whether the user could swap the program that pkexec
// would run as root. pkexec asks for authorisation for a *path*; if that file
// or any directory above it is writable by anyone but root, a same-user
// process could replace it between the prompt and the exec and gain root. So
// the helper is only ever started from a root-owned, non-writable location
// (for example /usr/local/bin after `sudo make install`).
func SafeHelperBinary(path string, lstat func(string) (os.FileInfo, error)) error {
	if lstat == nil {
		lstat = os.Lstat
	}
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return fmt.Errorf("%q is not a clean absolute path", path)
	}
	for p := path; ; p = filepath.Dir(p) {
		fi, err := lstat(p)
		if err != nil {
			return err
		}
		st, ok := fi.Sys().(*syscall.Stat_t)
		if !ok {
			return fmt.Errorf("cannot determine the owner of %s", p)
		}
		if st.Uid != 0 || fi.Mode().Perm()&0o022 != 0 || fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("%s is not root-owned and protected from other users", p)
		}
		if p == "/" {
			return nil
		}
	}
}

func (p Pkexec) Available() (bool, string) {
	if os.Geteuid() == 0 {
		return true, ""
	}
	if !p.Run.Has("pkexec") {
		return false, "pkexec (polkit) is not installed, so TurboShift cannot ask for administrator permission"
	}
	if err := SafeHelperBinary(p.Self, p.Lstat); err != nil {
		return false, "administrator actions are disabled because turbo is running from a location you can modify (" + err.Error() + "). Install it system-wide first: sudo make install"
	}
	return true, ""
}

func (p Pkexec) Do(ctx context.Context, a Action) (Result, error) {
	if err := Validate(a); err != nil { // fail fast before prompting the user
		return Result{}, err
	}
	if ok, why := p.Available(); !ok {
		return Result{}, errors.New(why)
	}
	out, err := p.helper(ctx, a.Op, a.Path, a.Value)
	if err != nil {
		return Result{}, err
	}
	return Result{Previous: out}, nil
}

// helper runs `pkexec turbo __priv args...` and maps polkit's failures to
// messages a person can act on. It returns the helper's trimmed stdout.
func (p Pkexec) helper(ctx context.Context, args ...string) (string, error) {
	res, err := p.Run.Run(ctx, "pkexec", append([]string{p.Self, HelperCommand}, args...)...)
	if err != nil {
		switch res.ExitCode {
		case 126:
			return "", errors.New("administrator authorisation was dismissed or denied")
		case 127:
			return "", errors.New("polkit could not start the privileged helper (is a graphical or text polkit agent running?)")
		}
		msg := strings.TrimSpace(res.Stderr)
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("privileged helper failed: %s", msg)
	}
	return strings.TrimSpace(res.Stdout), nil
}

// maxBatch bounds one helper run (no machine has this many CPUs).
const maxBatch = 4096

// Batcher is implemented by executors that can perform several allowlisted
// actions of one operation under a single authorisation.
type Batcher interface {
	DoBatch(ctx context.Context, as []Action) error
}

// DoAll performs the actions with as few authorisation prompts as the
// executor allows. Every action is validated before any is performed, and a
// failure on one does not stop the others (a restore must be best-effort).
func DoAll(ctx context.Context, ex Executor, as []Action) error {
	if len(as) == 0 {
		return nil
	}
	if b, ok := ex.(Batcher); ok {
		return b.DoBatch(ctx, as)
	}
	if err := validateAll(as); err != nil {
		return err
	}
	var errs []error
	for _, a := range as {
		if _, err := ex.Do(ctx, a); err != nil {
			errs = append(errs, err)
			if ctx.Err() != nil {
				break
			}
		}
	}
	return errors.Join(errs...)
}

func validateAll(as []Action) error {
	if len(as) > maxBatch {
		return fmt.Errorf("%w: too many actions in one request (%d)", ErrDenied, len(as))
	}
	for _, a := range as {
		if err := Validate(a); err != nil {
			return err
		}
	}
	return nil
}

// DoBatch is one polkit prompt and one root helper run for all actions.
func (p Pkexec) DoBatch(ctx context.Context, as []Action) error {
	if err := validateAll(as); err != nil { // fail fast before prompting
		return err
	}
	if ok, why := p.Available(); !ok {
		return errors.New(why)
	}
	args := []string{as[0].Op}
	for _, a := range as {
		if a.Op != as[0].Op {
			return fmt.Errorf("%w: mixed operations in one request", ErrDenied)
		}
		args = append(args, a.Path, a.Value)
	}
	_, err := p.helper(ctx, args...)
	return err
}

// DoBatch performs the actions in-process (root helper and tests).
func (d Direct) DoBatch(ctx context.Context, as []Action) error {
	if err := validateAll(as); err != nil {
		return err
	}
	var errs []error
	for _, a := range as {
		if _, err := d.Do(ctx, a); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// RunHelper is the body of `turbo __priv`: it runs as root, re-validates the
// request and prints the previous value on success.
func RunHelper(ctx context.Context, fs platform.FS, euid int, args []string, stdout func(string)) error {
	if euid != 0 {
		return errors.New("the privileged helper must run as root (it is started through pkexec)")
	}
	if len(args) < 3 || len(args)%2 != 1 {
		return fmt.Errorf("usage: turbo %s <op> <path> <value> [<path> <value> ...]", HelperCommand)
	}
	var as []Action
	for i := 1; i < len(args); i += 2 {
		as = append(as, Action{Op: args[0], Path: args[i], Value: args[i+1]})
	}
	// Nothing is written unless every pair passes the allowlist.
	if err := validateAll(as); err != nil {
		return err
	}
	var errs []error
	for _, a := range as {
		res, err := Direct{FS: fs}.Do(ctx, a)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		stdout(res.Previous)
	}
	return errors.Join(errs...)
}

// Fake is a test Executor that records actions and writes through an FS.
type Fake struct {
	FS      platform.FS
	Actions []Action
	Err     error // returned for every Do when set
	NoAuth  string
	Batches int // number of DoBatch calls (each is one authorisation prompt)
}

func (f *Fake) Available() (bool, string) {
	if f.NoAuth != "" {
		return false, f.NoAuth
	}
	return true, ""
}

// DoBatch counts as one authorisation: Batches records how many there were.
func (f *Fake) DoBatch(ctx context.Context, as []Action) error {
	if err := validateAll(as); err != nil {
		return err
	}
	f.Batches++
	var errs []error
	for _, a := range as {
		if _, err := f.Do(ctx, a); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (f *Fake) Do(ctx context.Context, a Action) (Result, error) {
	if err := Validate(a); err != nil {
		return Result{}, err
	}
	f.Actions = append(f.Actions, a)
	if f.Err != nil {
		return Result{}, f.Err
	}
	return Direct{FS: f.FS}.Do(ctx, a)
}
