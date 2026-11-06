package privilege

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/mttcn83/turboshift/internal/platform"
)

const gov = "/sys/devices/system/cpu/cpu3/cpufreq/scaling_governor"

func TestValidateAllowlist(t *testing.T) {
	ok := []Action{
		{OpWriteSysfs, gov, "performance"},
		{OpWriteSysfs, "/sys/devices/system/cpu/cpu0/cpufreq/scaling_governor", "powersave"},
		{OpWriteSysfs, gov, "schedutil"},
	}
	for _, a := range ok {
		if err := Validate(a); err != nil {
			t.Errorf("%+v: %v", a, err)
		}
	}
	bad := []Action{
		{"exec", gov, "performance"},
		{"", gov, "performance"},
		{OpWriteSysfs, "/etc/passwd", "x"},
		{OpWriteSysfs, "/sys/devices/system/cpu/cpu0/cpufreq/scaling_governor/../../../../../etc/shadow", "x"},
		{OpWriteSysfs, "/sys/devices/system/cpu/cpu0/cpufreq/scaling_max_freq", "1"},
		{OpWriteSysfs, "/sys/devices/system/cpu/cpu0/cpufreq/scaling_governor\n", "performance"},
		{OpWriteSysfs, "/sys/devices/system/cpu/cpuX/cpufreq/scaling_governor", "performance"},
		{OpWriteSysfs, "/sys/devices/system/cpu/cpu99999/cpufreq/scaling_governor", "performance"},
		{OpWriteSysfs, gov, ""},
		{OpWriteSysfs, gov, "Performance"},
		{OpWriteSysfs, gov, "a b"},
		{OpWriteSysfs, gov, "x\nperformance"},
		{OpWriteSysfs, gov, "$(reboot)"},
		{OpWriteSysfs, gov, strings.Repeat("a", 33)},
	}
	for _, a := range bad {
		if err := Validate(a); !errors.Is(err, ErrDenied) {
			t.Errorf("%q should be denied, got %v", a, err)
		}
	}
}

func TestDirectWritesAndVerifies(t *testing.T) {
	fs := platform.NewMemFS().Set(gov, "powersave\n")
	res, err := Direct{FS: fs}.Do(context.Background(), Action{OpWriteSysfs, gov, "performance"})
	if err != nil || res.Previous != "powersave" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if v, _ := platform.ReadTrim(fs, gov); v != "performance" {
		t.Errorf("value = %q", v)
	}
}

func TestDirectReportsWriteFailureAndKernelRejection(t *testing.T) {
	fs := platform.NewMemFS().Set(gov, "powersave\n")
	fs.WriteErr[gov] = errors.New("permission denied")
	if _, err := (Direct{FS: fs}).Do(context.Background(), Action{OpWriteSysfs, gov, "performance"}); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("err = %v", err)
	}
}

func TestDirectNeverTouchesNonAllowlistedPaths(t *testing.T) {
	fs := platform.NewMemFS().Set("/etc/hostname", "box\n")
	if _, err := (Direct{FS: fs}).Do(context.Background(), Action{OpWriteSysfs, "/etc/hostname", "evil"}); !errors.Is(err, ErrDenied) {
		t.Fatalf("err = %v", err)
	}
	if v, _ := platform.ReadTrim(fs, "/etc/hostname"); v != "box" {
		t.Error("a denied action modified the file")
	}
	if len(fs.Writes) != 0 {
		t.Errorf("writes: %v", fs.Writes)
	}
}

func TestPkexecArgvAndFailureMapping(t *testing.T) {
	run := platform.NewFakeRunner()
	run.Install("pkexec")
	run.On("pkexec /usr/bin/turbo __priv write-sysfs "+gov+" performance", platform.Reply{Stdout: "powersave\n"})
	p := Pkexec{Run: run, Self: "/usr/bin/turbo", Lstat: rootOwned(nil)}
	res, err := p.Do(context.Background(), Action{OpWriteSysfs, gov, "performance"})
	if err != nil || res.Previous != "powersave" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if !run.Called("pkexec /usr/bin/turbo __priv write-sysfs " + gov + " performance") {
		t.Errorf("unexpected argv: %v", run.Calls)
	}

	// Validation happens before any prompt is shown.
	run2 := platform.NewFakeRunner()
	run2.Install("pkexec")
	if _, err := (Pkexec{Run: run2, Self: "/x", Lstat: rootOwned(nil)}).Do(context.Background(), Action{OpWriteSysfs, "/etc/passwd", "x"}); !errors.Is(err, ErrDenied) || len(run2.Calls) != 0 {
		t.Errorf("err=%v calls=%v", err, run2.Calls)
	}

	// pkexec missing is explained, not attempted.
	if _, err := (Pkexec{Run: platform.NewFakeRunner(), Self: "/x", Lstat: rootOwned(nil)}).Do(context.Background(), Action{OpWriteSysfs, gov, "performance"}); err == nil || !strings.Contains(err.Error(), "pkexec") {
		t.Errorf("err = %v", err)
	}
}

