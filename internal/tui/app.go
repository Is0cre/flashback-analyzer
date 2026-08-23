package tui

import (
	"context"
	"embed"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/backflash-cli/backflash/internal/diagnostics"
	"github.com/backflash-cli/backflash/internal/external"
	"github.com/backflash-cli/backflash/internal/external/polisen"
	"github.com/backflash-cli/backflash/internal/flashback"
	"github.com/backflash-cli/backflash/internal/gandr"
	"github.com/backflash-cli/backflash/internal/mesh"
	meshruntime "github.com/backflash-cli/backflash/internal/mesh/runtime"
	"github.com/backflash-cli/backflash/internal/service"
	"github.com/backflash-cli/backflash/internal/store"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

//go:embed assets/backflash.ans
var splashFS embed.FS

type View int

const (
	ViewOverview View = iota
	ViewForums
	ViewThreads
	ViewReader
	ViewRemoteSearch
	ViewExternalEvents
	ViewMesh
	ViewGandr
)

type dataMsg struct {
	kind       string
	forums     []flashback.ForumNode
	threads    []flashback.ThreadSummary
	posts      []flashback.Post
	results    []flashback.SearchResult
	events     []external.ExternalEvent
	detail     *external.ExternalEvent
	refresh    bool
	refreshURL string
	err        error
}

type dashboardMsg struct {
	snapshot service.DashboardSnapshot
	err      error
}

type meshMsg struct {
	snapshot meshruntime.Snapshot
	err      error
}

type meshTickMsg struct{}
type App struct {
	Store        *store.Store
	Client       *flashback.Client
	CurrentView  View
	Width        int
	Height       int
	Forums       []flashback.ForumNode
	Threads      []flashback.ThreadSummary
	Posts        []flashback.Post
	Results      []flashback.SearchResult
	Stack        []flashback.ForumNode
	Cursor       int
	Status       string
	Input        textinput.Model
	SearchRemote bool
	Query        string
	RemotePage   int
	Events       []external.ExternalEvent
	EventDetail  *external.ExternalEvent
	EventService *service.ExternalEventsService
	Dashboard    service.DashboardSnapshot
	DashboardSvc *service.DashboardService
	Gandr        *gandr.Subsystem
	MeshRuntime  *meshruntime.Runtime
	MeshState    meshruntime.Snapshot
}

var (
	accent     = lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81"))
	muted      = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	selected   = lipgloss.NewStyle().Reverse(true)
)

const navigationSource = "flashback:navigation"

func New(s *store.Store, c *flashback.Client) App {
	input := textinput.New()
	input.Prompt = "> "
	input.CharLimit = 200
	eventClient := polisen.NewClient(nil, nil)
	eventService := &service.ExternalEventsService{Store: s, Provider: eventClient, RefreshAfter: 2 * time.Minute, Now: time.Now}
	meshConfig := mesh.Load()
	dashboard := &service.DashboardService{Store: s, Now: time.Now, MeshConfigured: meshConfig.Enabled}
	return App{Store: s, Client: c, CurrentView: ViewOverview, Input: input, Status: "REDO · cache lokal", RemotePage: 1, EventService: eventService, DashboardSvc: dashboard, Gandr: gandr.New(), MeshRuntime: meshruntime.New(meshConfig)}
}

func Splash(w io.Writer, width int) {
	if width < 80 {
		fmt.Fprintln(w, "BACKFLASH")
		return
	}
	b, err := splashFS.ReadFile("assets/backflash.ans")
	if err != nil {
		fmt.Fprintln(w, "BACKFLASH")
		return
	}
	if width < 120 {
		fmt.Fprintln(w, accent.Render("BACKFLASH // DISKURS-NOC"))
		return
	}
	_, _ = w.Write(append(b, []byte("\x1b[0m\n")...))
}

func (a App) Init() tea.Cmd {
	return tea.Batch(loadCachedEvents(a.EventService), loadDashboard(a.DashboardSvc), startMesh(a.MeshRuntime))
}
func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		a.Width, a.Height = m.Width, m.Height
	case dataMsg:
		if m.err != nil {
			a.Status = "FEL · " + m.err.Error()
			return a, nil
		}
		a.Status = "REDO · cache lokal"
		switch m.kind {
		case "forums":
			a.Forums, a.CurrentView = m.forums, ViewForums
			if m.refresh {
				target := m.refreshURL
				if target == "" {
					target = flashback.BaseURL
				}
				return a, refreshNavigation(a.Store, a.Client, target)
			}
		case "threads":
			a.Threads, a.CurrentView = m.threads, ViewThreads
			if m.refresh {
				target := m.refreshURL
				if target == "" {
					target = flashback.BaseURL
				}
				return a, refreshThreads(a.Store, a.Client, target, activeForum(a))
			}
		case "posts":
			a.Posts, a.CurrentView = m.posts, ViewReader
		case "search":
			a.Results, a.CurrentView = m.results, ViewRemoteSearch
		case "events":
			a.Events = m.events
			if a.CurrentView == ViewExternalEvents {
				a.EventDetail = m.detail
			}
			if m.refresh {
				return a, refreshEvents(a.EventService)
			}
		}
	case dashboardMsg:
		if m.err != nil {
			a.Status = "FEL · lokalt dashboard"
			return a, nil
		}
		a.Dashboard = m.snapshot
	case meshMsg:
		a.MeshState = m.snapshot
		a.applyMeshSnapshot(m.snapshot)
		if m.err != nil {
			a.Status = "MESH · FEL"
		}
		if m.snapshot.State == meshruntime.Disabled {
			return a, nil
		}
		return a, meshTick()
	case meshTickMsg:
		if a.MeshRuntime == nil {
			return a, nil
		}
		a.MeshState = a.MeshRuntime.Snapshot()
		a.applyMeshSnapshot(a.MeshState)
		return a, meshTick()
	case tea.KeyMsg:
		if a.Input.Focused() {
			if m.String() == "enter" {
				q := strings.TrimSpace(a.Input.Value())
				a.Input.Blur()
				if q != "" {
					a.Query = q
					if a.SearchRemote {
						a.RemotePage = 1
						return a, remoteSearch(a.Client, q, 1)
					}
					return a, localSearch(a.Store, q, activeThread(a))
				}
			}
			if m.String() == "esc" {
				a.Input.Blur()
				return a, nil
			}
			var cmd tea.Cmd
			a.Input, cmd = a.Input.Update(m)
			return a, cmd
		}
		switch m.String() {
		case "ctrl+c", "q":
			if a.CurrentView != ViewOverview {
				a.CurrentView, a.Cursor, a.EventDetail = ViewOverview, 0, nil
				return a, loadDashboard(a.DashboardSvc)
			}
			return a, tea.Quit
		case "home", "h":
			a.CurrentView, a.Cursor, a.EventDetail = ViewOverview, 0, nil
			return a, loadDashboard(a.DashboardSvc)
		case "f":
			a.CurrentView, a.Cursor = ViewForums, 0
			return a, loadRoot(a.Store, a.Client)
		case "t":
			a.CurrentView, a.Status = ViewOverview, "SPARADE TRÅDAR · lokal data"
			return a, nil
		case "p":
			a.CurrentView, a.Cursor, a.EventDetail = ViewExternalEvents, 0, nil
			return a, loadCachedEvents(a.EventService)
		case "m":
			a.CurrentView, a.Cursor = ViewMesh, 0
			return a, nil
		case "g":
			a.CurrentView, a.Cursor = ViewGandr, 0
			return a, nil
		case "b":
			if len(a.Stack) > 0 {
				a.Stack = a.Stack[:len(a.Stack)-1]
				return a, loadChildren(a.Store, a.Stack)
			}
			a.CurrentView = ViewOverview
			return a, loadDashboard(a.DashboardSvc)
		case "j", "down":
			a.move(1)
		case "k", "up":
			a.move(-1)
		case "/":
			a.SearchRemote = true
			a.Input.SetValue("")
			a.Input.Focus()
			return a, nil
		case "ctrl+f":
			a.SearchRemote = false
			a.Input.SetValue("")
			a.Input.Focus()
			return a, nil
		case "enter":
			if a.CurrentView == ViewExternalEvents && a.Cursor < len(a.Events) {
				a.EventDetail = &a.Events[a.Cursor]
				return a, nil
			}
			return a.openSelected()
		case "r":
			if a.CurrentView == ViewExternalEvents {
				a.Status = "POLISHÄNDELSER · HÄMTAR…"
				return a, refreshEvents(a.EventService)
			}
		case "]":
			if a.CurrentView == ViewRemoteSearch {
				a.RemotePage++
				return a, remoteSearch(a.Client, a.Query, a.RemotePage)
			}
		case "[":
			if a.CurrentView == ViewRemoteSearch && a.RemotePage > 1 {
				a.RemotePage--
				return a, remoteSearch(a.Client, a.Query, a.RemotePage)
			}
		case "esc":
			a.Input.Blur()
		}
	}
	return a, nil
}

