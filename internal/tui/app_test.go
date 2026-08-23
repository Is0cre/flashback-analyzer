package tui

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/backflash-cli/backflash/internal/external"
	"github.com/backflash-cli/backflash/internal/flashback"
	"github.com/backflash-cli/backflash/internal/gandr"
	"github.com/backflash-cli/backflash/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

func TestNewStartsOnDashboard(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "backflash.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := New(s, flashback.NewClient(flashback.AnonymousSession{}))
	if a.CurrentView != ViewOverview {
		t.Fatalf("startade på vy %v, väntade översikt", a.CurrentView)
	}
	if !strings.Contains(a.View(), "BACKFLASH // DISKURS-NOC") {
		t.Fatal("dashboard saknar BACKFLASH-diskurs-NOC")
	}
}

func TestBackFromFeatureReturnsToDashboard(t *testing.T) {
	s, err := store.Open(filepath.Join(t.TempDir(), "backflash.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := New(s, flashback.NewClient(flashback.AnonymousSession{}))
	a.CurrentView = ViewGandr
	model, _ := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if model.(App).CurrentView != ViewOverview {
		t.Fatal("q från Gandr gick inte tillbaka till dashboard")
	}
}

func TestGandrStartsLocked(t *testing.T) {
	a := New(nil, nil)
	if a.Gandr == nil {
		t.Fatal("Gandr-gränsen saknas")
	}
	if got := a.Gandr.Summary().State; got != gandr.Locked {
		t.Fatalf("Gandr ska starta låst, fick %q", got)
	}
}

func TestDashboardHasSingleHeadingAndFitsWideLayout(t *testing.T) {
	a := New(nil, nil)
	a.Width = 128
	view := a.View()
	if got := strings.Count(view, "BACKFLASH // DISKURS-NOC"); got != 1 {
		t.Fatalf("dashboardrubriken visas %d gånger", got)
	}
	for _, line := range strings.Split(view, "\n") {
		if len([]rune(line)) > a.Width+2 {
			t.Fatalf("dashboardrad klipper utanför terminalbredden: %q", line)
		}
	}
}

func TestFormatSwedishEventTimeUsesActualMonth(t *testing.T) {
	value := time.Date(2026, time.August, 16, 9, 3, 0, 0, time.Local)
	if got := formatSwedishEventTime(value); got != "16 aug 09:03" {
		t.Fatalf("datum formatterade fel: %q", got)
	}
}

func TestRenderEventWindowDoesNotRenderWholeFeed(t *testing.T) {
	events := make([]external.ExternalEvent, 50)
	for i := range events {
		events[i].Title = "händelse"
	}
	got := renderEventWindow(events, 25, 5)
	if strings.Count(got, "händelse") != 5 {
		t.Fatalf("renderade %d händelser, väntade 5", strings.Count(got, "händelse"))
	}
}
