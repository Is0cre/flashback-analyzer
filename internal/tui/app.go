package tui

import (
	"context"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
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
	"github.com/charmbracelet/bubbles/viewport"
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
	ViewGandrChat
)

type dataMsg struct {
	kind          string
	forums        []flashback.ForumNode
	threads       []flashback.ThreadSummary
	posts         []flashback.Post
	threadID      string
	threadTitle   string
	results       []flashback.SearchResult
	events        []external.ExternalEvent
	detail        *external.ExternalEvent
	refresh       bool
	refreshURL    string
	refreshParent string
	err           error
}

type dashboardMsg struct {
	snapshot service.DashboardSnapshot
	err      error
}

type meshMsg struct {
	snapshot meshruntime.Snapshot
	err      error
}

type gandrMsg struct {
	summary gandr.Summary
	err     error
	created bool
}

type gandrSessionMsg struct {
	session  *gandr.Session
	channels []gandr.Channel
	peers    []gandr.Peer
	groups   []gandr.PrivateGroup
	offline  bool
	err      error
}

type gandrIncomingMsg struct {
	message gandr.Message
}

type gandrChannelsMsg struct {
	channels []gandr.Channel
	err      error
}

type gandrStatusMsg struct{ err error }
type gandrInvitationMsg struct {
	token string
	err   error
}
type gandrDeleteMsg struct{ err error }
type gandrPeersMsg struct {
	peers []gandr.Peer
	err   error
}

type gandrContactMsg struct{ err error }
type gandrGroupMsg struct {
	groups   []gandr.PrivateGroup
	active   *[32]byte
	messages []gandr.PrivateGroupMessage
	err      error
}

type meshTickMsg struct{}
type gandrPeerTickMsg struct{}
type App struct {
	Store              *store.Store
	Client             *flashback.Client
	CurrentView        View
	Width              int
	Height             int
	Forums             []flashback.ForumNode
	Threads            []flashback.ThreadSummary
	Posts              []flashback.Post
	ThreadID           string
	ThreadTitle        string
	Results            []flashback.SearchResult
	Stack              []flashback.ForumNode
	Cursor             int
	Status             string
	Input              textinput.Model
	SearchRemote       bool
	Query              string
	RemotePage         int
	Events             []external.ExternalEvent
	EventDetail        *external.ExternalEvent
	EventService       *service.ExternalEventsService
	Dashboard          service.DashboardSnapshot
	DashboardSvc       *service.DashboardService
	Gandr              *gandr.Subsystem
	GandrCreating      bool
	GandrConfirming    bool
	GandrPassphrase    string
	GandrFailures      int
	GandrLockedUntil   time.Time
	GandrDeleteConfirm bool
	GandrRecreate      bool
	GandrSession       *gandr.Session
	GandrChannels      []gandr.Channel
	GandrMessages      map[[32]byte][]gandr.Message
	GandrContacts      []gandr.Contact
	GandrPeers         []gandr.Peer
	GandrAddMode       bool
	GandrGroups        []gandr.PrivateGroup
	GandrActiveGroup   *[32]byte
	GandrGroupMessages map[[32]byte][]gandr.PrivateGroupMessage
	GandrRightCursor   int
	MeshRuntime        *meshruntime.Runtime
	MeshState          meshruntime.Snapshot
	PaletteOpen        bool
	PaletteCursor      int
	PostViewport       viewport.Model
	PostViewportReady  bool
}

var (
	brand        = lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
	accent       = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81"))
	sectionStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("141"))
	muted        = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	metadata     = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	online       = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
	warning      = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	critical     = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
	selected     = lipgloss.NewStyle().Foreground(lipgloss.Color("232")).Background(lipgloss.Color("81")).Bold(true)
	selectedMeta = lipgloss.NewStyle().Foreground(lipgloss.Color("235")).Background(lipgloss.Color("81"))
	pinStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("220"))
)

// Bump this when the persisted navigation shape/parser changes. It causes
// one background refresh instead of trusting a snapshot written by the old
// flat-root bug.
const navigationSource = "flashback:navigation:v3"

