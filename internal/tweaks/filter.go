package tweaks

import (
	"fmt"
	"strings"
)

// View narrows the catalog by what is true on this PC right now.
type View string

const (
	ViewAll       View = ""          // everything, including what this PC cannot use
	ViewAvailable View = "available" // only what can be applied here
	ViewSuggested View = "suggested" // only what the live readings of this PC recommend
)

// Filter is the one definition of "which tweaks are shown", shared by the
// app's catalog and `turbo tweaks list`, so both always agree.
type Filter struct {
	Query    string   // free text, every word must match
	Category Category // "" = any
	Tier     Tier     // "" = any
	MaxRisk  Risk     // 0 = any; otherwise this risk or lower
	View     View
	NoAdmin  bool // only changes that need no administrator password
}

// Active reports whether any narrowing is in force.
func (f Filter) Active() bool {
	return strings.TrimSpace(f.Query) != "" || f.Category != "" || f.Tier != "" || f.MaxRisk != 0 || f.View != ViewAll || f.NoAdmin
}

// Match reports whether a tweak passes. known is false before the first scan:
// then the availability-based views cannot be judged and do not hide anything.
func (f Filter) Match(m Meta, as Assessment, known bool) bool {
	if f.Category != "" && m.Category != f.Category {
		return false
	}
	if f.Tier != "" && m.Tier != f.Tier {
		return false
	}
	if f.MaxRisk != 0 && m.Risk > f.MaxRisk {
		return false
	}
	if f.NoAdmin && m.NeedsAuth {
		return false
	}
	if known {
		switch f.View {
		case ViewAvailable:
			if !as.Available {
				return false
			}
		case ViewSuggested:
			if !as.Available || as.Recommend == nil {
				return false
			}
		}
	}
	return f.matchesQuery(m)
}

func (f Filter) matchesQuery(m Meta) bool {
	q := strings.ToLower(strings.TrimSpace(f.Query))
	if q == "" {
		return true
	}
	hay := strings.ToLower(m.ID + " " + m.Name + " " + m.Summary + " " + string(m.Category) + " " + string(m.Tier) + " risk " + m.Risk.String())
	for _, w := range strings.Fields(q) {
		if !strings.Contains(hay, w) {
			return false
		}
	}
	return true
}

// Describe lists the active narrowing in words, for the line above the list.
func (f Filter) Describe() string {
	var p []string
	if q := strings.TrimSpace(f.Query); q != "" {
		p = append(p, fmt.Sprintf("search %q", q))
	}
	if f.Category != "" {
		p = append(p, string(f.Category))
	}
	if f.Tier != "" {
		p = append(p, string(f.Tier)+" only")
	}
	if f.MaxRisk != 0 {
		p = append(p, "risk "+f.MaxRisk.String()+" or lower")
	}
	switch f.View {
	case ViewAvailable:
		p = append(p, "works on this PC")
	case ViewSuggested:
		p = append(p, "suggested for this PC")
	}
	if f.NoAdmin {
		p = append(p, "no password needed")
	}
	return strings.Join(p, " · ")
}

// ParseRisk turns "low", "medium" or "high" into a Risk.
func ParseRisk(s string) (Risk, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "low":
		return RiskLow, true
	case "medium":
		return RiskMedium, true
	case "high":
		return RiskHigh, true
	}
	return 0, false
}

// ParseTier accepts the full word or the short tag shown in the app.
func ParseTier(s string) (Tier, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "recommended", "rec":
		return TierRecommended, true
	case "situational", "sit":
		return TierSituational, true
	case "advanced", "adv":
		return TierAdvanced, true
	}
	return "", false
}