func TestPkexecDismissedPromptMessage(t *testing.T) {
	run := platform.NewFakeRunner()
	run.Install("pkexec")
	run.On("pkexec /x __priv write-sysfs "+gov+" performance", platform.Reply{Code: 126, Err: errors.New("exit 126")})
	_, err := (Pkexec{Run: run, Self: "/x", Lstat: rootOwned(nil)}).Do(context.Background(), Action{OpWriteSysfs, gov, "performance"})
	if err == nil || !strings.Contains(err.Error(), "dismissed or denied") {
		t.Errorf("err = %v", err)
	}
}

func TestRunHelperRequiresRootAndValidatesAgain(t *testing.T) {
	fs := platform.NewMemFS().Set(gov, "powersave\n")
	if err := RunHelper(context.Background(), fs, 1000, []string{OpWriteSysfs, gov, "performance"}, func(string) {}); err == nil {
		t.Error("non-root must be refused")
	}
	if err := RunHelper(context.Background(), fs, 0, []string{OpWriteSysfs, "/etc/passwd", "x"}, func(string) {}); !errors.Is(err, ErrDenied) {
		t.Errorf("root helper must re-validate: %v", err)
	}
	if err := RunHelper(context.Background(), fs, 0, []string{"a", "b"}, func(string) {}); err == nil {
		t.Error("wrong arity must be refused")
	}
	var printed string
	if err := RunHelper(context.Background(), fs, 0, []string{OpWriteSysfs, gov, "performance"}, func(s string) { printed = s }); err != nil || printed != "powersave" {
		t.Errorf("err=%v printed=%q", err, printed)
	}
}

// ---- helper-binary safety ----

type fakeInfo struct {
	name string
	mode fs.FileMode
	st   *syscall.Stat_t
}

func (f fakeInfo) Name() string       { return f.name }
func (f fakeInfo) Size() int64        { return 0 }
func (f fakeInfo) Mode() fs.FileMode  { return f.mode }
func (f fakeInfo) ModTime() time.Time { return time.Time{} }
func (f fakeInfo) IsDir() bool        { return f.mode.IsDir() }
func (f fakeInfo) Sys() any           { return f.st }

// rootOwned fakes a tree where everything is root-owned 0755 except the
// overrides (path -> owner uid / mode).
func rootOwned(over map[string]fakeInfo) func(string) (os.FileInfo, error) {
	return func(p string) (os.FileInfo, error) {
		if o, ok := over[p]; ok {
			return o, nil
		}
		return fakeInfo{p, 0o755, &syscall.Stat_t{Uid: 0}}, nil
	}
}

func TestSafeHelperBinary(t *testing.T) {
	const bin = "/usr/local/bin/turbo"
	if err := SafeHelperBinary(bin, rootOwned(nil)); err != nil {
		t.Errorf("root-owned tree rejected: %v", err)
	}
	bad := map[string]map[string]fakeInfo{
		"binary owned by user":      {bin: {bin, 0o755, &syscall.Stat_t{Uid: 1000}}},
		"binary group-writable":     {bin: {bin, 0o775, &syscall.Stat_t{Uid: 0}}},
		"binary world-writable":     {bin: {bin, 0o757, &syscall.Stat_t{Uid: 0}}},
		"parent owned by user":      {"/usr/local": {"/usr/local", 0o755, &syscall.Stat_t{Uid: 1000}}},
		"parent world-writable":     {"/usr/local/bin": {"/usr/local/bin", 0o777, &syscall.Stat_t{Uid: 0}}},
		"root dir writable (/tmp?)": {"/usr": {"/usr", 0o1777, &syscall.Stat_t{Uid: 0}}},
		"is a symlink":              {bin: {bin, 0o755 | fs.ModeSymlink, &syscall.Stat_t{Uid: 0}}},
	}
	for name, over := range bad {
		if err := SafeHelperBinary(bin, rootOwned(over)); err == nil {
			t.Errorf("%s: should be refused", name)
		}
	}
	for _, p := range []string{"turbo", "/usr/../usr/local/bin/turbo", "/usr//local/bin/turbo", ""} {
		if err := SafeHelperBinary(p, rootOwned(nil)); err == nil {
			t.Errorf("%q: non-canonical path accepted", p)
		}
	}
}

