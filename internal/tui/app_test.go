package tui

import (
	"path/filepath"
	"strings"
	"testing"

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
