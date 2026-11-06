// Package tweaks defines the typed optimisation contract and its registry.
//
// A tweak is a small, independently testable unit that can answer: is it
// supported here, what is the current state, what exactly would change, how to
// apply it, how to verify the *effective* result, and how to undo it.
package tweaks

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/mttcn83/turboshift/internal/detect"
	"github.com/mttcn83/turboshift/internal/platform"
	"github.com/mttcn83/turboshift/internal/privilege"
)

type Risk int

const (
	RiskLow Risk = iota + 1
	RiskMedium
	RiskHigh
)

func (r Risk) String() string {
	switch r {
	case RiskLow:
		return "low"
	case RiskMedium:
		return "medium"
	case RiskHigh:
		return "high"
	}
	return "unknown"
}

// Tier is how strongly a tweak is endorsed.
type Tier string

const (
	// TierRecommended: low risk and clearly beneficial when its precondition
	// holds (e.g. a power-saver profile left on while plugged in).
	TierRecommended Tier = "recommended"
	// TierSituational: helps only for specific workloads or hardware and has a
	// real cost elsewhere (battery, heat). Never selected automatically.
	TierSituational Tier = "situational"
	// TierAdvanced: experimental or expert-only.
	TierAdvanced Tier = "advanced"
)

type Category string

const (
	CatCPU     Category = "CPU & Power"
	CatNetwork Category = "Network"
	CatGaming  Category = "Gaming"
	CatMemory  Category = "Memory"
	CatStorage Category = "Storage"
	CatInput   Category = "Input & Latency"
	CatDesktop Category = "Desktop & Graphics"
)

// Scope describes how long a change lasts.
type Scope string

const (
	ScopePersistent Scope = "persistent"   // survives reboot (kept by another service/config)
	ScopeUntilBoot  Scope = "until reboot" // kernel runtime state
)

// State is a flat key/value description of the settings a tweak manages.
// Before/After/live reads all use the same key set, which makes verification
// and drift detection a plain map comparison.
type State map[string]string

func (s State) Clone() State {
	c := make(State, len(s))
	for k, v := range s {
		c[k] = v
	}
	return c
}

// Equal reports whether every key in want has the same value in s.
func (s State) Covers(want State) bool {
	for k, v := range want {
		if s[k] != v {
			return false
		}
	}
	return true
}

type Option struct {
	Value, Label, Note string
}

type Meta struct {
	ID       string
	Name     string
	Summary  string // one plain-language sentence
	Details  string // what exactly it changes and why it can help
	Helps    string // who benefits and in which situation
	Tradeoff string // the real cost
	Evidence string // how to measure whether it helped
	Category Category
	Tier     Tier
	Risk     Risk
	Scope    Scope
	// NeedsAuth is true when applying requires administrator permission.
	NeedsAuth bool
	// Disruptive is true when the change may briefly interrupt something (e.g. Wi-Fi).
	Disruptive bool
	Reboot     bool
	// Conflicts lists tweak IDs that must not be applied in the same transaction.
	Conflicts []string
	// ParamName/Options describe the single user-selectable parameter.
	ParamName string
	Options   []Option
}

// Recommendation is advice produced from live state, with the reason shown to the user.
type Recommendation struct {
	Params map[string]string
	Why    string
}

// Assessment is a tweak's answer to "does this apply to this machine, now?".
type Assessment struct {
	Available bool
	Reason    string // why unavailable, or a caveat when available
	Current   State
	// Options may narrow Meta.Options to what this machine supports.
	Options []Option
	// Recommend is non-nil when the live state suggests the tweak would help.
	Recommend *Recommendation
}

type Change struct{ What, From, To string }

// Plan is the exact, previewable change. Before is captured from the live
// system when planning and is what Restore puts back.
type Plan struct {
	TweakID    string
	Params     map[string]string
	Before     State
	After      State
	Changes    []Change
	Warnings   []string
	NeedsAuth  bool
	Disruptive bool
}