func TestPkexecRefusesUserWritableBinary(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root")
	}
	run := platform.NewFakeRunner()
	run.Install("pkexec")
	self := filepath.Join(t.TempDir(), "turbo") // owned by the test user, so never acceptable
	_ = os.WriteFile(self, []byte("x"), 0o755)
	p := Pkexec{Run: run, Self: self}
	if ok, why := p.Available(); ok || !strings.Contains(why, "sudo make install") {
		t.Errorf("ok=%v why=%q", ok, why)
	}
	if _, err := p.Do(context.Background(), Action{OpWriteSysfs, gov, "performance"}); err == nil || len(run.Calls) != 0 {
		t.Errorf("a user-writable helper must never reach pkexec: err=%v calls=%v", err, run.Calls)
	}
}

func TestDirectNeverCreatesMissingAttributes(t *testing.T) {
	fs := platform.NewMemFS()
	if _, err := (Direct{FS: fs}).Do(context.Background(), Action{OpWriteSysfs, gov, "performance"}); err == nil {
		t.Fatal("writing a non-existent attribute must fail")
	}
	if len(fs.Writes) != 0 {
		t.Errorf("created %v", fs.Writes)
	}
}

// ---- batching: one polkit prompt for all CPUs ----

const gov7 = "/sys/devices/system/cpu/cpu7/cpufreq/scaling_governor"

func TestPkexecBatchIsOnePromptAndValidatesEverything(t *testing.T) {
	run := platform.NewFakeRunner()
	run.Install("pkexec")
	want := "pkexec /usr/bin/turbo __priv write-sysfs " + gov + " performance " + gov7 + " performance"
	run.On(want, platform.Reply{Stdout: "powersave\npowersave\n"})
	p := Pkexec{Run: run, Self: "/usr/bin/turbo", Lstat: rootOwned(nil)}
	as := []Action{{OpWriteSysfs, gov, "performance"}, {OpWriteSysfs, gov7, "performance"}}
	if err := p.DoBatch(context.Background(), as); err != nil {
		t.Fatal(err)
	}
	if len(run.Calls) != 1 || !run.Called(want) {
		t.Errorf("want exactly one pkexec call, got %v", run.Calls)
	}

	// One bad entry refuses the whole request before any prompt.
	run2 := platform.NewFakeRunner()
	run2.Install("pkexec")
	bad := append(as, Action{OpWriteSysfs, "/etc/shadow", "x"})
	if err := (Pkexec{Run: run2, Self: "/x", Lstat: rootOwned(nil)}).DoBatch(context.Background(), bad); !errors.Is(err, ErrDenied) || len(run2.Calls) != 0 {
		t.Errorf("err=%v calls=%v", err, run2.Calls)
	}
	// And the same protection holds for a user-writable binary (a root caller
	// has no pkexec step, so this part only applies to ordinary users).
	if os.Geteuid() != 0 {
		over := map[string]fakeInfo{"/usr/bin/turbo": {"turbo", 0o775, &syscall.Stat_t{Uid: 0}}}
		if err := (Pkexec{Run: run2, Self: "/usr/bin/turbo", Lstat: rootOwned(over)}).DoBatch(context.Background(), as); err == nil || len(run2.Calls) != 0 {
			t.Errorf("unsafe binary reached pkexec: %v %v", err, run2.Calls)
		}
	}
}

func TestRunHelperBatchWritesAllOrNothingOnValidation(t *testing.T) {
	fs := platform.NewMemFS().Set(gov, "powersave\n").Set(gov7, "powersave\n")
	var printed []string
	say := func(s string) { printed = append(printed, s) }
	if err := RunHelper(context.Background(), fs, 0, []string{OpWriteSysfs, gov, "performance", gov7, "performance"}, say); err != nil {
		t.Fatal(err)
	}
	if v, _ := platform.ReadTrim(fs, gov); v != "performance" {
		t.Error("first not written")
	}
	if v, _ := platform.ReadTrim(fs, gov7); v != "performance" {
		t.Error("second not written")
	}
	if len(printed) != 2 || printed[0] != "powersave" {
		t.Errorf("previous values: %v", printed)
	}

	// A bad pair anywhere means NOTHING is written.
	fs2 := platform.NewMemFS().Set(gov, "powersave\n")
	err := RunHelper(context.Background(), fs2, 0, []string{OpWriteSysfs, gov, "performance", "/etc/passwd", "x"}, say)
	if !errors.Is(err, ErrDenied) {
		t.Errorf("want ErrDenied, got %v", err)
	}
	if v, _ := platform.ReadTrim(fs2, gov); v != "powersave" {
		t.Errorf("a rejected batch must not write anything, governor is %q", v)
	}
	// Odd/even arity mistakes are refused.
	if err := RunHelper(context.Background(), fs2, 0, []string{OpWriteSysfs, gov}, say); err == nil {
		t.Error("missing value accepted")
	}
	if err := RunHelper(context.Background(), fs2, 0, []string{OpWriteSysfs, gov, "performance", gov7}, say); err == nil {
		t.Error("dangling path accepted")
	}
}