func New(s *store.Store, c *flashback.Client) App {
	input := textinput.New()
	input.Prompt = "> "
	input.CharLimit = 200
	eventClient := polisen.NewClient(nil, nil)
	eventService := &service.ExternalEventsService{Store: s, Provider: eventClient, RefreshAfter: 2 * time.Minute, Now: time.Now}
	meshConfig := mesh.Load()
	dashboard := &service.DashboardService{Store: s, Now: time.Now, MeshConfigured: meshConfig.Enabled}
	return App{Store: s, Client: c, CurrentView: ViewOverview, Input: input, Status: "REDO · cache lokal", RemotePage: 1, EventService: eventService, DashboardSvc: dashboard, Gandr: gandr.New(), MeshRuntime: meshruntime.New(meshConfig), PostViewport: viewport.New(20, 6)}
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
		fmt.Fprintln(w, brand.Render("BACKFLASH // DISKURS-NOC"))
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
		a.resizePostViewport()
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
				a.Status = "FORUM · UPPDATERAR…"
				target := m.refreshURL
				if target == "" {
					target = flashback.BaseURL
				}
				if m.refreshParent != "" {
					return a, refreshForumNavigation(a.Store, a.Client, target, m.refreshParent)
				}
				return a, refreshNavigation(a.Store, a.Client, target)
			}
		case "threads":
			a.Threads, a.CurrentView = m.threads, ViewThreads
			if len(m.threads) == 0 {
				a.Status = "INGA TRÅDAR · tryck r för att hämta igen"
			}
			if m.refresh {
				target := m.refreshURL
				if target == "" {
					target = flashback.BaseURL
				}
				return a, refreshThreads(a.Store, a.Client, target, activeForum(a))
			}
		case "posts":
			a.Posts, a.CurrentView = m.posts, ViewReader
			if m.threadID != "" {
				a.ThreadID = m.threadID
			}
			if m.threadTitle != "" {
				a.ThreadTitle = m.threadTitle
			}
			a.refreshPostViewport(true)
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
	case gandrMsg:
		if m.err != nil {
			if !m.created {
				a.GandrFailures++
			}
			if m.created {
				a.Status = "GANDR · FEL · valvet kunde inte skapas"
			} else {
				a.Status = "GANDR · FEL · lösenordet kunde inte verifieras"
			}
			if a.GandrFailures >= 3 {
				a.GandrLockedUntil = time.Now().Add(10 * time.Second)
				a.Input.Blur()
				a.Input.SetValue("")
				a.Status = "GANDR · för många fel · upplåsning pausad i 10 sekunder"
			} else if !m.created {
				a.Input.SetValue("")
				a.Input.Placeholder = "Försök igen · GANDR-lösenord"
				a.Input.Focus()
			}
		} else {
			a.GandrCreating, a.GandrConfirming, a.GandrPassphrase = false, false, ""
			a.GandrFailures = 0
			a.GandrLockedUntil = time.Time{}
			a.Input.EchoMode = textinput.EchoNormal
			a.Input.Placeholder = ""
			a.Status = "GANDR · identiteten är upplåst lokalt"
			return a, connectGandr(a.Gandr)
		}
	case gandrSessionMsg:
		if m.err != nil {
			a.Status = "GANDR · daemon ej ansluten · starta gandrd separat"
			return a, nil
		}
		a.GandrSession = m.session
		a.GandrChannels = m.channels
		a.GandrPeers = m.peers
		a.GandrGroups = m.groups
		a.GandrActiveGroup = nil
		a.GandrGroupMessages = make(map[[32]byte][]gandr.PrivateGroupMessage)
		a.GandrContacts, _ = m.session.Contacts()
		a.GandrMessages = make(map[[32]byte][]gandr.Message)
		for _, channel := range m.channels {
			history, err := m.session.Messages(channel.ID, 200)
			if err != nil {
				continue
			}
			for _, item := range history {
				a.GandrMessages[channel.ID] = append(a.GandrMessages[channel.ID], gandr.Message{
					Hash: item.Hash, ChannelID: item.ChannelID, Sender: item.Sender,
					Content: item.Content, At: item.At, Local: item.Local,
				})
			}
		}
		if m.offline {
			a.Status = fmt.Sprintf("GANDR · lokal runtime · daemon ej ansluten · %d kanaler", len(m.channels))
		} else if m.session.Online() {
			a.Status = fmt.Sprintf("GANDR · nätverk ansluten · %d kanaler", len(m.channels))
		} else {
			a.Status = fmt.Sprintf("GANDR · lokal runtime · %d kanaler", len(m.channels))
		}
		return a, tea.Batch(waitGandrIncoming(m.session), refreshGandrPeers(m.session))
	case gandrIncomingMsg:
		if a.GandrSession != nil {
			_ = a.GandrSession.SaveMessage(gandr.ChatMessage{
				Hash: m.message.Hash, ChannelID: m.message.ChannelID,
				Sender: m.message.Sender, Content: m.message.Content,
				At: m.message.At,
			})
		}
		if a.GandrMessages == nil {
			a.GandrMessages = make(map[[32]byte][]gandr.Message)
		}
		appendGandrMessage(a.GandrMessages, m.message)
		if a.GandrSession != nil {
			return a, waitGandrIncoming(a.GandrSession)
		}
	case gandrPeerTickMsg:
		if a.GandrSession != nil && a.GandrSession.Online() {
			return a, loadGandrPeers(a.GandrSession)
		}
	case gandrPeersMsg:
		if m.err == nil {
			a.GandrPeers = m.peers
		}
		if a.GandrSession != nil && a.GandrSession.Online() {
			return a, refreshGandrPeers(a.GandrSession)
		}
	case gandrContactMsg:
		if m.err != nil {
			a.Status = "GANDR · användaren kunde inte läggas till · " + m.err.Error()
		} else if a.GandrSession != nil {
			a.GandrContacts, _ = a.GandrSession.Contacts()
			a.GandrAddMode = false
			a.Status = "GANDR · användaren sparad lokalt"
		}
	case gandrGroupMsg:
		if m.err != nil {
			a.Status = "GANDR · privat grupp · " + m.err.Error()
		} else {
			a.GandrGroups = m.groups
			a.GandrActiveGroup = m.active
			if m.active != nil {
				if a.GandrGroupMessages == nil {
					a.GandrGroupMessages = make(map[[32]byte][]gandr.PrivateGroupMessage)
				}
				a.GandrGroupMessages[*m.active] = m.messages
			}
			a.Status = "GANDR · privat grupp · krypterad lokalt"
		}
	case gandrChannelsMsg:
		if m.err != nil {
			a.Status = "GANDR · kanalåtgärden misslyckades · " + m.err.Error()
		} else {
			a.GandrChannels = m.channels
			a.Cursor = min(a.Cursor, max(0, len(a.GandrChannels)-1))
			a.Status = fmt.Sprintf("GANDR · %d kanaler", len(m.channels))
		}
	case gandrStatusMsg:
		if m.err != nil {
			a.Status = "GANDR · blockering misslyckades · " + m.err.Error()
		} else {
			a.Status = "GANDR · kontakt blockerad lokalt"
		}
	case gandrInvitationMsg:
		if m.err != nil {
			a.Status = "GANDR · inbjudan misslyckades · " + m.err.Error()
		} else if m.token != "" {
			a.Status = "GANDR · kopiera denna inbjudan: " + m.token
		} else {
			a.Status = "GANDR · kontakt tillagd via inbjudan"
		}
	case gandrDeleteMsg:
		if m.err != nil {
			a.Status = "GANDR · radering misslyckades · " + m.err.Error()
		} else {
			a.GandrDeleteConfirm = false
			if a.GandrRecreate {
				a.GandrRecreate = false
				a.GandrCreating = true
				a.GandrConfirming = false
				a.Input.EchoMode = textinput.EchoPassword
				a.Input.Placeholder = "Nytt GANDR-lösenord"
				a.Input.Focus()
				a.Status = "GANDR · gamla valvet raderat · skapa nytt valv"
			} else {
				a.Status = "GANDR · valv och privata data raderade"
			}
		}
	case meshTickMsg:
		if a.MeshRuntime == nil {
			return a, nil
		}
		a.MeshState = a.MeshRuntime.Snapshot()
		a.applyMeshSnapshot(a.MeshState)
		return a, meshTick()
	case tea.KeyMsg:
		if a.PaletteOpen {
			switch m.String() {
			case "esc", "q", "ctrl+p", "?":
				a.PaletteOpen = false
				return a, nil
			case "j", "down":
				a.PaletteCursor = (a.PaletteCursor + 1) % len(paletteItems)
				return a, nil
			case "k", "up":
				a.PaletteCursor = (a.PaletteCursor - 1 + len(paletteItems)) % len(paletteItems)
				return a, nil
			case "enter":
				a.PaletteOpen = false
				return a.runPaletteItem()
			}
			return a, nil
		}
		if a.Input.Focused() {
			// A forgotten password must not make the destructive reset
			// unreachable. Ctrl+X bypasses the password field, but still
			// requires the exact RADERA confirmation below.
			if m.String() == "ctrl+x" && a.CurrentView == ViewGandr && a.Gandr != nil && a.Gandr.HasVault() {
				beginGandrDelete(&a, false)
				return a, nil
			}
			// Escape leaves the password field so destructive vault actions
			// cannot be triggered accidentally while typing a password.
			if m.String() == "esc" {
				a.Input.Blur()
				a.Input.SetValue("")
				return a, nil
			}
			if m.String() == "enter" {
				raw := a.Input.Value()
				q := strings.TrimSpace(raw)
				a.Input.Blur()
				if a.CurrentView == ViewGandr {
					a.Input.SetValue("")
					if a.GandrDeleteConfirm {
						if q != "RADERA" {
							a.Status = "GANDR · radering avbruten"
							return a, nil
						}
						a.Input.Blur()
						a.GandrDeleteConfirm = false
						closeGandrSession(&a)
						return a, deleteGandr(a.Gandr)
					}
					if time.Now().Before(a.GandrLockedUntil) {
						a.Status = fmt.Sprintf("GANDR · upplåsning pausad · %s kvar", time.Until(a.GandrLockedUntil).Round(time.Second))
						a.Input.Blur()
						return a, nil
					}
					if a.GandrCreating {
						if !a.GandrConfirming {
							if raw == "" {
								a.Status = "GANDR · lösenordet får inte vara tomt"
								a.Input.Focus()
								return a, nil
							}
							a.GandrPassphrase = raw
							a.GandrConfirming = true
							a.Input.Placeholder = "Upprepa GANDR-lösenordet"
							a.Input.Focus()
							return a, nil
						}
						if raw != a.GandrPassphrase {
							a.GandrPassphrase = ""
							a.GandrConfirming = false
							a.Input.Placeholder = "Nytt GANDR-lösenord"
							a.Status = "GANDR · lösenorden matchar inte"
							a.Input.Focus()
							return a, nil
						}
						passphrase := a.GandrPassphrase
						a.GandrPassphrase = ""
						return a, createGandr(a.Gandr, passphrase)
					}
					return a, unlockGandr(a.Gandr, raw)
				}
				if a.CurrentView == ViewGandrChat {
					if q == "/invite" {
						return a, createGandrInvitation(a.GandrSession)
					}
					if strings.HasPrefix(q, "/invite accept ") {
						return a, acceptGandrInvitation(a.GandrSession, strings.TrimSpace(strings.TrimPrefix(q, "/invite accept ")))
					}
					if a.GandrAddMode || strings.HasPrefix(q, "/add ") {
						a.GandrAddMode = false
						return a, addGandrContact(a.GandrSession, strings.TrimSpace(strings.TrimPrefix(q, "/add ")))
					}
					if q == "" || a.GandrSession == nil {
						return a, nil
					}
					if strings.HasPrefix(q, "/grupp") {
						return a, gandrGroupCommand(a.GandrSession, q)
					}
					if strings.HasPrefix(q, "/join ") {
						return a, gandrJoin(a.GandrSession, strings.TrimSpace(strings.TrimPrefix(q, "/join ")))
					}
					if q == "/leave" && len(a.GandrChannels) > 0 {
						channel := a.GandrChannels[min(a.Cursor, len(a.GandrChannels)-1)]
						return a, gandrLeave(a.GandrSession, channel.ID)
					}
					if a.GandrActiveGroup != nil {
						return a, sendPrivateGandrGroup(a.GandrSession, *a.GandrActiveGroup, q)
					}
					if len(a.GandrChannels) > 0 {
						channel := a.GandrChannels[min(a.Cursor, len(a.GandrChannels)-1)]
						own := gandr.Message{ChannelID: channel.ID, Content: q, At: time.Now().UnixNano(), Local: true}
						appendGandrMessage(a.GandrMessages, own)
						return a, gandrSend(a.GandrSession, channel.ID, q)
					}
					return a, nil
				}
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
				a.Input.SetValue("")
				a.GandrCreating, a.GandrConfirming, a.GandrPassphrase = false, false, ""
				a.GandrAddMode = false
				return a, nil
			}
			var cmd tea.Cmd
			a.Input, cmd = a.Input.Update(m)
			return a, cmd
		}
		switch m.String() {
		case "ctrl+p", "?":
			a.PaletteOpen = true
			a.PaletteCursor = 0
			return a, nil
		case "ctrl+c", "q":
			if a.CurrentView != ViewOverview {
				closeGandrSession(&a)
				a.CurrentView, a.Cursor, a.EventDetail = ViewOverview, 0, nil
				a.Input.Blur()
				a.GandrCreating, a.GandrConfirming, a.GandrPassphrase = false, false, ""
				return a, loadDashboard(a.DashboardSvc)
			}
			return a, tea.Quit
		case "home", "h":
			closeGandrSession(&a)
			a.CurrentView, a.Cursor, a.EventDetail = ViewOverview, 0, nil
			a.Input.Blur()
			a.GandrCreating, a.GandrConfirming, a.GandrPassphrase = false, false, ""
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
			a.GandrCreating, a.GandrConfirming, a.GandrPassphrase = false, false, ""
			a.Input.SetValue("")
			a.Input.EchoMode = textinput.EchoPassword
			if time.Now().Before(a.GandrLockedUntil) {
				a.Status = fmt.Sprintf("GANDR · upplåsning pausad · %s kvar", time.Until(a.GandrLockedUntil).Round(time.Second))
				a.Input.Placeholder = "Försök igen senare"
			} else if a.Gandr != nil && a.Gandr.HasVault() {
				a.Input.Placeholder = "GANDR-lösenord"
				a.Input.Focus()
			} else {
				a.Input.Placeholder = ""
			}
			return a, nil
		case "c":
			if a.CurrentView == ViewGandr && (a.Gandr == nil || !a.Gandr.HasVault()) {
				a.GandrCreating, a.GandrConfirming, a.GandrPassphrase = true, false, ""
				a.Input.SetValue("")
				a.Input.EchoMode = textinput.EchoPassword
				a.Input.Placeholder = "Nytt GANDR-lösenord"
				a.Input.Focus()
			}
			return a, nil
		case "n":
			if a.CurrentView == ViewGandr && a.Gandr != nil && a.Gandr.HasVault() {
				closeGandrSession(&a)
				a.GandrRecreate = true
				a.GandrDeleteConfirm = true
				a.Input.SetValue("")
				a.Input.EchoMode = textinput.EchoNormal
				a.Input.Placeholder = "Skriv RADERA för att skapa nytt valv"
				a.Input.Focus()
				return a, nil
			}
			return a, nil
		case "d":
			if a.CurrentView == ViewGandr && a.Gandr != nil && a.Gandr.HasVault() {
				beginGandrDelete(&a, false)
				return a, nil
			}
			return a, nil
		case "b":
			if a.CurrentView == ViewGandrChat {
				return a, nil
			}
			if len(a.Stack) > 0 {
				a.Stack = a.Stack[:len(a.Stack)-1]
				return a, loadChildren(a.Store, a.Stack)
			}
			a.CurrentView = ViewOverview
			return a, loadDashboard(a.DashboardSvc)
		case "j", "down":
			if a.CurrentView == ViewReader {
				a.move(1)
				a.refreshPostViewport(false)
				break
			}
			a.move(1)
		case "k", "up":
			if a.CurrentView == ViewReader {
				a.move(-1)
				a.refreshPostViewport(false)
				break
			}
			a.move(-1)
		case "pgdown", "ctrl+d":
			if a.CurrentView == ViewReader {
				a.PostViewport, _ = a.PostViewport.Update(m)
				return a, nil
			}
		case "pgup", "ctrl+u":
			if a.CurrentView == ViewReader {
				a.PostViewport, _ = a.PostViewport.Update(m)
				return a, nil
			}
		case "x":
			if a.CurrentView == ViewGandr && a.Gandr != nil && a.Gandr.HasVault() {
				beginGandrDelete(&a, false)
				return a, nil
			}
			if a.CurrentView == ViewGandrChat {
				return a, blockSelectedGandr(a)
			}
		case "a":
			if a.CurrentView == ViewGandrChat && a.GandrSession != nil {
				a.GandrAddMode = true
				a.Input.SetValue("")
				a.Input.EchoMode = textinput.EchoNormal
				a.Input.Placeholder = "publik nyckel + namn"
				a.Input.Focus()
				return a, nil
			}
		case "/":
			if a.CurrentView == ViewGandrChat {
				a.Input.SetValue("")
				a.Input.EchoMode = textinput.EchoNormal
				a.Input.Placeholder = "Meddelande eller /join kanal"
				a.Input.Focus()
				return a, nil
			}
			a.SearchRemote = true
			a.Input.SetValue("")
			a.Input.EchoMode = textinput.EchoNormal
			a.Input.Placeholder = "Sök på Flashback"
			a.Input.Focus()
			return a, nil
		case "ctrl+f":
			a.SearchRemote = false
			a.Input.SetValue("")
			a.Input.EchoMode = textinput.EchoNormal
			a.Input.Placeholder = "Sök lokalt"
			a.Input.Focus()
			return a, nil
		case "enter":
			if a.CurrentView == ViewGandr && a.GandrSession != nil {
				a.CurrentView, a.Cursor = ViewGandrChat, 0
				return a, nil
			}
			if a.CurrentView == ViewGandrChat && a.GandrSession != nil {
				a.Input.EchoMode = textinput.EchoNormal
				a.Input.Placeholder = "Meddelande eller /join kanal"
				a.Input.Focus()
				return a, nil
			}
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
			if a.CurrentView == ViewThreads && len(a.Stack) > 0 {
				a.Status = "TRÅDAR · HÄMTAR…"
				return a, refreshThreads(a.Store, a.Client, a.Stack[len(a.Stack)-1].URL, activeForum(a))
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
	case tea.MouseMsg:
		if a.CurrentView == ViewGandrChat && m.Action == tea.MouseActionPress && m.Button == tea.MouseButtonLeft {
			// Header occupies rows 0–3; the channel list starts on row 6.
			if m.X < 26 && m.Y >= 6 && m.Y < 6+len(a.GandrChannels) {
				a.Cursor = m.Y - 6
				return a, nil
			}
			if m.X < 26 && m.Y == 9+len(a.GandrChannels) && a.GandrSession != nil {
				a.Input.SetValue("/join ")
				a.Input.EchoMode = textinput.EchoNormal
				a.Input.Placeholder = "KANALNAMN"
				a.Input.Focus()
				return a, nil
			}
			if m.X < 26 && m.Y == 10+len(a.GandrChannels) && a.GandrSession != nil {
				a.Input.SetValue("/grupp skapa ")
				a.Input.EchoMode = textinput.EchoNormal
				a.Input.Placeholder = "NAMN LÖSENORD"
				a.Input.Focus()
				return a, nil
			}
			if m.X < 26 && m.Y == 11+len(a.GandrChannels) && a.Gandr != nil && a.Gandr.HasVault() {
				beginGandrDelete(&a, false)
				return a, nil
			}
			if a.Width >= 100 && m.X >= a.Width-30 && m.Y >= 6 {
				contactIndex := (m.Y - 6) / 2
				if contactIndex >= 0 && contactIndex < len(a.GandrContacts) {
					a.GandrRightCursor = contactIndex
					return a, nil
				}
				if a.GandrSession != nil {
					a.GandrAddMode = true
					a.Input.SetValue("")
					a.Input.EchoMode = textinput.EchoNormal
					a.Input.Placeholder = "publik nyckel + namn"
					a.Input.Focus()
					return a, nil
				}
			}
			if m.Y >= 6+len(a.GandrChannels) && a.GandrSession != nil {
				a.Input.EchoMode = textinput.EchoNormal
				a.Input.Placeholder = "Meddelande eller /join kanal"
				a.Input.Focus()
			}
		}
	}
	return a, nil
}