var (
	ErrNoChange     = errors.New("already in the requested state; nothing to change")
	ErrInvalidParam = errors.New("invalid parameter")
	ErrUnavailable  = errors.New("not available on this system")
)

// Env bundles the OS boundary and the one-shot detection report.
type Env struct {
	FS     platform.FS
	Run    platform.Runner
	Priv   privilege.Executor
	Report detect.Report
}

// Tweak is the optimisation contract.
type Tweak interface {
	Meta() Meta
	// Assess checks support and conflicts, reads current state and may recommend.
	Assess(ctx context.Context, env Env) Assessment
	// Plan validates params and builds the change preview. It must not modify the system.
	Plan(ctx context.Context, env Env, params map[string]string) (Plan, error)
	// Apply makes plan.After true.
	Apply(ctx context.Context, env Env, p Plan) error
	// Read returns the current live state for the keys in want.
	Read(ctx context.Context, env Env, want State) (State, error)
	// Restore puts plan.Before back. It must be idempotent (safe after a partial Apply).
	Restore(ctx context.Context, env Env, p Plan) error
}

// Guard protects a disruptive Apply with an automatic safety net
// (NetworkManager checkpoint). Tweaks opt in by implementing Guarded.
type Guard interface {
	Commit(ctx context.Context) error
	Abort(ctx context.Context) error
}

type Guarded interface {
	BeginGuard(ctx context.Context, env Env, p Plan) (Guard, error)
}

// Summarizer is an optional human-readable one-liner for a state, for tweaks
// whose state is several settings that read badly when simply joined.
type Summarizer interface {
	Summary(State) string
}

// Checker is an optional extra check of the *effective* result, for tweaks
// whose stored setting can differ from what the hardware is really doing.
type Checker interface {
	Check(ctx context.Context, env Env, p Plan) error
}

// Verify checks that the live system reflects plan.After, then runs the
// tweak's own effective-result check if it has one.
func Verify(ctx context.Context, t Tweak, env Env, p Plan) error {
	if err := VerifyState(ctx, t, env, p.After); err != nil {
		return err
	}
	if c, ok := t.(Checker); ok {
		return c.Check(ctx, env, p)
	}
	return nil
}

// VerifyState compares live state against want only.
func VerifyState(ctx context.Context, t Tweak, env Env, want State) error {
	p := Plan{After: want}
	got, err := t.Read(ctx, env, p.After)
	if err != nil {
		return fmt.Errorf("could not read back %s: %w", t.Meta().Name, err)
	}
	var bad []string
	for k, want := range p.After {
		if got[k] != want {
			bad = append(bad, fmt.Sprintf("%s is %q, expected %q", k, got[k], want))
		}
	}
	if len(bad) > 0 {
		sort.Strings(bad)
		return fmt.Errorf("change did not take effect: %s", strings.Join(bad, "; "))
	}
	return nil
}

// ---- registry -----------------------------------------------------------

type Registry struct {
	byID  map[string]Tweak
	order []string
}

func NewRegistry() *Registry { return &Registry{byID: map[string]Tweak{}} }

// Register adds a tweak; duplicate IDs are a programming error.
func (r *Registry) Register(t Tweak) {
	id := t.Meta().ID
	if _, dup := r.byID[id]; dup {
		panic("tweaks: duplicate ID " + id)
	}
	r.byID[id] = t
	r.order = append(r.order, id)
}

func (r *Registry) Get(id string) (Tweak, bool) { t, ok := r.byID[id]; return t, ok }

// All returns tweaks in registration order.
func (r *Registry) All() []Tweak {
	out := make([]Tweak, len(r.order))
	for i, id := range r.order {
		out[i] = r.byID[id]
	}
	return out
}

