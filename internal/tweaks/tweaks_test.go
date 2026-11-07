package tweaks

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/mttcn83/turboshift/internal/detect"
	"github.com/mttcn83/turboshift/internal/platform"
)

// A restore point is plain JSON on disk. Values read back from it must be
// validated before reaching a program's argument list.
func TestRestoreRejectsOptionLikeValuesFromJournal(t *testing.T) {
	run := platform.NewFakeRunner().Install("powerprofilesctl").Install("nmcli")
	env := Env{FS: platform.NewMemFS(), Run: run}
	for _, v := range []string{"--help", "-x", "", "a b", "x;reboot", "Balanced", "$(id)"} {
		err := PowerProfile{}.Restore(context.Background(), env, Plan{Before: State{"profile": v}})
		if !errors.Is(err, ErrInvalidParam) {
			t.Errorf("profile %q: err=%v", v, err)
		}
		err = WifiPowerSave{}.Restore(context.Background(), env, Plan{Before: State{"nm_powersave": v}})
		if !errors.Is(err, ErrInvalidParam) {
			t.Errorf("wifi %q: err=%v", v, err)
		}
	}
	if len(run.Calls) != 0 {
		t.Errorf("invalid journal values reached a program: %v", run.Calls)
	}
}

// Every tweak must say what it costs, and a cost that can lose data or remove
// a protection must not be filed under "low risk".
func TestTradeoffsAreHonest(t *testing.T) {
	serious := regexp.MustCompile(`(?i)data[- ]loss|gone for good|lose the last minute|removes? a protection|switches off a kernel protection`)
	absolute := regexp.MustCompile(`(?i)^(none|essentially none|almost none|nothing)\b`)
	for _, tw := range Default().All() {
		m := tw.Meta()
		if strings.TrimSpace(m.Tradeoff) == "" {
			t.Errorf("%s has no trade-off text", m.ID)
		}
		if absolute.MatchString(m.Tradeoff) {
			t.Errorf("%s trade-off claims no cost at all: %q", m.ID, m.Tradeoff)
		}
		if serious.MatchString(m.Tradeoff) && m.Risk < RiskMedium {
			t.Errorf("%s can lose data or weaken a protection but is rated %q", m.ID, m.Risk)
		}
	}
}

// What people read first must be everyday language: no setting names or file
// paths. The precise text is kept separately as Technical.
func TestEveryTweakHasPlainText(t *testing.T) {
	jargon := regexp.MustCompile(`/proc|/sys/|\b(vm|kernel|net)\.[a-z_]+|\b[a-z]+_[a-z_]+\b|sysctl|sysfs|hyprctl|gsettings`)
	for _, tw := range Default().All() {
		m := tw.Meta()
		if _, ok := plainTexts[m.ID]; !ok {
			t.Errorf("%s has no plain-language text (add it to plain.go)", m.ID)
			continue
		}
		if m.Technical == "" || m.Technical == m.Details {
			t.Errorf("%s: the technical description was not kept separately", m.ID)
		}
		if j := jargon.FindString(m.Details); j != "" {
			t.Errorf("%s: Details uses technical wording (%q): %s", m.ID, j, m.Details)
		}
		for _, o := range m.Options {
			if j := jargon.FindString(o.Note); j != "" {
				t.Errorf("%s option %s: note uses technical wording (%q): %s", m.ID, o.Value, j, o.Note)
			}
		}
	}
}