func beginGandrDelete(a *App, recreate bool) {
	if a == nil {
		return
	}
	closeGandrSession(a)
	a.CurrentView = ViewGandr
	a.GandrRecreate = recreate
	a.GandrDeleteConfirm = true
	a.Input.SetValue("")
	a.Input.EchoMode = textinput.EchoNormal
	if recreate {
		a.Input.Placeholder = "Skriv RADERA för att skapa nytt valv"
	} else {
		a.Input.Placeholder = "Skriv RADERA för att bekräfta"
	}
	a.Input.Focus()
}

func (a *App) move(delta int) {
	n := a.itemCount()
	if n == 0 {
		return
	}
	a.Cursor = (a.Cursor + delta + n) % n
}

func (a *App) resizePostViewport() {
	if a == nil {
		return
	}
	width := a.Width
	if width < 20 {
		width = 20
	}
	height := a.Height - 8
	if height < 6 {
		height = 6
	}
	a.PostViewport.Width = width
	a.PostViewport.Height = height
	if len(a.Posts) > 0 {
		a.refreshPostViewport(false)
	}
}

// refreshPostViewport keeps the reader bounded even for very large threads.
// The selected post remains the semantic cursor; the viewport is only the
// presentation layer around that cursor.
func (a *App) refreshPostViewport(reset bool) {
	if a == nil || len(a.Posts) == 0 {
		a.PostViewportReady = false
		return
	}
	a.PostViewport.SetContent(renderPostsWidth(a.Posts, a.Cursor, a.PostViewport.Width))
	a.PostViewportReady = true
	if reset {
		a.PostViewport.GotoTop()
		return
	}
	// Keep the selected post in view without forcing a complete redraw model.
	// Moving down is cheap because the content is already held by the viewport.
	if a.Cursor > 0 {
		a.PostViewport.SetYOffset(min(a.Cursor*2, max(0, len(a.Posts)*2-a.PostViewport.Height)))
	}
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
	case ViewGandrChat:
		return len(a.GandrChannels)
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
			// The persisted has_children bit is only a rendering hint. Flashback
			// has many forum levels and templates do not always expose that fact
			// on the parent link. Inspect the selected forum page itself; the
			// loader chooses cached/remote subforums first, then first-page
			// threads when it is a leaf.
			a.Status = "FORUM · HÄMTAR…"
			return a, loadForumChildren(a.Store, a.Client, n)
		}
	case ViewThreads:
		if a.Cursor < len(a.Threads) {
			selected := a.Threads[a.Cursor]
			a.CurrentView = ViewReader
			a.Cursor = 0
			a.ThreadID, a.ThreadTitle = selected.ID, selected.Title
			return a, loadPosts(a.Store, a.Client, selected.ID, a.MeshRuntime)
		}
	case ViewRemoteSearch:
		if a.Cursor < len(a.Results) {
			r := a.Results[a.Cursor]
			a.CurrentView = ViewReader
			a.Status = "TRÅD HÄMTAS…"
			a.ThreadID, a.ThreadTitle = r.ThreadID, r.Title
			return a, loadRemoteThread(a.Store, a.Client, r, a.MeshRuntime)
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
	if a.CurrentView == ViewGandr || a.CurrentView == ViewGandrChat {
		b.WriteString(titleStyle.Render("ᚷ GANDR") + "  " + metadata.Render("· PRIVAT NÄTVERK"))
	} else {
		b.WriteString(brand.Render("BACKFLASH // DISKURS-NOC"))
	}
	b.WriteString("\n\n")
	switch a.CurrentView {
	case ViewOverview:
		b.WriteString(renderDashboard(a))
	case ViewForums:
		b.WriteString(viewHeading("FORUM", a.breadcrumb()))
		b.WriteString("\n\n")
		b.WriteString(renderNodes(a.Forums, a.Cursor))
	case ViewThreads:
		b.WriteString(viewHeading("TRÅDAR", a.breadcrumb()))
		b.WriteString("\n\n")
		b.WriteString(renderThreadWorkspace(a))
	case ViewReader:
		context := a.ThreadTitle
		if context == "" {
			context = "tråd " + a.ThreadID
		}
		b.WriteString(viewHeading("INLÄGG", context))
		b.WriteString("\n\n")
		if a.PostViewportReady {
			b.WriteString(a.PostViewport.View())
		} else {
			b.WriteString(renderPosts(a.Posts, a.Cursor))
		}
	case ViewRemoteSearch:
		b.WriteString(viewHeading("SÖK PÅ FLASHBACK", a.Query))
		b.WriteString("\n\n")
		b.WriteString(renderResults(a.Results, a.Cursor))
	case ViewExternalEvents:
		b.WriteString(viewHeading("POLISHÄNDELSER", "POLISEN · PUBLIK KÄLLA"))
		b.WriteString("\n\n")
		b.WriteString(renderPoliceMap(a.Events, a.Width))
		b.WriteString("\n\n")
		rows := 12
		if a.Height > 0 && a.Height-25 > rows {
			rows = a.Height - 25
		}
		b.WriteString(titleStyle.Render(fmt.Sprintf("SENASTE HÄNDELSER · %d AV %d", minInt(rows, len(a.Events)), len(a.Events))))
		b.WriteString("\n")
		b.WriteString(renderEventWindow(a.Events, a.Cursor, rows))
		if a.EventDetail != nil {
			b.WriteString("\n\n" + renderEventDetail(*a.EventDetail))
		}
	case ViewMesh:
		b.WriteString(viewHeading("CACHE-MESH", "BACKFLASH · PUBLIK CACHE"))
		b.WriteString(renderMeshDetail(a.MeshState))
	case ViewGandr:
		b.WriteString(viewHeading("ᚷ GANDR", "SEPARAT PRIVAT SUBSYSTEM"))
		summary := gandr.Summary{State: gandr.Locked}
		if a.Gandr != nil {
			summary = a.Gandr.Summary()
		}
		if a.Gandr == nil || !a.Gandr.HasVault() {
			summary.State = gandr.Missing
		}
		b.WriteString("\n\nVAULT       " + statusValue(string(summary.State)))
		if summary.Fingerprint != "" {
			b.WriteString("\nIDENTITET   ~" + summary.Fingerprint)
		}
		switch summary.State {
		case gandr.Locked:
			b.WriteString("\n\nLösenordsruta aktiv. Tryck Enter för att låsa upp.")
			if a.Gandr != nil && a.Gandr.HasVault() {
				b.WriteString("\nGlömt lösenord? Tryck Ctrl+X och skriv RADERA för att radera valvet.")
			}
		case gandr.Missing:
			if a.GandrCreating {
				if a.GandrConfirming {
					b.WriteString("\n\nNytt valv · upprepa lösenordet och tryck Enter.")
				} else {
					b.WriteString("\n\nNytt valv · ange ett lösenord och tryck Enter.")
				}
			} else {
				b.WriteString("\n\nInget GANDR-valv finns ännu.")
				b.WriteString("\nTryck c för att skapa ett nytt valv, eller q för att gå tillbaka.")
			}
		case gandr.UnlockErr:
			b.WriteString("\n\n" + critical.Render("Upplåsningen misslyckades."))
		case gandr.Unlocked:
			if a.GandrSession != nil {
				b.WriteString("\n\n" + online.Render("Daemon ansluten."))
				b.WriteString("\nTryck Enter för att öppna meddelanden och kanaler.")
			} else {
				b.WriteString("\n\nIdentiteten är dekrypterad i minnet.")
				b.WriteString("\nGANDR-daemon är inte ansluten ännu.")
			}
		}
		if a.GandrDeleteConfirm {
			if a.GandrRecreate {
				b.WriteString("\n\n" + critical.Render("VARNING: gamla GANDR-identiteten och client.db raderas."))
				b.WriteString("\nDet går inte att återställa ett bortglömt lösenord. Skriv RADERA för nytt valv.")
			} else {
				b.WriteString("\n\n" + critical.Render("VARNING: detta raderar GANDR-identiteten och den privata client.db."))
				b.WriteString("\nSkriv RADERA och tryck Enter för att fortsätta.")
			}
		}
		b.WriteString("\n\n" + muted.Render("Gandr-identitet, privat databas och petnames hålls separerade från BACKFLASH."))
	case ViewGandrChat:
		b.WriteString(renderGandrChat(a))
	}
	if a.Input.Focused() {
		b.WriteString("\n\n" + a.Input.View())
	}
	if a.Status != "" {
		b.WriteString("\n\n" + muted.Render(a.Status))
	}
	if a.PaletteOpen {
		b.WriteString("\n\n" + renderPalette(a))
	}
	if a.CurrentView == ViewGandr || a.CurrentView == ViewGandrChat {
		b.WriteString("\n\n" + muted.Render("j/k flytta · Enter öppna · / kommando · Esc lämna fält · x radera valv · n radera + skapa nytt · q tillbaka"))
	} else {
		b.WriteString("\n\n" + muted.Render("j/k flytta · Enter öppna · ") + accent.Render("f") + muted.Render(" forum · ") + accent.Render("/") + muted.Render(" fjärrsök · ") + accent.Render("Ctrl+F") + muted.Render(" lokalt · ") + accent.Render("p") + muted.Render(" polis · ") + accent.Render("m") + muted.Render(" mesh · ") + accent.Render("g") + muted.Render(" Gandr · ") + accent.Render("h") + muted.Render(" hem · q tillbaka/avsluta"))
	}
	return b.String()
}

