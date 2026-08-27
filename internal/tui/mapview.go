package tui

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/backflash-cli/backflash/internal/external"
	"github.com/charmbracelet/lipgloss"
)

// swedenOutline is a hand-simplified polygon of Sweden's coastline and land
// border, traced clockwise from the northernmost point (Treriksröset). It is
// accurate enough to be recognisable at terminal resolution without shipping
// an external SVG/image asset — everything needed to draw the map lives in
// this binary.
var swedenOutline = [][2]float64{
	{69.05, 20.55}, // Treriksröset – northernmost point
	{68.35, 22.00},
	{67.85, 23.35},
	{66.80, 23.85},
	{65.83, 24.15}, // Haparanda
	{65.00, 22.80},
	{64.30, 21.50},
	{63.83, 20.26}, // Umeå
	{62.95, 18.70},
	{62.39, 17.31}, // Sundsvall
	{61.30, 17.25},
	{60.67, 17.14}, // Gävle
	{59.86, 19.10},
	{59.33, 18.06}, // Stockholm
	{58.59, 16.19}, // Norrköping
	{57.75, 16.65},
	{56.66, 16.36}, // Kalmar
	{56.20, 15.60},
	{55.60, 14.30},
	{55.34, 13.36}, // Smygehuk – southernmost point
	{55.60, 13.00}, // Malmö
	{56.05, 12.69}, // Helsingborg
	{57.10, 12.25},
	{57.71, 11.97}, // Göteborg
	{58.35, 11.35},
	{58.93, 11.17}, // Strömstad
	{59.50, 12.30}, // Norway border, heading north
	{60.50, 12.60},
	{61.50, 12.30},
	{62.50, 12.50},
	{63.20, 12.30},
	{64.00, 13.50},
	{65.00, 14.50},
	{65.90, 15.20},
	{66.80, 16.10},
	{67.50, 17.50},
	{68.10, 18.20},
	{68.60, 19.00},
}

// gotlandOutline is a small separate polygon: Gotland is a real forum/event
// hotspot (it shows up in Polisen data) and instantly recognisable, so it is
// worth the extra shape even at this resolution.
var gotlandOutline = [][2]float64{
	{57.95, 18.80},
	{57.80, 18.95},
	{57.55, 18.75},
	{57.10, 18.35},
	{56.90, 18.10},
	{57.05, 17.95},
	{57.40, 18.15},
	{57.75, 18.55},
}

const (
	mapLatMin = 54.8
	mapLatMax = 69.3
	mapLonMin = 10.5
	mapLonMax = 24.8
)

// swedenProjection returns a lat/lon -> pixel mapper for a canvas of the
// given size. It applies the standard equirectangular longitude correction
// (scaling by cos of the mean latitude) so the silhouette keeps Sweden's
// real elongated proportions instead of looking squashed, then centers the
// projected shape within the canvas (letterboxed on whichever axis has
// slack) rather than stretching it to fill the box exactly.
func swedenProjection(width, height int) func(lat, lon float64) (float64, float64) {
	correctedLonRange, latRange := swedenLonLatRange()
	scale := math.Min(float64(width)/correctedLonRange, float64(height)/latRange)
	drawnWidth := correctedLonRange * scale
	drawnHeight := latRange * scale
	offsetX := (float64(width) - drawnWidth) / 2
	offsetY := (float64(height) - drawnHeight) / 2
	return func(lat, lon float64) (float64, float64) {
		x := (lon-mapLonMin)*math.Cos((mapLatMin+mapLatMax)/2*math.Pi/180)*scale + offsetX
		y := (mapLatMax-lat)*scale + offsetY
		return x, y
	}
}

// swedenLonLatRange returns the longitude-corrected width and the latitude
// height of the map's bounding box, in the same "square" units — dividing
// the two gives Sweden's true width:height aspect ratio at this projection.
func swedenLonLatRange() (correctedLonRange, latRange float64) {
	meanLat := (mapLatMin + mapLatMax) / 2 * math.Pi / 180
	lonScale := math.Cos(meanLat)
	return (mapLonMax - mapLonMin) * lonScale, mapLatMax - mapLatMin
}

// fillPolygon rasterizes a closed lat/lon polygon into mask using a scanline
// even-odd fill. mask is indexed mask[y][x].
func fillPolygon(mask [][]bool, poly [][2]float64, project func(lat, lon float64) (float64, float64)) {
	if len(mask) == 0 || len(poly) < 3 {
		return
	}
	height := len(mask)
	width := len(mask[0])
	pts := make([][2]float64, len(poly))
	for i, p := range poly {
		x, y := project(p[0], p[1])
		pts[i] = [2]float64{x, y}
	}
	n := len(pts)
	for y := 0; y < height; y++ {
		yf := float64(y) + 0.5
		var xs []float64
		for i := 0; i < n; i++ {
			x1, y1 := pts[i][0], pts[i][1]
			x2, y2 := pts[(i+1)%n][0], pts[(i+1)%n][1]
			if (y1 <= yf && y2 > yf) || (y2 <= yf && y1 > yf) {
				t := (yf - y1) / (y2 - y1)
				xs = append(xs, x1+t*(x2-x1))
			}
		}
		sort.Float64s(xs)
		for i := 0; i+1 < len(xs); i += 2 {
			start := int(math.Ceil(xs[i]))
			end := int(math.Floor(xs[i+1]))
			if start < 0 {
				start = 0
			}
			if end > width-1 {
				end = width - 1
			}
			for x := start; x <= end; x++ {
				mask[y][x] = true
			}
		}
	}
}

