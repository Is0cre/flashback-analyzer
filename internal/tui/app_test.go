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

func TestCommandPaletteOpensAndCloses(t *testing.T) {
	a := New(nil, nil)
	model, _ := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	updated := model.(App)
	if !updated.PaletteOpen {
		t.Fatal("frågetecken öppnade inte kommandocentret")
	}
	if !strings.Contains(updated.View(), "KOMMANDOCENTER") {
		t.Fatal("kommandocentret renderades inte")
	}
	model, _ = updated.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if model.(App).PaletteOpen {
		t.Fatal("Esc stängde inte kommandocentret")
	}
}

func TestCommandPaletteOpensForum(t *testing.T) {
	a := New(nil, nil)
	model, _ := a.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	updated := model.(App)
	// Forum is the second item in the palette.
	model, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	model, _ = model.(App).Update(tea.KeyMsg{Type: tea.KeyEnter})
	if model.(App).CurrentView != ViewForums {
		t.Fatalf("kommandocentret öppnade inte forumvyn: %v", model.(App).CurrentView)
	}
}

func TestReaderUsesBoundedViewport(t *testing.T) {
	a := New(nil, nil)
	a.Width, a.Height = 80, 24
	a.resizePostViewport()
	a.Posts = make([]flashback.Post, 80)
	for i := range a.Posts {
		a.Posts[i] = flashback.Post{ID: string(rune('a' + i%26)), Author: "användare", Text: "ett långt inlägg som ska kunna scrollas"}
	}
	a.refreshPostViewport(true)
	if !a.PostViewportReady {
		t.Fatal("läsarens viewport initierades inte")
	}
	if a.PostViewport.Height != 16 {
		t.Fatalf("oväntad viewporthöjd: %d", a.PostViewport.Height)
	}
	if got := a.PostViewport.TotalLineCount(); got <= a.PostViewport.Height {
		t.Fatalf("viewporten innehåller inte en längre tråd: %d rader", got)
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

func TestGandrVaultResetIsReachableWithoutPassword(t *testing.T) {
	subsystem := gandr.NewAt(filepath.Join(t.TempDir(), "gandr", "identity.key"))
	if err := subsystem.Create("gammalt-losenord"); err != nil {
		t.Fatal(err)
	}
	a := New(nil, nil)
	a.CurrentView = ViewGandr
	a.Gandr = subsystem
	model, _ := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	updated := model.(App)
	if !updated.GandrDeleteConfirm || !updated.Input.Focused() {
		t.Fatal("x öppnade inte den gated valvraderingen")
	}
	if updated.Input.Placeholder != "Skriv RADERA för att bekräfta" {
		t.Fatalf("oväntad bekräftelsetext: %q", updated.Input.Placeholder)
	}
}

func TestGandrChatShowsUsersAndMeshPresence(t *testing.T) {
	var onlineID [32]byte
	onlineID[0] = 0x42
	var offlineID [32]byte
	offlineID[0] = 0x99
	a := New(nil, nil)
	a.CurrentView = ViewGandrChat
	a.Width = 120
	a.GandrContacts = []gandr.Contact{
		{Pubkey: onlineID, Name: "gröna katten"},
		{Pubkey: offlineID, Name: "gammal vän"},
	}
	a.GandrPeers = []gandr.Peer{{Identity: onlineID}}
	view := a.View()
	if !strings.Contains(view, "ANVÄNDARE") {
		t.Fatal("GANDR-vyn saknar användarpanel")
	}
	if !strings.Contains(view, "PÅ MESH") || !strings.Contains(view, "EJ PÅ MESH") {
		t.Fatalf("GANDR-vyn visar inte båda närvarolägena: %q", view)
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
