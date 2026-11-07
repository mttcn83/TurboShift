package tweaks

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/mttcn83/turboshift/internal/detect"
)

// WifiPowerSave controls the power-save setting of the *active* Wi-Fi
// connection in NetworkManager. The change is a per-connection property in
// NetworkManager's own profile, so it needs no root, is persistent, and is
// restored by writing the previous value back.
type WifiPowerSave struct{}

// PollInterval is how often Check re-reads the radio state; tests shorten it.
var PollInterval = 600 * time.Millisecond

// nm powersave enum names as accepted/printed by nmcli.
const (
	nmDefault = "default"
	nmIgnore  = "ignore"
	nmDisable = "disable"
	nmEnable  = "enable"
)

var wifiOptions = []Option{
	{"off", "Off", "Radio stays awake. Can reduce small, variable delays on a good signal; costs battery."},
	{"on", "On", "Radio dozes between packets to save battery."},
	{"default", "NetworkManager default", "Let NetworkManager's global configuration decide."},
}

var paramToNM = map[string]string{"off": nmDisable, "on": nmEnable, "default": nmDefault}

func (WifiPowerSave) Meta() Meta { return plained(WifiPowerSave{}.rawMeta()) }

func (WifiPowerSave) rawMeta() Meta {
	return Meta{
		ID:       "net.wifi-powersave",
		Name:     "Steadier Wi-Fi (turn off radio power saving)",
		Summary:  "Stop the Wi-Fi radio from napping between packets, which can add small, random delays.",
		Details:  "Sets the power-save option of your active NetworkManager Wi-Fi connection and re-applies it to the adapter without a full reconnect. With power saving on, the radio sleeps between packets and wakes on the access point's beacon.",
		Helps:    "Laptops on a good Wi-Fi signal that see jitter or lag spikes in calls and games while plugged in.",
		Tradeoff: "Uses more battery on laptops. It is not a fix for a weak signal, a congested channel or router/ISP problems — if Network Doctor shows those, power saving is not your bottleneck.",
		Evidence: "Run Network Doctor before and after: compare gateway jitter and p95 latency over a few runs. If they do not improve, switch it back.",
		Category: CatNetwork, Tier: TierSituational, Risk: RiskLow, Scope: ScopePersistent,
		Disruptive: true, ParamName: "powersave", Options: wifiOptions,
	}
}

