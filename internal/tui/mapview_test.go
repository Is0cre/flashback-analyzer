package tui

import (
	"strings"
	"testing"

	"github.com/backflash-cli/backflash/internal/external"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestFillPolygonFillsInsideAndLeavesOutsideEmpty(t *testing.T) {
	// A 5x5 square inset within a 10x10 pixel grid (pixels 2..6 on each
	// axis), using an identity projection so the polygon points are plain
	// pixel coordinates. Margin on every side keeps the "outside" checks
	// unambiguous, unlike a square that touches the grid's own corner.
	mask := make([][]bool, 10)
	for y := range mask {
		mask[y] = make([]bool, 10)
	}
	square := [][2]float64{{2, 2}, {2, 6}, {6, 6}, {6, 2}}
	identity := func(lat, lon float64) (float64, float64) { return lon, lat }
	fillPolygon(mask, square, identity)
	if !mask[4][4] {
		t.Fatal("mittpunkten i polygonen fylldes inte")
	}
	if mask[0][0] || mask[9][9] {
		t.Fatal("punkter utanför polygonen fylldes felaktigt")
	}
}

func TestSwedenProjectionOrdersNorthAboveSouth(t *testing.T) {
	project := swedenProjection(60, 120)
	_, northY := project(69.0, 18.0)
	_, southY := project(56.0, 18.0)
	if northY >= southY {
		t.Fatalf("norr (y=%.1f) hamnade inte ovanför söder (y=%.1f)", northY, southY)
	}
}

func TestRenderSwedenMapPlacesEventsAsColoredPixelsWithinCanvas(t *testing.T) {
	lat, lon := 59.33, 18.06 // Stockholm — well inside Sweden's bounding box
	events := []external.ExternalEvent{{EventType: "Mord/dråp", Latitude: &lat, Longitude: &lon}}
	got := renderSwedenMap(events, 60, 16, -1)
	for _, line := range strings.Split(got, "\n") {
		if w := ansi.StringWidth(line); w > 60 {
			t.Fatalf("kartrad bredare än duken: %d > 60: %q", w, line)
		}
	}
	if !strings.Contains(got, "VÅLD 1") {
		t.Fatalf("kartlegenden saknar den enda händelsen: %s", got)
	}
}

func TestRenderSwedenMapHighlightsSelectedEventsDot(t *testing.T) {
	// Color-only differences are invisible under the test binary's default
	// color profile (no tty attached), so force true color the same way the
	// other style-sensitive tests in this package do.
	original := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(original)

	lat, lon := 59.33, 18.06
	events := []external.ExternalEvent{{EventType: "Mord/dråp", Latitude: &lat, Longitude: &lon}}
	withHighlight := renderSwedenMap(events, 60, 16, 0)
	withoutHighlight := renderSwedenMap(events, 60, 16, -1)
	if withHighlight == withoutHighlight {
		t.Fatal("den markerade listraden gav ingen synlig skillnad på kartan")
	}
}