type paletteItem struct {
	Key   string
	Title string
	Hint  string
}

var paletteItems = []paletteItem{
	{Key: "h", Title: "Översikt", Hint: "lokalt NOC-dashboard"},
	{Key: "f", Title: "Forum", Hint: "Flashbacks forumträd"},
	{Key: "/", Title: "Fjärrsök", Hint: "sök på Flashback"},
	{Key: "ctrl+f", Title: "Lokalsök", Hint: "sök i sparat innehåll"},
	{Key: "p", Title: "Polishändelser", Hint: "publika händelser och karta"},
	{Key: "m", Title: "Cache-mesh", Hint: "status och peer-cache"},
	{Key: "g", Title: "GANDR", Hint: "separat privat subsystem"},
}

func renderPalette(a App) string {
	width := a.Width - 8
	if width < 42 {
		width = 42
	}
	if width > 72 {
		width = 72
	}
	var b strings.Builder
	b.WriteString(sectionStyle.Render("KOMMANDOCENTER") + "  " + metadata.Render("snabbnavigering"))
	b.WriteString("\n" + muted.Render(strings.Repeat("─", width-4)))
	for i, item := range paletteItems {
		line := fmt.Sprintf("%-8s %-20s %s", "["+item.Key+"]", item.Title, item.Hint)
		line = clip(line, width-4)
		if i == a.PaletteCursor {
			line = selected.Render("› " + line)
		} else {
			line = "  " + line
		}
		b.WriteString("\n" + line)
	}
	b.WriteString("\n\n" + muted.Render("j/k välj · Enter öppna · Esc stäng"))
	return lipgloss.NewStyle().Width(width).Border(lipgloss.DoubleBorder()).BorderForeground(lipgloss.Color("81")).Padding(0, 1).Render(b.String())
}

func (a App) runPaletteItem() (tea.Model, tea.Cmd) {
	if a.PaletteCursor < 0 || a.PaletteCursor >= len(paletteItems) {
		return a, nil
	}
	item := paletteItems[a.PaletteCursor]
	switch item.Key {
	case "h":
		closeGandrSession(&a)
		a.CurrentView, a.Cursor, a.EventDetail = ViewOverview, 0, nil
		return a, loadDashboard(a.DashboardSvc)
	case "f":
		a.CurrentView, a.Cursor = ViewForums, 0
		return a, loadRoot(a.Store, a.Client)
	case "/", "ctrl+f":
		a.SearchRemote = item.Key == "/"
		a.Input.SetValue("")
		a.Input.EchoMode = textinput.EchoNormal
		if a.SearchRemote {
			a.Input.Placeholder = "Sök på Flashback"
		} else {
			a.Input.Placeholder = "Sök lokalt"
		}
		a.Input.Focus()
		return a, nil
	case "p":
		a.CurrentView, a.Cursor, a.EventDetail = ViewExternalEvents, 0, nil
		return a, loadCachedEvents(a.EventService)
	case "m":
		a.CurrentView, a.Cursor = ViewMesh, 0
		return a, nil
	case "g":
		a.CurrentView, a.Cursor = ViewGandr, 0
		a.GandrCreating, a.GandrConfirming, a.GandrPassphrase = false, false, ""
		a.Input.SetValue("")
		a.Input.EchoMode = textinput.EchoPassword
		if a.Gandr != nil && a.Gandr.HasVault() {
			a.Input.Placeholder = "GANDR-lösenord"
			a.Input.Focus()
		}
		return a, nil
	}
	return a, nil
}
func viewHeading(label, context string) string {
	return titleStyle.Render(label) + "  " + metadata.Render("· "+context)
}

