package tui

import (
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/backflash-cli/backflash/internal/external"
	"github.com/backflash-cli/backflash/internal/flashback"
	"github.com/backflash-cli/backflash/internal/gandr"
	"github.com/backflash-cli/backflash/internal/geo"
	"github.com/backflash-cli/backflash/internal/store"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
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

func TestPressingFResetsStaleForumBreadcrumb(t *testing.T) {
	// A leftover Stack from an earlier browse session must not leak into
	// the breadcrumb path once "f" restarts forum browsing from the root.
	s, err := store.Open(filepath.Join(t.TempDir(), "backflash.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	a := New(s, flashback.NewClient(flashback.AnonymousSession{}))
	a.Stack = []flashback.ForumNode{{ID: "1", Title: "Gammal rot"}, {ID: "2", Title: "Gammalt underforum"}}
	model, _ := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	updated := model.(App)
	if len(updated.Stack) != 0 {
		t.Fatalf("f rensade inte den gamla brödsmulestacken: %#v", updated.Stack)
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

func TestThreadWorkspaceShowsContextAndMetadata(t *testing.T) {
	a := New(nil, nil)
	a.Width = 120
	a.CurrentView = ViewThreads
	a.Stack = []flashback.ForumNode{{Title: "Dator och IT"}, {Title: "Retrospel"}}
	a.Threads = []flashback.ThreadSummary{{ID: "123", Title: "En riktig tråd", Replies: 12, Views: 3456, PageCount: 7}}
	view := a.View()
	for _, want := range []string{"FORUMTRÄD", "Dator och IT", "TRÅDAR", "En riktig tråd", "#123", "12 svar", "3456 visningar", "7 sidor", "DETALJER"} {
		if !strings.Contains(view, want) {
			t.Fatalf("trådsvyn saknar %q: %s", want, view)
		}
	}
	for _, line := range strings.Split(view, "\n") {
		if len([]rune(line)) > a.Width+2 {
			t.Fatalf("trådsvyn överskrider terminalbredden: %q", line)
		}
	}
}

func TestThreadWorkspaceKeepsForumPathOnLaptopWidth(t *testing.T) {
	a := New(nil, nil)
	a.Width = 90
	a.CurrentView = ViewThreads
	a.Stack = []flashback.ForumNode{{Title: "Dator och IT"}, {Title: "Dator- och konsolspel"}, {Title: "Retrospel"}}
	a.Threads = []flashback.ThreadSummary{{ID: "123", Title: "En riktig tråd", Replies: 12}}
	view := a.View()
	for _, want := range []string{"FORUMTRÄD", "Dator och IT", "Retrospel", "TRÅDAR", "En riktig tråd"} {
		if !strings.Contains(view, want) {
			t.Fatalf("laptop-layout saknar %q: %s", want, view)
		}
	}
}

func TestReaderWorkspaceKeepsForumAndThreadContext(t *testing.T) {
	a := New(nil, nil)
	a.Width, a.Height = 140, 40
	a.CurrentView = ViewReader
	a.Stack = []flashback.ForumNode{{Title: "Dator och IT"}, {Title: "Retrospel"}}
	a.Threads = []flashback.ThreadSummary{{ID: "123", Title: "En riktig tråd", Replies: 12}}
	a.ThreadID, a.ThreadTitle = "123", "En riktig tråd"
	a.Posts = []flashback.Post{{ID: "p1", Author: "läsare", Text: "Ett läsbart inlägg."}}
	view := a.View()
	for _, want := range []string{"FORUMTRÄD", "Dator och IT", "TRÅDAR", "En riktig tråd", "LÄSER", "Ett läsbart inlägg."} {
		if !strings.Contains(view, want) {
			t.Fatalf("läsarens arbetsyta saknar %q: %s", want, view)
		}
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

func TestGandrSidebarRowsStayAlignedWithClickActions(t *testing.T) {
	// Before this fix, the mouse handler hand-counted row offsets
	// separately from the render loop, and they had drifted apart:
	// clicking "+ skapa privat grupp" actually fired the join-channel
	// action, and clicking "[!] radera E2E-CHATT-valv" fired create-group.
	// gandrSidebarRows is now the single source both sides read from, so
	// lines and actions can never drift again — this pins that invariant.
	subsystem := gandr.NewAt(filepath.Join(t.TempDir(), "gandr", "identity.key"))
	if err := subsystem.Create("password"); err != nil {
		t.Fatal(err)
	}
	a := New(nil, nil)
	a.Gandr = subsystem
	a.GandrChannels = []gandr.Channel{{Name: "general"}, {Name: "support"}}
	a.GandrGroups = []gandr.PrivateGroup{{Name: "Hemlig grupp"}}

	lines, actions := gandrSidebarRows(a, 24)
	if len(lines) != len(actions) {
		t.Fatalf("lines (%d) och actions (%d) har olika längd", len(lines), len(actions))
	}
	// Index 2 and 3 are the two channel rows.
	if actions[2].Kind != gandrActionSelectChannel || actions[2].Index != 0 {
		t.Fatalf("rad 2 borde välja kanal 0: %#v", actions[2])
	}
	if actions[3].Kind != gandrActionSelectChannel || actions[3].Index != 1 {
		t.Fatalf("rad 3 borde välja kanal 1: %#v", actions[3])
	}
	// "+ skapa privat grupp" is the last of the four fixed rows after the
	// channel list.
	createGroupIdx := 3 + 1 + 3 // channels(2) done at idx3, then join/leave/createchannel/creategroup
	if actions[createGroupIdx].Kind != gandrActionCreateGroup {
		t.Fatalf("'+ skapa privat grupp' pekar inte på gandrActionCreateGroup: rad %d = %#v (%q)", createGroupIdx, actions[createGroupIdx], lines[createGroupIdx])
	}
	if !strings.Contains(lines[createGroupIdx], "skapa privat grupp") {
		t.Fatalf("index %d matchar inte den rad den påstår sig vara: %q", createGroupIdx, lines[createGroupIdx])
	}
	// The vault-delete row must line up with the delete action, not with
	// whatever the old hand-counted offset happened to land on.
	deleteIdx := createGroupIdx + 1
	if actions[deleteIdx].Kind != gandrActionDeleteVault {
		t.Fatalf("'[!] radera E2E-CHATT-valv' pekar inte på gandrActionDeleteVault: rad %d = %#v (%q)", deleteIdx, actions[deleteIdx], lines[deleteIdx])
	}
	if !strings.Contains(lines[deleteIdx], "radera E2E-CHATT-valv") {
		t.Fatalf("index %d matchar inte den rad den påstår sig vara: %q", deleteIdx, lines[deleteIdx])
	}
	// The one private group must be clickable too — this row didn't exist
	// in the old click handler at all.
	groupIdx := deleteIdx + 3 // blank + "PRIVATA GRUPPER" header + the group itself
	if actions[groupIdx].Kind != gandrActionOpenGroup || actions[groupIdx].Index != 0 {
		t.Fatalf("gruppraden är inte klickbar: rad %d = %#v (%q)", groupIdx, actions[groupIdx], lines[groupIdx])
	}
	if !strings.Contains(lines[groupIdx], "Hemlig grupp") {
		t.Fatalf("index %d matchar inte gruppraden: %q", groupIdx, lines[groupIdx])
	}
}

func TestGandrSidebarShowsUnreadBadgeOnlyForInactiveChannel(t *testing.T) {
	general := gandr.Channel{Name: "general"}
	general.ID = gandr.ChannelID("general")
	support := gandr.Channel{Name: "support"}
	support.ID = gandr.ChannelID("support")

	a := New(nil, nil)
	a.GandrChannels = []gandr.Channel{general, support}
	a.Cursor = 0 // viewing "general"
	a.GandrMessages = map[[32]byte][]gandr.Message{
		general.ID: {{Content: "a"}, {Content: "b"}}, // 2 messages, 0 marked read
		support.ID: {{Content: "c"}},                  // 1 message, 0 marked read
	}

	lines, _ := gandrSidebarRows(a, 24)
	var generalLine, supportLine string
	for _, line := range lines {
		if strings.Contains(line, "general") {
			generalLine = line
		}
		if strings.Contains(line, "support") {
			supportLine = line
		}
	}
	if strings.Contains(generalLine, "(") {
		t.Fatalf("den aktiva kanalen visade en oläst-badge: %q", generalLine)
	}
	if !strings.Contains(supportLine, "(1)") {
		t.Fatalf("den inaktiva kanalen med olästa meddelanden saknar badge: %q", supportLine)
	}
}

func TestGandrMarkChannelReadClearsUnreadBadge(t *testing.T) {
	var sender [32]byte
	sender[0] = 0x01
	channel := gandr.Channel{Name: "general"}
	channel.ID = gandr.ChannelID("general")

	a := New(nil, nil)
	a.CurrentView = ViewGandrChat
	a.GandrChannels = []gandr.Channel{channel}
	a.GandrMessages = map[[32]byte][]gandr.Message{
		channel.ID: {{Sender: sender, Content: "hej"}},
	}
	if got := gandrUnreadCount(a, channel.ID, len(a.GandrMessages[channel.ID])); got != 1 {
		t.Fatalf("förväntade 1 oläst innan markering, fick %d", got)
	}
	gandrMarkChannelRead(&a, channel.ID)
	if got := gandrUnreadCount(a, channel.ID, len(a.GandrMessages[channel.ID])); got != 0 {
		t.Fatalf("förväntade 0 olästa efter markering, fick %d", got)
	}
}

func TestAppendGandrMessageTrimsUnboundedInMemoryGrowth(t *testing.T) {
	channel := gandr.Channel{Name: "general"}
	channel.ID = gandr.ChannelID("general")

	a := New(nil, nil)
	a.GandrChannels = []gandr.Channel{channel}
	a.GandrReadCounts = map[[32]byte]int{channel.ID: gandrMaxMessagesPerConversation}

	for i := 0; i < gandrMaxMessagesPerConversation+50; i++ {
		appendGandrMessage(&a, gandr.Message{ChannelID: channel.ID, Content: fmt.Sprintf("m%d", i)})
	}

	if got := len(a.GandrMessages[channel.ID]); got != gandrMaxMessagesPerConversation {
		t.Fatalf("in-memory history grew unbounded: %d messages, want capped at %d", got, gandrMaxMessagesPerConversation)
	}
	// The oldest 50 were dropped, so the 50 that were "read" among them
	// no longer exist to be read — the read count must shrink to match,
	// not keep counting messages that aren't there anymore.
	if got := a.GandrReadCounts[channel.ID]; got != gandrMaxMessagesPerConversation-50 {
		t.Fatalf("read count wasn't adjusted for the trim: got %d, want %d", got, gandrMaxMessagesPerConversation-50)
	}
	// The newest message must still be the actual most recent one, not
	// something dropped by the trim.
	last := a.GandrMessages[channel.ID][len(a.GandrMessages[channel.ID])-1]
	if last.Content != fmt.Sprintf("m%d", gandrMaxMessagesPerConversation+49) {
		t.Fatalf("trim kept the wrong messages: last is %q", last.Content)
	}
}

func TestIncomingMessageForActiveChannelNeverShowsUnread(t *testing.T) {
	var sender [32]byte
	sender[0] = 0x02
	channel := gandr.Channel{Name: "general"}
	channel.ID = gandr.ChannelID("general")

	a := New(nil, nil)
	a.CurrentView = ViewGandrChat
	a.GandrChannels = []gandr.Channel{channel}
	a.Cursor = 0

	updated, _ := a.Update(gandrIncomingMsg{message: gandr.Message{ChannelID: channel.ID, Sender: sender, Content: "hej"}})
	got := updated.(App)
	if unread := gandrUnreadCount(got, channel.ID, len(got.GandrMessages[channel.ID])); unread != 0 {
		t.Fatalf("ett meddelande i den aktiva kanalen räknades som oläst: %d", unread)
	}
}

func TestGandrSidebarSeparatesDMsFromRealGroups(t *testing.T) {
	var peer [32]byte
	peer[0] = 0x11
	a := New(nil, nil)
	a.GandrGroups = []gandr.PrivateGroup{
		{Name: "Hemlig grupp"},
		{Name: "Bob", PeerPubkey: &peer},
	}

	lines, actions := gandrSidebarRows(a, 24)
	if len(lines) != len(actions) {
		t.Fatalf("lines (%d) och actions (%d) har olika längd", len(lines), len(actions))
	}
	joined := strings.Join(lines, "\n")
	dmHeader := strings.Index(joined, "DIREKTMEDDELANDEN")
	groupHeader := strings.Index(joined, "PRIVATA GRUPPER")
	bobLine := strings.Index(joined, "Bob")
	secretLine := strings.Index(joined, "Hemlig grupp")
	if dmHeader < 0 || groupHeader < 0 {
		t.Fatalf("saknar en av rubrikerna: %q", joined)
	}
	if !(dmHeader < bobLine && bobLine < groupHeader) {
		t.Fatalf("Bob (DM) hamnade inte under DIREKTMEDDELANDEN före PRIVATA GRUPPER: %q", joined)
	}
	if !(groupHeader < secretLine) {
		t.Fatalf("Hemlig grupp (riktig grupp) hamnade inte under PRIVATA GRUPPER: %q", joined)
	}
	for i, line := range lines {
		if strings.Contains(line, "Bob") {
			if actions[i].Kind != gandrActionOpenGroup || actions[i].Index != 1 {
				t.Fatalf("DM-raden pekar inte på rätt index i a.GandrGroups: %#v", actions[i])
			}
		}
	}
}

func TestClickingLockedGroupPromptsForPassword(t *testing.T) {
	var groupID [32]byte
	groupID[0] = 0x9
	a := New(nil, nil)
	a.CurrentView = ViewGandrChat
	a.GandrSession = &gandr.Session{} // zero-value: no group keys cached, i.e. everything reads as locked
	a.GandrGroups = []gandr.PrivateGroup{{ID: groupID, Name: "Hemlig grupp"}}

	model, _ := gandrHandleSidebarClick(a, gandrSidebarAction{Kind: gandrActionOpenGroup, Index: 0})
	updated := model.(App)
	if updated.GandrUnlockGroupID == nil || *updated.GandrUnlockGroupID != groupID {
		t.Fatal("klick på en låst grupp bad inte om lösenord")
	}
	if !updated.Input.Focused() || updated.Input.EchoMode != textinput.EchoPassword {
		t.Fatal("lösenordsfältet fick inte fokus/dold inmatning vid klick på låst grupp")
	}
	if updated.GandrActiveGroup != nil {
		t.Fatal("en låst grupp öppnades utan lösenord")
	}
}

func TestSlashConnectDispatchesToGandrSession(t *testing.T) {
	subsystem := gandr.NewAt(filepath.Join(t.TempDir(), "gandr", "identity.key"))
	if err := subsystem.Create("password"); err != nil {
		t.Fatal(err)
	}
	session, err := subsystem.Connect("") // no daemon in this test env — offline mode
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	a := New(nil, nil)
	a.CurrentView = ViewGandrChat
	a.GandrSession = session
	a.Input.Focus()
	a.Input.SetValue("/connect " + strings.Repeat("ab", 32))

	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("/connect gav inget kommando")
	}
	msg, ok := cmd().(gandrConnectMsg)
	if !ok {
		t.Fatalf("förväntade gandrConnectMsg, fick %#v", msg)
	}
	// No daemon running in this test, so the connect attempt itself fails —
	// what matters here is that /connect actually reached ConnectPeer
	// instead of being swallowed as an unrecognized command or a chat
	// message to a channel that doesn't exist.
	if msg.err == nil || !strings.Contains(msg.err.Error(), "gandrd") {
		t.Fatalf("förväntade ett gandrd-anslutningsfel, fick: %v", msg.err)
	}
}

func TestDefaultSeedYggdrasilKeyIsWellFormed(t *testing.T) {
	if defaultSeedYggdrasilKey == "" {
		t.Skip("no baked-in seed configured yet")
	}
	if len(defaultSeedYggdrasilKey) != 64 {
		t.Fatalf("defaultSeedYggdrasilKey should be 64 hex chars, got %d: %q", len(defaultSeedYggdrasilKey), defaultSeedYggdrasilKey)
	}
	if _, err := hex.DecodeString(defaultSeedYggdrasilKey); err != nil {
		t.Fatalf("defaultSeedYggdrasilKey is not valid hex: %v", err)
	}
}

func TestGandrSessionStartAutoConnectsToConfiguredSeed(t *testing.T) {
	subsystem := gandr.NewAt(filepath.Join(t.TempDir(), "gandr", "identity.key"))
	if err := subsystem.Create("password"); err != nil {
		t.Fatal(err)
	}
	session, err := subsystem.Connect("") // no daemon in this test env — offline mode
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	a := New(nil, nil)
	a.SeedYggdrasilKey = strings.Repeat("ab", 32)

	_, cmd := a.Update(gandrSessionMsg{session: session, channels: nil, offline: false})
	if cmd == nil {
		t.Fatal("förväntade ett kommando efter sessionsstart")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("förväntade en batch av kommandon efter sessionsstart, fick %#v", cmd())
	}
	found := false
	for _, c := range batch {
		if _, ok := c().(seedConnectMsg); ok {
			found = true
		}
	}
	if !found {
		t.Fatal("seed-anslutningen kördes inte automatiskt när en seed-nyckel är konfigurerad")
	}
}

func TestGandrSessionStartSkipsSeedConnectWithoutConfiguredSeed(t *testing.T) {
	subsystem := gandr.NewAt(filepath.Join(t.TempDir(), "gandr", "identity.key"))
	if err := subsystem.Create("password"); err != nil {
		t.Fatal(err)
	}
	session, err := subsystem.Connect("")
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	a := New(nil, nil)
	a.SeedYggdrasilKey = ""

	_, cmd := a.Update(gandrSessionMsg{session: session, channels: nil, offline: false})
	if cmd == nil {
		t.Fatal("förväntade fortfarande waitGandrIncoming/refreshGandrPeers")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		return // a single non-batch cmd cannot be a seed connect either
	}
	for _, c := range batch {
		if _, ok := c().(seedConnectMsg); ok {
			t.Fatal("seed-anslutning kördes trots att ingen seed är konfigurerad")
		}
	}
}

func TestGandrSessionStartSkipsSeedConnectWhenOffline(t *testing.T) {
	subsystem := gandr.NewAt(filepath.Join(t.TempDir(), "gandr", "identity.key"))
	if err := subsystem.Create("password"); err != nil {
		t.Fatal(err)
	}
	session, err := subsystem.Connect("")
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	a := New(nil, nil)
	a.SeedYggdrasilKey = strings.Repeat("ab", 32)

	_, cmd := a.Update(gandrSessionMsg{session: session, channels: nil, offline: true})
	if cmd == nil {
		t.Fatal("förväntade fortfarande waitGandrIncoming/refreshGandrPeers")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		return
	}
	for _, c := range batch {
		if _, ok := c().(seedConnectMsg); ok {
			t.Fatal("seed-anslutning kördes trots offline-läge (ingen gandrd att koppla via)")
		}
	}
}

func TestOfflineFallbackSurfacesWhyTheEmbeddedDaemonFailed(t *testing.T) {
	subsystem := gandr.NewAt(filepath.Join(t.TempDir(), "gandr", "identity.key"))
	if err := subsystem.Create("password"); err != nil {
		t.Fatal(err)
	}
	session, err := subsystem.Connect("")
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	a := New(nil, nil)
	got, _ := a.Update(gandrSessionMsg{
		session:       session,
		offline:       true,
		offlineReason: errors.New("disk full, could not write objects"),
	})
	status := got.(App).Status
	if !strings.Contains(status, "disk full, could not write objects") {
		t.Fatalf("statusraden döljer den faktiska felorsaken: %q", status)
	}
	if strings.Contains(status, "starta gandrd separat") {
		t.Fatalf("statusraden ger fortfarande föråldrade råd om en separat gandrd-process: %q", status)
	}
}

func TestClickingUnlockedGroupOpensItDirectly(t *testing.T) {
	subsystem := gandr.NewAt(filepath.Join(t.TempDir(), "gandr", "identity.key"))
	if err := subsystem.Create("password"); err != nil {
		t.Fatal(err)
	}
	session, err := subsystem.Connect("")
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	group, err := session.CreatePrivateGroup("Hemlig grupp", "grupplosen")
	if err != nil {
		t.Fatal(err)
	}
	// CreatePrivateGroup leaves the new group's key cached in this session
	// (IsGroupUnlocked == true), matching "I just made it, of course it's
	// still open" — clicking it again should not re-ask for the password.

	a := New(nil, nil)
	a.CurrentView = ViewGandrChat
	a.GandrSession = session
	a.GandrGroups = []gandr.PrivateGroup{group}

	model, cmd := gandrHandleSidebarClick(a, gandrSidebarAction{Kind: gandrActionOpenGroup, Index: 0})
	updated := model.(App)
	if updated.GandrUnlockGroupID != nil {
		t.Fatal("en redan upplåst grupp bad om lösenord igen")
	}
	if cmd == nil {
		t.Fatal("förväntade ett kommando som öppnar den upplåsta gruppen")
	}
	msg, ok := cmd().(gandrGroupMsg)
	if !ok || msg.err != nil || msg.active == nil || *msg.active != group.ID {
		t.Fatalf("kommandot öppnade inte rätt grupp: %#v", msg)
	}
}

func TestTypingPasswordAfterClickingLockedGroupUnlocksIt(t *testing.T) {
	// End-to-end: create a group, then reconnect a fresh session against
	// the same identity (its key cache starts empty, so the group reads as
	// locked again) — the click-then-type-password flow must still unlock
	// and open the right group.
	identityPath := filepath.Join(t.TempDir(), "gandr", "identity.key")
	subsystem := gandr.NewAt(identityPath)
	if err := subsystem.Create("password"); err != nil {
		t.Fatal(err)
	}
	setupSession, err := subsystem.Connect("")
	if err != nil {
		t.Fatal(err)
	}
	group, err := setupSession.CreatePrivateGroup("Hemlig grupp", "grupplosen")
	if err != nil {
		t.Fatal(err)
	}
	setupSession.Close()

	freshSession, err := subsystem.Connect("")
	if err != nil {
		t.Fatal(err)
	}
	defer freshSession.Close()
	if freshSession.IsGroupUnlocked(group.ID) {
		t.Fatal("en nyansluten session ska inte redan ha gruppens nyckel")
	}

	a := New(nil, nil)
	a.CurrentView = ViewGandrChat
	a.GandrSession = freshSession
	a.GandrGroups = []gandr.PrivateGroup{group}

	model, _ := gandrHandleSidebarClick(a, gandrSidebarAction{Kind: gandrActionOpenGroup, Index: 0})
	a = model.(App)
	if a.GandrUnlockGroupID == nil {
		t.Fatal("klicket satte inte upp lösenordsprompten")
	}

	a.Input.SetValue("grupplosen")
	model, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated := model.(App)
	if updated.GandrUnlockGroupID != nil {
		t.Fatal("GandrUnlockGroupID rensades inte efter Enter")
	}
	if cmd == nil {
		t.Fatal("förväntade ett upplåsningskommando")
	}
	msg, ok := cmd().(gandrGroupMsg)
	if !ok || msg.err != nil {
		t.Fatalf("upplåsningen misslyckades: %#v", msg)
	}
	if msg.active == nil || *msg.active != group.ID {
		t.Fatalf("fel grupp öppnades: %#v", msg)
	}
}

func TestGandrChatShowsPresenceForOnlinePeersWithNoSavedName(t *testing.T) {
	var knownID [32]byte
	knownID[0] = 0x10
	var strangerID [32]byte
	strangerID[0] = 0xAB
	strangerID[1] = 0xCD

	a := New(nil, nil)
	a.CurrentView = ViewGandrChat
	a.Width = 120
	a.GandrContacts = []gandr.Contact{{Pubkey: knownID, Name: "känd vän"}}
	a.GandrPeers = []gandr.Peer{{Identity: knownID}, {Identity: strangerID}}

	view := a.View()
	if !strings.Contains(view, "OKÄNDA") {
		t.Fatalf("vyn saknar OKÄNDA-sektionen för en online-peer utan sparat namn: %q", view)
	}
	if !strings.Contains(view, hex.EncodeToString(strangerID[:4])) {
		t.Fatalf("vyn visar inte fingeravtrycket för den okända peern: %q", view)
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

func TestGandrChatMessageLogShowsSavedPetnameNotRawHex(t *testing.T) {
	var sender [32]byte
	sender[0] = 0x42
	channel := gandr.Channel{Name: "general"}
	channel.ID = gandr.ChannelID("general")

	a := New(nil, nil)
	a.CurrentView = ViewGandrChat
	a.Width = 120
	a.GandrChannels = []gandr.Channel{channel}
	a.GandrContacts = []gandr.Contact{{Pubkey: sender, Name: "gröna katten"}}
	a.GandrMessages = map[[32]byte][]gandr.Message{
		channel.ID: {{Sender: sender, Content: "hej"}},
	}

	view := a.View()
	if !strings.Contains(view, "gröna katten") {
		t.Fatalf("meddelandeloggen visade inte det sparade smeknamnet: %q", view)
	}
	if strings.Contains(view, hex.EncodeToString(sender[:4])) {
		t.Fatalf("meddelandeloggen visade rå hex trots sparat smeknamn: %q", view)
	}
}

func TestGandrChatShowsWelcomeBannerAndPersistentInputBox(t *testing.T) {
	a := New(nil, nil)
	a.CurrentView = ViewGandrChat
	a.Width = 120
	a.GandrChannels = []gandr.Channel{{Name: "general"}}
	view := a.View()
	if !strings.Contains(view, "Välkommen till #general") {
		t.Fatalf("kanalvyn saknar välkomstbanner: %q", view)
	}
	// The input box must render even though nothing focused it — that's
	// the whole point of a persistent input area instead of one that
	// only appears once you start typing.
	if !strings.Contains(view, "Enter för att skriva") {
		t.Fatalf("kanalvyn saknar det permanenta skrivfältet när inget är fokuserat: %q", view)
	}
}

func TestGandrChatShowsPeerCountNotJustAGreenDot(t *testing.T) {
	// A real (but bootstrap-peer-less) embedded session: Online() is
	// true — there's a real client — but it has zero actual peers,
	// exactly the "green dot, nothing reachable" state that was
	// invisible before and cost a very long debugging session to
	// diagnose from the outside.
	subsystem := gandr.NewAt(filepath.Join(t.TempDir(), "gandr", "identity.key"))
	if err := subsystem.Create("password"); err != nil {
		t.Fatal(err)
	}
	session, err := subsystem.ConnectEmbedded(gandr.EmbeddedOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if !session.Online() {
		t.Fatal("en embedded session ska alltid rapportera Online")
	}

	a := New(nil, nil)
	a.CurrentView = ViewGandrChat
	a.Width = 120
	a.GandrSession = session
	a.GandrChannels = []gandr.Channel{{Name: "general"}}
	a.GandrPeers = nil

	view := a.View()
	if !strings.Contains(view, "0 peers — ingen ansluten ännu") {
		t.Fatalf("visade inte att nätverket är uppe men utan peers: %q", view)
	}

	a.GandrPeers = []gandr.Peer{{Identity: [32]byte{1}}}
	view = a.View()
	if !strings.Contains(view, "NÄTVERK · 1 peer") {
		t.Fatalf("visade inte peer-antalet när en peer finns: %q", view)
	}
}

func TestGandrMessageWindowAnchorsToAFixedPointAsNewMessagesArrive(t *testing.T) {
	// Scrolled back 5 from a 20-message history: window should be
	// [start=20-5-18=-3->0, end=15) i.e. the oldest 15 messages.
	start, end, scrolled := gandrMessageWindow(20, 5)
	if start != 0 || end != 15 || !scrolled {
		t.Fatalf("got start=%d end=%d scrolled=%v, want 0,15,true", start, end, scrolled)
	}
	// Two more messages arrive (total 22) with the same scrollBack: the
	// window should still end 5 messages before the new live end (17),
	// i.e. still showing the same messages the user was looking at, not
	// jumping to show two different, newer ones.
	start, end, scrolled = gandrMessageWindow(22, 5)
	if start != 0 || end != 17 || !scrolled {
		t.Fatalf("got start=%d end=%d scrolled=%v, want 0,17,true", start, end, scrolled)
	}
}

func TestGandrMessageWindowPinnedToLiveEndByDefault(t *testing.T) {
	start, end, scrolled := gandrMessageWindow(30, 0)
	if start != 12 || end != 30 || scrolled {
		t.Fatalf("got start=%d end=%d scrolled=%v, want 12,30,false", start, end, scrolled)
	}
}

func TestGandrMessageWindowClampsScrollBackToHistoryLength(t *testing.T) {
	start, end, scrolled := gandrMessageWindow(5, 1000)
	if start != 0 || end != 0 || !scrolled {
		t.Fatalf("got start=%d end=%d scrolled=%v, want 0,0,true", start, end, scrolled)
	}
}

func TestPgupScrollsGandrChatHistoryAndPgdownReturnsToLive(t *testing.T) {
	channel := gandr.Channel{Name: "general"}
	channel.ID = gandr.ChannelID("general")
	var messages []gandr.Message
	for i := 0; i < 25; i++ {
		messages = append(messages, gandr.Message{Content: fmt.Sprintf("m%d", i), Local: true})
	}

	a := New(nil, nil)
	a.CurrentView = ViewGandrChat
	a.GandrChannels = []gandr.Channel{channel}
	a.GandrMessages = map[[32]byte][]gandr.Message{channel.ID: messages}

	updated, _ := a.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	got := updated.(App)
	if got.GandrScrollBack != gandrScrollPageSize {
		t.Fatalf("pgup gav GandrScrollBack=%d, want %d", got.GandrScrollBack, gandrScrollPageSize)
	}

	updated, _ = got.Update(tea.KeyMsg{Type: tea.KeyPgDown})
	got = updated.(App)
	if got.GandrScrollBack != 0 {
		t.Fatalf("pgdown gav GandrScrollBack=%d, want 0", got.GandrScrollBack)
	}
}

func TestGandrSenderStyleIsStableAndSelfIsDistinct(t *testing.T) {
	var alice [32]byte
	alice[0] = 0x11
	first := gandrSenderStyle(alice, false)
	second := gandrSenderStyle(alice, false)
	if first.GetForeground() != second.GetForeground() {
		t.Fatal("samma avsändare fick olika färg mellan två anrop — namnfärgen måste vara stabil")
	}
	self := gandrSenderStyle([32]byte{}, true)
	if self.GetForeground() == first.GetForeground() {
		t.Fatal("egna meddelanden fick samma färg som en annan avsändares hash-baserade färg av en slump — bör vara en fast, skild stil")
	}
}

func TestPressingNOpensUserMenuForLastSpeakerAndRenameOptionPrefillsAddContact(t *testing.T) {
	var sender [32]byte
	sender[0] = 0xAB
	sender[1] = 0xCD
	channel := gandr.Channel{Name: "general"}
	channel.ID = gandr.ChannelID("general")

	a := New(nil, nil)
	a.CurrentView = ViewGandrChat
	a.GandrChannels = []gandr.Channel{channel}
	a.GandrMessages = map[[32]byte][]gandr.Message{
		channel.ID: {
			{Sender: [32]byte{}, Content: "mitt eget meddelande", Local: true},
			{Sender: sender, Content: "hej där"},
		},
	}

	updated, _ := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	got := updated.(App)
	if got.GandrUserMenu == nil || got.GandrUserMenu.Sender != sender {
		t.Fatal("n-genvägen öppnade inte användarmenyn för den senaste avsändaren")
	}

	updated, _ = got.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	got = updated.(App)
	if !got.GandrAddMode {
		t.Fatal("menyvalet 'Byt smeknamn' aktiverade inte lägg-till-läge")
	}
	wantPrefix := hex.EncodeToString(sender[:]) + " "
	if got.Input.Value() != wantPrefix {
		t.Fatalf("inmatningen förifylldes fel: got %q, want %q", got.Input.Value(), wantPrefix)
	}
	if !got.Input.Focused() {
		t.Fatal("inmatningsfältet fokuserades inte")
	}
	if got.GandrUserMenu != nil {
		t.Fatal("menyn stängdes inte efter valet")
	}
}

func TestClickingAMessageOpensUserMenuForThatSender(t *testing.T) {
	var sender [32]byte
	sender[0] = 0xEF
	channel := gandr.Channel{Name: "general"}
	channel.ID = gandr.ChannelID("general")

	subsystem := gandr.NewAt(filepath.Join(t.TempDir(), "gandr", "identity.key"))
	if err := subsystem.Create("password"); err != nil {
		t.Fatal(err)
	}
	session, err := subsystem.ConnectEmbedded(gandr.EmbeddedOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()

	a := New(nil, nil)
	a.CurrentView = ViewGandrChat
	a.Width = 80
	a.GandrSession = session
	a.GandrChannels = []gandr.Channel{channel}
	a.GandrMessages = map[[32]byte][]gandr.Message{
		channel.ID: {
			{Sender: [32]byte{}, Content: "mitt eget meddelande", Local: true},
			{Sender: sender, Content: "hej där"},
		},
	}

	// Row 0 (Y=7) is the self message — clicking it must not open a menu
	// naming yourself.
	updated, _ := a.Update(tea.MouseMsg{X: 30, Y: 7, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	got := updated.(App)
	if got.GandrUserMenu != nil {
		t.Fatal("klick på ett eget meddelande öppnade en användarmeny")
	}

	// Row 1 (Y=8) is the other sender's message.
	updated, _ = got.Update(tea.MouseMsg{X: 30, Y: 8, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	got = updated.(App)
	if got.GandrUserMenu == nil || got.GandrUserMenu.Sender != sender {
		t.Fatal("klick på meddelandet öppnade inte menyn för rätt avsändare")
	}
}

func TestUserMenuCopyPublicKeyOptionClosesMenuWithoutTouchingInput(t *testing.T) {
	var sender [32]byte
	sender[0] = 0x11
	a := New(nil, nil)
	a.CurrentView = ViewGandrChat
	a.GandrUserMenu = &gandrUserMenuState{Sender: sender, Cursor: 2}

	updated, _ := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(App)
	if got.GandrUserMenu != nil {
		t.Fatal("menyn stängdes inte efter 'kopiera publik nyckel'")
	}
	if got.GandrAddMode {
		t.Fatal("kopiera publik nyckel skulle inte aktivera lägg-till-läge")
	}
}

func TestUserMenuEscCancelsWithoutSideEffects(t *testing.T) {
	var sender [32]byte
	sender[0] = 0x22
	a := New(nil, nil)
	a.CurrentView = ViewGandrChat
	a.GandrUserMenu = &gandrUserMenuState{Sender: sender}

	updated, _ := a.Update(tea.KeyMsg{Type: tea.KeyEsc})
	got := updated.(App)
	if got.GandrUserMenu != nil {
		t.Fatal("esc stängde inte användarmenyn")
	}
}

func TestGandrPresenceMarkerIsColorCoded(t *testing.T) {
	original := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(original)

	var onlineID [32]byte
	onlineID[0] = 0x42
	a := New(nil, nil)
	a.CurrentView = ViewGandrChat
	a.Width = 120
	a.GandrRightCursor = -1 // not this row, so the plain (non-selected) marker branch renders
	a.GandrContacts = []gandr.Contact{{Pubkey: onlineID, Name: "gröna katten"}}
	a.GandrPeers = []gandr.Peer{{Identity: onlineID}}
	view := a.View()
	if !strings.Contains(view, online.Render("●")) {
		t.Fatalf("den nätanslutna markören är inte färgkodad grön: %q", view)
	}
}

func TestRenderNodesMutesCategoryNodesButNotForums(t *testing.T) {
	original := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(original)

	nodes := []flashback.ForumNode{
		{ID: "category:x", Title: "En kategori", Browsable: false},
		{ID: "1", Title: "Ett riktigt forum", Browsable: true},
	}
	got := renderNodes(nodes, -1)
	if !strings.Contains(got, muted.Render("En kategori")) {
		t.Fatalf("kategori-noden är inte nedtonad: %q", got)
	}
	if strings.Contains(got, muted.Render("Ett riktigt forum")) {
		t.Fatalf("ett riktigt forum ska inte nedtonas som en kategori: %q", got)
	}
}

func TestPressingBLeavesAnOpenPrivateGroup(t *testing.T) {
	// Before this fix, "b" — the same key used to go back everywhere else
	// in the app — was a hard no-op inside ViewGandrChat, so once a private
	// group was opened there was no way to leave it again.
	a := New(nil, nil)
	a.CurrentView = ViewGandrChat
	a.GandrSession = &gandr.Session{}
	var groupID [32]byte
	groupID[0] = 0x7
	a.GandrActiveGroup = &groupID
	model, _ := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	updated := model.(App)
	if updated.GandrActiveGroup != nil {
		t.Fatal("b lämnade inte den öppna privata gruppen")
	}
	if updated.CurrentView != ViewGandrChat {
		t.Fatalf("b borde stanna kvar i kanalvyn, fick %v", updated.CurrentView)
	}
}

func TestNavigatingHomeKeepsGandrSessionAlive(t *testing.T) {
	a := New(nil, nil)
	a.CurrentView = ViewGandrChat
	a.GandrSession = &gandr.Session{}
	model, _ := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	updated := model.(App)
	if updated.GandrSession == nil {
		t.Fatal("GANDR-sessionen stängdes vid hemnavigering, den ska förbli uppkopplad i bakgrunden")
	}
	if updated.CurrentView != ViewOverview {
		t.Fatalf("förväntade ViewOverview efter h, fick %v", updated.CurrentView)
	}
}

func TestReenteringGandrWithLiveSessionSkipsPasswordPrompt(t *testing.T) {
	a := New(nil, nil)
	a.CurrentView = ViewOverview
	a.GandrSession = &gandr.Session{}
	model, _ := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	updated := model.(App)
	if updated.CurrentView != ViewGandrChat {
		t.Fatalf("en redan upplåst session ska gå direkt till chatten, fick vy %v", updated.CurrentView)
	}
	if updated.Input.Focused() {
		t.Fatal("lösenordsfältet fick fokus trots att GANDR-sessionen redan var uppkopplad")
	}
}

func paletteIndex(t *testing.T, key string) int {
	t.Helper()
	for i, item := range paletteItems {
		if item.Key == key {
			return i
		}
	}
	t.Fatalf("inget kommandopalett-objekt med nyckel %q", key)
	return -1
}

func TestPaletteHomeKeepsGandrSessionAlive(t *testing.T) {
	a := New(nil, nil)
	a.CurrentView = ViewGandrChat
	a.GandrSession = &gandr.Session{}
	a.PaletteCursor = paletteIndex(t, "h")
	model, _ := a.runPaletteItem()
	updated := model.(App)
	if updated.GandrSession == nil {
		t.Fatal("kommandopalettens hem-åtgärd stängde GANDR-sessionen")
	}
}

func TestPaletteGandrWithLiveSessionSkipsPasswordPrompt(t *testing.T) {
	a := New(nil, nil)
	a.CurrentView = ViewOverview
	a.GandrSession = &gandr.Session{}
	a.PaletteCursor = paletteIndex(t, "g")
	model, _ := a.runPaletteItem()
	updated := model.(App)
	if updated.CurrentView != ViewGandrChat {
		t.Fatalf("kommandopaletten borde gå direkt till chatten med aktiv session, fick vy %v", updated.CurrentView)
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

func TestDashboardShowsLiveGandrStateNotHardcodedLocked(t *testing.T) {
	subsystem := gandr.NewAt(filepath.Join(t.TempDir(), "gandr", "identity.key"))
	if err := subsystem.Create("dashboard-test-passphrase"); err != nil {
		t.Fatal(err)
	}
	a := New(nil, nil)
	a.Gandr = subsystem
	a.Width = 128
	view := a.View()
	if !strings.Contains(view, "UPPLÅST") {
		t.Fatalf("dashboarden visade inte det upplåsta GANDR-läget: %s", view)
	}
	if strings.Contains(view, "ᚷ           "+string(gandr.Locked)) {
		t.Fatal("dashboarden visade GANDR som låst trots att valvet är upplåst")
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

func TestPoliceCategoryIndexClassifiesRealEventTypes(t *testing.T) {
	cases := map[string]string{
		"Trafikkontroll":               "TRAFIK",
		"Trafikolycka, vilt":           "TRAFIK",
		"Trafikolycka, smitning från":  "TRAFIK",
		"Mord/dråp":                    "VÅLD",
		"Olaga hot":                    "VÅLD",
		"Narkotikabrott":               "MISSBRUK",
		"Fylleri":                      "MISSBRUK",
		"Skadegörelse":                 "EGENDOM",
		"Räddningsinsats":              "RÄDDNING",
		"Försvunnen person":            "RÄDDNING",
		"Anträffad död":                "RÄDDNING",
		"Brand":                        "RÄDDNING",
		"Sammanfattning natt":          "RUTIN",
		"Kontroll":                     "RUTIN",
		"Något helt okänt händelsetyp": "ÖVRIGT",
	}
	for input, want := range cases {
		got := policeCategories[policeCategoryIndex(input)].Label
		if got != want {
			t.Errorf("policeCategoryIndex(%q) = %s, want %s", input, got, want)
		}
	}
}

func TestRenderPoliceMapShowsLegendWithCategoryCounts(t *testing.T) {
	lat1, lon1 := 59.33, 18.06
	lat2, lon2 := 57.71, 11.97
	events := []external.ExternalEvent{
		{EventType: "Trafikkontroll", Latitude: &lat1, Longitude: &lon1},
		{EventType: "Trafikkontroll", Latitude: &lat1, Longitude: &lon1},
		{EventType: "Mord/dråp", Latitude: &lat2, Longitude: &lon2},
	}
	got := renderSwedenMap(events, 60, 16, -1)
	if !strings.Contains(got, "TRAFIK") || !strings.Contains(got, "VÅLD") {
		t.Fatalf("kartlegenden saknar kategorierna: %s", got)
	}
	if !strings.Contains(got, "2") {
		t.Fatalf("kartlegenden visar inte antalet per kategori: %s", got)
	}
}

func TestRenderPoliceWorkspacePlacesMapBesideListOnWideTerminals(t *testing.T) {
	lat, lon := 59.33, 18.06
	events := []external.ExternalEvent{{EventType: "Mord/dråp", Latitude: &lat, Longitude: &lon, LocationName: "Stockholm"}}
	a := App{Events: events, Cursor: 0, Width: 140, Height: 40}
	got := renderPoliceWorkspace(a)
	lines := strings.Split(got, "\n")
	found := false
	for _, line := range lines {
		if strings.Contains(line, "Stockholm") && (strings.ContainsRune(line, '▀') || strings.ContainsRune(line, '▄')) {
			found = true
		}
	}
	if !found {
		t.Fatalf("förväntade listan och kartan på samma rad på en bred terminal: %s", got)
	}
}

func TestRenderPoliceWorkspaceStacksOnNarrowTerminals(t *testing.T) {
	lat, lon := 59.33, 18.06
	events := []external.ExternalEvent{{EventType: "Mord/dråp", Latitude: &lat, Longitude: &lon, LocationName: "Stockholm"}}
	a := App{Events: events, Cursor: 0, Width: 70, Height: 40}
	got := renderPoliceWorkspace(a)
	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "Stockholm") && (strings.ContainsRune(line, '▀') || strings.ContainsRune(line, '▄')) {
			t.Fatalf("väntade en staplad layout på en smal terminal, fick en kombinerad rad: %s", got)
		}
	}
}

func TestCheckProximityAlertsSkipsBaselineBatchButAlertsOnNewNearbyEvent(t *testing.T) {
	stockholm := geo.Location{Latitude: 59.33, Longitude: 18.06}
	lat, lon := 59.33, 18.06 // right on top of the user
	a := App{AlertRadiusKM: 10, UserLocation: &stockholm}

	nearby := external.ExternalEvent{Source: "polisen", ExternalID: "1", EventType: "Stöld", LocationName: "Stockholm", Latitude: &lat, Longitude: &lon}
	a = a.checkProximityAlerts([]external.ExternalEvent{nearby})
	if a.ActiveAlert != nil {
		t.Fatal("den första händelsebatchen (startup-cachen) larmade trots att den bara ska sätta baslinjen")
	}

	fresh := external.ExternalEvent{Source: "polisen", ExternalID: "2", EventType: "Rån", LocationName: "Stockholm", Latitude: &lat, Longitude: &lon}
	a = a.checkProximityAlerts([]external.ExternalEvent{nearby, fresh})
	if a.ActiveAlert == nil || a.ActiveAlert.ExternalID != "2" {
		t.Fatalf("förväntade larm för den nya händelsen inom radien, fick %+v", a.ActiveAlert)
	}
}

func TestCheckProximityAlertsIgnoresEventsOutsideRadius(t *testing.T) {
	stockholm := geo.Location{Latitude: 59.33, Longitude: 18.06}
	lat, lon := 57.71, 11.97 // Göteborg — ~400km away
	a := App{AlertRadiusKM: 10, UserLocation: &stockholm}

	a = a.checkProximityAlerts([]external.ExternalEvent{{Source: "polisen", ExternalID: "1", Latitude: &lat, Longitude: &lon}})
	far := external.ExternalEvent{Source: "polisen", ExternalID: "2", Latitude: &lat, Longitude: &lon}
	a = a.checkProximityAlerts([]external.ExternalEvent{far})
	if a.ActiveAlert != nil {
		t.Fatalf("en händelse ~400km bort med en 10km-radie ska inte larma, fick %+v", a.ActiveAlert)
	}
}

func TestCheckProximityAlertsIsNoopWithoutLocationOrRadius(t *testing.T) {
	lat, lon := 59.33, 18.06
	events := []external.ExternalEvent{{Source: "polisen", ExternalID: "1", Latitude: &lat, Longitude: &lon}}

	a := App{}
	a = a.checkProximityAlerts(events)
	if a.AlertedEventIDs != nil || a.AlertBaseline {
		t.Fatal("checkProximityAlerts ska inte göra något alls när larm inte är aktiverat")
	}
}

func TestPressingAAcknowledgesActiveAlert(t *testing.T) {
	a := App{ActiveAlert: &external.ExternalEvent{EventType: "Stöld"}}
	updated, _ := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	got := updated.(App)
	if got.ActiveAlert != nil {
		t.Fatal("tryck på a kvitterade inte det aktiva larmet")
	}
}

func TestJoinPanelsPadsShorterColumnsSoLaterColumnsStayAligned(t *testing.T) {
	// A panel much shorter than its neighbors (e.g. a short forum path next
	// to a long thread list) must not cause every row below its own height
	// to collapse leftward — each row's total width must stay constant.
	short := []string{"AAAA", "BBBB"}
	tall := []string{"CCCCCC", "DDDDDD", "EEEEEE", "FFFFFF"}
	got := joinPanels(short, tall)
	lines := strings.Split(got, "\n")
	if len(lines) != 4 {
		t.Fatalf("got %d rows, want 4: %q", len(lines), got)
	}
	wantWidth := len("AAAA") + len("   ") + len("CCCCCC")
	for i, line := range lines {
		if w := ansi.StringWidth(line); w != wantWidth {
			t.Fatalf("row %d bredd %d, väntade %d (kolumnerna glider isär): %q", i, w, wantWidth, line)
		}
	}
	if !strings.HasSuffix(strings.TrimRight(lines[2], " "), "EEEEEE") {
		t.Fatalf("rad under den kortare panelens slut tappade sin kolumn-offset: %q", lines[2])
	}
}

func TestThreadListWindowSkipsHighlightForNegativeCursor(t *testing.T) {
	// Tests run without a tty, so lipgloss normally strips styling; force an
	// ANSI profile here so selected.Render actually emits escape codes and
	// the highlight behavior is observable.
	original := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(original)

	threads := []flashback.ThreadSummary{{ID: "1", Title: "Första"}, {ID: "2", Title: "Andra"}}
	// ThreadCursor is set to -1 when a thread is opened from remote search,
	// meaning "no local list position" rather than "select row zero" — the
	// window must not apply the *selection* highlight to any row in that
	// state. Rows are still styled either way now (bold titles, colored
	// metadata fields), so this checks for the selection style specifically
	// rather than "no ANSI at all".
	unhighlighted := strings.Join(threadListWindow(threads, -1, 40, 4), "\n")
	if strings.Contains(unhighlighted, selected.Render("Första")) {
		t.Fatalf("negativ cursor markerade en rad med markeringsstil: %q", unhighlighted)
	}
	highlighted := strings.Join(threadListWindow(threads, 0, 40, 4), "\n")
	if !strings.Contains(highlighted, selected.Render("  1  Första")) {
		t.Fatalf("cursor 0 borde markera raden med markeringsstil: %q", highlighted)
	}
}

func TestHighlightURLsStylesLinkButExcludesTrailingPunctuation(t *testing.T) {
	// Without a forced color profile, Render() is a no-op in this
	// (non-tty) test environment, which would make a styled span
	// indistinguishable from a same-length plain substring below.
	original := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(original)

	got := highlightURLs("Källa: https://example.com/path?a=1, läs mer.")
	if !strings.Contains(got, linkStyle.Render("https://example.com/path?a=1")) {
		t.Fatalf("URL:en fick inte länkstil: %q", got)
	}
	if strings.Contains(got, linkStyle.Render("https://example.com/path?a=1,")) {
		t.Fatalf("det avslutande kommat räknades felaktigt in i länken: %q", got)
	}
	if !strings.HasSuffix(strings.TrimSpace(got), "läs mer.") {
		t.Fatalf("texten efter URL:en tappades: %q", got)
	}
}

func TestThreadMetaLineColoredGivesEachFieldItsOwnColor(t *testing.T) {
	n := flashback.ThreadSummary{ID: "123", Replies: 5, Views: 900, PageCount: 3}
	got := threadMetaLineColored(n)
	if !strings.Contains(got, metadata.Render("#123")) {
		t.Fatalf("trådnumret fick inte metadata-färgen: %q", got)
	}
	if !strings.Contains(got, accent.Render("5 svar")) {
		t.Fatalf("svarsantalet fick inte accent-färgen: %q", got)
	}
	if !strings.Contains(got, titleStyle.Render("900 visningar")) {
		t.Fatalf("visningsantalet fick inte titleStyle-färgen: %q", got)
	}
	if !strings.Contains(got, sectionStyle.Render("3 sidor")) {
		t.Fatalf("sidantalet fick inte sectionStyle-färgen: %q", got)
	}
}

func TestPaletteHasNoPinkOrPurple(t *testing.T) {
	// ANSI-256 pink/magenta (200-207, incl. the old brand color 205) and
	// purple/lavender (129-135, 141-183 band, incl. the old sectionStyle
	// color 141) must not reappear in the shared style palette.
	banned := map[string]bool{"205": true, "141": true, "129": true, "165": true, "201": true, "207": true}
	for name, style := range map[string]lipgloss.Style{
		"brand": brand, "titleStyle": titleStyle, "sectionStyle": sectionStyle,
		"accent": accent, "online": online, "warning": warning, "critical": critical,
	} {
		if fg := style.GetForeground(); banned[string(fg.(lipgloss.Color))] {
			t.Fatalf("%s använder en förbjuden rosa/lila färg: %v", name, fg)
		}
	}
}

func TestClipKeepsStyledRowsWithinPanelWidth(t *testing.T) {
	styled := selected.Render("en mycket lång markerad trådtitel som annars kan slå om")
	got := clip(styled, 24)
	if width := ansi.StringWidth(got); width > 24 {
		t.Fatalf("stylad rad är %d kolumner bred, väntade högst 24", width)
	}
	if !strings.Contains(got, "…") {
		t.Fatalf("förväntade ellips vid klippning: %q", got)
	}
}