func (a *App) move(delta int) {
	n := a.itemCount()
	if n == 0 {
		return
	}
	a.Cursor = (a.Cursor + delta + n) % n
}
func (a App) itemCount() int {
	switch a.CurrentView {
	case ViewForums:
		return len(a.Forums)
	case ViewThreads:
		return len(a.Threads)
	case ViewReader:
		return len(a.Posts)
	case ViewRemoteSearch:
		return len(a.Results)
	case ViewExternalEvents:
		return len(a.Events)
	}
	return 0
}
func activeThread(a App) string {
	if len(a.Threads) > 0 && a.Cursor < len(a.Threads) {
		return a.Threads[a.Cursor].ID
	}
	return ""
}

func activeForum(a App) string {
	if len(a.Stack) == 0 {
		return ""
	}
	return a.Stack[len(a.Stack)-1].ID
}
func (a App) openSelected() (tea.Model, tea.Cmd) {
	switch a.CurrentView {
	case ViewForums:
		if a.Cursor < len(a.Forums) {
			n := a.Forums[a.Cursor]
			a.Stack = append(a.Stack, n)
			a.Cursor = 0
			if n.HasChildren {
				return a, loadForumChildren(a.Store, a.Client, n)
			}
			a.CurrentView = ViewThreads
			return a, loadForum(a.Store, a.Client, n)
		}
	case ViewThreads:
		if a.Cursor < len(a.Threads) {
			selected := a.Threads[a.Cursor]
			a.CurrentView = ViewReader
			a.Cursor = 0
			return a, loadPosts(a.Store, a.Client, selected.ID)
		}
	case ViewRemoteSearch:
		if a.Cursor < len(a.Results) {
			r := a.Results[a.Cursor]
			a.CurrentView = ViewReader
			a.Status = "TRÅD HÄMTAS…"
			return a, loadRemoteThread(a.Store, a.Client, r)
		}
	case ViewExternalEvents:
		if a.Cursor < len(a.Events) {
			a.EventDetail = &a.Events[a.Cursor]
		}
	}
	return a, nil
}

