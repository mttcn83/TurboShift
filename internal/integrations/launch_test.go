package integrations

import (
	"strings"
	"testing"
)

func TestSteamLaunchOptions(t *testing.T) {
	got := LaunchSpec{GameMode: true, MangoHud: true}.Steam()
	if got != "gamemoderun mangohud %command%" {
		t.Errorf("got %q", got)
	}
	got = LaunchSpec{MangoHud: true, LogSeconds: 60, LogDir: "/home/u/My Logs", GPU: GPUNvidia}.Steam()
	for _, want := range []string{"__NV_PRIME_RENDER_OFFLOAD=1", "__VK_LAYER_NV_optimus=NVIDIA_only",
		"MANGOHUD_CONFIG='output_folder=/home/u/My Logs,autostart_log=10,log_duration=60'", "mangohud %command%"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
	if got := (LaunchSpec{GPU: GPUMesa}).Steam(); got != "DRI_PRIME=1 %command%" {
		t.Errorf("got %q", got)
	}
}

func TestValidateAndQuote(t *testing.T) {
	bad := []LaunchSpec{
		{MangoHud: true, LogSeconds: 99999, LogDir: "/x"},
		{LogSeconds: 5, LogDir: "/x"},
		{MangoHud: true, LogSeconds: 5},
		{MangoHud: true, LogSeconds: 5, LogDir: "/x,evil=1"},
		{GPU: "amd; rm -rf /"},
	}
	for _, s := range bad {
		if s.Validate() == nil {
			t.Errorf("accepted %+v", s)
		}
	}
	if (LaunchSpec{GameMode: true}).Validate() != nil {
		t.Error("simple spec rejected")
	}
	for in, want := range map[string]string{"abc": "abc", "a b": "'a b'", "it's": `'it'\''s'`, "": "''", "$(x)": "'$(x)'", "a;b": "'a;b'"} {
		if got := ShellQuote(in); got != want {
			t.Errorf("ShellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}