// Every suggestion must fire on its evidence and stay silent without it, and
// what it suggests must be a request the tweak itself accepts.
func TestSuggestionsFollowTheEvidence(t *testing.T) {
	ac, battery := true, false
	pct := func(v float64) *float64 { return &v }
	type tc struct {
		name, id string
		rep      func(*detect.Report)
		cur      State
		want     string // expected suggested value, "" = no suggestion
	}
	zram := func(r *detect.Report) { r.Memory.ZramDevices = 1; r.Memory.SwapTotalKB = 8 << 20 }
	cases := []tc{
		{"swappiness with zram", "mem.swappiness", zram, State{"swappiness": "60"}, "180"},
		{"swappiness already high", "mem.swappiness", zram, State{"swappiness": "180"}, ""},
		{"swappiness without zram", "mem.swappiness", func(r *detect.Report) { r.Memory.SwapTotalKB = 8 << 20 }, State{"swappiness": "60"}, ""},
		{"dirty with 64 GB", "mem.dirty", func(r *detect.Report) { r.Memory.TotalKB = 64 << 20 }, State{"background": "10", "hard": "20"}, "responsive"},
		{"dirty with 16 GB and calm disk", "mem.dirty", func(r *detect.Report) { r.Memory.TotalKB = 16 << 20 }, State{"background": "10", "hard": "20"}, ""},
		{"dirty with 16 GB and waiting disk", "mem.dirty", func(r *detect.Report) { r.Memory.TotalKB = 16 << 20; r.Memory.PSIIOSome = pct(12) }, State{"background": "10", "hard": "20"}, "responsive"},
		{"dirty already tuned", "mem.dirty", func(r *detect.Report) { r.Memory.TotalKB = 64 << 20 }, State{"background": "5", "hard": "10"}, ""},
		{"map count too low", "game.max-map-count", nil, State{"max_map_count": "65530"}, "1048576"},
		{"map count fine", "game.max-map-count", nil, State{"max_map_count": "1048576"}, ""},
		{"qdisc plain fifo", "net.qdisc", nil, State{"default_qdisc": "pfifo_fast"}, "fq_codel"},
		{"qdisc already fq_codel", "net.qdisc", nil, State{"default_qdisc": "fq_codel"}, ""},
		{"zswap with busy disk swap", "mem.zswap", func(r *detect.Report) { r.Memory.SwapTotalKB = 8 << 20; r.Memory.SwapFreeKB = 4 << 20 }, State{"zswap": "0"}, "1"},
		{"zswap with idle swap", "mem.zswap", func(r *detect.Report) { r.Memory.SwapTotalKB = 8 << 20; r.Memory.SwapFreeKB = 8 << 20 }, State{"zswap": "0"}, ""},
		{"zswap alongside zram", "mem.zswap", func(r *detect.Report) {
			r.Memory.SwapTotalKB = 8 << 20
			r.Memory.SwapFreeKB = 1 << 20
			r.Memory.ZramDevices = 1
		}, State{"zswap": "0"}, ""},
		{"watchers too few", "sys.watchers", func(r *detect.Report) { r.Memory.TotalKB = 16 << 20 }, State{"watches": "8192", "instances": "128"}, "more"},
		{"watchers too few on a small machine", "sys.watchers", func(r *detect.Report) { r.Memory.TotalKB = 4 << 20 }, State{"watches": "8192", "instances": "128"}, "some"},
		{"watchers plenty", "sys.watchers", nil, State{"watches": "524288", "instances": "512"}, ""},
		{"autogroup off", "cpu.autogroup", nil, State{"autogroup": "0"}, "1"},
		{"autogroup on", "cpu.autogroup", nil, State{"autogroup": "1"}, ""},
		{"laptop stuck in low-power on AC", "platform.profile", func(r *detect.Report) { r.Power.OnAC = &ac }, State{"platform_profile": "low-power"}, "balanced"},
		{"laptop low-power on battery", "platform.profile", func(r *detect.Report) { r.Power.OnAC = &battery }, State{"platform_profile": "low-power"}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tw, ok := Default().Get(c.id)
			if !ok {
				t.Fatalf("no tweak %s", c.id)
			}
			var rep detect.Report
			rep.Power.OnAC = &ac
			if c.rep != nil {
				c.rep(&rep)
			}
			got := suggestions[c.id](Env{Report: rep}, c.cur)
			if c.want == "" {
				if got != nil {
					t.Fatalf("suggested %+v without evidence", got)
				}
				return
			}
			if got == nil {
				t.Fatal("no suggestion although the evidence is there")
			}
			m := tw.Meta()
			v, ok := got.Params[m.ParamName]
			if !ok || v != c.want {
				t.Fatalf("suggested %v, want %s=%s", got.Params, m.ParamName, c.want)
			}
			if err := CheckParam(m, nil, v); err != nil {
				t.Fatalf("the suggested value is not one the tweak accepts: %v", err)
			}
			if strings.TrimSpace(got.Why) == "" {
				t.Fatal("a suggestion must say why")
			}
		})
	}
}

