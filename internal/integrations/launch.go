// Package integrations builds launch commands for tools TurboShift
// cooperates with (GameMode, MangoHud, GPU offload). It never installs or
// reconfigures them; it only composes command lines for the user's game.
package integrations

import (
	"fmt"
	"strings"
)

// GPU selects the render device for hybrid-graphics laptops.
type GPU string

const (
	GPUAuto   GPU = ""
	GPUNvidia GPU = "nvidia" // NVIDIA PRIME render offload
	GPUMesa   GPU = "mesa"   // DRI_PRIME=1 (AMD/Intel/nouveau via Mesa)
)

type LaunchSpec struct {
	GameMode   bool
	MangoHud   bool
	LogSeconds int    // >0: MangoHud auto-starts a log of this length
	LogDir     string // required when LogSeconds > 0
	GPU        GPU
}

// Env returns KEY=VALUE pairs the launch needs.
func (s LaunchSpec) Env() []string {
	var env []string
	switch s.GPU {
	case GPUNvidia:
		env = append(env, "__NV_PRIME_RENDER_OFFLOAD=1", "__GLX_VENDOR_LIBRARY_NAME=nvidia", "__VK_LAYER_NV_optimus=NVIDIA_only")
	case GPUMesa:
		env = append(env, "DRI_PRIME=1")
	}
	if s.MangoHud && s.LogSeconds > 0 && s.LogDir != "" {
		// Keys verified against MangoHud's README: output_folder, autostart_log, log_duration.
		env = append(env, fmt.Sprintf("MANGOHUD_CONFIG=output_folder=%s,autostart_log=10,log_duration=%d", s.LogDir, s.LogSeconds))
	}
	return env
}

// Wrappers returns the command prefix, e.g. ["gamemoderun", "mangohud"].
func (s LaunchSpec) Wrappers() []string {
	var w []string
	if s.GameMode {
		w = append(w, "gamemoderun")
	}
	if s.MangoHud {
		w = append(w, "mangohud")
	}
	return w
}

// Steam renders the string to paste into a game's Steam "Launch Options".
func (s LaunchSpec) Steam() string {
	var p []string
	for _, e := range s.Env() {
		k, v, _ := strings.Cut(e, "=")
		p = append(p, k+"="+ShellQuote(v))
	}
	p = append(p, s.Wrappers()...)
	p = append(p, "%command%")
	return strings.Join(p, " ")
}

// Validate rejects specs that would produce a broken or unsafe command.
func (s LaunchSpec) Validate() error {
	if s.LogSeconds < 0 || s.LogSeconds > 3600 {
		return fmt.Errorf("log duration must be between 0 and 3600 seconds")
	}
	if s.LogSeconds > 0 && !s.MangoHud {
		return fmt.Errorf("logging needs MangoHud enabled")
	}
	if s.LogSeconds > 0 && s.LogDir == "" {
		return fmt.Errorf("logging needs a log directory")
	}
	if strings.ContainsAny(s.LogDir, ",\n\r\x00") {
		return fmt.Errorf("log directory may not contain commas or control characters (MangoHud config separator)")
	}
	switch s.GPU {
	case GPUAuto, GPUNvidia, GPUMesa:
		return nil
	}
	return fmt.Errorf("unknown GPU choice %q (use nvidia or mesa)", s.GPU)
}

// ShellQuote quotes s for POSIX shells only when needed.
func ShellQuote(s string) string {
	if s != "" && strings.IndexFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("_-./=,:@%+", r))
	}) < 0 {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