type wifiTarget struct{ UUID, Iface string }

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F-]{36}$`)
var ifaceRe = regexp.MustCompile(`^[A-Za-z0-9_.@:-]{1,15}$`)

func (WifiPowerSave) target(ctx context.Context, env Env) (wifiTarget, error) {
	res, err := env.Run.Run(ctx, "nmcli", "-t", "-f", "UUID,TYPE,DEVICE", "connection", "show", "--active")
	if err != nil {
		return wifiTarget{}, fmt.Errorf("nmcli: %w", err)
	}
	for _, line := range strings.Split(res.Stdout, "\n") {
		f := strings.Split(strings.TrimSpace(line), ":")
		if len(f) == 3 && f[1] == "802-11-wireless" && uuidRe.MatchString(f[0]) && ifaceRe.MatchString(f[2]) {
			return wifiTarget{UUID: f[0], Iface: f[2]}, nil
		}
	}
	return wifiTarget{}, errors.New("no active Wi-Fi connection")
}

func (w WifiPowerSave) Assess(ctx context.Context, env Env) Assessment {
	switch {
	case !env.Run.Has("nmcli"):
		return Assessment{Reason: "NetworkManager's nmcli is not installed."}
	case !env.Report.Network.NetworkManager:
		return Assessment{Reason: "NetworkManager is not running, so it is not managing your Wi-Fi."}
	case !env.Run.Has("iw"):
		return Assessment{Reason: "The 'iw' tool is needed to confirm the change really reached the Wi-Fi adapter."}
	}
	if _, err := w.target(ctx, env); err != nil {
		return Assessment{Reason: "You are not connected to a Wi-Fi network."}
	}
	cur, err := w.Read(ctx, env, State{"nm_powersave": ""})
	if err != nil {
		return Assessment{Reason: "Could not read the connection's settings: " + err.Error()}
	}
	a := Assessment{Available: true, Current: cur, Options: plainOpts("net.wifi-powersave", wifiOptions)}
	if on := env.Report.Power.OnAC; on != nil && *on && env.Report.Network.WifiPowerSave == "on" && cur["nm_powersave"] != nmDisable {
		a.Recommend = &Recommendation{
			Params: map[string]string{"powersave": "off"},
			Why:    "Wi-Fi power saving is on while you are plugged in. If you see jitter or lag spikes on a good signal, turning it off removes one possible cause (it is not a guaranteed fix).",
		}
	}
	return a
}

func (w WifiPowerSave) Read(ctx context.Context, env Env, _ State) (State, error) {
	t, err := w.target(ctx, env)
	if err != nil {
		return nil, err
	}
	res, err := env.Run.Run(ctx, "nmcli", "-g", "802-11-wireless.powersave", "connection", "show", t.UUID)
	if err != nil {
		return nil, err
	}
	return State{"nm_powersave": normalizeNMPowersave(res.Stdout)}, nil
}

// normalizeNMPowersave accepts "default", "2 (disable)", "2" etc.
func normalizeNMPowersave(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, n := range []string{nmDefault, nmIgnore, nmDisable, nmEnable} {
		if strings.Contains(s, n) {
			return n
		}
	}
	switch strings.Fields(s + " ")[0] {
	case "0":
		return nmDefault
	case "1":
		return nmIgnore
	case "2":
		return nmDisable
	case "3":
		return nmEnable
	}
	return s
}

func (w WifiPowerSave) Plan(ctx context.Context, env Env, params map[string]string) (Plan, error) {
	m := w.Meta()
	v := params["powersave"]
	if err := CheckParam(m, nil, v); err != nil {
		return Plan{}, err
	}
	a := w.Assess(ctx, env)
	if !a.Available {
		return Plan{}, fmt.Errorf("%w: %s", ErrUnavailable, a.Reason)
	}
	want := paramToNM[v]
	if a.Current["nm_powersave"] == want {
		return Plan{}, ErrNoChange
	}
	t, _ := w.target(ctx, env)
	pl := Plan{
		TweakID: m.ID, Params: map[string]string{"powersave": v},
		Before: a.Current.Clone(), After: State{"nm_powersave": want},
		Changes:    []Change{{"Wi-Fi power saving (" + t.Iface + ")", a.Current["nm_powersave"], want}},
		Disruptive: true,
	}
	if v == "off" {
		pl.Warnings = append(pl.Warnings, "Uses more battery on laptops.")
	}
	pl.Warnings = append(pl.Warnings, "The change is re-applied to the adapter live; a very brief hiccup is possible. A NetworkManager checkpoint will undo it automatically if it goes wrong.")
	return pl, nil
}

// set writes the NetworkManager property and re-applies it to the adapter.
// strict=false is used for restores: once the stored profile is back to its
// original value a failed live re-apply is not an error (the adapter either
// never changed, or follows the profile at the next reconnect).
func (w WifiPowerSave) set(ctx context.Context, env Env, val string, strict bool) error {
	if err := safeToken("Wi-Fi power-save mode", val); err != nil {
		return err
	}
	t, err := w.target(ctx, env)
	if err != nil {
		return err
	}
	if _, err := env.Run.Run(ctx, "nmcli", "connection", "modify", t.UUID, "802-11-wireless.powersave", val); err != nil {
		return fmt.Errorf("nmcli connection modify: %w", err)
	}
	if _, err := env.Run.Run(ctx, "nmcli", "device", "reapply", t.Iface); err != nil && strict {
		return fmt.Errorf("nmcli device reapply: %w", err)
	}
	return nil
}

func (w WifiPowerSave) Apply(ctx context.Context, env Env, p Plan) error {
	return w.set(ctx, env, p.After["nm_powersave"], true)
}

func (w WifiPowerSave) Restore(ctx context.Context, env Env, p Plan) error {
	return w.set(ctx, env, p.Before["nm_powersave"], false)
}

// Check confirms the adapter itself now reports the requested mode — the
// stored NetworkManager setting alone does not prove the radio changed.
func (w WifiPowerSave) Check(ctx context.Context, env Env, p Plan) error {
	var want string
	switch p.After["nm_powersave"] {
	case nmDisable:
		want = "off"
	case nmEnable:
		want = "on"
	default:
		return nil // default/ignore leave the radio mode to NM/driver; nothing to assert
	}
	t, err := w.target(ctx, env)
	if err != nil {
		return err
	}
	var got string
	for i := 0; i < 6; i++ {
		res, err := env.Run.Run(ctx, "iw", "dev", t.Iface, "get", "power_save")
		if err == nil {
			if got = detect.ParseIWPowerSave(res.Stdout); got == want {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(PollInterval):
		}
	}
	return fmt.Errorf("setting saved, but the adapter still reports power save %q (wanted %q)", got, want)
}

// ---- NetworkManager checkpoint guard ------------------------------------

const (
	nmDest      = "org.freedesktop.NetworkManager"
	nmPath      = "/org/freedesktop/NetworkManager"
	checkpointS = 60 // seconds before NetworkManager rolls back on its own
)

var checkpointPathRe = regexp.MustCompile(`"(/org/freedesktop/NetworkManager/Checkpoint/[0-9]+)"`)

type nmGuard struct {
	env  Env
	path string
}

// BeginGuard creates a NetworkManager checkpoint. If TurboShift crashes or
// the connection is lost, NetworkManager itself rolls the configuration back
// after the timeout — no TurboShift process needs to survive for that.
// (nmcli has no checkpoint command; the D-Bus API is used through busctl.)
func (WifiPowerSave) BeginGuard(ctx context.Context, env Env, _ Plan) (Guard, error) {
	if !env.Run.Has("busctl") {
		return nil, errors.New("busctl not found; cannot create a NetworkManager checkpoint")
	}
	res, err := env.Run.Run(ctx, "busctl", "--system", "call", nmDest, nmPath, nmDest,
		"CheckpointCreate", "aouu", "0", fmt.Sprint(checkpointS), "0")
	if err != nil {
		return nil, fmt.Errorf("CheckpointCreate: %w", err)
	}
	m := checkpointPathRe.FindStringSubmatch(res.Stdout)
	if m == nil {
		return nil, fmt.Errorf("unexpected CheckpointCreate reply %q", strings.TrimSpace(res.Stdout))
	}
	return &nmGuard{env: env, path: m[1]}, nil
}

func (g *nmGuard) call(ctx context.Context, method string) error {
	_, err := g.env.Run.Run(ctx, "busctl", "--system", "call", nmDest, nmPath, nmDest, method, "o", g.path)
	return err
}

// Commit keeps the new configuration and cancels the pending rollback.
func (g *nmGuard) Commit(ctx context.Context) error { return g.call(ctx, "CheckpointDestroy") }

// Abort rolls the checkpoint back. Rollback may need extra polkit
// authorisation; if it is refused the checkpoint is destroyed and the caller
// restores from its own journal instead.
func (g *nmGuard) Abort(ctx context.Context) error {
	if err := g.call(ctx, "CheckpointRollback"); err != nil {
		_ = g.call(ctx, "CheckpointDestroy")
		return fmt.Errorf("checkpoint rollback refused (%w); restoring from the journal instead", err)
	}
	return nil
}