// Default is the built-in catalogue. Every entry is fully implemented; unsupported
// ideas are documented in docs/ROADMAP.md instead of appearing as dead controls.
func Default() *Registry {
	r := NewRegistry()
	// CPU & power
	r.Register(PowerProfile{})
	r.Register(CPUPolicy{})
	r.Register(CPUEnergyPreference())
	r.Register(CPUBoost())
	r.Register(CPUMinPerf())
	r.Register(CPUHWPBoost())
	r.Register(PlatformProfile())
	r.Register(SchedAutogroup())
	r.Register(NMIWatchdog())
	r.Register(PCIeASPM())
	r.Register(USBAutosuspend())
	// Input & latency
	r.Register(MousePoll())
	r.Register(KeyboardPoll())
	r.Register(GamepadPoll())
	// Network
	r.Register(WifiPowerSave{})
	r.Register(DNSResolver{})
	r.Register(TCPCongestion())
	r.Register(DefaultQdisc())
	r.Register(TCPFastOpen())
	r.Register(TCPSlowStartAfterIdle())
	r.Register(TCPNotsentLowat())
	r.Register(TCPBuffers())
	r.Register(NetdevBudget())
	r.Register(NetdevBacklog())
	r.Register(TCPMTUProbing())
	r.Register(TCPECN())
	// Memory
	r.Register(Swappiness())
	r.Register(Zswap())
	r.Register(ZswapCompressor())
	r.Register(PageCluster())
	r.Register(VFSCachePressure())
	r.Register(CompactionProactiveness())
	r.Register(TransparentHugepages())
	r.Register(THPDefrag())
	r.Register(MinFree())
	r.Register(WatermarkScale())
	r.Register(KSM())
	r.Register(StatInterval())
	// Storage
	r.Register(IOScheduler())
	r.Register(ReadAhead())
	r.Register(RQAffinity())
	r.Register(NoMerges())
	r.Register(AddRandom())
	r.Register(DirtyWriteback())
	r.Register(WritebackTiming())
	// Gaming & graphics
	r.Register(MaxMapCount())
	r.Register(SplitLockMitigate())
	r.Register(AMDGPUPerformanceLevel())
	r.Register(IntelGPUMinClock())
	r.Register(DGPUAwake())
	// Desktop
	r.Register(HyprEffects())
	r.Register(HyprTearing())
	r.Register(HyprDirectScanout())
	r.Register(GnomeAnimations())
	return r
}

// Matches reports whether a tweak matches a free-text search query.
func Matches(m Meta, q string) bool {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return true
	}
	hay := strings.ToLower(m.ID + " " + m.Name + " " + m.Summary + " " + string(m.Category) + " " + string(m.Tier))
	for _, w := range strings.Fields(q) {
		if !strings.Contains(hay, w) {
			return false
		}
	}
	return true
}

// CheckParam validates v against the allowed option values.
func CheckParam(m Meta, opts []Option, v string) error {
	if len(opts) == 0 {
		opts = m.Options
	}
	for _, o := range opts {
		if o.Value == v {
			return nil
		}
	}
	vals := make([]string, len(opts))
	for i, o := range opts {
		vals[i] = o.Value
	}
	return fmt.Errorf("%w: %s=%q (allowed: %s)", ErrInvalidParam, m.ParamName, v, strings.Join(vals, ", "))
}

// activeManagers lists active power-policy managers other than `except`.
func activeManagers(rep detect.Report, except string) []string {
	var out []string
	for _, m := range rep.Power.Managers {
		if m.Active && m.Name != except {
			out = append(out, m.Name)
		}
	}
	return out
}

// tokenRe matches the short lowercase words that settings use (power profile
// names, NetworkManager enum values). Values read back from a restore point
// are checked against it before being passed to any program, so a damaged or
// edited journal cannot smuggle an option such as "--help" into a command.
var tokenRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)

func safeToken(what, v string) error {
	if !tokenRe.MatchString(v) {
		return fmt.Errorf("%w: %s %q is not a valid value", ErrInvalidParam, what, v)
	}
	return nil
}
