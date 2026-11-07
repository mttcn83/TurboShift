package tweaks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/mttcn83/turboshift/internal/platform"
)

// Desktop tweaks change per-user compositor / toolkit settings through the
// program's own command-line tool. They need no administrator rights, never
// touch a config file, and are restored by writing the previous values back.

// ScopeSession is for settings that live in the running desktop session.
const ScopeSession Scope = "this session"

// cmdTweak is a tweak whose state is a few named settings read and written
// through a command. Like fileTweak, each concrete tweak supplies only facts.
type cmdTweak struct {
	meta Meta
	what string
	keys []string
	// gate returns why the tweak cannot be offered (or "").
	gate func(ctx context.Context, env Env) string
	read func(ctx context.Context, env Env, key string) (string, error)
	// write must validate both key and value before running anything.
	write   func(ctx context.Context, env Env, key, val string) error
	presets map[string]map[string]string
	opts    []Option
	warn    []string
}

func (c cmdTweak) Meta() Meta { return plained(c.meta) }

func (c cmdTweak) readKeys(ctx context.Context, env Env, keys []string) (State, error) {
	cur := State{}
	for _, k := range keys {
		v, err := c.read(ctx, env, k)
		if err != nil {
			return nil, fmt.Errorf("could not read %s: %w", k, err)
		}
		cur[k] = v
	}
	return cur, nil
}

func (c cmdTweak) Assess(ctx context.Context, env Env) Assessment {
	if why := c.gate(ctx, env); why != "" {
		return Assessment{Reason: why}
	}
	cur, err := c.readKeys(ctx, env, c.keys)
	if err != nil {
		return Assessment{Reason: err.Error()}
	}
	return Assessment{Available: true, Current: cur, Options: plainOpts(c.meta.ID, c.opts)}
}