func renderGandrChat(a App) string {
	width := a.Width
	if width < 60 {
		width = 80
	}
	sidebarWidth := 24
	if width < 90 {
		sidebarWidth = 20
	}
	mainWidth := width - sidebarWidth - 3
	if mainWidth < 28 {
		mainWidth = 28
	}

	var sidebar strings.Builder
	sidebar.WriteString(sectionStyle.Render("KANALER"))
	sidebar.WriteString("\n" + metadata.Render("────────────────────") + "\n")
	if len(a.GandrChannels) == 0 {
		sidebar.WriteString(muted.Render("inga kanaler"))
	} else {
		for i, channel := range a.GandrChannels {
			line := "# " + truncate(channel.Name, sidebarWidth-4)
			if i == a.Cursor {
				line = selected.Render("› " + truncate(channel.Name, sidebarWidth-4))
			}
			sidebar.WriteString(line + "\n")
		}
	}
	sidebar.WriteString("\n" + muted.Render("/join namn"))
	sidebar.WriteString("\n" + muted.Render("/leave"))
	sidebar.WriteString("\n" + accent.Render("+ skapa kanal"))
	sidebar.WriteString("\n" + accent.Render("+ skapa privat grupp"))
	if a.Gandr != nil && a.Gandr.HasVault() {
		sidebar.WriteString("\n" + critical.Render("[!] radera GANDR-valv"))
	}
	if len(a.GandrGroups) > 0 {
		sidebar.WriteString("\n\n" + sectionStyle.Render("PRIVATA GRUPPER"))
		for _, group := range a.GandrGroups {
			marker := "🔒"
			if a.GandrActiveGroup != nil && *a.GandrActiveGroup == group.ID {
				marker = "›"
			}
			sidebar.WriteString("\n" + marker + " " + truncate(group.Name, sidebarWidth-5))
		}
		sidebar.WriteString("\n" + muted.Render("/grupp lista"))
	}

	var main strings.Builder
	if a.GandrActiveGroup != nil {
		groupName := fmt.Sprintf("~%x", a.GandrActiveGroup[:4])
		for _, group := range a.GandrGroups {
			if group.ID == *a.GandrActiveGroup {
				groupName = group.Name
				break
			}
		}
		main.WriteString(sectionStyle.Render("🔒 " + groupName))
		main.WriteString("  " + online.Render("● E2E-KRYPTERAD"))
		main.WriteString("\n" + metadata.Render(strings.Repeat("─", max(1, mainWidth-2))) + "\n")
		messages := a.GandrGroupMessages[*a.GandrActiveGroup]
		if len(messages) == 0 {
			main.WriteString(muted.Render("Inga privata meddelanden ännu."))
		} else {
			start := 0
			if len(messages) > 18 {
				start = len(messages) - 18
			}
			for _, message := range messages[start:] {
				sender := fmt.Sprintf("~%x", message.Sender[:4])
				stamp := time.Unix(0, message.At).Local().Format("15:04")
				main.WriteString(metadata.Render(stamp) + " " + accent.Render(fmt.Sprintf("%-8s", sender)) + " " + message.Content + "\n")
			}
		}
	} else if len(a.GandrChannels) == 0 {
		main.WriteString(sectionStyle.Render("MEDDELANDEN"))
		main.WriteString("\n\n" + warning.Render("Ingen kanal vald."))
		main.WriteString("\n" + muted.Render("Tryck Enter för skrivfältet och skriv /join kanal."))
	} else {
		channel := a.GandrChannels[min(a.Cursor, len(a.GandrChannels)-1)]
		main.WriteString(sectionStyle.Render("# " + channel.Name))
		if a.GandrSession.Online() {
			main.WriteString("  " + online.Render("● NÄTVERK"))
		} else {
			main.WriteString("  " + warning.Render("○ LOKAL"))
		}
		main.WriteString("\n" + metadata.Render(strings.Repeat("─", max(1, mainWidth-2))) + "\n")
		messages := a.GandrMessages[channel.ID]
		if len(messages) == 0 {
			main.WriteString(muted.Render("Inga meddelanden ännu."))
		} else {
			start := 0
			if len(messages) > 18 {
				start = len(messages) - 18
			}
			for _, message := range messages[start:] {
				sender := fmt.Sprintf("~%x", message.Sender[:4])
				if message.Local || message.Sender == ([32]byte{}) {
					sender = "du"
				}
				stamp := time.Unix(0, message.At).Local().Format("15:04")
				main.WriteString(metadata.Render(stamp) + " " + accent.Render(fmt.Sprintf("%-8s", sender)) + " " + message.Content + "\n")
			}
		}
	}

	left := gandrPanel(sidebar.String(), sidebarWidth)
	mainView := gandrPanel(main.String(), mainWidth)
	if width >= 100 {
		memberWidth := 28
		var members strings.Builder
		members.WriteString(sectionStyle.Render("ANVÄNDARE"))
		members.WriteString("\n" + metadata.Render("────────────────────────") + "\n")
		if len(a.GandrContacts) == 0 {
			members.WriteString(muted.Render("inga sparade användare"))
		} else {
			for i, contact := range a.GandrContacts {
				name := contact.Name
				if name == "" {
					name = fmt.Sprintf("~%x", contact.Pubkey[:4])
				}
				marker := "○"
				presence := "EJ PÅ MESH"
				if gandrPeerOnline(a.GandrPeers, contact.Pubkey) {
					marker = "●"
					presence = "PÅ MESH"
				}
				line := marker + " " + truncate(name, memberWidth-4)
				if i == a.GandrRightCursor {
					line = selected.Render("› " + line)
				}
				members.WriteString(line + "\n")
				if gandrPeerOnline(a.GandrPeers, contact.Pubkey) {
					members.WriteString(online.Render("  "+presence) + "\n")
				} else {
					members.WriteString(muted.Render("  "+presence) + "\n")
				}
			}
		}
		members.WriteString("\n" + muted.Render("a lägg till · x blockera"))
		memberView := gandrPanel(members.String(), memberWidth)
		return viewHeading("ᚷ GANDR · IRC", "PRIVAT NÄTVERK") + "\n\n" + lipgloss.JoinHorizontal(lipgloss.Top, left, mainView, memberView) + "\n\n" + muted.Render("j/k kanal · ↑/↓ användare · Enter skriv · a lägg till · /invite · /grupp skapa|öppna · x blockera · q tillbaka")
	}
	return viewHeading("ᚷ GANDR · IRC", "PRIVAT NÄTVERK") + "\n\n" + lipgloss.JoinHorizontal(lipgloss.Top, left, mainView) + "\n\n" + muted.Render("j/k kanal · Enter skriv · /invite · /grupp lista · q tillbaka")
}

func gandrPanel(content string, width int) string {
	if width < 1 {
		return content
	}
	return lipgloss.NewStyle().Width(width).Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("238")).Padding(0, 1).Render(content)
}

func gandrPeerOnline(peers []gandr.Peer, identity [32]byte) bool {
	for _, peer := range peers {
		if peer.Identity == identity {
			return true
		}
	}
	return false
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
			line += "  " + titleStyle.Render("›")
		}
		if i == c {
			line = selected.Render(line)
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}
func renderThreads(xs []flashback.ThreadSummary, c int) string {
	if len(xs) == 0 {
		return muted.Render("Ingen trådlista finns lokalt ännu.")
	}
	var b strings.Builder
	for i, n := range xs {
		prefix := fmt.Sprintf("%3d  ", i+1)
		if n.Sticky {
			prefix = "📌 " + prefix
		}
		line := prefix + firstNonEmpty(n.Title, "Tråd #"+n.ID)
		if i == c {
			line = selected.Render(line)
		}
		meta := fmt.Sprintf("      #%s · %s svar · %s visningar · %s sidor", n.ID, number(n.Replies), number(n.Views), pageCount(n.PageCount))
		if i == c {
			meta = selectedMeta.Render(meta)
		}
		b.WriteString(line + "\n" + meta + "\n")
	}
	return b.String()
}

// renderThreadWorkspace keeps forum context visible while the thread list is
// being browsed. The list remains driven by the current cursor, so this is a
// presentation layout only; loading and persistence stay in the services.
func renderThreadWorkspace(a App) string {
	width := a.Width
	if width < 1 {
		width = 120
	}
	if width < 100 {
		return renderThreads(a.Threads, a.Cursor)
	}

	leftWidth, rightWidth := 27, 39
	centerWidth := width - leftWidth - rightWidth - 6
	if centerWidth < 34 {
		centerWidth = 34
	}

	left := []string{".."}
	for i, node := range a.Stack {
		marker := "  "
		if i == len(a.Stack)-1 {
			marker = "› "
		}
		left = append(left, marker+clip(node.Title, leftWidth-4))
	}
	if len(a.Stack) == 0 {
		left = append(left, muted.Render("Ingen forumvald"))
	}

	threadLines := make([]string, 0, len(a.Threads)*2+1)
	if len(a.Threads) == 0 {
		threadLines = append(threadLines, muted.Render("Ingen trådar hämtade ännu."), muted.Render("Tryck r för att uppdatera."))
	} else {
		for i, thread := range a.Threads {
			title := firstNonEmpty(thread.Title, "Tråd #"+thread.ID)
			prefix := fmt.Sprintf("%3d ", i+1)
			if thread.Sticky {
				prefix = "📌 " + prefix
			}
			line := prefix + clip(title, centerWidth-4)
			meta := fmt.Sprintf("    #%s · %s svar · %s visningar · %s sidor", thread.ID, number(thread.Replies), number(thread.Views), pageCount(thread.PageCount))
			if i == a.Cursor {
				line = selected.Render(line)
				meta = selectedMeta.Render(meta)
			}
			threadLines = append(threadLines, line, meta)
		}
	}

	detail := []string{"Ingen tråd vald."}
	if a.Cursor >= 0 && a.Cursor < len(a.Threads) {
		thread := a.Threads[a.Cursor]
		detail = []string{
			clip(firstNonEmpty(thread.Title, "Tråd #"+thread.ID), rightWidth-2),
			"",
			"ID          #" + thread.ID,
			"Svar        " + number(thread.Replies),
			"Visningar   " + number(thread.Views),
			"Sidor       " + pageCount(thread.PageCount),
			"Författare   " + firstNonEmpty(thread.Author, "—"),
			"Senast      " + firstNonEmpty(thread.LastPostAuthor, "—"),
			"Tid         " + formatPostTime(thread.LastPostAt),
			"",
			muted.Render("Enter öppnar tråden"),
		}
	}

	return joinPanels(
		renderPanel("FORUMTRÄD", left, leftWidth),
		renderPanel("TRÅDAR", threadLines, centerWidth),
		renderPanel("DETALJER", detail, rightWidth),
	)
}