func (a App) View() string {
	finish := diagnostics.Start("tui.view")
	defer finish()
	var b strings.Builder
	b.WriteString(accent.Render("BACKFLASH // DISKURS-NOC"))
	b.WriteString("\n\n")
	switch a.CurrentView {
	case ViewOverview:
		b.WriteString(renderDashboard(a))
	case ViewForums:
		b.WriteString(titleStyle.Render("FORUM · " + a.breadcrumb()))
		b.WriteString("\n\n")
		b.WriteString(renderNodes(a.Forums, a.Cursor))
	case ViewThreads:
		b.WriteString(titleStyle.Render("TRÅDAR · " + a.breadcrumb()))
		b.WriteString("\n\n")
		b.WriteString(renderThreads(a.Threads, a.Cursor))
	case ViewReader:
		b.WriteString(titleStyle.Render("INLÄGG"))
		b.WriteString("\n\n")
		b.WriteString(renderPosts(a.Posts, a.Cursor))
	case ViewRemoteSearch:
		b.WriteString(titleStyle.Render("SÖK PÅ FLASHBACK: " + a.Query))
		b.WriteString("\n\n")
		b.WriteString(renderResults(a.Results, a.Cursor))
	case ViewExternalEvents:
		b.WriteString(titleStyle.Render("POLISHÄNDELSER"))
		b.WriteString("\n\n")
		b.WriteString(renderEvents(a.Events, a.Cursor))
		if a.EventDetail != nil {
			b.WriteString("\n\n" + renderEventDetail(*a.EventDetail))
		}
	case ViewMesh:
		b.WriteString(titleStyle.Render("CACHE-MESH"))
		b.WriteString(renderMeshDetail(a.MeshState))
	case ViewGandr:
		b.WriteString(titleStyle.Render("ᚷ GANDR"))
		b.WriteString("\n\nVAULT       LÅST\n\nGandr startas inte automatiskt. Lås upp subsystemet explicit när det behövs.\n\n" + muted.Render("Gandr-identitet, privat databas och petnames hålls separerade från BACKFLASH."))
	}
	if a.Input.Focused() {
		b.WriteString("\n\n" + a.Input.View())
	}
	b.WriteString("\n\n" + muted.Render("j/k flytta · Enter öppna · f forum · / fjärrsök · Ctrl+F lokalt · p polis · m mesh · g Gandr · h dashboard · q tillbaka/avsluta"))
	return b.String()
}
func (a App) breadcrumb() string {
	p := []string{"FLASHBACK"}
	for _, n := range a.Stack {
		p = append(p, n.Title)
	}
	return strings.Join(p, " > ")
}
func renderNodes(xs []flashback.ForumNode, c int) string {
	var b strings.Builder
	for i, n := range xs {
		line := n.Title
		if n.HasChildren {
			line += "  ›"
		}
		if i == c {
			line = selected.Render(line)
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}
func renderThreads(xs []flashback.ThreadSummary, c int) string {
	var b strings.Builder
	for i, n := range xs {
		line := fmt.Sprintf("%s%s · %d svar", map[bool]string{true: "📌 ", false: ""}[n.Sticky], n.Title, n.Replies)
		if i == c {
			line = selected.Render(line)
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}
func renderPosts(xs []flashback.Post, c int) string {
	var b strings.Builder
	for i, n := range xs {
		line := fmt.Sprintf("#%s  %s  %s\n    %s", n.ID, n.Author, n.Timestamp.Format("2006-01-02 15:04"), n.Text)
		if i == c {
			line = selected.Render(line)
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}
func renderResults(xs []flashback.SearchResult, c int) string {
	var b strings.Builder
	for i, n := range xs {
		line := fmt.Sprintf("#%s  %s · %s\n    %s", n.PostID, n.Title, n.Author, n.Snippet)
		if i == c {
			line = selected.Render(line)
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

func renderEvents(xs []external.ExternalEvent, c int) string {
	var b strings.Builder
	for i, e := range xs {
		line := fmt.Sprintf("%s · %s · %s", e.Timestamp.Local().Format("02 jan 15:04"), e.EventType, e.LocationName)
		if e.Title != "" {
			line += " · " + e.Title
		}
		if i == c {
			line = selected.Render(line)
		}
		b.WriteString(line + "\n")
	}
	if len(xs) == 0 {
		return "Inga sparade polishändelser."
	}
	return b.String()
}
func renderEventSummary(xs []external.ExternalEvent) string {
	if len(xs) > 3 {
		xs = xs[:3]
	}
	return renderEvents(xs, -1)
}
func renderEventDetail(e external.ExternalEvent) string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(e.Title) + "\n\n")
	b.WriteString("TID        " + e.Timestamp.Local().Format("2006-01-02 15:04") + "\n")
	b.WriteString("TYP        " + e.EventType + "\n")
	b.WriteString("PLATS      " + e.LocationName + "\n\n")
	b.WriteString(e.Summary)
	if e.URL != "" {
		b.WriteString("\n\nKÄLLA      " + e.URL)
	}
	return b.String()
}

func renderDashboard(a App) string {
	d := a.Dashboard
	width := a.Width
	if width == 0 {
		width = 120
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render("BACKFLASH // DISKURS-NOC"))
	b.WriteString("\n\n")
	if width >= 120 {
		b.WriteString("LOKAL DATA                         AKTIVITET                         STATUS\n")
		b.WriteString("────────────────────────────       ────────────────────────────       ───────────────\n")
		b.WriteString(fmt.Sprintf("Forum        %s                     Inlägg / 60m  %s                  DB        REDO\n", number(d.ForumCount), number(d.PostsLastHour)))
		b.WriteString(fmt.Sprintf("Trådar       %s                     Aktiva trådar %s                  Nätverk   %s\n", number(d.ThreadCount), number(d.ActiveThreads), d.Network))
		b.WriteString(fmt.Sprintf("Inlägg       %s                     Aktiva forum  %s                  Session   %s\n", number(d.PostCount), number(d.ActiveForums), d.Session))
		b.WriteString(fmt.Sprintf("DB           %s                     Nya trådar    %s                  Synk      %s\n", bytes(d.DBSize), number(d.NewThreads), d.Sync))
		b.WriteString("\nHETAST JUST NU                    CACHE-MESH                         GANDR\n")
		b.WriteString("────────────────────────────       ────────────────────────────       ───────────────\n")
		b.WriteString(renderHot(d.HotThreads))
		b.WriteString(fmt.Sprintf("                                   Yggdrasil    %s                 ᚷ %s\n", d.Mesh, d.Gandr))
		b.WriteString(fmt.Sprintf("                                   Peers        %d                 privat läge\n", d.MeshPeers))
		b.WriteString(fmt.Sprintf("                                   Delning      %s                 objekt %d\n", d.MeshSharing, d.MeshObjects))
		b.WriteString(fmt.Sprintf("                                   RX/TX        %s / %s\n", bytesUint(d.MeshRX), bytesUint(d.MeshTX)))
		b.WriteString("\nPOLISHÄNDELSER\n────────────────────────────\n")
		if len(a.Events) == 0 {
			b.WriteString("Inga sparade polishändelser.\n")
		} else {
			b.WriteString(renderEventSummary(a.Events))
		}
	} else if width >= 80 {
		b.WriteString(fmt.Sprintf("LOKAL DATA\nForum %s · Trådar %s · Inlägg %s · DB %s\n\n", number(d.ForumCount), number(d.ThreadCount), number(d.PostCount), bytes(d.DBSize)))
		b.WriteString(fmt.Sprintf("AKTIVITET\nInlägg / 60m %s · Aktiva trådar %s · Nya trådar %s\n\n", number(d.PostsLastHour), number(d.ActiveThreads), number(d.NewThreads)))
		b.WriteString(fmt.Sprintf("STATUS\nDB REDO · Nätverk %s · Session %s · Synk %s\n\nCACHE-MESH %s · peers %d · delning %s · GANDR ᚷ %s\n", d.Network, d.Session, d.Sync, d.Mesh, d.MeshPeers, d.MeshSharing, d.Gandr))
		b.WriteString("POLISHÄNDELSER\n" + renderEventSummary(a.Events))
	} else {
		b.WriteString("DATA\n")
		b.WriteString(fmt.Sprintf("%s forum\n%s trådar\n%s inlägg\n\n", number(d.ForumCount), number(d.ThreadCount), number(d.PostCount)))
		b.WriteString("AKTIVITET\n" + number(d.PostsLastHour) + " / 60m\n\n")
		b.WriteString("MESH " + d.Mesh + "\npeers " + number(d.MeshPeers) + " · objekt " + number(d.MeshObjects) + "\nGANDR ᚷ " + d.Gandr)
	}
	b.WriteString("\n\n" + muted.Render("[f] Forum  [/] Sök  [p] Polis  [m] Mesh  [g] Gandr  [h] Hem"))
	return b.String()
}

func renderHot(rows []service.HotThread) string {
	if len(rows) == 0 {
		return "—                                "
	}
	var b strings.Builder
	for _, row := range rows[:min(len(rows), 3)] {
		b.WriteString(fmt.Sprintf("▲ %4s/h  %s\n", number(row.Posts), row.Title))
	}
	return b.String()
}

func number(n int) string {
	if n == 0 {
		return "0"
	}
	return fmt.Sprintf("%d", n)
}

func bytes(n int64) string {
	if n <= 0 {
		return "—"
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%d KB", n/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
}

func bytesUint(n uint64) string {
	if n == 0 {
		return "0 B"
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%d KB", n/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/(1024*1024))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func loadDashboard(dashboard *service.DashboardService) tea.Cmd {
	return func() tea.Msg {
		if dashboard == nil {
			return dashboardMsg{}
		}
		snapshot, err := dashboard.Snapshot(context.Background())
		return dashboardMsg{snapshot: snapshot, err: err}
	}
}

func startMesh(runtime *meshruntime.Runtime) tea.Cmd {
	return func() tea.Msg {
		if runtime == nil {
			return meshMsg{}
		}
		err := runtime.Start(context.Background())
		return meshMsg{snapshot: runtime.Snapshot(), err: err}
	}
}

func meshTick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return meshTickMsg{} })
}

func meshStateLabel(state meshruntime.State) string {
	switch state {
	case meshruntime.Disabled:
		return "AV"
	case meshruntime.Configured:
		return "VALD"
	case meshruntime.Starting:
		return "STARTAR"
	case meshruntime.Running:
		return "PÅ"
	case meshruntime.Degraded:
		return "DEGRADED"
	case meshruntime.Stopping:
		return "STOPPAR"
	case meshruntime.Error:
		return "FEL"
	default:
		return "—"
	}
}

func (a *App) applyMeshSnapshot(snapshot meshruntime.Snapshot) {
	a.Dashboard.Mesh = meshStateLabel(snapshot.State)
	a.Dashboard.MeshSharing = map[bool]string{true: "PÅ", false: "AV"}[snapshot.ShareCache]
	a.Dashboard.MeshPeers = snapshot.Peers
	a.Dashboard.MeshObjects = snapshot.Objects
	a.Dashboard.MeshRX = snapshot.BytesRecv
	a.Dashboard.MeshTX = snapshot.BytesSent
}

func renderMeshDetail(snapshot meshruntime.Snapshot) string {
	var b strings.Builder
	b.WriteString("\n\nSTATUS      " + meshStateLabel(snapshot.State))
	b.WriteString("\nDELNING     " + map[bool]string{true: "PÅ", false: "AV"}[snapshot.ShareCache])
	b.WriteString("\nIDENTITET   " + snapshot.Identity)
	b.WriteString(fmt.Sprintf("\nPEERS       %d\nOBJEKT      %d", snapshot.Peers, snapshot.Objects))
	if snapshot.LastError != "" {
		b.WriteString("\nFEL         " + snapshot.LastError)
	}
	b.WriteString("\n\n" + muted.Render("Endast publika cacheobjekt. Ingen Gandr-identitet, cookie eller läshistorik delas."))
	return b.String()
}

// Shutdown is called by the process owner after Bubble Tea exits.
func (a App) Shutdown() error {
	if a.MeshRuntime == nil {
		return nil
	}
	return a.MeshRuntime.Stop()
}

func loadRoot(s *store.Store, c *flashback.Client) tea.Cmd {
	return func() tea.Msg {
		finish := diagnostics.Start("navigation.root")
		defer finish()
		rows, err := s.Forums("")
		var out []flashback.ForumNode
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var n flashback.ForumNode
				var child int
				if e := rows.Scan(&n.ID, &n.Title, &n.URL, &child); e == nil {
					n.HasChildren = child != 0
					out = append(out, n)
				}
			}
		}
		if len(out) > 0 {
			state, _ := s.ExternalSyncState(navigationSource)
			return dataMsg{kind: "forums", forums: out, refresh: state.LastSyncedAt.IsZero() || time.Since(state.LastSyncedAt) >= 24*time.Hour, refreshURL: flashback.BaseURL}
		}
		nodes, e := c.Forum(context.Background(), flashback.BaseURL)
		if e == nil {
			_ = s.SaveForums(nodes)
			_ = s.SetExternalSyncState(external.SyncState{Source: navigationSource, LastSyncedAt: time.Now(), Status: "ok"})
		}
		return dataMsg{kind: "forums", forums: nodes, err: e}
	}
}
func loadChildren(s *store.Store, stack []flashback.ForumNode) tea.Cmd {
	return func() tea.Msg {
		parent := ""
		if len(stack) > 0 {
			parent = stack[len(stack)-1].ID
		}
		rows, e := s.Forums(parent)
		if e != nil {
			return dataMsg{kind: "forums", err: e}
		}
		defer rows.Close()
		var out []flashback.ForumNode
		for rows.Next() {
			var n flashback.ForumNode
			var child int
			if e := rows.Scan(&n.ID, &n.Title, &n.URL, &child); e == nil {
				n.HasChildren = child != 0
				out = append(out, n)
			}
		}
		return dataMsg{kind: "forums", forums: out}
	}
}
func loadForumChildren(s *store.Store, c *flashback.Client, n flashback.ForumNode) tea.Cmd {
	return func() tea.Msg {
		rows, e := s.Forums(n.ID)
		if e == nil {
			defer rows.Close()
			var out []flashback.ForumNode
			for rows.Next() {
				var child flashback.ForumNode
				var hasChildren int
				if scanErr := rows.Scan(&child.ID, &child.Title, &child.URL, &hasChildren); scanErr == nil {
					child.HasChildren = hasChildren != 0
					out = append(out, child)
				}
			}
			if len(out) > 0 {
				state, _ := s.ExternalSyncState(navigationSource + ":" + n.ID)
				return dataMsg{kind: "forums", forums: out, refresh: state.LastSyncedAt.IsZero() || time.Since(state.LastSyncedAt) >= 24*time.Hour, refreshURL: n.URL}
			}
		}
		out, e := c.Forum(context.Background(), n.URL)
		if e == nil {
			_ = s.SaveForums(out)
			_ = s.SetExternalSyncState(external.SyncState{Source: navigationSource + ":" + n.ID, LastSyncedAt: time.Now(), Status: "ok"})
		}
		return dataMsg{kind: "forums", forums: out, err: e}
	}
}

func refreshNavigation(s *store.Store, c *flashback.Client, rawURL string) tea.Cmd {
	return func() tea.Msg {
		nodes, err := c.Forum(context.Background(), rawURL)
		if err != nil {
			return dataMsg{kind: "forums", err: err}
		}
		if err = s.SaveForums(nodes); err != nil {
			return dataMsg{kind: "forums", err: err}
		}
		_ = s.SetExternalSyncState(external.SyncState{Source: navigationSource, LastSyncedAt: time.Now(), Status: "ok"})
		return dataMsg{kind: "forums", forums: nodes}
	}
}
func loadForum(s *store.Store, c *flashback.Client, n flashback.ForumNode) tea.Cmd {
	return func() tea.Msg {
		finish := diagnostics.Start("forum.threads")
		defer finish()
		dbRows, e := s.DB.Query(`SELECT t.id,t.title,t.url,t.replies,t.views,t.last_post_at,t.last_post_author,t.sticky,t.page_count FROM forum_threads ft JOIN threads t ON t.id=ft.thread_id WHERE ft.forum_id=? AND trim(t.title)<>'' AND lower(trim(t.title)) NOT LIKE 'utan titel%' ORDER BY ft.position`, n.ID)
		if e == nil {
			defer dbRows.Close()
			var out []flashback.ThreadSummary
			for dbRows.Next() {
				var t flashback.ThreadSummary
				var sticky int
				if e := dbRows.Scan(&t.ID, &t.Title, &t.URL, &t.Replies, &t.Views, &t.LastPostAt, &t.LastPostAuthor, &sticky, &t.PageCount); e == nil {
					t.Sticky = sticky != 0
					out = append(out, t)
				}
			}
			if len(out) > 0 {
				state, _ := s.ExternalSyncState("flashback:threads:" + n.ID)
				return dataMsg{kind: "threads", threads: out, refresh: state.LastSyncedAt.IsZero() || time.Since(state.LastSyncedAt) >= 10*time.Minute, refreshURL: n.URL}
			}
		}
		threads, e := c.Threads(context.Background(), n)
		if e == nil {
			_ = s.SaveThreads(n.ID, threads)
			_ = s.SetExternalSyncState(external.SyncState{Source: "flashback:threads:" + n.ID, LastSyncedAt: time.Now(), Status: "ok"})
		}
		return dataMsg{kind: "threads", threads: threads, err: e}
	}
}

func refreshThreads(s *store.Store, c *flashback.Client, rawURL, forumID string) tea.Cmd {
	return func() tea.Msg {
		forum := flashback.ForumNode{ID: forumID, URL: rawURL}
		threads, err := c.Threads(context.Background(), forum)
		if err != nil {
			return dataMsg{kind: "threads", err: err}
		}
		if err = s.SaveThreads(forumID, threads); err != nil {
			return dataMsg{kind: "threads", err: err}
		}
		_ = s.SetExternalSyncState(external.SyncState{Source: "flashback:threads:" + forumID, LastSyncedAt: time.Now(), Status: "ok"})
		return dataMsg{kind: "threads", threads: threads}
	}
}
func loadPosts(s *store.Store, c *flashback.Client, id string) tea.Cmd {
	return func() tea.Msg {
		finish := diagnostics.Start("thread.posts")
		defer finish()
		rows, e := s.Posts(id)
		if e == nil {
			defer rows.Close()
			var out []flashback.Post
			for rows.Next() {
				var p flashback.Post
				if e := rows.Scan(&p.ID, &p.Author, &p.Timestamp, &p.Text); e == nil {
					p.ThreadID = id
					out = append(out, p)
				}
			}
			if len(out) > 0 {
				return dataMsg{kind: "posts", posts: out}
			}
		}
		p, e := c.Thread(context.Background(), id, 1)
		if e == nil {
			_ = s.SavePage(p)
		}
		return dataMsg{kind: "posts", posts: p.Posts, err: e}
	}
}
func loadRemoteThread(s *store.Store, c *flashback.Client, r flashback.SearchResult) tea.Cmd {
	return loadPosts(s, c, r.ThreadID)
}
func remoteSearch(c *flashback.Client, q string, page int) tea.Cmd {
	return func() tea.Msg {
		r, e := c.Search(context.Background(), q, page)
		return dataMsg{kind: "search", results: r, err: e}
	}
}
func localSearch(s *store.Store, q, id string) tea.Cmd {
	return func() tea.Msg {
		rows, e := s.DB.Query(`SELECT post_id,thread_id,author,snippet(post_search,3,'','…',12) FROM post_search WHERE post_search MATCH ? AND (?='' OR thread_id=?) LIMIT 100`, q, id, id)
		if e != nil {
			return dataMsg{kind: "search", err: e}
		}
		defer rows.Close()
		var r []flashback.SearchResult
		for rows.Next() {
			var x flashback.SearchResult
			x.ResultType = "post"
			if e := rows.Scan(&x.PostID, &x.ThreadID, &x.Author, &x.Snippet); e == nil {
				r = append(r, x)
			}
		}
		return dataMsg{kind: "search", results: r}
	}
}

func loadCachedEvents(events *service.ExternalEventsService) tea.Cmd {
	return func() tea.Msg {
		if events == nil {
			return dataMsg{kind: "events"}
		}
		cached, err := events.Cached(polisen.Source, 100)
		if err != nil {
			return dataMsg{kind: "events", err: err}
		}
		return dataMsg{kind: "events", events: cached, refresh: events.Stale(polisen.Source)}
	}
}
func refreshEvents(events *service.ExternalEventsService) tea.Cmd {
	return func() tea.Msg {
		if events == nil {
			return dataMsg{kind: "events"}
		}
		found, err := events.Refresh(context.Background())
		return dataMsg{kind: "events", events: found, refresh: false, err: err}
	}
}