func (c cmdTweak) Read(ctx context.Context, env Env, want State) (State, error) {
	var keys []string
	for k := range want {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return c.readKeys(ctx, env, keys)
}

func (c cmdTweak) Plan(ctx context.Context, env Env, params map[string]string) (Plan, error) {
	a := c.Assess(ctx, env)
	if !a.Available {
		return Plan{}, fmt.Errorf("%w: %s", ErrUnavailable, a.Reason)
	}
	v := params[c.meta.ParamName]
	if err := CheckParam(c.meta, a.Options, v); err != nil {
		return Plan{}, err
	}
	after := State{}
	changed := 0
	for _, k := range c.keys {
		want := c.presets[v][k]
		after[k] = want
		if a.Current[k] != want {
			changed++
		}
	}
	if changed == 0 {
		return Plan{}, ErrNoChange
	}
	return Plan{
		TweakID: c.meta.ID, Params: map[string]string{c.meta.ParamName: v},
		Before: a.Current.Clone(), After: after,
		Changes:  []Change{{c.what, c.Summary(a.Current), c.Summary(after)}},
		Warnings: c.warn,
	}, nil
}

// Summary describes a state as the option it equals ("off"/"on") or "custom".
func (c cmdTweak) Summary(s State) string {
	for _, o := range c.opts {
		if len(s) > 0 && s.Covers(State(c.presets[o.Value])) {
			return o.Label
		}
	}
	var parts []string
	for _, k := range c.keys {
		parts = append(parts, k+"="+s[k])
	}
	return "custom (" + strings.Join(parts, ", ") + ")"
}

func (c cmdTweak) put(ctx context.Context, env Env, s State) error {
	var errs []error
	for _, k := range c.keys {
		v, ok := s[k]
		if !ok {
			continue
		}
		if err := c.write(ctx, env, k, v); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (c cmdTweak) Apply(ctx context.Context, env Env, p Plan) error { return c.put(ctx, env, p.After) }
func (c cmdTweak) Restore(ctx context.Context, env Env, p Plan) error {
	return c.put(ctx, env, p.Before)
}

// ---- Hyprland ------------------------------------------------------------------

var hyprVal = regexp.MustCompile(`^[0-9]{1,2}$`)

// hyprLuaConfig is the file a Lua-configured Hyprland (0.55 and newer) loads.
// Such a Hyprland answers `hyprctl keyword` with "unknown request", so a
// runtime change is impossible; say so instead of offering a switch that fails.
const hyprLuaConfig = ".config/hypr/hyprland.lua"

func hyprGate(keys ...string) func(context.Context, Env) string {
	return func(ctx context.Context, env Env) string {
		if !env.Run.Has("hyprctl") {
			return "Hyprland's hyprctl is not installed."
		}
		if env.Home != "" {
			if _, ok := platform.ReadTrim(env.FS, path.Join(env.Home, hyprLuaConfig)); ok {
				return "Your Hyprland is configured with Lua (hyprland.lua), which does not accept runtime changes from outside. Put this setting in hyprland.lua instead."
			}
		}
		for _, k := range keys {
			if _, err := hyprRead(ctx, env, k); err != nil {
				return "Hyprland is not running, or this version does not know the option " + k + "."
			}
		}
		return ""
	}
}

// hyprRead returns an option as "0"/"1" or its integer. Hyprland prints its
// on/off options as {"bool": true} and its numbers as {"int": 2}.
func hyprRead(ctx context.Context, env Env, key string) (string, error) {
	res, err := env.Run.Run(ctx, "hyprctl", "getoption", key, "-j")
	if err != nil {
		return "", err
	}
	var o struct {
		Option string `json:"option"`
		Int    *int64 `json:"int"`
		Bool   *bool  `json:"bool"`
	}
	if err := json.Unmarshal([]byte(res.Stdout), &o); err != nil || o.Option != key {
		return "", errors.New("unexpected hyprctl reply")
	}
	switch {
	case o.Int != nil:
		return fmt.Sprint(*o.Int), nil
	case o.Bool != nil && *o.Bool:
		return "1", nil
	case o.Bool != nil:
		return "0", nil
	}
	return "", errors.New("unexpected hyprctl reply")
}

func hyprWrite(allowed ...string) func(context.Context, Env, string, string) error {
	return func(ctx context.Context, env Env, key, val string) error {
		ok := false
		for _, a := range allowed {
			ok = ok || a == key
		}
		if !ok || !hyprVal.MatchString(val) {
			return fmt.Errorf("%w: refusing to set %q to %q", ErrInvalidParam, key, val)
		}
		res, err := env.Run.Run(ctx, "hyprctl", "keyword", key, val)
		if err != nil {
			return fmt.Errorf("hyprctl keyword %s: %w", key, err)
		}
		if got := strings.TrimSpace(res.Stdout); got == "unknown request" {
			return fmt.Errorf("hyprctl keyword %s: this Hyprland does not accept runtime keywords (Lua configuration); nothing was changed", key)
		} else if got != "ok" {
			return fmt.Errorf("hyprctl keyword %s: %s", key, got)
		}
		return nil
	}
}

func onOff(on, off string) []Option {
	return []Option{{"off", "Off", off}, {"on", "On", on}}
}

// HyprEffects turns Hyprland's animations, blur and shadows off (or on).
func HyprEffects() Tweak {
	keys := []string{"animations:enabled", "decoration:blur:enabled", "decoration:shadow:enabled"}
	return cmdTweak{
		meta: Meta{
			ID: "desktop.hypr-effects", Name: "Plain, fast Hyprland (no animations, blur or shadows)",
			Summary:  "Switch off window animations, background blur and drop shadows so the compositor does far less work per frame.",
			Details:  "Uses `hyprctl keyword` to set animations:enabled, decoration:blur:enabled and decoration:shadow:enabled to 0. Blur is the most expensive effect: it re-renders what is behind every blurred window on every frame. Animations add delay between your action and the final picture.",
			Helps:    "Integrated graphics, older GPUs, low-latency gaming on the desktop, and anyone who prefers windows that appear without animation.",
			Tradeoff: "The desktop looks flatter. Hyprland resets runtime keywords when its configuration is reloaded or on next login, so this is a per-session switch; edit hyprland.conf to make it permanent.",
			Evidence: "Compare GPU load and frame times with the compositor under a game or video, several runs each.",
			Category: CatDesktop, Tier: TierSituational, Risk: RiskLow, Scope: ScopeSession,
			ParamName: "effects", Options: onOff("Everything as configured in your Hyprland setup.", "No animations, blur or shadows."),
		},
		what: "Hyprland visual effects", keys: keys,
		presets: map[string]map[string]string{
			"off": {keys[0]: "0", keys[1]: "0", keys[2]: "0"},
			"on":  {keys[0]: "1", keys[1]: "1", keys[2]: "1"},
		},
		opts: onOff("Everything as configured in your Hyprland setup.", "No animations, blur or shadows."),
		gate: hyprGate(keys...), read: hyprRead, write: hyprWrite(keys...),
		warn: []string{"Lasts until Hyprland reloads its config or you log out."},
	}
}

// HyprTearing allows tearing for games that ask for it.
func HyprTearing() Tweak {
	k := "general:allow_tearing"
	opts := onOff("Windows that request immediate presentation (via a windowrule) may tear.", "Always wait for vertical sync.")
	return cmdTweak{
		meta: Meta{
			ID: "desktop.hypr-tearing", Name: "Lower input lag in games (allow tearing)",
			Summary:  "Let full-screen games skip waiting for the screen refresh, so your mouse and keyboard show up sooner.",
			Details:  "Sets general:allow_tearing = 1. Hyprland then honours `windowrule = immediate` for games, which present frames the moment they are ready instead of at the next refresh. That removes up to one refresh of delay, at the cost of occasional visible tear lines.",
			Helps:    "Competitive games where every millisecond of input delay counts, on a high-refresh monitor.",
			Tradeoff: "Tear lines can appear. The option alone changes nothing: you also need an `immediate` window rule for the game, and your GPU driver must support tearing.",
			Evidence: "Compare end-to-end input latency with a high-speed camera or a latency tool; the effect is a fraction of a refresh.",
			Category: CatDesktop, Tier: TierSituational, Risk: RiskLow, Scope: ScopeSession,
			ParamName: "tearing", Options: opts,
		},
		what: "Hyprland tearing", keys: []string{k},
		presets: map[string]map[string]string{"on": {k: "1"}, "off": {k: "0"}},
		opts:    []Option{opts[1], opts[0]},
		gate:    hyprGate(k), read: hyprRead, write: hyprWrite(k),
		warn: []string{"Needs an `immediate` window rule for the game; lasts until Hyprland reloads its config."},
	}
}

// HyprDirectScanout lets a full-screen game bypass the compositor.
func HyprDirectScanout() Tweak {
	k := "render:direct_scanout"
	opts := onOff("A full-screen window is sent straight to the screen, skipping the compositor's copy.", "Always composite.")
	return cmdTweak{
		meta: Meta{
			ID: "desktop.hypr-direct-scanout", Name: "Full-screen games skip the compositor",
			Summary:  "Send a full-screen game directly to the monitor instead of copying it through the desktop first.",
			Details:  "Sets render:direct_scanout = 1. When one window covers the whole screen, Hyprland can hand its buffer straight to the display, saving a GPU copy and a frame of delay.",
			Helps:    "Full-screen games and video players, especially on high-resolution displays.",
			Tradeoff: "Rarely, a driver or game mishandles it and shows glitches or a black frame; switch it off if so. Hyprland versions differ in what the value means.",
			Evidence: "Compare GPU load and frame times in the same full-screen scene with it on and off.",
			Category: CatDesktop, Tier: TierSituational, Risk: RiskLow, Scope: ScopeSession,
			ParamName: "scanout", Options: opts,
		},
		what: "Hyprland direct scan-out", keys: []string{k},
		presets: map[string]map[string]string{"on": {k: "1"}, "off": {k: "0"}},
		opts:    []Option{opts[1], opts[0]},
		gate:    hyprGate(k), read: hyprRead, write: hyprWrite(k),
		warn: []string{"Lasts until Hyprland reloads its config."},
	}
}

// ---- GNOME / GTK ------------------------------------------------------------------

const gnomeSchema, gnomeKey = "org.gnome.desktop.interface", "enable-animations"

func gnomeRead(ctx context.Context, env Env, _ string) (string, error) {
	res, err := env.Run.Run(ctx, "gsettings", "get", gnomeSchema, gnomeKey)
	if err != nil {
		return "", err
	}
	switch v := strings.TrimSpace(res.Stdout); v {
	case "true", "false":
		return v, nil
	default:
		return "", fmt.Errorf("unexpected gsettings reply %q", v)
	}
}

// GnomeAnimations toggles GTK/GNOME interface animations.
func GnomeAnimations() Tweak {
	const k = "enable-animations"
	opts := onOff("Window, menu and workspace animations as designed.", "Window, menu and workspace changes without animation.")
	return cmdTweak{
		meta: Meta{
			ID: "desktop.gnome-animations", Name: "GNOME / GTK without animations",
			Summary:  "Turn off GNOME and GTK animations so windows, menus and workspaces respond without the animation delay.",
			Details:  "Sets org.gnome.desktop.interface enable-animations through gsettings. Every animation adds tens to hundreds of milliseconds between your action and the finished screen, and costs GPU time on weak graphics.",
			Helps:    "GNOME and GTK-app users on integrated graphics, remote desktops and anyone who finds animations slow.",
			Tradeoff: "The interface feels abrupt. This is a normal GNOME setting, kept across logins until you change it back.",
			Evidence: "A stopwatch on opening the overview or switching workspaces is enough: the difference is visible, not subtle.",
			Category: CatDesktop, Tier: TierSituational, Risk: RiskLow, Scope: ScopePersistent,
			ParamName: "animations", Options: opts,
		},
		what: "interface animations", keys: []string{k},
		presets: map[string]map[string]string{"off": {k: "false"}, "on": {k: "true"}},
		opts:    opts,
		gate: func(ctx context.Context, env Env) string {
			if !env.Run.Has("gsettings") {
				return "gsettings is not installed."
			}
			if _, err := gnomeRead(ctx, env, k); err != nil {
				return "The GNOME/GTK setting is not readable here (no desktop session, or no GSettings schema)."
			}
			return ""
		},
		read: gnomeRead,
		write: func(ctx context.Context, env Env, key, val string) error {
			if key != k || (val != "true" && val != "false") {
				return fmt.Errorf("%w: refusing to set %q to %q", ErrInvalidParam, key, val)
			}
			_, err := env.Run.Run(ctx, "gsettings", "set", gnomeSchema, gnomeKey, val)
			return err
		},
	}
}
