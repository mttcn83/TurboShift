package tui

import (
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// TestRandomInputNeverPanicsOrOverflows throws seeded random keys, clicks and
// terminal resizes at the whole app (every screen, every dialog) on a
// simulated machine. Nothing may panic, and a frame must never be wider or
// taller than the terminal it is drawn for.
func TestRandomInputNeverPanicsOrOverflows(t *testing.T) {
	keys := []string{"enter", "esc", "tab", "up", "down", "left", "right", "space", "backspace",
		"1", "2", "3", "4", "5", "6", "7", "8", "s", "o", "p", "c", "g", "d", "n", "m", "u", "a", "y", "i", "/", "?", "x", "j", "k", "e", "r", "f", "b", "t", "w", "z"}
	sizes := [][2]int{{110, 34}, {80, 24}, {60, 20}, {120, 40}, {200, 60}, {45, 14}, {100, 12}, {30, 8}}
	seeds, steps := 8, 260
	if testing.Short() {
		seeds = 3
	}
	if n, _ := strconv.Atoi(os.Getenv("TURBO_TORTURE_SEEDS")); n > 0 { // e.g. 300 for a long soak
		seeds, steps = n, 600
	}
	for seed := 1; seed <= seeds; seed++ {
		seed := seed
		t.Run(fmt.Sprintf("seed%d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewSource(int64(seed)))
			r := newRig(t)
			var trail []string
			defer func() {
				if p := recover(); p != nil {
					last := trail
					if len(last) > 12 {
						last = last[len(last)-12:]
					}
					t.Fatalf("panic: %v\nlast events: %s", p, strings.Join(last, " "))
				}
			}()
			for i := 0; i < steps; i++ {
				var cmd tea.Cmd
				switch n := rng.Intn(20); {
				case n < 14:
					k := keys[rng.Intn(len(keys))]
					trail = append(trail, k)
					_, cmd = r.m.Update(keyMsg(k))
				case n < 18:
					x, y := rng.Intn(r.m.w+4), rng.Intn(r.m.h+2)
					trail = append(trail, fmt.Sprintf("click(%d,%d)", x, y))
					cmd = r.m.View().OnMouse(tea.MouseClickMsg(tea.Mouse{X: x, Y: y, Button: tea.MouseLeft}))
				default:
					sz := sizes[rng.Intn(len(sizes))]
					trail = append(trail, fmt.Sprintf("resize(%dx%d)", sz[0], sz[1]))
					_, cmd = r.m.Update(tea.WindowSizeMsg{Width: sz[0], Height: sz[1]})
				}
				r.settle(cmd)
				out := ansi.Strip(r.m.View().Content)
				lines := strings.Split(out, "\n")
				if r.m.w >= 30 && r.m.h >= 8 {
					if len(lines) > r.m.h {
						t.Fatalf("frame is %d rows tall in a %d-row terminal after %v\n%s", len(lines), r.m.h, trail[max(0, len(trail)-8):], out)
					}
					for _, l := range lines {
						if ansi.StringWidth(l) > r.m.w {
							t.Fatalf("a line is %d cells wide in a %d-column terminal after %v:\n%q", ansi.StringWidth(l), r.m.w, trail[max(0, len(trail)-8):], l)
						}
					}
				}
			}
		})
	}
}