func TestDoAllContinuesAfterOneFailure(t *testing.T) {
	fs := platform.NewMemFS().Set(gov, "powersave\n") // gov7 does not exist
	err := DoAll(context.Background(), Direct{FS: fs}, []Action{{OpWriteSysfs, gov7, "performance"}, {OpWriteSysfs, gov, "performance"}})
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("want the missing-attribute error, got %v", err)
	}
	if v, _ := platform.ReadTrim(fs, gov); v != "performance" {
		t.Error("one failure must not stop the remaining writes (restores are best-effort)")
	}
}

// ---- the wider allowlist ----

func TestAllowlistCoversExactlyTheKnownKernelAttributes(t *testing.T) {
	cpu := "/sys/devices/system/cpu/"
	ok := []Action{
		{OpWriteSysfs, cpu + "cpu0/cpufreq/energy_performance_preference", "balance_performance"},
		{OpWriteSysfs, cpu + "intel_pstate/no_turbo", "0"},
		{OpWriteSysfs, cpu + "cpufreq/boost", "1"},
		{OpWriteSysfs, "/sys/firmware/acpi/platform_profile", "low-power"},
		{OpWriteSysfs, "/proc/sys/net/ipv4/tcp_congestion_control", "bbr"},
		{OpWriteSysfs, "/proc/sys/vm/swappiness", "0"},
		{OpWriteSysfs, "/proc/sys/vm/swappiness", "180"},
		{OpWriteSysfs, "/proc/sys/vm/swappiness", "200"},
		{OpWriteSysfs, "/sys/block/nvme0n1/queue/scheduler", "none"},
		{OpWriteSysfs, "/sys/block/sda/queue/scheduler", "mq-deadline"},
		{OpWriteSysfs, "/sys/block/vdb/queue/scheduler", "bfq"},
		{OpWriteSysfs, "/sys/block/mmcblk0/queue/scheduler", "kyber"},
	}
	for _, a := range ok {
		if err := Validate(a); err != nil {
			t.Errorf("%+v: %v", a, err)
		}
	}
	bad := []Action{
		{OpWriteSysfs, cpu + "intel_pstate/no_turbo", "2"},
		{OpWriteSysfs, cpu + "intel_pstate/no_turbo", "01"},
		{OpWriteSysfs, cpu + "intel_pstate/max_perf_pct", "100"},
		{OpWriteSysfs, "/proc/sys/vm/swappiness", "201"},
		{OpWriteSysfs, "/proc/sys/vm/swappiness", "-1"},
		{OpWriteSysfs, "/proc/sys/vm/swappiness", "1e2"},
		{OpWriteSysfs, "/proc/sys/vm/overcommit_memory", "1"},
		{OpWriteSysfs, "/proc/sys/kernel/core_pattern", "x"},
		{OpWriteSysfs, "/proc/sys/net/ipv4/ip_forward", "1"},
		{OpWriteSysfs, "/proc/sys/net/ipv4/tcp_congestion_control", "bbr cubic"},
		{OpWriteSysfs, "/sys/block/sda/queue/scheduler", "none [x]"},
		{OpWriteSysfs, "/sys/block/sda/queue/nr_requests", "1"},
		{OpWriteSysfs, "/sys/block/sda/../sdb/queue/scheduler", "none"},
		{OpWriteSysfs, "/sys/block/loop0/queue/scheduler", "none"},
		{OpWriteSysfs, "/sys/block/sda1/queue/scheduler", "none"},
		{OpWriteSysfs, "/sys/block/dm-0/queue/scheduler", "none"},
		{OpWriteSysfs, "/sys/firmware/acpi/platform_profile_choices", "x"},
		{OpWriteSysfs, "/sys/class/power_supply/BAT0/charge_control_end_threshold", "80"},
	}
	for _, a := range bad {
		if err := Validate(a); !errors.Is(err, ErrDenied) {
			t.Errorf("%+v should be denied, got %v", a, err)
		}
	}
}

func TestActiveChoiceAndSchedulerReadback(t *testing.T) {
	for in, want := range map[string]string{
		"none [mq-deadline] kyber bfq": "mq-deadline",
		"[none] mq-deadline":           "none",
		"mq-deadline\n":                "mq-deadline",
		"":                             "",
		"[]":                           "[]",
	} {
		if got := ActiveChoice(in); got != want {
			t.Errorf("ActiveChoice(%q) = %q, want %q", in, got, want)
		}
	}
	const sched = "/sys/block/nvme0n1/queue/scheduler"
	fs := platform.NewMemFS().Set(sched, "[none] mq-deadline kyber\n")
	res, err := Direct{FS: fs}.Do(context.Background(), Action{OpWriteSysfs, sched, "kyber"})
	if err != nil || res.Previous != "none" {
		t.Fatalf("res=%+v err=%v", res, err)
	}
}
