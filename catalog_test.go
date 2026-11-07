package turboshift

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/mttcn83/turboshift/internal/tweaks"
)

// The README's catalogue is generated from the registry, so the document can
// never claim a tier, risk or setting the program does not have. Regenerate
// with:  UPDATE_README=1 go test -run TestReadmeCatalogIsCurrent .
const catStart, catEnd = "<!-- catalog:start -->", "<!-- catalog:end -->"

func catalogMarkdown() string {
	var order []tweaks.Category
	by := map[tweaks.Category][]tweaks.Meta{}
	for _, tw := range tweaks.Default().All() {
		m := tw.Meta()
		if _, ok := by[m.Category]; !ok {
			order = append(order, m.Category)
		}
		by[m.Category] = append(by[m.Category], m)
	}
	var b strings.Builder
	for _, c := range order {
		fmt.Fprintf(&b, "### %s (%d)\n\n| ID | What you get | Tier | Risk | Admin | Lasts |\n| --- | --- | --- | --- | --- | --- |\n", c, len(by[c]))
		for _, m := range by[c] {
			admin := "no"
			if m.NeedsAuth {
				admin = "yes"
			}
			fmt.Fprintf(&b, "| `%s` | **%s**: %s | %s | %s | %s | %s |\n", m.ID, m.Name, m.Summary, m.Tier, m.Risk, admin, m.Scope)
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

func TestReadmeCatalogIsCurrent(t *testing.T) {
	raw, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	i, j := strings.Index(s, catStart), strings.Index(s, catEnd)
	if i < 0 || j < i {
		t.Fatalf("README.md needs the %s ... %s markers around the catalogue", catStart, catEnd)
	}
	want := s[:i+len(catStart)] + "\n" + catalogMarkdown() + s[j:]
	if want == s {
		return
	}
	if os.Getenv("UPDATE_README") != "" {
		if err := os.WriteFile("README.md", []byte(want), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Fatal("README.md's catalogue is out of date: run  UPDATE_README=1 go test -run TestReadmeCatalogIsCurrent .")
}

func TestReadmeCountMatchesTheCatalogue(t *testing.T) {
	raw, _ := os.ReadFile("README.md")
	want := fmt.Sprintf("## Optimizations included (%d)", len(tweaks.Default().All()))
	if !strings.Contains(string(raw), want) {
		t.Errorf("README.md must say %q", want)
	}
}
