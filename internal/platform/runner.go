package platform

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Result is the outcome of a finished process.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// ErrNotInstalled is returned when the requested executable is not on PATH.
var ErrNotInstalled = errors.New("not installed")

// Runner executes external programs. Arguments are always passed as an
// array; nothing in TurboShift ever builds a shell string.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) (Result, error)
	// Has reports whether an executable is available on PATH.
	Has(name string) bool
}

// ExecRunner is the real Runner.
type ExecRunner struct {
	// Timeout bounds every command (default 10s) so a hung helper can never
	// freeze a diagnostic pass.
	Timeout time.Duration
}

func (r ExecRunner) Has(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func (r ExecRunner) Run(ctx context.Context, name string, args ...string) (Result, error) {
	if _, err := exec.LookPath(name); err != nil {
		return Result{ExitCode: -1}, fmt.Errorf("%s: %w", name, ErrNotInstalled)
	}
	to := r.Timeout
	if to == 0 {
		to = 10 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, to)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	// Stable, parseable output regardless of the user's locale.
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	err := cmd.Run()
	res := Result{Stdout: so.String(), Stderr: se.String()}
	var ee *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &ee):
		res.ExitCode = ee.ExitCode()
	default:
		res.ExitCode = -1
	}
	return res, err
}

// FakeRunner is a scriptable Runner for tests. Commands are matched on the
// exact "name arg1 arg2" string.
type FakeRunner struct {
	mu      sync.Mutex
	replies map[string]Reply
	Calls   []string
}

// Reply is a canned command result.
type Reply struct {
	Stdout, Stderr string
	Code           int
	Err            error
}

func NewFakeRunner() *FakeRunner { return &FakeRunner{replies: map[string]Reply{}} }

// On registers a reply; the tool is then considered installed.
func (f *FakeRunner) On(cmdline string, r Reply) *FakeRunner {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replies[cmdline] = r
	return f
}

// Install marks a tool as present without scripting any command.
func (f *FakeRunner) Install(name string) *FakeRunner { return f.On(name, Reply{}) }

func (f *FakeRunner) Has(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for k := range f.replies {
		if k == name || strings.HasPrefix(k, name+" ") {
			return true
		}
	}
	return false
}

func (f *FakeRunner) Run(_ context.Context, name string, args ...string) (Result, error) {
	line := strings.TrimSpace(name + " " + strings.Join(args, " "))
	f.mu.Lock()
	f.Calls = append(f.Calls, line)
	r, ok := f.replies[line]
	f.mu.Unlock()
	if !ok {
		if !f.Has(name) {
			return Result{ExitCode: -1}, fmt.Errorf("%s: %w", name, ErrNotInstalled)
		}
		return Result{ExitCode: 127, Stderr: "fake: unscripted command: " + line},
			fmt.Errorf("fake runner: unscripted command %q", line)
	}
	res := Result{Stdout: r.Stdout, Stderr: r.Stderr, ExitCode: r.Code}
	if r.Err != nil {
		return res, r.Err
	}
	if r.Code != 0 {
		return res, fmt.Errorf("exit status %d", r.Code)
	}
	return res, nil
}

// Called reports whether a command line was executed.
func (f *FakeRunner) Called(cmdline string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.Calls {
		if c == cmdline {
			return true
		}
	}
	return false
}