func pageCount(value int) string {
	if value < 1 {
		return "—"
	}
	return number(value)
}
func renderPosts(xs []flashback.Post, c int) string {
	return renderPostsWidth(xs, c, 120)
}
func renderPostsWidth(xs []flashback.Post, c, width int) string {
	if width < 32 {
		width = 32
	}
	var b strings.Builder
	for i, n := range xs {
		header := fmt.Sprintf("#%s  %s  %s", n.ID, firstNonEmpty(n.Author, "okänd användare"), formatPostTime(n.Timestamp))
		if i == c {
			header = selected.Render(clip(header, width))
		} else {
			header = titleStyle.Render(clip(header, width))
		}
		b.WriteString(header + "\n")
		for _, line := range wrapText(firstNonEmpty(n.Text, "(tomt inlägg)"), width-4) {
			b.WriteString("  " + line + "\n")
		}
		for _, quote := range n.Quotes {
			b.WriteString(muted.Render("  │ Citat: ") + muted.Render(clip(quote.Text, width-11)) + "\n")
		}
		if i+1 < len(xs) {
			b.WriteString(metadata.Render(strings.Repeat("─", width)) + "\n")
		}
	}
	return b.String()
}

func wrapText(value string, width int) []string {
	if width < 1 {
		return []string{""}
	}
	words := strings.Fields(value)
	if len(words) == 0 {
		return []string{""}
	}
	lines := []string{}
	line := ""
	for _, word := range words {
		if line == "" {
			line = word
			continue
		}
		if lipgloss.Width(line)+1+lipgloss.Width(word) <= width {
			line += " " + word
		} else {
			lines = append(lines, line)
			line = word
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

func formatPostTime(value time.Time) string {
	if value.IsZero() {
		return "—"
	}
	return value.Local().Format("2006-01-02 15:04")
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
	return renderEventWindow(xs, c, len(xs))
}

func renderEventWindow(xs []external.ExternalEvent, c, maxRows int) string {
	var b strings.Builder
	if len(xs) == 0 {
		return "Inga sparade polishändelser."
	}
	if maxRows <= 0 || maxRows >= len(xs) {
		maxRows = len(xs)
	}
	if c < 0 {
		c = 0
	}
	if c >= len(xs) {
		c = len(xs) - 1
	}
	start := c - maxRows/2
	if start < 0 {
		start = 0
	}
	if start+maxRows > len(xs) {
		start = len(xs) - maxRows
	}
	end := start + maxRows
	if start > 0 {
		b.WriteString(muted.Render("↑ fler tidigare") + "\n")
	}
	for i := start; i < end; i++ {
		e := xs[i]
		line := fmt.Sprintf("%s · %s · %s", formatSwedishEventTime(e.Timestamp), e.EventType, e.LocationName)
		if e.Title != "" {
			line += " · " + e.Title
		}
		if i == c {
			line = selected.Render(line)
		}
		b.WriteString(line + "\n")
	}
	if end < len(xs) {
		b.WriteString(muted.Render("↓ fler senare") + "\n")
	}
	return b.String()
}

func formatSwedishEventTime(value time.Time) string {
	if value.IsZero() {
		return "okänd tid"
	}
	months := [...]string{"", "jan", "feb", "mar", "apr", "maj", "jun", "jul", "aug", "sep", "okt", "nov", "dec"}
	return fmt.Sprintf("%02d %s %02d:%02d", value.Local().Day(), months[value.Local().Month()], value.Local().Hour(), value.Local().Minute())
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
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

func renderPoliceMap(events []external.ExternalEvent, width int) string {
	mapWidth := width - 4
	if mapWidth <= 0 || mapWidth > 68 {
		mapWidth = 64
	}
	const mapHeight = 16
	grid := make([][]rune, mapHeight)
	for y := range grid {
		grid[y] = make([]rune, mapWidth)
		for x := range grid[y] {
			grid[y][x] = ' '
		}
	}
	// A restrained terminal silhouette: the coordinates, rather than the
	// artwork, are authoritative. It remains useful when terminal graphics
	// are unavailable and can later be replaced by a richer ANSI asset.
	outline := []string{
		"                         ╭──╮",
		"                       ╭─╯  ╰╮",
		"                      ╭╯     │",
		"                     ╭╯      │",
		"                    ╭╯       │",
		"                   ╭╯        │",
		"                  ╭╯         │",
		"                 ╭╯          │",
		"                ╭╯           │",
		"               ╭╯            │",
		"              ╭╯             │",
		"             ╭╯              │",
		"            ╭╯               │",
		"           ╰╮               ╭╯",
		"            ╰───────────────╯",
		"                 ╰──────╯",
	}
	for y, line := range outline {
		start := (mapWidth - len([]rune(line))) / 2
		for x, r := range []rune(line) {
			if start+x >= 0 && start+x < mapWidth {
				grid[y][start+x] = r
			}
		}
	}
	points := 0
	for _, event := range events {
		if event.Latitude == nil || event.Longitude == nil {
			continue
		}
		// Sweden roughly spans 55–69°N and 10–24°E.
		x := int((*event.Longitude - 10) / 14 * float64(mapWidth-1))
		y := int((69 - *event.Latitude) / 14 * float64(mapHeight-1))
		if x < 0 || x >= mapWidth || y < 0 || y >= mapHeight {
			continue
		}
		grid[y][x] = '●'
		points++
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render("KARTA · POLISHÄNDELSER") + "\n")
	if points == 0 {
		b.WriteString(muted.Render("Inga händelser med koordinater."))
		return b.String()
	}
	for _, row := range grid {
		b.WriteString(string(row) + "\n")
	}
	b.WriteString(muted.Render(fmt.Sprintf("● %d händelser · 55–69°N · 10–24°E", points)))
	return b.String()
}

func renderDashboard(a App) string {
	d := a.Dashboard
	width := a.Width
	if width == 0 {
		width = 120
	}
	var b strings.Builder
	if width >= 120 {
		columnWidth := (width - 6) / 3
		b.WriteString(joinPanels(
			renderPanel("LOKAL DATA", []string{
				fmt.Sprintf("Forum       %s", number(d.ForumCount)),
				fmt.Sprintf("Trådar      %s", number(d.ThreadCount)),
				fmt.Sprintf("Inlägg      %s", number(d.PostCount)),
				fmt.Sprintf("DB          %s", bytes(d.DBSize)),
			}, columnWidth),
			renderPanel("AKTIVITET", []string{
				fmt.Sprintf("Inlägg / 60m     %s", number(d.PostsLastHour)),
				fmt.Sprintf("Aktiva trådar    %s", number(d.ActiveThreads)),
				fmt.Sprintf("Aktiva forum     %s", number(d.ActiveForums)),
				fmt.Sprintf("Nya trådar       %s", number(d.NewThreads)),
			}, columnWidth),
			renderPanel("STATUS", []string{
				"DB          " + statusValue("REDO"),
				fmt.Sprintf("Nätverk     %s", statusValue(d.Network)),
				fmt.Sprintf("Session     %s", statusValue(d.Session)),
				fmt.Sprintf("Synk        %s", statusValue(d.Sync)),
			}, columnWidth),
		))
		b.WriteString("\n\n")
		b.WriteString(joinPanels(
			renderPanel("HETAST JUST NU", hotLines(d.HotThreads), columnWidth),
			renderPanel("CACHE-MESH", []string{
				fmt.Sprintf("Yggdrasil   %s", d.Mesh),
				fmt.Sprintf("Noder       %d", d.MeshPeers+1),
				fmt.Sprintf("Fjärrpeers  %d", d.MeshPeers),
				fmt.Sprintf("Delning     %s", d.MeshSharing),
				fmt.Sprintf("Objekt      %d", d.MeshObjects),
				fmt.Sprintf("RX/TX       %s / %s", bytesUint(d.MeshRX), bytesUint(d.MeshTX)),
			}, columnWidth),
			renderPanel("GANDR", []string{
				fmt.Sprintf("ᚷ           %s", d.Gandr),
				"Privat läge",
				"Ingen data delas",
			}, columnWidth),
		))
		b.WriteString("\n\n")
		eventLines := []string{"Inga sparade polishändelser."}
		if len(a.Events) > 0 {
			eventLines = strings.Split(strings.TrimSuffix(renderEventSummary(a.Events), "\n"), "\n")
		}
		b.WriteString(strings.Join(renderPanel("POLISHÄNDELSER", eventLines, width), "\n"))
	} else if width >= 80 {
		b.WriteString(fmt.Sprintf("LOKAL DATA\nForum %s · Trådar %s · Inlägg %s · DB %s\n\n", number(d.ForumCount), number(d.ThreadCount), number(d.PostCount), bytes(d.DBSize)))
		b.WriteString(fmt.Sprintf("AKTIVITET\nInlägg / 60m %s · Aktiva trådar %s · Nya trådar %s\n\n", number(d.PostsLastHour), number(d.ActiveThreads), number(d.NewThreads)))
		b.WriteString(fmt.Sprintf("STATUS\nDB REDO · Nätverk %s · Session %s · Synk %s\n\nCACHE-MESH %s · noder %d · fjärrpeers %d · delning %s · GANDR ᚷ %s\n", d.Network, d.Session, d.Sync, d.Mesh, d.MeshPeers+1, d.MeshPeers, d.MeshSharing, d.Gandr))
		b.WriteString("POLISHÄNDELSER\n" + renderEventSummary(a.Events))
	} else {
		b.WriteString("DATA\n")
		b.WriteString(fmt.Sprintf("%s forum\n%s trådar\n%s inlägg\n\n", number(d.ForumCount), number(d.ThreadCount), number(d.PostCount)))
		b.WriteString("AKTIVITET\n" + number(d.PostsLastHour) + " / 60m\n\n")
		b.WriteString("MESH " + d.Mesh + "\nnoder " + number(d.MeshPeers+1) + " · fjärrpeers " + number(d.MeshPeers) + " · objekt " + number(d.MeshObjects) + "\nGANDR ᚷ " + d.Gandr)
	}
	b.WriteString("\n\n" + muted.Render("[f] Forum  [/] Sök  [p] Polis  [m] Mesh  [g] Gandr  [h] Hem"))
	return b.String()
}

func renderHot(rows []service.HotThread) string {
	return strings.Join(hotLines(rows), "\n") + "\n"
}

func hotLines(rows []service.HotThread) []string {
	if len(rows) == 0 {
		return []string{"—"}
	}
	lines := make([]string, 0, min(len(rows), 3))
	for _, row := range rows[:min(len(rows), 3)] {
		lines = append(lines, fmt.Sprintf("▲ %4s/h  %s", number(row.Posts), row.Title))
	}
	return lines
}

func renderPanel(title string, lines []string, width int) []string {
	if width < 1 {
		return nil
	}
	out := []string{sectionStyle.Render(clip(title, width)), muted.Render(strings.Repeat("─", width))}
	for _, line := range lines {
		out = append(out, clip(line, width))
	}
	for len(out) < 6 {
		out = append(out, "")
	}
	for i, line := range out {
		out[i] = line + strings.Repeat(" ", width-displayWidth(line))
	}
	return out
}

func statusValue(value string) string {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "PÅ", "ONLINE", "AKTIV", "REDO", "VILAR":
		return online.Render(value)
	case "FEL", "OFFLINE", "DEGRADED":
		return critical.Render(value)
	case "STARTAR", "VALD", "UPPDATERAR…":
		return warning.Render(value)
	default:
		return metadata.Render(value)
	}
}

func joinPanels(panels ...[]string) string {
	maxRows := 0
	for _, panel := range panels {
		if len(panel) > maxRows {
			maxRows = len(panel)
		}
	}
	var b strings.Builder
	for row := 0; row < maxRows; row++ {
		for i, panel := range panels {
			if i > 0 {
				b.WriteString("   ")
			}
			if row < len(panel) {
				b.WriteString(panel[row])
			}
		}
		if row+1 < maxRows {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func displayWidth(value string) int {
	return lipgloss.Width(value)
}

func clip(value string, width int) string {
	if displayWidth(value) <= width {
		return value
	}
	if width <= 1 {
		return "…"
	}
	for len([]rune(value)) > 0 {
		runes := []rune(value)
		candidate := string(runes[:len(runes)-1]) + "…"
		if displayWidth(candidate) <= width {
			return candidate
		}
		value = string(runes[:len(runes)-1])
	}
	return "…"
}

func truncate(value string, width int) string {
	if width <= 0 {
		return ""
	}
	runes := []rune(value)
	if len(runes) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	return string(runes[:width-1]) + "…"
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

func max(a, b int) int {
	if a > b {
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

func unlockGandr(subsystem *gandr.Subsystem, passphrase string) tea.Cmd {
	return func() tea.Msg {
		if subsystem == nil {
			return gandrMsg{err: fmt.Errorf("GANDR-gränsen saknas")}
		}
		err := subsystem.Unlock(passphrase)
		return gandrMsg{summary: subsystem.Summary(), err: err}
	}
}

func connectGandr(subsystem *gandr.Subsystem) tea.Cmd {
	return func() tea.Msg {
		if subsystem == nil {
			return gandrSessionMsg{err: fmt.Errorf("GANDR-gränsen saknas")}
		}
		socket := gandrSocketPath()
		session, err := subsystem.Connect(socket)
		offline := false
		if err != nil {
			// Keep the local encrypted chat usable when gandrd is stopped. The
			// session is deliberately marked offline; sends are stored locally
			// and are not presented as delivered over the network.
			session, err = subsystem.Connect("")
			if err != nil {
				return gandrSessionMsg{err: err}
			}
			offline = true
		}
		channels, err := session.Channels()
		if err != nil {
			_ = session.Close()
			return gandrSessionMsg{err: err}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		channels, err = session.EnsureDefaultChannels(ctx)
		if err != nil {
			_ = session.Close()
			return gandrSessionMsg{err: err}
		}
		for _, channel := range channels {
			if err := session.Subscribe(ctx, channel.ID); err != nil {
				_ = session.Close()
				return gandrSessionMsg{err: err}
			}
		}
		peers, _ := session.Peers(ctx)
		groups, _ := session.PrivateGroups()
		return gandrSessionMsg{session: session, channels: channels, peers: peers, groups: groups, offline: offline}
	}
}

func gandrSocketPath() string {
	if path := strings.TrimSpace(os.Getenv("BACKFLASH_GANDR_SOCKET")); path != "" {
		return path
	}
	return "/var/run/gandrd/gandr.sock"
}

func loadGandrPeers(session *gandr.Session) tea.Cmd {
	return func() tea.Msg {
		if session == nil || !session.Online() {
			return gandrPeersMsg{}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		peers, err := session.Peers(ctx)
		return gandrPeersMsg{peers: peers, err: err}
	}
}

func refreshGandrPeers(session *gandr.Session) tea.Cmd {
	if session == nil || !session.Online() {
		return nil
	}
	return tea.Tick(10*time.Second, func(time.Time) tea.Msg { return gandrPeerTickMsg{} })
}

func waitGandrIncoming(session *gandr.Session) tea.Cmd {
	return func() tea.Msg {
		if session == nil {
			return nil
		}
		incoming := session.Incoming()
		if incoming == nil {
			return nil
		}
		for {
			env, ok := <-incoming
			if !ok || env == nil {
				return gandrSessionMsg{err: fmt.Errorf("GANDR-daemon kopplades från")}
			}
			message, err := gandr.DecodeChat(env)
			if err == nil {
				return gandrIncomingMsg{message: message}
			}
		}
	}
}

func gandrJoin(session *gandr.Session, name string) tea.Cmd {
	return func() tea.Msg {
		if strings.TrimSpace(name) == "" {
			return gandrChannelsMsg{err: fmt.Errorf("kanalnamn saknas")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		channels, err := session.Join(ctx, name)
		return gandrChannelsMsg{channels: channels, err: err}
	}
}

func gandrLeave(session *gandr.Session, id [32]byte) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		channels, err := session.Leave(ctx, id)
		return gandrChannelsMsg{channels: channels, err: err}
	}
}

func gandrSend(session *gandr.Session, id [32]byte, content string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := session.SendChannel(ctx, id, content); err != nil {
			return gandrChannelsMsg{err: err}
		}
		return nil
	}
}

func addGandrContact(session *gandr.Session, value string) tea.Cmd {
	return func() tea.Msg {
		if session == nil {
			return gandrContactMsg{err: fmt.Errorf("GANDR-sessionen är inte aktiv")}
		}
		value = strings.TrimSpace(value)
		parts := strings.Fields(value)
		if len(parts) < 2 {
			return gandrContactMsg{err: fmt.Errorf("använd: /add PUBLIK_NYCKEL NAMN")}
		}
		decoded, err := hex.DecodeString(parts[0])
		if err != nil || len(decoded) != 32 {
			return gandrContactMsg{err: fmt.Errorf("publik nyckel ska vara 64 hextecken")}
		}
		var pubkey [32]byte
		copy(pubkey[:], decoded)
		name := strings.TrimSpace(strings.TrimPrefix(value, parts[0]))
		if name == "" {
			return gandrContactMsg{err: fmt.Errorf("namn saknas")}
		}
		return gandrContactMsg{err: session.AddContact(pubkey, name, "")}
	}
}

func createGandrInvitation(session *gandr.Session) tea.Cmd {
	return func() tea.Msg {
		token, err := session.CreateInvitation()
		return gandrInvitationMsg{token: token, err: err}
	}
}

func acceptGandrInvitation(session *gandr.Session, value string) tea.Cmd {
	return func() tea.Msg {
		parts := strings.Fields(value)
		if len(parts) == 0 {
			return gandrInvitationMsg{err: fmt.Errorf("använd: /invite accept INBJUDAN [NAMN]")}
		}
		name := strings.TrimSpace(strings.TrimPrefix(value, parts[0]))
		_, err := session.AcceptInvitation(parts[0], name)
		return gandrInvitationMsg{err: err}
	}
}

func gandrGroupCommand(session *gandr.Session, command string) tea.Cmd {
	return func() tea.Msg {
		parts := strings.Fields(command)
		if len(parts) < 2 {
			return gandrGroupMsg{err: fmt.Errorf("använd: /grupp skapa, /grupp öppna eller /grupp lista")}
		}
		switch parts[1] {
		case "lista":
			groups, err := session.PrivateGroups()
			return gandrGroupMsg{groups: groups, err: err}
		case "skapa":
			if len(parts) < 4 {
				return gandrGroupMsg{err: fmt.Errorf("använd: /grupp skapa NAMN LÖSENORD")}
			}
			name := strings.Join(parts[2:len(parts)-1], " ")
			group, err := session.CreatePrivateGroup(name, parts[len(parts)-1])
			if err != nil {
				return gandrGroupMsg{err: err}
			}
			groups, _ := session.PrivateGroups()
			return gandrGroupMsg{groups: groups, active: &group.ID}
		case "öppna":
			if len(parts) != 4 {
				return gandrGroupMsg{err: fmt.Errorf("använd: /grupp öppna ID LÖSENORD")}
			}
			decoded, err := hex.DecodeString(parts[2])
			if err != nil || len(decoded) != 32 {
				return gandrGroupMsg{err: fmt.Errorf("grupp-ID ska vara 64 hextecken")}
			}
			var id [32]byte
			copy(id[:], decoded)
			if err := session.UnlockPrivateGroup(id, parts[3]); err != nil {
				return gandrGroupMsg{err: err}
			}
			messages, err := session.PrivateGroupMessages(id, 200)
			groups, _ := session.PrivateGroups()
			return gandrGroupMsg{groups: groups, active: &id, messages: messages, err: err}
		case "stäng":
			groups, err := session.PrivateGroups()
			return gandrGroupMsg{groups: groups, err: err}
		default:
			return gandrGroupMsg{err: fmt.Errorf("okänt gruppkommando")}
		}
	}
}

func sendPrivateGandrGroup(session *gandr.Session, id [32]byte, content string) tea.Cmd {
	return func() tea.Msg {
		if _, err := session.SendPrivateGroup(id, content); err != nil {
			return gandrGroupMsg{err: err}
		}
		messages, err := session.PrivateGroupMessages(id, 200)
		groups, _ := session.PrivateGroups()
		return gandrGroupMsg{groups: groups, active: &id, messages: messages, err: err}
	}
}

func appendGandrMessage(messages map[[32]byte][]gandr.Message, message gandr.Message) {
	if messages == nil {
		return
	}
	if message.Hash != ([32]byte{}) {
		for _, existing := range messages[message.ChannelID] {
			if existing.Hash == message.Hash {
				return
			}
		}
	}
	messages[message.ChannelID] = append(messages[message.ChannelID], message)
}

func blockSelectedGandr(a App) tea.Cmd {
	if a.GandrSession == nil || len(a.GandrContacts) == 0 {
		return func() tea.Msg { return gandrStatusMsg{err: fmt.Errorf("ingen kontakt vald")} }
	}
	contact := a.GandrContacts[min(a.GandrRightCursor, len(a.GandrContacts)-1)]
	return func() tea.Msg {
		return gandrStatusMsg{err: a.GandrSession.Block(contact.Pubkey, "blockerad lokalt")}
	}
}

func closeGandrSession(a *App) {
	if a == nil || a.GandrSession == nil {
		return
	}
	_ = a.GandrSession.Close()
	a.GandrSession = nil
	a.GandrChannels = nil
	a.GandrMessages = nil
	a.GandrContacts = nil
	a.GandrPeers = nil
	a.GandrGroups = nil
	a.GandrActiveGroup = nil
	a.GandrGroupMessages = nil
}

func createGandr(subsystem *gandr.Subsystem, passphrase string) tea.Cmd {
	return func() tea.Msg {
		if subsystem == nil {
			return gandrMsg{err: fmt.Errorf("GANDR-gränsen saknas")}
		}
		err := subsystem.Create(passphrase)
		return gandrMsg{summary: subsystem.Summary(), err: err, created: true}
	}
}

func deleteGandr(subsystem *gandr.Subsystem) tea.Cmd {
	return func() tea.Msg {
		if subsystem == nil {
			return gandrDeleteMsg{err: fmt.Errorf("GANDR-gränsen saknas")}
		}
		return gandrDeleteMsg{err: subsystem.DeleteVault()}
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
	b.WriteString(fmt.Sprintf("\nNODER       %d\nFJÄRRPEERS  %d\nOBJEKT      %d", snapshot.Peers+1, snapshot.Peers, snapshot.Objects))
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
				return dataMsg{kind: "forums", forums: out, refresh: state.LastSyncedAt.IsZero() || time.Since(state.LastSyncedAt) >= 24*time.Hour, refreshURL: n.URL, refreshParent: n.ID}
			}
		}
		if cached, err := cachedThreads(s, n.ID); err == nil && len(cached) > 0 {
			state, _ := s.ExternalSyncState("flashback:threads:" + n.ID)
			return dataMsg{kind: "threads", threads: cached, refresh: state.LastSyncedAt.IsZero() || time.Since(state.LastSyncedAt) >= 10*time.Minute, refreshURL: n.URL}
		}
		out, threads, forumErr := c.ForumPage(context.Background(), n)
		if forumErr == nil && len(out) > 0 {
			_ = s.SaveForums(out)
			_ = s.SetExternalSyncState(external.SyncState{Source: navigationSource + ":" + n.ID, LastSyncedAt: time.Now(), Status: "ok"})
			return dataMsg{kind: "forums", forums: out}
		}

		// A Flashback forum can advertise children in the navbar while the
		// opened page contains only a thread listing (or a changed forum
		// template). Never leave the user on a blank forum view: try the
		// thread-list parser as the second semantic interpretation of the same
		// page and persist what it finds.
		if forumErr == nil {
			if err := s.SaveThreads(n.ID, threads); err != nil {
				return dataMsg{kind: "threads", err: err}
			}
			_ = s.SetExternalSyncState(external.SyncState{Source: "flashback:threads:" + n.ID, LastSyncedAt: time.Now(), Status: "ok"})
			return dataMsg{kind: "threads", threads: threads}
		}
		return dataMsg{kind: "forums", err: forumErr}
	}
}

func cachedThreads(s *store.Store, forumID string) ([]flashback.ThreadSummary, error) {
	rows, err := s.DB.Query(`SELECT t.id,t.title,t.url,t.replies,t.views,t.last_post_at,t.last_post_author,t.sticky,t.page_count FROM forum_threads ft JOIN threads t ON t.id=ft.thread_id WHERE ft.forum_id=? AND trim(t.title)<>'' AND lower(trim(t.title)) NOT LIKE 'utan titel%' ORDER BY ft.position`, forumID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []flashback.ThreadSummary
	for rows.Next() {
		var t flashback.ThreadSummary
		var sticky int
		if err := rows.Scan(&t.ID, &t.Title, &t.URL, &t.Replies, &t.Views, &t.LastPostAt, &t.LastPostAuthor, &sticky, &t.PageCount); err != nil {
			return nil, err
		}
		t.Sticky = sticky != 0
		out = append(out, t)
	}
	return out, rows.Err()
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
		// ParseNavigation returns the complete snapshot so parent links can be
		// persisted. The root pane must only receive actual roots; otherwise a
		// successful refresh flattens every subforum into the visible menu.
		var roots []flashback.ForumNode
		for _, node := range nodes {
			if node.ParentID == "" {
				roots = append(roots, node)
			}
		}
		return dataMsg{kind: "forums", forums: roots}
	}
}

func refreshForumNavigation(s *store.Store, c *flashback.Client, rawURL, parentID string) tea.Cmd {
	return func() tea.Msg {
		nodes, err := c.Forum(context.Background(), rawURL)
		if err != nil {
			return dataMsg{kind: "forums", err: err}
		}
		if err = s.SaveForums(nodes); err != nil {
			return dataMsg{kind: "forums", err: err}
		}
		_ = s.SetExternalSyncState(external.SyncState{Source: navigationSource + ":" + parentID, LastSyncedAt: time.Now(), Status: "ok"})
		rows, err := s.Forums(parentID)
		if err != nil {
			return dataMsg{kind: "forums", err: err}
		}
		defer rows.Close()
		var children []flashback.ForumNode
		for rows.Next() {
			var child flashback.ForumNode
			var hasChildren int
			if scanErr := rows.Scan(&child.ID, &child.Title, &child.URL, &hasChildren); scanErr != nil {
				return dataMsg{kind: "forums", err: scanErr}
			}
			child.HasChildren = hasChildren != 0
			children = append(children, child)
		}
		return dataMsg{kind: "forums", forums: children}
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
func loadPosts(s *store.Store, c *flashback.Client, id string, meshRuntime *meshruntime.Runtime) tea.Cmd {
	return func() tea.Msg {
		finish := diagnostics.Start("thread.posts")
		defer finish()
		threadTitle := ""
		if s != nil {
			_ = s.DB.QueryRow(`SELECT title FROM threads WHERE id=?`, id).Scan(&threadTitle)
		}
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
				return dataMsg{kind: "posts", posts: out, threadID: id, threadTitle: threadTitle}
			}
		}
		p, e := c.Thread(context.Background(), id, 1)
		if e == nil {
			_ = s.SavePage(p)
			if meshRuntime != nil {
				if payload, marshalErr := json.Marshal(p); marshalErr == nil {
					object := mesh.NewObject(mesh.ThreadPageSnapshot, "flashback", id+":1", time.Now(), payload, mesh.OriginVerified)
					_ = meshRuntime.PutLocal(object)
				}
			}
			return dataMsg{kind: "posts", posts: p.Posts, threadID: id, threadTitle: firstNonEmpty(p.Title, threadTitle)}
		}
		if meshRuntime != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if object, meshErr := meshRuntime.GetResource(ctx, "flashback", id+":1", mesh.ThreadPageSnapshot); meshErr == nil {
				var cached flashback.ParsedPage
				if decodeErr := json.Unmarshal(object.Payload, &cached); decodeErr == nil {
					_ = s.SavePage(cached)
					return dataMsg{kind: "posts", posts: cached.Posts, threadID: id, threadTitle: firstNonEmpty(cached.Title, threadTitle)}
				}
			}
		}
		return dataMsg{kind: "posts", posts: p.Posts, threadID: id, threadTitle: threadTitle, err: e}
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
func loadRemoteThread(s *store.Store, c *flashback.Client, r flashback.SearchResult, meshRuntime *meshruntime.Runtime) tea.Cmd {
	return loadPosts(s, c, r.ThreadID, meshRuntime)
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
