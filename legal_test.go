package turboshift

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mttcn83/turboshift/internal/tweaks"
)

// These tests keep the promises in PRIVACY.md and the README from drifting:
// they fail the build when a change would quietly add a network connection, a
// tracker, an unreviewed URL, an unlicensed dependency, an absolute
// performance claim or personal data.

// files returns repo files (not .git, dist or generated output) matching ext.
func files(t *testing.T, exts ...string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if n := d.Name(); n == ".git" || n == "dist" || n == "testdata" {
				return fs.SkipDir
			}
			return nil
		}
		for _, e := range exts {
			if strings.HasSuffix(p, e) || filepath.Base(p) == e {
				b, err := os.ReadFile(p)
				if err != nil {
					return err
				}
				out[filepath.ToSlash(p)] = string(b)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestOnlyTheNetworkDoctorOpensConnections(t *testing.T) {
	allowed := map[string]bool{
		"internal/diagnostics/network.go": true,
		"internal/diagnostics/ping.go":    true,
	}
	netUse := regexp.MustCompile(`"net/http"|"golang.org/x/net/icmp"|net\.Dial|\.DialContext\(|net\.Listen|ListenPacket\(|LookupIPAddr\(|LookupHost\(|LookupAddr\(|net\.LookupIP|http\.(Get|Post|Head|NewRequest)`)
	for p, src := range files(t, ".go") {
		if strings.HasSuffix(p, "_test.go") || allowed[p] {
			continue
		}
		if m := netUse.FindString(src); m != "" {
			t.Errorf("%s uses %q: only the opt-in Network Doctor may touch the network (update PRIVACY.md and this test deliberately if that changes)", p, m)
		}
	}
}

func TestNoUnreviewedURLsInCodeOrScripts(t *testing.T) {
	reviewed := []string{
		"http://www.freedesktop.org/standards/PolicyKit/1/policyconfig.dtd", // polkit DTD identifier, never fetched
		"https://github.com/mttcn83/TurboShift",                             // polkit vendor URL (metadata)
		"https://go.dev/LICENSE",                                            // licence reference in the notices text
		"https://)",                                                         // a comment about URL syntax
	}
	url := regexp.MustCompile(`https?://[^\s"'` + "`" + `)<>]*\)?`)
	for p, src := range files(t, ".go", ".sh", ".in", "Makefile") {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		for _, u := range url.FindAllString(src, -1) {
			ok := false
			for _, r := range reviewed {
				if strings.HasPrefix(u, r) || strings.HasPrefix(r, u) {
					ok = true
				}
			}
			if !ok {
				t.Errorf("%s contains an unreviewed URL %q", p, u)
			}
		}
	}
}

func TestDocsLoadNothingFromThirdParties(t *testing.T) {
	bad := regexp.MustCompile(`(?i)<img|<script|<iframe|<link |@import|!\[[^\]]*\]\(|fonts\.googleapis|fonts\.gstatic|googletagmanager|google-analytics|gravatar|shields\.io|badge|unpkg\.com|jsdelivr|cdnjs`)
	for p, src := range files(t, ".md", ".html", ".css", ".svg") {
		if p == "THIRD_PARTY_NOTICES.md" {
			continue
		}
		if m := bad.FindString(src); m != "" {
			t.Errorf("%s contains %q: a document that embeds a remote resource makes the reader's browser send their IP address to a third party", p, m)
		}
	}
}

// Links in documents are fine, but each host is a deliberate choice: a link a
// reader clicks reveals their IP address to that host, so new hosts are reviewed.
func TestDocsLinkOnlyToReviewedHosts(t *testing.T) {
	hosts := map[string]bool{"github.com": true, "go.dev": true, "semver.org": true}
	url := regexp.MustCompile(`https?://([a-zA-Z0-9.-]+)`)
	for p, src := range files(t, ".md") {
		if p == "THIRD_PARTY_NOTICES.md" {
			continue
		}
		for _, m := range url.FindAllStringSubmatch(src, -1) {
			if !hosts[m[1]] {
				t.Errorf("%s links to %s, which is not a reviewed host", p, m[1])
			}
		}
	}
}

func TestLicencesAreBuiltIntoTheBinary(t *testing.T) {
	if !strings.Contains(License, "MIT License") || !strings.Contains(ThirdPartyNotices, "The Go Authors") {
		t.Fatal("the MIT licence or the Go licence text is not embedded")
	}
	out, err := exec.Command("go", "list", "-deps", "-f", "{{with .Module}}{{if not .Main}}{{.Path}}{{end}}{{end}}", "./cmd/turbo").Output()
	if err != nil {
		t.Skipf("go list unavailable: %v", err)
	}
	for _, mod := range strings.Fields(string(out)) {
		if !strings.Contains(ThirdPartyNotices, "## "+mod+" ") {
			t.Errorf("%s is linked into turbo but its licence is missing from THIRD_PARTY_NOTICES.md (run scripts/gen-notices.sh)", mod)
		}
	}
}

func TestNoAbsolutePerformancePromises(t *testing.T) {
	banned := regexp.MustCompile(`(?i)\b(instant(ly)?|guarantee[sd]?|zero lag|lag-free|stutter-free|100 ?%|risk-free|no risk|will make|always faster|never (lag|stutter|crash)\w*)\b`)
	check := func(where, s string) {
		if m := banned.FindString(s); m != "" {
			t.Errorf("%s promises a result (%q): describe the mechanism and say how to measure it instead: %q", where, m, s)
		}
	}
	for _, tw := range tweaks.Default().All() {
		m := tw.Meta()
		for k, v := range map[string]string{"Name": m.Name, "Summary": m.Summary, "Details": m.Details, "Helps": m.Helps, "Tradeoff": m.Tradeoff, "Evidence": m.Evidence} {
			check(m.ID+" "+k, v)
		}
		for _, o := range m.Options {
			check(m.ID+" option "+o.Value, o.Label+" "+o.Note)
		}
	}
	for _, p := range tweaks.Presets() {
		for k, v := range map[string]string{"Headline": p.Headline, "Blurb": p.Blurb, "Summary": p.Summary, "Best": p.Best, "Cost": p.Cost} {
			check("preset "+p.ID+" "+k, v)
		}
	}
}

func TestNoPersonalDataInTheRepository(t *testing.T) {
	personal := regexp.MustCompile(`(?i)@gmail\.com|@outlook\.|@hotmail\.|@yahoo\.|/Users/[a-z]+/|/home/` + "am" + `ert|\bam` + "ert" + `\b`)
	for p, src := range files(t, ".go", ".md", ".sh", ".in", ".yml", ".example", "Makefile", "LICENSE") {
		if m := personal.FindString(src); m != "" {
			t.Errorf("%s contains %q", p, m)
		}
	}
}