func TestGovernorSuggestionOnlyWhereItReallyMatters(t *testing.T) {
	ac := true
	mk := func(driver string, govs ...string) Env {
		var r detect.Report
		r.Power.OnAC = &ac
		r.CPU.Driver, r.CPU.Governors = driver, govs
		return Env{Report: r}
	}
	stuck := State{"cpu0.governor": "powersave", "cpu1.governor": "powersave"}
	if r := governorSuggestion(mk("acpi-cpufreq", "performance", "powersave", "schedutil"), stuck); r == nil || r.Params["governor"] != "schedutil" {
		t.Errorf("acpi-cpufreq + powersave should suggest schedutil: %+v", r)
	}
	if r := governorSuggestion(mk("acpi-cpufreq", "performance", "powersave", "ondemand"), stuck); r == nil || r.Params["governor"] != "ondemand" {
		t.Errorf("falls back to ondemand: %+v", r)
	}
	// With intel_pstate / amd-pstate-epp 'powersave' still scales in hardware: say nothing.
	for _, d := range []string{"intel_pstate", "amd-pstate-epp", ""} {
		if r := governorSuggestion(mk(d, "performance", "powersave", "schedutil"), stuck); r != nil {
			t.Errorf("driver %q must not be flagged: %+v", d, r)
		}
	}
	if r := governorSuggestion(mk("acpi-cpufreq", "performance", "powersave", "schedutil"), State{"cpu0.governor": "schedutil"}); r != nil {
		t.Errorf("already scaling: %+v", r)
	}
}

func TestEveryTierBadgeMeansSomething(t *testing.T) {
	var rec int
	for _, tw := range Default().All() {
		m := tw.Meta()
		if m.Tier != TierRecommended {
			continue
		}
		rec++
		if m.Risk != RiskLow {
			t.Errorf("%s is Recommended but risk %s", m.ID, m.Risk)
		}
		// A Recommended badge is a promise that the app will say so when the
		// readings call for it, so the tweak needs an evidence rule.
		if ft, ok := tw.(fileTweak); ok && ft.recommend == nil && suggestions[m.ID] == nil {
			t.Errorf("%s is Recommended but nothing ever suggests it", m.ID)
		}
	}
	if rec < 6 {
		t.Errorf("only %d Recommended settings", rec)
	}
}

func TestFilterMatching(t *testing.T) {
	m := Meta{ID: "mem.dirty", Name: "Avoid long freezes", Category: CatStorage, Tier: TierAdvanced, Risk: RiskMedium, NeedsAuth: true}
	rec := &Recommendation{Why: "x"}
	cases := []struct {
		name  string
		f     Filter
		as    Assessment
		known bool
		want  bool
	}{
		{"empty filter", Filter{}, Assessment{}, true, true},
		{"query words all match", Filter{Query: "freezes dirty"}, Assessment{}, true, true},
		{"query miss", Filter{Query: "gpu"}, Assessment{}, true, false},
		{"risk is searchable", Filter{Query: "medium"}, Assessment{}, true, true},
		{"category", Filter{Category: CatStorage}, Assessment{}, true, true},
		{"wrong category", Filter{Category: CatCPU}, Assessment{}, true, false},
		{"tier", Filter{Tier: TierRecommended}, Assessment{}, true, false},
		{"max risk low hides medium", Filter{MaxRisk: RiskLow}, Assessment{}, true, false},
		{"max risk medium keeps medium", Filter{MaxRisk: RiskMedium}, Assessment{}, true, true},
		{"no password hides admin tweaks", Filter{NoAdmin: true}, Assessment{}, true, false},
		{"available view hides unavailable", Filter{View: ViewAvailable}, Assessment{}, true, false},
		{"available view keeps available", Filter{View: ViewAvailable}, Assessment{Available: true}, true, true},
		{"suggested needs a suggestion", Filter{View: ViewSuggested}, Assessment{Available: true}, true, false},
		{"suggested keeps suggested", Filter{View: ViewSuggested}, Assessment{Available: true, Recommend: rec}, true, true},
		{"views cannot be judged before a scan", Filter{View: ViewSuggested}, Assessment{}, false, true},
	}
	for _, c := range cases {
		if got := c.f.Match(m, c.as, c.known); got != c.want {
			t.Errorf("%s: Match = %v, want %v", c.name, got, c.want)
		}
	}
	if (Filter{}).Active() || !(Filter{NoAdmin: true}).Active() {
		t.Error("Active() is wrong")
	}
}