const landColor = "24" // muted chart-blue, distinct from every category color

// renderSwedenMap draws a filled, categorized event map of Sweden using
// half-block characters (▀) with per-cell foreground/background colors, so
// it renders in any terminal with UTF-8 and 256-color support — no Sixel,
// Kitty graphics protocol, or external image asset required. Each terminal
// row packs two vertical pixels (top half via foreground, bottom half via
// background), doubling the effective vertical resolution of the old
// character-grid map.
func renderSwedenMap(events []external.ExternalEvent, maxWidth, rows int, highlight int) string {
	if rows < 8 {
		rows = 8
	}
	pixelHeight := rows * 2
	// Sweden is far taller than it is wide, so height is almost always the
	// binding constraint. Deriving the canvas width from the aspect ratio
	// (instead of just using the whole panel width) avoids the map getting
	// centered inside a canvas many times wider than the shape itself,
	// which left it looking like a thin, oddly offset sliver surrounded by
	// dead space. Only fall back to shrinking the height when the terminal
	// is too narrow even for that natural width.
	correctedLonRange, latRange := swedenLonLatRange()
	width := int(math.Round(float64(pixelHeight) * correctedLonRange / latRange))
	if width < 20 {
		width = 20
	}
	if maxWidth < 20 {
		maxWidth = 20
	}
	if width > maxWidth {
		width = maxWidth
		pixelHeight = int(math.Round(float64(width) * latRange / correctedLonRange))
		rows = max(8, pixelHeight/2)
		pixelHeight = rows * 2
	}
	project := swedenProjection(width, pixelHeight)

	land := make([][]bool, pixelHeight)
	for y := range land {
		land[y] = make([]bool, width)
	}
	fillPolygon(land, swedenOutline, project)
	fillPolygon(land, gotlandOutline, project)

	// dotCategory tracks which policeCategories index (if any) occupies each
	// pixel, so an event dot always wins over the land fill underneath it.
	dotCategory := make([][]int, pixelHeight)
	for y := range dotCategory {
		dotCategory[y] = make([]int, width)
		for x := range dotCategory[y] {
			dotCategory[y][x] = -1
		}
	}
	counts := make([]int, len(policeCategories))
	points := 0
	// highlightY/X pinpoint the pixel of the list's currently selected event
	// (if it has coordinates and is in range), so the caller's list cursor
	// and the map dot it corresponds to stay visibly in sync.
	highlightY, highlightX := -1, -1
	for i, event := range events {
		if event.Latitude == nil || event.Longitude == nil {
			continue
		}
		fx, fy := project(*event.Latitude, *event.Longitude)
		x, y := int(fx), int(fy)
		if x < 0 || x >= width || y < 0 || y >= pixelHeight {
			continue
		}
		idx := policeCategoryIndex(event.EventType)
		dotCategory[y][x] = idx
		counts[idx]++
		points++
		if i == highlight {
			highlightY, highlightX = y, x
		}
	}

	var b strings.Builder
	b.WriteString(titleStyle.Render("KARTA · POLISHÄNDELSER") + "\n")
	if points == 0 {
		b.WriteString(muted.Render("Inga händelser med koordinater."))
		return b.String()
	}

	// highlightColor is a bright white distinct from every category color and
	// from landColor, so the selected dot reads as "selected" rather than
	// just another category.
	const highlightColor = "231"
	pixelColor := func(y, x int) (color string, bold, set bool) {
		if y == highlightY && x == highlightX {
			return highlightColor, true, true
		}
		if idx := dotCategory[y][x]; idx >= 0 {
			return policeCategories[idx].Color, false, true
		}
		if land[y][x] {
			return landColor, false, true
		}
		return "", false, false
	}
	for row := 0; row < rows; row++ {
		var line strings.Builder
		for x := 0; x < width; x++ {
			top, topBold, topSet := pixelColor(row*2, x)
			bottom, bottomBold, bottomSet := pixelColor(row*2+1, x)
			switch {
			case !topSet && !bottomSet:
				// Neither half has land or an event: leave the cell blank
				// (terminal default) rather than coloring it.
				line.WriteRune(' ')
			case topSet && bottomSet:
				// ▀'s foreground paints the top half, background the bottom.
				style := lipgloss.NewStyle().Foreground(lipgloss.Color(top)).Background(lipgloss.Color(bottom)).Bold(topBold)
				line.WriteString(style.Render("▀"))
			case topSet:
				// Only the top half is set; leave the background unset so
				// the bottom half stays transparent instead of also top-colored.
				line.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(top)).Bold(topBold).Render("▀"))
			default:
				// Only the bottom half is set: ▄'s foreground paints the
				// bottom half, so the (unset) background keeps the top
				// half transparent.
				line.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(bottom)).Bold(bottomBold).Render("▄"))
			}
		}
		b.WriteString(line.String() + "\n")
	}

	legend := make([]string, 0, len(policeCategories))
	for i, c := range policeCategories {
		if counts[i] == 0 {
			continue
		}
		legend = append(legend, fmt.Sprintf("%s %s %s", c.Style.Render(c.Glyph), c.Label, number(counts[i])))
	}
	if len(legend) > 0 {
		b.WriteString(strings.Join(legend, "   ") + "\n")
	}
	b.WriteString(muted.Render(fmt.Sprintf("%d händelser · %.0f–%.0f°N · %.0f–%.0f°E", points, mapLatMin, mapLatMax, mapLonMin, mapLonMax)))
	return b.String()
}
