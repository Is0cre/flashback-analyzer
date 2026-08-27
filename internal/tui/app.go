package tui

import (
	"context"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/backflash-cli/backflash/internal/diagnostics"
	"github.com/backflash-cli/backflash/internal/external"
	"github.com/backflash-cli/backflash/internal/external/polisen"
	"github.com/backflash-cli/backflash/internal/flashback"
	"github.com/backflash-cli/backflash/internal/gandr"
	"github.com/backflash-cli/backflash/internal/geo"
	"github.com/backflash-cli/backflash/internal/mesh"
	meshruntime "github.com/backflash-cli/backflash/internal/mesh/runtime"
	"github.com/backflash-cli/backflash/internal/service"
	"github.com/backflash-cli/backflash/internal/store"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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
	page          int
	pageCount     int
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
type gandrConnectMsg struct{ err error }
type gandrGroupMsg struct {
	groups   []gandr.PrivateGroup
	active   *[32]byte
	messages []gandr.PrivateGroupMessage
	invite   string
	err      error
}

type meshTickMsg struct{}
type gandrPeerTickMsg struct{}
type userLocationMsg struct {
	loc geo.Location
	err error
}
type alertTickMsg struct{}
type seedConnectMsg struct{ err error }
type App struct {
	Store       *store.Store
	Client      *flashback.Client
	CurrentView View
	Width       int
	Height      int
	Forums      []flashback.ForumNode
	Threads     []flashback.ThreadSummary
	Posts       []flashback.Post
	ThreadID    string
	ThreadTitle string
	Results     []flashback.SearchResult
	Stack       []flashback.ForumNode
	Cursor      int
	// Cursor is the position inside the current view. ThreadCursor remains
	// fixed while reading posts so the middle panel keeps the opened thread
	// selected when j/k moves through the reader.
	ThreadCursor  int
	Status        string
	Input         textinput.Model
	SearchRemote  bool
	Query         string
	RemotePage    int
	ForumPage     int
	ReaderPage    int
	ReaderMaxPage int
	Events        []external.ExternalEvent
	EventDetail   *external.ExternalEvent
	EventService  *service.ExternalEventsService
	// Proximity alarm: opt-in via BACKFLASH_ALERT_RADIUS_KM (see New). Off
	// (AlertRadiusKM == 0) unless the user explicitly sets that env var,
	// since resolving GeoClient means sending this machine's public IP to a
	// third-party geolocation API.
	AlertRadiusKM      float64
	GeoClient          *geo.Client
	UserLocation       *geo.Location
	AlertBaseline      bool // true once the first event batch has been seen, so startup's cached history doesn't all alarm at once
	AlertedEventIDs    map[string]bool
	ActiveAlert        *external.ExternalEvent
	ActiveAlertKM      float64
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
	// GandrUnlockGroupID is set when the input is focused specifically to
	// unlock this locked group (from clicking it), as opposed to any other
	// use of the input field — the next Enter is treated as that group's
	// password rather than a chat message or slash command.
	GandrUnlockGroupID *[32]byte
	GandrGroupMessages map[[32]byte][]gandr.PrivateGroupMessage
	GandrRightCursor   int
	// SeedYggdrasilKey is the well-known first-contact peer dialed
	// automatically once a chat session comes online, so a new user never
	// has to learn what a Yggdrasil key even is to end up talking to
	// someone. See defaultSeedYggdrasilKey.
	SeedYggdrasilKey string
	// SeedBootstrapPeers are Yggdrasil transport peers an embedded chat
	// daemon dials to reach the overlay at all — SeedYggdrasilKey alone
	// is a federation target, not a physical link. See
	// defaultSeedBootstrapPeer.
	SeedBootstrapPeers []string
	MeshRuntime        *meshruntime.Runtime
	MeshState          meshruntime.Snapshot
	PaletteOpen        bool
	PaletteCursor      int
	PostViewport       viewport.Model
	PostViewportReady  bool
}

// Palette: deliberately small and consistent — one color per meaning, reused
// identically across Flashback, GANDR and backflash-cache (see
// internal/cacheui/app.go), so the same hue always means the same thing no
// matter which view or binary you're in. No pink/purple, and every color is
// pulled down a shade from a "loud" default: warning/critical/accent/online
// are muted rather than neon so nothing competes for attention on its own —
// severity is read from hue + bold + the accompanying glyph/label, not from
// brightness. accent and warning are deliberately distinct hues (they used
// to collide on the same orange, making a keybinding hint look like an
// active warning).
var (
	brand        = lipgloss.NewStyle().Foreground(lipgloss.Color("75")).Bold(true)  // app title/logo — sky blue
	titleStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81"))  // view headings — cyan
	sectionStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("67"))  // panel sub-headers — steel blue
	accent       = lipgloss.NewStyle().Foreground(lipgloss.Color("215")).Bold(true) // interactive hints (keybindings) — peach
	muted        = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))            // de-emphasized text — grey
	metadata     = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))            // secondary metadata — light grey
	online       = lipgloss.NewStyle().Foreground(lipgloss.Color("108")).Bold(true) // success/ok — sage green
	warning      = lipgloss.NewStyle().Foreground(lipgloss.Color("178"))            // degraded/caution — muted gold
	critical     = lipgloss.NewStyle().Foreground(lipgloss.Color("167")).Bold(true) // error/danger — muted brick red
	strong       = lipgloss.NewStyle().Bold(true)                                   // emphasis without forcing a hue (thread/post titles)
	selected     = lipgloss.NewStyle().Foreground(lipgloss.Color("232")).Background(lipgloss.Color("31")).Bold(true)
	selectedMeta = lipgloss.NewStyle().Foreground(lipgloss.Color("235")).Background(lipgloss.Color("31"))
	pinStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("215"))
	linkStyle    = titleStyle.Underline(true) // URLs in post text — cyan+underline, the conventional "this is a link" signal
)

var urlPattern = regexp.MustCompile(`https?://[^\s]+`)

// highlightURLs styles URLs found in post text so they stand out from
// surrounding prose without needing to read every word to spot them.
// Trailing punctuation (sentence periods, closing parens/quotes the poster
// wrapped the URL in) is excluded from the styled span so it doesn't look
// like part of the link.
func highlightURLs(text string) string {
	return urlPattern.ReplaceAllStringFunc(text, func(match string) string {
		trimmed := strings.TrimRight(match, ".,;:!?)]}'\"")
		return linkStyle.Render(trimmed) + match[len(trimmed):]
	})
}

// Bump this when the persisted navigation shape/parser changes. It causes
// one background refresh instead of trusting a snapshot written by the old
// flat-root bug.
const navigationSource = "flashback:navigation:v6-sitemap-parent-ancestors"

// defaultSeedYggdrasilKey is BACKFLASH's own well-known chat seed: a gandrd
// daemon run on the project's cache server with no job other than being a
// first contact for new clients (see the Peering section in README.md). It
// never sees message content and stores nothing identity-linkable — it's a
// courier, not a host, same as any other node on the network.
//
// Empty until that daemon is actually deployed and its printed Yggdrasil
// key (from its own startup log: "gandrd: yggdrasil node key: <hex>") is
// pasted in here. Override for self-hosting or testing with
// BACKFLASH_SEED_KEY; set it to "-" to disable auto-connect entirely.
const defaultSeedYggdrasilKey = "66de53ae2ecbef6c404cd2ffec0fa261c0eae4c978727472017bffa0ef655a31"

// defaultSeedBootstrapPeer is the same well-known seed's own reachable
// Yggdrasil transport address. It does double duty: it's the physical
// link an embedded chat daemon dials to reach the overlay at all
// (defaultSeedYggdrasilKey alone is a federation target, not a route —
// see EmbeddedOptions.BootstrapPeers), and once on the overlay, the same
// node is also that federation target. Override with
// BACKFLASH_SEED_PEER (comma-separated for more than one).
const defaultSeedBootstrapPeer = "tcp://77.42.49.189:4243"

func New(s *store.Store, c *flashback.Client) App {
	input := textinput.New()
	input.Prompt = "> "
	input.CharLimit = 200
	eventClient := polisen.NewClient(nil, nil)
	eventService := &service.ExternalEventsService{Store: s, Provider: eventClient, RefreshAfter: 2 * time.Minute, Now: time.Now}
	meshConfig := mesh.Load()
	dashboard := &service.DashboardService{Store: s, Now: time.Now, MeshConfigured: meshConfig.Enabled}
	app := App{Store: s, Client: c, CurrentView: ViewOverview, Input: input, Status: "REDO · cache lokal", RemotePage: 1, ForumPage: 1, EventService: eventService, DashboardSvc: dashboard, Gandr: gandr.New(), MeshRuntime: meshruntime.New(meshConfig), PostViewport: viewport.New(20, 6)}
	// Proximity alarm is opt-in: only resolve a location (and thus only ever
	// contact the third-party geolocation API) when the user has explicitly
	// set a radius via BACKFLASH_ALERT_RADIUS_KM.
	if radius, err := strconv.ParseFloat(strings.TrimSpace(os.Getenv("BACKFLASH_ALERT_RADIUS_KM")), 64); err == nil && radius > 0 {
		app.AlertRadiusKM = radius
		app.GeoClient = geo.NewClient(nil)
	}
	app.SeedYggdrasilKey = defaultSeedYggdrasilKey
	if override := strings.TrimSpace(os.Getenv("BACKFLASH_SEED_KEY")); override != "" {
		app.SeedYggdrasilKey = override
	}
	if app.SeedYggdrasilKey == "-" {
		app.SeedYggdrasilKey = ""
	}
	app.SeedBootstrapPeers = []string{defaultSeedBootstrapPeer}
	if override := strings.TrimSpace(os.Getenv("BACKFLASH_SEED_PEER")); override != "" {
		app.SeedBootstrapPeers = strings.Split(override, ",")
	}
	return app
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
	cmds := []tea.Cmd{loadCachedEvents(a.EventService), loadDashboard(a.DashboardSvc), startMesh(a.MeshRuntime)}
	if a.AlertRadiusKM > 0 && a.GeoClient != nil {
		cmds = append(cmds, locateUser(a.GeoClient), alertTick())
	}
	return tea.Batch(cmds...)
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
			if m.page > 0 {
				a.ForumPage = m.page
			}
			if len(m.threads) == 0 {
				a.Status = "INGA TRÅDAR · tryck r för att hämta igen"
			}
			if m.refresh {
				target := m.refreshURL
				if target == "" {
					target = flashback.BaseURL
				}
				return a, refreshThreads(a.Store, a.Client, target, activeForum(a), a.ForumPage)
			}
		case "posts":
			a.Posts, a.CurrentView = m.posts, ViewReader
			if m.page > 0 {
				a.ReaderPage = m.page
			}
			if m.pageCount > 0 {
				a.ReaderMaxPage = m.pageCount
			}
			if m.threadID != "" {
				a.ThreadID = m.threadID
			}
			if m.threadTitle != "" {
				a.ThreadTitle = m.threadTitle
			}
			a.refreshPostViewport(true)
			if m.refresh && m.refreshURL != "" {
				return a, loadThreadPage(a.Store, a.Client, a.ThreadID, 1)
			}
		case "search":
			a.Results, a.CurrentView = m.results, ViewRemoteSearch
		case "events":
			a.Events = m.events
			if a.CurrentView == ViewExternalEvents {
				a.EventDetail = m.detail
			}
			a = a.checkProximityAlerts(m.events)
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
				a.Status = "E2E-CHATT · FEL · valvet kunde inte skapas"
			} else {
				a.Status = "E2E-CHATT · FEL · lösenordet kunde inte verifieras"
			}
			if a.GandrFailures >= 3 {
				a.GandrLockedUntil = time.Now().Add(10 * time.Second)
				a.Input.Blur()
				a.Input.SetValue("")
				a.Status = "E2E-CHATT · för många fel · upplåsning pausad i 10 sekunder"
			} else if !m.created {
				a.Input.SetValue("")
				a.Input.Placeholder = "Försök igen · E2E-CHATT-lösenord"
				a.Input.Focus()
			}
		} else {
			a.GandrCreating, a.GandrConfirming, a.GandrPassphrase = false, false, ""
			a.GandrFailures = 0
			a.GandrLockedUntil = time.Time{}
			a.Input.EchoMode = textinput.EchoNormal
			a.Input.Placeholder = ""
			a.Status = "E2E-CHATT · identiteten är upplåst lokalt"
			playSound(soundConfirmation)
			return a, connectGandr(a.Gandr, a.SeedYggdrasilKey, a.SeedBootstrapPeers)
		}
	case gandrSessionMsg:
		if m.err != nil {
			a.Status = "E2E-CHATT · daemon ej ansluten · starta gandrd separat"
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
			a.Status = fmt.Sprintf("E2E-CHATT · lokal runtime · daemon ej ansluten · %d kanaler", len(m.channels))
		} else if m.session.Online() {
			a.Status = fmt.Sprintf("E2E-CHATT · nätverk ansluten · %d kanaler", len(m.channels))
		} else {
			a.Status = fmt.Sprintf("E2E-CHATT · lokal runtime · %d kanaler", len(m.channels))
		}
		cmds := []tea.Cmd{waitGandrIncoming(m.session), refreshGandrPeers(m.session)}
		// Auto-dial the well-known seed so a brand new user ends up able to
		// talk to someone without ever learning what a Yggdrasil key is.
		// Skipped entirely offline (no daemon to dial through) or when no
		// seed is configured.
		if !m.offline && a.SeedYggdrasilKey != "" {
			cmds = append(cmds, connectSeed(m.session, a.SeedYggdrasilKey))
		}
		return a, tea.Batch(cmds...)
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
		if !m.message.Local {
			playSound(soundIncomingMessage)
		}
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
			a.Status = "E2E-CHATT · användaren kunde inte läggas till · " + m.err.Error()
		} else if a.GandrSession != nil {
			a.GandrContacts, _ = a.GandrSession.Contacts()
			a.GandrAddMode = false
			a.Status = "E2E-CHATT · användaren sparad lokalt"
			playSound(soundContactAdded)
		}
	case gandrConnectMsg:
		if m.err != nil {
			a.Status = "E2E-CHATT · anslutning misslyckades · " + m.err.Error()
		} else {
			a.Status = "E2E-CHATT · federationsförsök startat · väntar på handskakning"
		}
	case gandrGroupMsg:
		if m.err != nil {
			a.Status = "E2E-CHATT · privat grupp · " + m.err.Error()
		} else {
			a.GandrGroups = m.groups
			a.GandrActiveGroup = m.active
			// m.messages is only populated by the commands that actually
			// fetch it (skapa/öppna); a nil check here keeps /grupp bjud
			// (which reuses this same message to report its invite string)
			// from wiping an already-loaded conversation back to empty.
			if m.active != nil && m.messages != nil {
				if a.GandrGroupMessages == nil {
					a.GandrGroupMessages = make(map[[32]byte][]gandr.PrivateGroupMessage)
				}
				a.GandrGroupMessages[*m.active] = m.messages
			}
			if m.invite != "" {
				a.Status = "E2E-CHATT · inbjudan (delas som lösenordet, inte publikt): " + m.invite
			} else {
				a.Status = "E2E-CHATT · privat grupp · krypterad lokalt"
			}
		}
	case gandrChannelsMsg:
		if m.err != nil {
			a.Status = "E2E-CHATT · kanalåtgärden misslyckades · " + m.err.Error()
		} else {
			a.GandrChannels = m.channels
			a.Cursor = min(a.Cursor, max(0, len(a.GandrChannels)-1))
			a.Status = fmt.Sprintf("E2E-CHATT · %d kanaler", len(m.channels))
		}
	case gandrStatusMsg:
		if m.err != nil {
			a.Status = "E2E-CHATT · blockering misslyckades · " + m.err.Error()
		} else {
			a.Status = "E2E-CHATT · kontakt blockerad lokalt"
		}
	case gandrInvitationMsg:
		if m.err != nil {
			a.Status = "E2E-CHATT · inbjudan misslyckades · " + m.err.Error()
		} else if m.token != "" {
			copyToClipboard(m.token)
			a.Status = "E2E-CHATT · inbjudan kopierad till urklipp: " + m.token
		} else {
			a.Status = "E2E-CHATT · kontakt tillagd via inbjudan"
		}
	case gandrDeleteMsg:
		if m.err != nil {
			a.Status = "E2E-CHATT · radering misslyckades · " + m.err.Error()
		} else {
			a.GandrDeleteConfirm = false
			if a.GandrRecreate {
				a.GandrRecreate = false
				a.GandrCreating = true
				a.GandrConfirming = false
				a.Input.EchoMode = textinput.EchoPassword
				a.Input.Placeholder = "Nytt E2E-CHATT-lösenord"
				a.Input.Focus()
				a.Status = "E2E-CHATT · gamla valvet raderat · skapa nytt valv"
			} else {
				a.Status = "E2E-CHATT · valv och privata data raderade"
			}
		}
	case meshTickMsg:
		if a.MeshRuntime == nil {
			return a, nil
		}
		previousPeers := a.MeshState.Peers
		a.MeshState = a.MeshRuntime.Snapshot()
		a.applyMeshSnapshot(a.MeshState)
		if a.MeshState.Peers > previousPeers {
			playSound(soundPeerConnected)
		}
		return a, meshTick()
	case userLocationMsg:
		if m.err != nil {
			// Best-effort: leave UserLocation nil so alertTick just keeps
			// polling without ever being able to trigger an alarm, rather
			// than treating a flaky geolocation API as a fatal error.
			a.Status = "LARM · plats kunde inte slås upp · " + m.err.Error()
			return a, nil
		}
		loc := m.loc
		a.UserLocation = &loc
		return a, nil
	case alertTickMsg:
		if a.EventService != nil && a.EventService.Stale(polisen.Source) {
			return a, tea.Batch(refreshEvents(a.EventService), alertTick())
		}
		return a, alertTick()
	case seedConnectMsg:
		// Quiet by design: the user never asked for this connection
		// attempt, so a failure (seed offline, no network yet) shouldn't
		// read as something they need to fix. Success is worth a nod since
		// it's the whole point of "simple as fuck" onboarding.
		if m.err == nil {
			a.Status = "E2E-CHATT · ansluten till publik seed"
		}
		return a, nil
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
							a.Status = "E2E-CHATT · radering avbruten"
							return a, nil
						}
						a.Input.Blur()
						a.GandrDeleteConfirm = false
						closeGandrSession(&a)
						return a, deleteGandr(a.Gandr)
					}
					if time.Now().Before(a.GandrLockedUntil) {
						a.Status = fmt.Sprintf("E2E-CHATT · upplåsning pausad · %s kvar", time.Until(a.GandrLockedUntil).Round(time.Second))
						a.Input.Blur()
						return a, nil
					}
					if a.GandrCreating {
						if !a.GandrConfirming {
							if raw == "" {
								a.Status = "E2E-CHATT · lösenordet får inte vara tomt"
								a.Input.Focus()
								return a, nil
							}
							a.GandrPassphrase = raw
							a.GandrConfirming = true
							a.Input.Placeholder = "Upprepa E2E-CHATT-lösenordet"
							a.Input.Focus()
							return a, nil
						}
						if raw != a.GandrPassphrase {
							a.GandrPassphrase = ""
							a.GandrConfirming = false
							a.Input.Placeholder = "Nytt E2E-CHATT-lösenord"
							a.Status = "E2E-CHATT · lösenorden matchar inte"
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
					if a.GandrUnlockGroupID != nil {
						id := *a.GandrUnlockGroupID
						a.GandrUnlockGroupID = nil
						// Takes the password directly instead of building a
						// "/grupp öppna ID LÖSENORD" string: that command is
						// space-delimited, so a password containing a space
						// would silently be cut short if routed through it.
						return a, unlockAndOpenGandrGroup(a.GandrSession, id, raw)
					}
					if q == "" || a.GandrSession == nil {
						return a, nil
					}
					if strings.HasPrefix(q, "/grupp") {
						return a, gandrGroupCommand(a.GandrSession, a.GandrActiveGroup, q)
					}
					if strings.HasPrefix(q, "/connect ") {
						return a, connectGandrPeer(a.GandrSession, strings.TrimSpace(strings.TrimPrefix(q, "/connect ")))
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
				// Leaving GANDR must not close the active session — once
				// unlocked, it stays connected in the background so
				// returning to GANDR later does not require the password
				// again. Only explicit lock/delete actions close it.
				a.CurrentView, a.Cursor, a.EventDetail = ViewOverview, 0, nil
				a.Input.Blur()
				a.GandrCreating, a.GandrConfirming, a.GandrPassphrase = false, false, ""
				return a, loadDashboard(a.DashboardSvc)
			}
			return a, tea.Quit
		case "home", "h":
			a.CurrentView, a.Cursor, a.EventDetail = ViewOverview, 0, nil
			a.Input.Blur()
			a.GandrCreating, a.GandrConfirming, a.GandrPassphrase = false, false, ""
			return a, loadDashboard(a.DashboardSvc)
		case "f":
			// "f" always jumps to the forum root, like "h" jumps to the
			// dashboard. The breadcrumb Stack must be cleared here too, or
			// leftover entries from an earlier browse session corrupt the
			// forum path shown once the user descends again.
			a.CurrentView, a.Cursor, a.Stack = ViewForums, 0, nil
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
			a.Cursor = 0
			if a.GandrSession != nil {
				// The session survived the earlier navigation away from
				// GANDR (see "q"/"home" above), so it is still unlocked and
				// connected — go straight back into the chat instead of
				// re-prompting for the password.
				a.CurrentView = ViewGandrChat
				return a, nil
			}
			a.CurrentView = ViewGandr
			a.GandrCreating, a.GandrConfirming, a.GandrPassphrase = false, false, ""
			a.Input.SetValue("")
			a.Input.EchoMode = textinput.EchoPassword
			if time.Now().Before(a.GandrLockedUntil) {
				a.Status = fmt.Sprintf("E2E-CHATT · upplåsning pausad · %s kvar", time.Until(a.GandrLockedUntil).Round(time.Second))
				a.Input.Placeholder = "Försök igen senare"
			} else if a.Gandr != nil && a.Gandr.HasVault() {
				a.Input.Placeholder = "E2E-CHATT-lösenord"
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
				a.Input.Placeholder = "Nytt E2E-CHATT-lösenord"
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
			// Petname shortcut: name whoever spoke most recently in the
			// current channel, instead of hunting down their hex pubkey
			// to type into /add by hand. Pre-fills the input with the key
			// already in place — just type a name and press Enter.
			if a.CurrentView == ViewGandrChat && a.GandrActiveGroup == nil && len(a.GandrChannels) > 0 {
				channel := a.GandrChannels[min(a.Cursor, len(a.GandrChannels)-1)]
				messages := a.GandrMessages[channel.ID]
				for i := len(messages) - 1; i >= 0; i-- {
					sender := messages[i].Sender
					if messages[i].Local || sender == ([32]byte{}) {
						continue // that's you — nothing to name
					}
					a.GandrAddMode = true
					a.Input.EchoMode = textinput.EchoNormal
					a.Input.Placeholder = "publik nyckel + namn"
					a.Input.SetValue(hex.EncodeToString(sender[:]) + " ")
					a.Input.CursorEnd()
					a.Input.Focus()
					break
				}
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
				// "b" only ever meant "back up a level" everywhere else in
				// the app; inside a private group it did nothing, so there
				// was no way to leave one once opened. Closing the group
				// here mirrors what "/grupp stäng" already did, just
				// discoverable without knowing the slash command exists.
				if a.GandrActiveGroup != nil {
					a.GandrActiveGroup = nil
					a.Status = "E2E-CHATT · lämnade den privata gruppen"
				}
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
			if a.ActiveAlert != nil {
				a.ActiveAlert = nil
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
				return a, refreshThreads(a.Store, a.Client, a.Stack[len(a.Stack)-1].URL, activeForum(a), a.ForumPage)
			}
		case "f5":
			// Only set the "updating" status on a view that actually starts a
			// refresh command below. Views with no case here (Gandr, mesh,
			// remote search, ...) or a ViewThreads with an empty Stack must
			// leave a.Status untouched instead of getting stuck on a message
			// that no in-flight command will ever clear.
			switch a.CurrentView {
			case ViewOverview:
				a.Status = "UPPDATERAR · hämtar aktuell vy…"
				return a, tea.Batch(loadDashboard(a.DashboardSvc), loadCachedEvents(a.EventService))
			case ViewForums:
				if len(a.Stack) == 0 {
					a.Status = "UPPDATERAR · hämtar aktuell vy…"
					return a, refreshNavigation(a.Store, a.Client, flashback.BaseURL)
				}
				current := a.Stack[len(a.Stack)-1]
				a.Status = "UPPDATERAR · hämtar aktuell vy…"
				if isCategory(current) {
					return a, refreshCategoryNavigation(a.Store, a.Client, current)
				}
				return a, refreshForumNavigation(a.Store, a.Client, current.URL, current.ID)
			case ViewThreads:
				if len(a.Stack) > 0 {
					current := a.Stack[len(a.Stack)-1]
					a.Status = "UPPDATERAR · hämtar aktuell vy…"
					return a, refreshThreads(a.Store, a.Client, current.URL, current.ID, a.ForumPage)
				}
			case ViewReader:
				a.Status = "UPPDATERAR · hämtar aktuell vy…"
				return a, loadThreadPage(a.Store, a.Client, a.ThreadID, a.ReaderPage)
			case ViewExternalEvents:
				a.Status = "UPPDATERAR · hämtar aktuell vy…"
				return a, refreshEvents(a.EventService)
			}
		case "]", "right":
			if a.CurrentView == ViewRemoteSearch {
				a.RemotePage++
				return a, remoteSearch(a.Client, a.Query, a.RemotePage)
			}
			if a.CurrentView == ViewThreads && len(a.Stack) > 0 {
				a.ForumPage++
				a.Cursor = 0
				a.Status = fmt.Sprintf("TRÅDAR · SIDA %d HÄMTAS…", a.ForumPage)
				return a, refreshThreads(a.Store, a.Client, a.Stack[len(a.Stack)-1].URL, activeForum(a), a.ForumPage)
			}
			if a.CurrentView == ViewReader && a.ReaderPage < a.ReaderMaxPage {
				a.ReaderPage++
				a.Cursor = 0
				a.Status = fmt.Sprintf("INLÄGG · SIDA %d HÄMTAS…", a.ReaderPage)
				return a, loadThreadPage(a.Store, a.Client, a.ThreadID, a.ReaderPage)
			}
		case "[", "left":
			if a.CurrentView == ViewRemoteSearch && a.RemotePage > 1 {
				a.RemotePage--
				return a, remoteSearch(a.Client, a.Query, a.RemotePage)
			}
			if a.CurrentView == ViewThreads && a.ForumPage > 1 && len(a.Stack) > 0 {
				a.ForumPage--
				a.Cursor = 0
				a.Status = fmt.Sprintf("TRÅDAR · SIDA %d HÄMTAS…", a.ForumPage)
				return a, refreshThreads(a.Store, a.Client, a.Stack[len(a.Stack)-1].URL, activeForum(a), a.ForumPage)
			}
			if a.CurrentView == ViewReader && a.ReaderPage > 1 {
				a.ReaderPage--
				a.Cursor = 0
				a.Status = fmt.Sprintf("INLÄGG · SIDA %d HÄMTAS…", a.ReaderPage)
				return a, loadThreadPage(a.Store, a.Client, a.ThreadID, a.ReaderPage)
			}
		case "esc":
			a.Input.Blur()
		}
	case tea.MouseMsg:
		if a.CurrentView == ViewGandrChat && m.Action == tea.MouseActionPress && m.Button == tea.MouseButtonLeft {
			sidebarWidth := gandrSidebarWidth(a)
			// The header (title + blank line) and the panel's own top
			// border/title/separator occupy 4 screen rows before the
			// sidebar's first content line (gandrSidebarRows index 0).
			const gandrSidebarScreenOffset = 4
			_, actions := gandrSidebarRows(a, sidebarWidth)
			if m.X < sidebarWidth+2 {
				if row := m.Y - gandrSidebarScreenOffset; row >= 0 && row < len(actions) {
					return gandrHandleSidebarClick(a, actions[row])
				}
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
			if m.Y >= gandrSidebarScreenOffset+len(actions) && a.GandrSession != nil {
				a.Input.EchoMode = textinput.EchoNormal
				a.Input.Placeholder = "Meddelande eller /join kanal"
				a.Input.Focus()
			}
		}
	}
	return a, nil
}

// gandrHandleSidebarClick dispatches a click on one of the sidebar rows
// produced by gandrSidebarRows. Kept separate from the MouseMsg case so the
// (line, action) pairing and its dispatch logic sit next to each other.
func gandrHandleSidebarClick(a App, action gandrSidebarAction) (tea.Model, tea.Cmd) {
	switch action.Kind {
	case gandrActionSelectChannel:
		a.Cursor = action.Index
	case gandrActionJoinChannel:
		if a.GandrSession != nil {
			a.Input.SetValue("/join ")
			a.Input.EchoMode = textinput.EchoNormal
			a.Input.Placeholder = "KANALNAMN"
			a.Input.Focus()
		}
	case gandrActionLeaveChannel:
		if a.GandrSession != nil && len(a.GandrChannels) > 0 {
			channel := a.GandrChannels[min(a.Cursor, len(a.GandrChannels)-1)]
			return a, gandrLeave(a.GandrSession, channel.ID)
		}
	case gandrActionCreateGroup:
		if a.GandrSession != nil {
			a.Input.SetValue("/grupp skapa ")
			a.Input.EchoMode = textinput.EchoNormal
			a.Input.Placeholder = "NAMN LÖSENORD"
			a.Input.Focus()
		}
	case gandrActionDeleteVault:
		if a.Gandr != nil && a.Gandr.HasVault() {
			beginGandrDelete(&a, false)
		}
	case gandrActionListGroups:
		if a.GandrSession != nil {
			return a, gandrGroupCommand(a.GandrSession, a.GandrActiveGroup, "/grupp lista")
		}
	case gandrActionOpenGroup:
		if a.GandrSession == nil || action.Index < 0 || action.Index >= len(a.GandrGroups) {
			break
		}
		group := a.GandrGroups[action.Index]
		switch {
		case a.GandrActiveGroup != nil && *a.GandrActiveGroup == group.ID:
			// Already open — nothing to do.
		case a.GandrSession.IsGroupUnlocked(group.ID):
			return a, openGandrGroup(a.GandrSession, group.ID)
		default:
			a.GandrUnlockGroupID = &group.ID
			a.Input.SetValue("")
			a.Input.EchoMode = textinput.EchoPassword
			a.Input.Placeholder = "LÖSENORD för " + group.Name
			a.Input.Focus()
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
	cursor := a.Cursor
	if a.CurrentView == ViewReader {
		cursor = a.ThreadCursor
	}
	if len(a.Threads) > 0 && cursor >= 0 && cursor < len(a.Threads) {
		return a.Threads[cursor].ID
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
			a.ForumPage = 1
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
			a.ThreadCursor = a.Cursor
			a.CurrentView = ViewReader
			a.Cursor = 0
			a.ThreadID, a.ThreadTitle = selected.ID, selected.Title
			a.ReaderPage, a.ReaderMaxPage = 1, selected.PageCount
			return a, loadPosts(a.Store, a.Client, selected.ID, a.MeshRuntime)
		}
	case ViewRemoteSearch:
		if a.Cursor < len(a.Results) {
			r := a.Results[a.Cursor]
			a.ThreadCursor = -1
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
		b.WriteString(titleStyle.Render("BACKFLASH E2E-CHATT") + "  " + metadata.Render("· PRIVAT NÄTVERK"))
	} else {
		b.WriteString(brand.Render("BACKFLASH // DISKURS-NOC"))
	}
	// Keep the shell compact: one breathing line below the header and one
	// above the footer is enough on short terminals.
	b.WriteString("\n")
	// The proximity alarm banner shows regardless of which view is active
	// (a nearby event matters just as much while reading a thread as while
	// on the police page) and stays until acknowledged with "a".
	if a.ActiveAlert != nil {
		b.WriteString(renderProximityAlertBanner(*a.ActiveAlert, a.ActiveAlertKM))
	}
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
		if a.ReaderMaxPage > 0 {
			b.WriteString("  " + metadata.Render(fmt.Sprintf("· SIDA %d / %d · ◀/▶ byt sida", a.ReaderPage, a.ReaderMaxPage)))
		}
		b.WriteString("\n\n")
		if a.Width >= 100 {
			b.WriteString(renderReaderWorkspace(a))
		} else if a.PostViewportReady {
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
		b.WriteString(renderPoliceWorkspace(a))
		if a.EventDetail != nil {
			b.WriteString("\n\n" + renderEventDetail(*a.EventDetail))
		}
	case ViewMesh:
		b.WriteString(viewHeading("CACHE-MESH", "BACKFLASH · PUBLIK CACHE"))
		b.WriteString(renderMeshDetail(a.MeshState))
	case ViewGandr:
		b.WriteString(viewHeading("BACKFLASH E2E-CHATT", "KRYPTERAD CHATT · PETNAMN · YGGDRASIL"))
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
				b.WriteString("\n\nInget E2E-CHATT-valv finns ännu.")
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
				b.WriteString("\nE2E-CHATT-daemon är inte ansluten ännu.")
			}
		}
		if a.GandrDeleteConfirm {
			if a.GandrRecreate {
				b.WriteString("\n\n" + critical.Render("VARNING: gamla E2E-CHATT-identiteten och client.db raderas."))
				b.WriteString("\nDet går inte att återställa ett bortglömt lösenord. Skriv RADERA för nytt valv.")
			} else {
				b.WriteString("\n\n" + critical.Render("VARNING: detta raderar E2E-CHATT-identiteten och den privata client.db."))
				b.WriteString("\nSkriv RADERA och tryck Enter för att fortsätta.")
			}
		}
		b.WriteString("\n\n" + muted.Render("E2E-CHATT-identitet, privat databas och petnames hålls separerade från BACKFLASH."))
	case ViewGandrChat:
		b.WriteString(renderGandrChat(a))
	}
	if a.Input.Focused() && a.CurrentView != ViewGandrChat {
		b.WriteString("\n" + a.Input.View())
	}
	if a.Status != "" {
		b.WriteString("\n" + muted.Render(a.Status))
	}
	if a.PaletteOpen {
		b.WriteString("\n" + renderPalette(a))
	}
	if a.CurrentView == ViewGandr {
		b.WriteString("\n" + muted.Render("j/k flytta · Enter öppna · / kommando · Esc lämna fält · x radera valv · n radera + skapa nytt · q tillbaka"))
	} else if a.CurrentView != ViewGandrChat {
		b.WriteString("\n" + muted.Render("j/k · Enter · ") + accent.Render("F5") + muted.Render(" uppdatera · ") + accent.Render("f") + muted.Render(" forum · ") + accent.Render("/") + muted.Render(" fjärr · ") + accent.Render("Ctrl+F") + muted.Render(" lokalt · ") + accent.Render("p") + muted.Render(" polis · ") + accent.Render("m") + muted.Render(" mesh · ") + accent.Render("g") + muted.Render(" chatt · ") + accent.Render("h") + muted.Render(" hem · q tillbaka"))
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
	{Key: "g", Title: "E2E-chatt", Hint: "krypterad chatt · yggdrasil"},
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
		// Same as the direct "h"/"home" keybinding: navigating home must not
		// close an already-unlocked GANDR session.
		a.CurrentView, a.Cursor, a.EventDetail = ViewOverview, 0, nil
		return a, loadDashboard(a.DashboardSvc)
	case "f":
		// Same as the direct "f" keybinding: reset the breadcrumb Stack so
		// a previous browse session cannot leak into the forum path.
		a.CurrentView, a.Cursor, a.Stack = ViewForums, 0, nil
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
		a.Cursor = 0
		if a.GandrSession != nil {
			// Same as the direct "g" keybinding: a still-connected session
			// goes straight back to the chat instead of re-prompting.
			a.CurrentView = ViewGandrChat
			return a, nil
		}
		a.CurrentView = ViewGandr
		a.GandrCreating, a.GandrConfirming, a.GandrPassphrase = false, false, ""
		a.Input.SetValue("")
		a.Input.EchoMode = textinput.EchoPassword
		if a.Gandr != nil && a.Gandr.HasVault() {
			a.Input.Placeholder = "E2E-CHATT-lösenord"
			a.Input.Focus()
		}
		return a, nil
	}
	return a, nil
}
func viewHeading(label, context string) string {
	return titleStyle.Render(label) + "  " + metadata.Render("· "+context)
}

// gandrGroupHint swaps the footer's group hint depending on whether a
// private group is currently open, so "leave the group" is only advertised
// when there is actually a group to leave.
func gandrGroupHint(a App) string {
	if a.GandrActiveGroup != nil {
		return " b lämna grupp · /grupp bjud ·"
	}
	return " /grupp skapa|öppna ·"
}

// gandrSidebarWidth mirrors renderGandrChat's own width breakpoint so the
// mouse handler and the renderer never compute a different sidebar width
// from each other.
func gandrSidebarWidth(a App) int {
	width := a.Width
	if width < 60 {
		width = 80
	}
	if width < 90 {
		return 20
	}
	return 24
}

type gandrSidebarActionKind int

const (
	gandrActionNone gandrSidebarActionKind = iota
	gandrActionSelectChannel
	gandrActionJoinChannel
	gandrActionLeaveChannel
	gandrActionCreateGroup
	gandrActionDeleteVault
	gandrActionListGroups
	gandrActionOpenGroup
)

type gandrSidebarAction struct {
	Kind  gandrSidebarActionKind
	Index int // into a.GandrChannels or a.GandrGroups, depending on Kind
}

// gandrSidebarRows builds the GANDR sidebar as parallel (line, action)
// slices: rendering and mouse click handling both read from this single
// function, so a click can never land on a different element than what's
// visually there — before this, the click handler kept its own hand-counted
// row offsets that had drifted out of sync with the actual render order.
func gandrSidebarRows(a App, sidebarWidth int) ([]string, []gandrSidebarAction) {
	lines := []string{sectionStyle.Render("KANALER"), metadata.Render("────────────────────")}
	actions := []gandrSidebarAction{{}, {}}
	if len(a.GandrChannels) == 0 {
		lines = append(lines, muted.Render("inga kanaler"))
		actions = append(actions, gandrSidebarAction{})
	} else {
		for i, channel := range a.GandrChannels {
			line := "# " + truncate(channel.Name, sidebarWidth-4)
			if i == a.Cursor {
				line = selected.Render("› " + truncate(channel.Name, sidebarWidth-4))
			}
			lines = append(lines, line)
			actions = append(actions, gandrSidebarAction{Kind: gandrActionSelectChannel, Index: i})
		}
	}
	lines = append(lines, muted.Render("/join namn"), muted.Render("/leave"), accent.Render("+ skapa kanal"), accent.Render("+ skapa privat grupp"))
	actions = append(actions,
		gandrSidebarAction{Kind: gandrActionJoinChannel},
		gandrSidebarAction{Kind: gandrActionLeaveChannel},
		gandrSidebarAction{Kind: gandrActionJoinChannel},
		gandrSidebarAction{Kind: gandrActionCreateGroup},
	)
	if a.Gandr != nil && a.Gandr.HasVault() {
		lines = append(lines, critical.Render("[!] radera E2E-CHATT-valv"))
		actions = append(actions, gandrSidebarAction{Kind: gandrActionDeleteVault})
	}
	if len(a.GandrGroups) > 0 {
		lines = append(lines, "", sectionStyle.Render("PRIVATA GRUPPER"))
		actions = append(actions, gandrSidebarAction{}, gandrSidebarAction{})
		for i, group := range a.GandrGroups {
			marker := "🔒"
			switch {
			case a.GandrActiveGroup != nil && *a.GandrActiveGroup == group.ID:
				marker = "›"
			case a.GandrSession != nil && a.GandrSession.IsGroupUnlocked(group.ID):
				marker = "🔓"
			}
			lines = append(lines, marker+" "+truncate(group.Name, sidebarWidth-5))
			actions = append(actions, gandrSidebarAction{Kind: gandrActionOpenGroup, Index: i})
		}
		lines = append(lines, muted.Render("/grupp lista"))
		actions = append(actions, gandrSidebarAction{Kind: gandrActionListGroups})
	}
	return lines, actions
}

// gandrSenderPalette assigns each chat participant a stable color derived
// from their pubkey — the same trick most IRC/chat clients use so a name
// reads consistently at a glance without parsing hex. Colors are chosen
// distinct from every other meaning already assigned elsewhere in this
// file (accent/warning/online/critical/brand/titleStyle), so a username
// is never mistaken for a status or severity signal.
var gandrSenderPalette = []string{"110", "150", "175", "182", "116", "223", "141", "108"}

// gandrSenderStyle picks this sender's color. "du" (your own messages)
// always gets the same fixed, distinct style rather than a palette color,
// so your own lines stay instantly recognizable regardless of what your
// own pubkey happens to hash to.
func gandrSenderStyle(sender [32]byte, isSelf bool) lipgloss.Style {
	if isSelf {
		return strong
	}
	sum := 0
	for _, b := range sender {
		sum += int(b)
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(gandrSenderPalette[sum%len(gandrSenderPalette)]))
}

// gandrChatInputBox renders the message input area. Unlike the rest of
// the app (where the shared input only appears once focused, e.g. mid
// search), the chat input stays visible at a fixed position the whole
// time — the layout shouldn't jump the moment you start typing, the way
// a real chat client's input bar never does.
func gandrChatInputBox(a App, width int) string {
	if a.Input.Focused() {
		return a.Input.View()
	}
	return muted.Render(truncate("› Enter för att skriva…", width))
}

func renderGandrChat(a App) string {
	width := a.Width
	if width < 60 {
		width = 80
	}
	sidebarWidth := gandrSidebarWidth(a)
	mainWidth := width - sidebarWidth - 3
	if mainWidth < 28 {
		mainWidth = 28
	}

	sidebarLines, _ := gandrSidebarRows(a, sidebarWidth)
	var sidebar strings.Builder
	sidebar.WriteString(strings.Join(sidebarLines, "\n"))

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
			main.WriteString(muted.Render(fmt.Sprintf("Välkommen till %s. Inga meddelanden ännu.", groupName)))
		} else {
			start := 0
			if len(messages) > 18 {
				start = len(messages) - 18
			}
			for _, message := range messages[start:] {
				isSelf := message.Sender == ([32]byte{})
				sender := fmt.Sprintf("~%x", message.Sender[:4])
				if isSelf {
					sender = "du"
				}
				style := gandrSenderStyle(message.Sender, isSelf)
				stamp := time.Unix(0, message.At).Local().Format("15:04")
				main.WriteString(metadata.Render(stamp) + " " + style.Render(fmt.Sprintf("%-8s", sender)) + " " + message.Content + "\n")
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
		main.WriteString(online.Render(fmt.Sprintf("Välkommen till #%s — meddelanden är krypterade och routas via Yggdrasil. Tryck n för att döpa den senaste avsändaren.", channel.Name)) + "\n")
		messages := a.GandrMessages[channel.ID]
		if len(messages) == 0 {
			main.WriteString(muted.Render("Inga meddelanden ännu."))
		} else {
			start := 0
			if len(messages) > 18 {
				start = len(messages) - 18
			}
			for _, message := range messages[start:] {
				isSelf := message.Local || message.Sender == ([32]byte{})
				sender := fmt.Sprintf("~%x", message.Sender[:4])
				if isSelf {
					sender = "du"
				}
				style := gandrSenderStyle(message.Sender, isSelf)
				stamp := time.Unix(0, message.At).Local().Format("15:04")
				main.WriteString(metadata.Render(stamp) + " " + style.Render(fmt.Sprintf("%-8s", sender)) + " " + message.Content + "\n")
			}
		}
	}
	main.WriteString("\n" + gandrChatInputBox(a, mainWidth-2))

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
				isOnline := gandrPeerOnline(a.GandrPeers, contact.Pubkey)
				marker, presence := "○", "EJ PÅ MESH"
				if isOnline {
					marker, presence = "●", "PÅ MESH"
				}
				var line string
				if i == a.GandrRightCursor {
					// The selection background already carries the row's
					// emphasis, so the marker keeps its plain glyph here
					// instead of nesting a second color inside it.
					line = selected.Render("› " + marker + " " + truncate(name, memberWidth-4))
				} else if isOnline {
					line = online.Render(marker) + " " + truncate(name, memberWidth-4)
				} else {
					line = muted.Render(marker) + " " + truncate(name, memberWidth-4)
				}
				members.WriteString(line + "\n")
				if isOnline {
					members.WriteString(online.Render("  "+presence) + "\n")
				} else {
					members.WriteString(muted.Render("  "+presence) + "\n")
				}
			}
		}
		members.WriteString("\n" + muted.Render("a lägg till · x blockera"))
		memberView := gandrPanel(members.String(), memberWidth)
		return viewHeading("BACKFLASH E2E-CHATT · IRC", "PRIVAT NÄTVERK") + "\n\n" + lipgloss.JoinHorizontal(lipgloss.Top, left, mainView, memberView) + "\n\n" + muted.Render("j/k kanal · ↑/↓ användare · Enter skriv · a lägg till · /invite · /connect ·"+gandrGroupHint(a)+" x blockera · q tillbaka")
	}
	return viewHeading("BACKFLASH E2E-CHATT · IRC", "PRIVAT NÄTVERK") + "\n\n" + lipgloss.JoinHorizontal(lipgloss.Top, left, mainView) + "\n\n" + muted.Render("j/k kanal · Enter skriv · /invite · /connect ·"+gandrGroupHint(a)+" q tillbaka")
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
		title := n.Title
		if !n.Browsable {
			// A category is a local structural grouping, not a real forum —
			// mark it the same muted grey used for de-emphasized text
			// elsewhere, so it reads as "not a destination" at a glance
			// instead of only being distinguishable by reading the label.
			title = muted.Render(title)
		}
		line := title
		if n.HasChildren {
			line += "  " + titleStyle.Render("›")
		}
		if i == c {
			line = selected.Render(n.Title)
			if n.HasChildren {
				line += "  " + selected.Render("›")
			}
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}
func renderThreads(xs []flashback.ThreadSummary, c int) string {
	return renderThreadsAtWidth(xs, c, 120)
}
func renderThreadsAtWidth(xs []flashback.ThreadSummary, c, width int) string {
	if len(xs) == 0 {
		return muted.Render("Ingen trådlista finns lokalt ännu.")
	}
	if width < 32 {
		width = 32
	}
	var b strings.Builder
	for i, n := range xs {
		prefix := fmt.Sprintf("%3d  ", i+1)
		if n.Sticky {
			prefix = "📌 " + prefix
		}
		line := prefix + clip(firstNonEmpty(n.Title, "Tråd #"+n.ID), width-lipgloss.Width(prefix))
		if i == c {
			line = selected.Render(line)
		}
		meta := clip(fmt.Sprintf("#%s · %s svar · %s visningar · %s sidor", n.ID, number(n.Replies), number(n.Views), pageCount(n.PageCount)), width)
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
	threadCursor := a.Cursor
	// Keep the forum path visible on ordinary laptop terminals. The old
	// breakpoint collapsed too early and made a long thread list context-free.
	if width < 72 {
		content := "◀ föregående sida · SIDA " + number(a.ForumPage) + " · ▶ nästa sida\n" + strings.Join(threadListWindow(a.Threads, threadCursor, width-4, visibleThreadCount(a.Height)), "\n")
		return lipgloss.NewStyle().Width(width-2).Border(lipgloss.NormalBorder()).BorderForeground(lipgloss.Color("238")).Padding(0, 1).Render(content)
	}
	if width < 100 {
		leftWidth := 27
		centerWidth := width - leftWidth - 3
		if centerWidth < 38 {
			centerWidth = 38
		}
		threads := []string{fmt.Sprintf("◀ sida %d ▶", a.ForumPage)}
		if len(a.Threads) == 0 {
			threads = append(threads, muted.Render("Inga trådar hämtade ännu."), muted.Render("Tryck r för att uppdatera."))
		} else {
			threads = append(threads, threadListWindow(a.Threads, threadCursor, centerWidth-2, visibleThreadCount(a.Height))...)
		}
		return joinPanels(
			renderPanel("FORUMTRÄD", forumPathLines(a, leftWidth-2), leftWidth),
			renderPanel("TRÅDAR", threads, centerWidth),
		)
	}

	leftWidth, rightWidth := 27, 39
	centerWidth := width - leftWidth - rightWidth - 6
	if centerWidth < 34 {
		centerWidth = 34
	}

	left := forumPathLines(a, leftWidth-2)

	threadLines := make([]string, 0, visibleThreadCount(a.Height)*2+1)
	if len(a.Threads) == 0 {
		threadLines = append(threadLines, muted.Render("Ingen trådar hämtade ännu."), muted.Render("Tryck r för att uppdatera."))
	} else {
		threadLines = threadListWindow(a.Threads, threadCursor, centerWidth-2, visibleThreadCount(a.Height))
	}

	detail := []string{"Ingen tråd vald."}
	if threadCursor >= 0 && threadCursor < len(a.Threads) {
		thread := a.Threads[threadCursor]
		detail = []string{
			strong.Render(clip(firstNonEmpty(thread.Title, "Tråd #"+thread.ID), rightWidth-2)),
			"",
			"ID          " + metadata.Render("#"+thread.ID),
			"Svar        " + accent.Render(number(thread.Replies)),
			"Visningar   " + titleStyle.Render(number(thread.Views)),
			"Sidor       " + sectionStyle.Render(pageCount(thread.PageCount)+" sidor"),
			"Författare   " + firstNonEmpty(thread.Author, "—"),
			"Senast      " + firstNonEmpty(thread.LastPostAuthor, "—"),
			"Tid         " + formatPostTime(thread.LastPostAt),
			"",
			muted.Render("Enter öppnar tråden"),
		}
	}

	return joinPanels(
		renderPanel("FORUMTRÄD", left, leftWidth),
		renderPanel(fmt.Sprintf("TRÅDAR · SIDA %d · ◀/▶ BYT SIDA", a.ForumPage), threadLines, centerWidth),
		renderPanel("DETALJER", detail, rightWidth),
	)
}

func forumPathLines(a App, width int) []string {
	if width < 10 {
		width = 10
	}
	lines := []string{"⌂ FLASHBACK"}
	for i, node := range a.Stack {
		marker := "  "
		if i == len(a.Stack)-1 {
			marker = "› "
		}
		lines = append(lines, marker+clip(node.Title, width))
	}
	if len(a.Stack) == 0 {
		lines = append(lines, muted.Render("Ingen forumvald"))
	} else {
		lines = append(lines, "", muted.Render("b · upp en nivå"))
	}
	return lines
}

// renderReaderWorkspace keeps the forum context and the current category
// visible while reading. The right panel is the only scrolling area; the
// forum path and thread cursor remain stable landmarks.
func renderReaderWorkspace(a App) string {
	width := a.Width
	if width < 100 {
		width = 100
	}
	leftWidth, centerWidth := 25, 35
	rightWidth := width - leftWidth - centerWidth - 6
	if rightWidth < 36 {
		rightWidth = 36
	}

	threadLines := threadListWindow(a.Threads, a.ThreadCursor, centerWidth-2, visibleThreadCount(a.Height))
	if len(a.Threads) == 0 {
		threadLines = []string{muted.Render("Ingen trådlista lokalt."), muted.Render("fylls på vid uppdatering")}
	}

	readerLines := []string{muted.Render("Inläggen hämtas…")}
	if len(a.Posts) > 0 {
		reader := a.PostViewport
		reader.Width = rightWidth - 2
		height := a.Height - 9
		if height < 6 {
			height = 6
		}
		reader.Height = height
		reader.SetContent(renderPostsWidth(a.Posts, a.Cursor, rightWidth-4))
		readerLines = strings.Split(reader.View(), "\n")
	}

	return joinPanels(
		renderPanel("FORUMTRÄD", forumPathLines(a, leftWidth-2), leftWidth),
		renderPanel("TRÅDAR · ◀/▶ BYT SIDA", append([]string{fmt.Sprintf("SIDA %d", a.ForumPage)}, threadLines...), centerWidth),
		renderPanel("LÄSER", readerLines, rightWidth),
	)
}

func visibleThreadCount(height int) int {
	if height < 1 {
		return 12
	}
	// Each thread occupies two rows. Keep the shell (heading and footer)
	// visible instead of letting a long list scroll the whole terminal.
	count := (height - 10) / 2
	if count < 4 {
		return 4
	}
	return count
}

func threadListWindow(xs []flashback.ThreadSummary, cursor, width, count int) []string {
	if len(xs) == 0 {
		return []string{muted.Render("Ingen trådlista finns lokalt ännu.")}
	}
	if width < 32 {
		width = 32
	}
	if count < 1 || count > len(xs) {
		count = len(xs)
	}
	// A negative cursor (e.g. ThreadCursor == -1 after opening a thread from
	// remote search) means "no local selection" rather than "select the
	// first row" — window around the top of the list but never highlight a
	// row that was never actually selected.
	hasSelection := cursor >= 0
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= len(xs) {
		cursor = len(xs) - 1
	}
	start := cursor - count/2
	if start < 0 {
		start = 0
	}
	if start+count > len(xs) {
		start = len(xs) - count
	}
	lines := make([]string, 0, count*2)
	for i := start; i < start+count; i++ {
		n := xs[i]
		prefix := fmt.Sprintf("%3d  ", i+1)
		if n.Sticky {
			prefix = "📌 " + prefix
		}
		title := clip(firstNonEmpty(n.Title, "Tråd #"+n.ID), width-lipgloss.Width(prefix))
		var line, meta string
		if hasSelection && i == cursor {
			// The selection background already carries the row's emphasis;
			// per-field colors would fight it, so this row stays uniform
			// (matches the same tradeoff made for police glyphs and the
			// GANDR presence markers elsewhere in this file).
			line = selected.Render(prefix + title)
			meta = selectedMeta.Render(clip(threadMetaLine(n), width))
		} else {
			line = prefix + strong.Render(title)
			meta = clip(threadMetaLineColored(n), width)
		}
		lines = append(lines, line, meta)
	}
	return lines
}

// threadMetaLine is the plain (unstyled) thread metadata line, used as-is
// under selection highlighting where per-field colors would fight the
// background, and as the basis threadMetaLineColored re-derives from.
func threadMetaLine(n flashback.ThreadSummary) string {
	return fmt.Sprintf("      #%s · %s svar · %s visningar · %s sidor", n.ID, number(n.Replies), number(n.Views), pageCount(n.PageCount))
}

// threadMetaLineColored gives each field its own color — thread number
// (de-emphasized, it's a reference not a measure), replies, views and page
// count — so they read as distinct values at a glance instead of one flat
// grey line, without needing to parse the labels.
func threadMetaLineColored(n flashback.ThreadSummary) string {
	return "      " + metadata.Render("#"+n.ID) +
		metadata.Render(" · ") + accent.Render(number(n.Replies)+" svar") +
		metadata.Render(" · ") + titleStyle.Render(number(n.Views)+" visningar") +
		metadata.Render(" · ") + sectionStyle.Render(pageCount(n.PageCount)+" sidor")
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
		for _, line := range wrapText(highlightURLs(firstNonEmpty(n.Text, "(tomt inlägg)")), width-4) {
			b.WriteString("  " + line + "\n")
		}
		for _, quote := range n.Quotes {
			// Not URL-highlighted: this whole line is already wrapped in
			// muted.Render(), and nesting another style's reset code inside
			// it would cut the grey styling short partway through the line.
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

// renderPoliceWorkspace lays out the police event list and the Sweden map.
// On wide terminals they sit side by side, list on the left and map on the
// right, with the map's dot for the currently selected event highlighted
// (see renderSwedenMap's highlight param) so the list cursor and its map
// position stay visibly linked. Narrow terminals fall back to the map
// stacked above the list, since there isn't room for both columns without
// truncating event text down to nothing.
func renderPoliceWorkspace(a App) string {
	if a.Width < 90 {
		mapRows := max(14, min(44, a.Height-24))
		rows := 12
		if a.Height > 0 && a.Height-(mapRows+9) > rows {
			rows = a.Height - (mapRows + 9)
		}
		var b strings.Builder
		b.WriteString(renderSwedenMap(a.Events, a.Width-4, mapRows, a.Cursor))
		b.WriteString("\n\n")
		b.WriteString(titleStyle.Render(fmt.Sprintf("SENASTE HÄNDELSER · %d AV %d", minInt(rows, len(a.Events)), len(a.Events))))
		b.WriteString("\n")
		b.WriteString(renderEventWindow(a.Events, a.Cursor, rows))
		return b.String()
	}

	listWidth := a.Width * 2 / 5
	listWidth = max(38, min(58, listWidth))
	mapWidth := a.Width - listWidth - 7
	if mapWidth < 24 {
		mapWidth = 24
	}
	mapRows := max(14, min(44, a.Height-13))
	listRows := mapRows + 2
	if a.Height > 0 {
		listRows = max(6, min(listRows, a.Height-8))
	}

	var list strings.Builder
	list.WriteString(titleStyle.Render(clip(fmt.Sprintf("SENASTE HÄNDELSER · %d AV %d", minInt(listRows, len(a.Events)), len(a.Events)), listWidth)))
	list.WriteString("\n")
	list.WriteString(renderEventWindowWidth(a.Events, a.Cursor, listRows, listWidth))

	mapBlock := renderSwedenMap(a.Events, mapWidth, mapRows, a.Cursor)
	return lipgloss.JoinHorizontal(lipgloss.Top, lipgloss.NewStyle().Width(listWidth).Render(list.String()), "   ", mapBlock)
}

func renderEventWindow(xs []external.ExternalEvent, c, maxRows int) string {
	return renderEventWindowWidth(xs, c, maxRows, 0)
}

// renderEventWindowWidth is renderEventWindow with each row clipped to a
// fixed width, for placement in a narrower column (e.g. beside the map)
// where an unclipped line could wrap and break the layout. width <= 0
// leaves rows unclipped.
func renderEventWindowWidth(xs []external.ExternalEvent, c, maxRows, width int) string {
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
		// The category glyph stays in its category color even on the
		// selected row; only the descriptive text gets the cursor highlight.
		text := fmt.Sprintf("%s · %s · %s", formatSwedishEventTime(e.Timestamp), e.EventType, e.LocationName)
		if e.Title != "" {
			text += " · " + e.Title
		}
		if i == c {
			text = selected.Render(text)
		}
		line := policeCategoryGlyph(e.EventType) + " " + text
		if width > 0 {
			line = clip(line, width)
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

// renderProximityAlertBanner renders the persistent "a police event just
// showed up near you" banner shown at the top of every view until
// acknowledged (see the "a" key handler in Update).
func renderProximityAlertBanner(e external.ExternalEvent, km float64) string {
	text := fmt.Sprintf("⚠ LARM · %s · %s · %.1f km bort · tryck a för att kvittera", e.EventType, e.LocationName, km)
	return critical.Render(text) + "\n\n"
}

// policeCategories buckets Polisen's free-text event types (there is no
// fixed enum in the API) by keyword. Order matters: the first matching
// category wins, so more specific/severe categories are listed before
// broader ones (e.g. "hot" as in "Olaga hot" must land in VÅLD before any
// looser category could claim it). The final entry has no keywords and is
// the catch-all for anything unmatched.
var policeCategories = []struct {
	Label    string
	Glyph    string
	Color    string // raw ANSI-256 code backing Style, reused for map pixel fills
	Style    lipgloss.Style
	Keywords []string
}{
	{"VÅLD", "✕", "167", critical, []string{"mord", "dråp", "misshandel", "rån", "sexual", "våldtäkt", "skottlossning", "knivlagen", "vapenlagen", "explosion", "detonation", "hot"}},
	{"TRAFIK", "▲", "215", accent, []string{"trafik", "rattfylleri"}},
	{"MISSBRUK", "◆", "178", warning, []string{"narkotika", "fylleri", "lob"}},
	{"EGENDOM", "▪", "178", warning, []string{"stöld", "inbrott", "skadegörelse", "bedrägeri"}},
	{"RÄDDNING", "✚", "108", online, []string{"räddning", "försvunnen", "anträffad", "olycka", "brand"}},
	{"RUTIN", "·", "241", muted, []string{"sammanfattning", "kontroll"}},
	{"ÖVRIGT", "●", "244", metadata, nil},
}

func policeCategoryIndex(eventType string) int {
	// Match per word (by prefix, to catch Swedish compounds like
	// "Trafikolycka" or "Narkotikabrott"), not by raw substring — a plain
	// strings.Contains would let short keywords like "rån" (robbery) falsely
	// match inside unrelated words such as "från" (from).
	words := strings.FieldsFunc(strings.ToLower(eventType), func(r rune) bool {
		return !unicode.IsLetter(r)
	})
	for i, c := range policeCategories {
		for _, kw := range c.Keywords {
			for _, w := range words {
				if strings.HasPrefix(w, kw) {
					return i
				}
			}
		}
	}
	return len(policeCategories) - 1
}

func policeCategoryGlyph(eventType string) string {
	c := policeCategories[policeCategoryIndex(eventType)]
	return c.Style.Render(c.Glyph)
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
			renderPanel("E2E-CHATT", gandrDashboardLines(a), columnWidth),
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
		b.WriteString(fmt.Sprintf("STATUS\nDB REDO · Nätverk %s · Session %s · Synk %s\n\nCACHE-MESH %s · noder %d · fjärrpeers %d · delning %s · RX/TX %s / %s · E2E-CHATT %s\n", d.Network, d.Session, d.Sync, d.Mesh, d.MeshPeers+1, d.MeshPeers, d.MeshSharing, bytesUint(d.MeshRX), bytesUint(d.MeshTX), gandrStateLabel(a)))
		b.WriteString("POLISHÄNDELSER\n" + renderEventSummary(a.Events))
	} else {
		b.WriteString("DATA\n")
		b.WriteString(fmt.Sprintf("%s forum\n%s trådar\n%s inlägg\n\n", number(d.ForumCount), number(d.ThreadCount), number(d.PostCount)))
		b.WriteString("AKTIVITET\n" + number(d.PostsLastHour) + " / 60m\n\n")
		b.WriteString("MESH " + d.Mesh + "\nnoder " + number(d.MeshPeers+1) + " · fjärrpeers " + number(d.MeshPeers) + " · objekt " + number(d.MeshObjects) + "\nE2E-CHATT " + gandrStateLabel(a))
	}
	b.WriteString("\n\n" + muted.Render("[f] Forum  [/] Sök  [p] Polis  [m] Mesh  [g] Chatt  [h] Hem"))
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
	case "PÅ", "ONLINE", "AKTIV", "REDO", "VILAR", "UPPLÅST":
		return online.Render(value)
	case "FEL", "OFFLINE", "DEGRADED":
		return critical.Render(value)
	case "STARTAR", "VALD", "UPPDATERAR…", "VALV SAKNAS":
		return warning.Render(value)
	default:
		return metadata.Render(value)
	}
}

// gandrStateLabel is the single-line counterpart of gandrDashboardLines, for
// the narrower dashboard layouts that only have room for one GANDR field.
func gandrStateLabel(a App) string {
	return string(a.Gandr.Summary().State)
}

// gandrDashboardLines reads GANDR's live in-memory state directly (a cheap,
// mutex-guarded read — no vault I/O), instead of the dashboard snapshot's
// static placeholder. The snapshot is built by DashboardService, which is
// deliberately kept out of GANDR's private boundary, so it cannot know
// whether the vault is actually unlocked; only the TUI layer holds both.
func gandrDashboardLines(a App) []string {
	summary := a.Gandr.Summary()
	switch summary.State {
	case gandr.Unlocked:
		lines := []string{"ᚷ           " + statusValue(string(gandr.Unlocked))}
		if summary.Fingerprint != "" {
			lines = append(lines, "Nyckel      "+summary.Fingerprint)
		}
		if a.GandrSession != nil {
			lines = append(lines, fmt.Sprintf("Kanaler     %d", len(a.GandrChannels)))
		}
		return append(lines, "Privat läge")
	case gandr.Missing:
		return []string{"ᚷ           " + statusValue(string(gandr.Missing)), "Inget valv skapat än", "Tryck g för att skapa"}
	case gandr.UnlockErr:
		return []string{"ᚷ           " + statusValue(string(gandr.UnlockErr)), "Senaste upplåsning misslyckades", "Privat läge"}
	default:
		return []string{"ᚷ           " + statusValue(string(gandr.Locked)), "Privat läge", "Ingen data delas"}
	}
}

func joinPanels(panels ...[]string) string {
	maxRows := 0
	// renderPanel pads every line of a panel to the same fixed width, so the
	// first line's width stands in for the whole column. Once a shorter
	// panel runs out of rows, later columns must still be offset by that
	// width — otherwise every row below the shortest panel collapses
	// leftward, dragging every column after it out of alignment.
	widths := make([]int, len(panels))
	for i, panel := range panels {
		if len(panel) > maxRows {
			maxRows = len(panel)
		}
		if len(panel) > 0 {
			widths[i] = displayWidth(panel[0])
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
			} else {
				b.WriteString(strings.Repeat(" ", widths[i]))
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
	if width <= 0 {
		return ""
	}
	// ANSI-aware truncation is important here: panel rows may already contain
	// Lip Gloss styling. Rune slicing can count escape bytes as visible text,
	// producing rows that wrap at column zero and destroy the three-pane layout.
	return ansi.Truncate(value, width, "…")
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
			return gandrMsg{err: fmt.Errorf("E2E-CHATT-gränsen saknas")}
		}
		err := subsystem.Unlock(passphrase)
		return gandrMsg{summary: subsystem.Summary(), err: err}
	}
}

func connectGandr(subsystem *gandr.Subsystem, seedKey string, bootstrapPeers []string) tea.Cmd {
	return func() tea.Msg {
		if subsystem == nil {
			return gandrSessionMsg{err: fmt.Errorf("E2E-CHATT-gränsen saknas")}
		}
		// Prefer an external gandrd if one happens to be running (self-
		// hosters, or the seed server itself) — otherwise BACKFLASH runs
		// its own daemon entirely in-process (see
		// Subsystem.ConnectEmbedded), so an ordinary user never has to
		// install or run a second background service just to chat.
		session, err := subsystem.Connect(gandrSocketPath())
		offline := false
		if err != nil {
			session, err = subsystem.ConnectEmbedded(gandr.EmbeddedOptions{
				SeedYggdrasilKey: seedKey,
				BootstrapPeers:   bootstrapPeers,
			})
		}
		if err != nil {
			// Last resort: keep the local encrypted chat usable even if
			// the embedded daemon itself couldn't start (e.g. no usable
			// Yggdrasil transport at all). The session is deliberately
			// marked offline; sends are stored locally and are not
			// presented as delivered over the network.
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
				return gandrSessionMsg{err: fmt.Errorf("E2E-CHATT-daemon kopplades från")}
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

// connectGandrPeer asks the local gandrd daemon to dial and federate with
// another node directly, given the *Yggdrasil transport key* they shared
// with you (their own gandrd prints it to stderr at startup as "gandrd:
// yggdrasil node key: <hex>" — that's a different key from their GANDR
// identity/contact pubkey). Both daemons just need to be reachable on the
// wider Yggdrasil overlay, not on each other's local network.
func connectGandrPeer(session *gandr.Session, yggKeyHex string) tea.Cmd {
	return func() tea.Msg {
		if session == nil {
			return gandrConnectMsg{err: fmt.Errorf("E2E-CHATT-sessionen är inte aktiv")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return gandrConnectMsg{err: session.ConnectPeer(ctx, yggKeyHex)}
	}
}

// connectSeed is connectGandrPeer's automatic counterpart: same underlying
// dial, but fired by the app on every session start rather than by a user
// typing /connect, so its result is reported separately (see seedConnectMsg
// in Update) and stays quiet on failure instead of surfacing a scary error
// for something the user never asked for.
func connectSeed(session *gandr.Session, yggKeyHex string) tea.Cmd {
	return func() tea.Msg {
		if session == nil {
			return seedConnectMsg{err: fmt.Errorf("E2E-CHATT-sessionen är inte aktiv")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return seedConnectMsg{err: session.ConnectPeer(ctx, yggKeyHex)}
	}
}

func addGandrContact(session *gandr.Session, value string) tea.Cmd {
	return func() tea.Msg {
		if session == nil {
			return gandrContactMsg{err: fmt.Errorf("E2E-CHATT-sessionen är inte aktiv")}
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

// unlockAndOpenGandrGroup unlocks id with password and opens it, without
// round-tripping the password through gandrGroupCommand's space-delimited
// command parser — used by the click-a-locked-group-to-unlock-it flow,
// where the password comes straight from the input field and may contain
// spaces.
func unlockAndOpenGandrGroup(session *gandr.Session, id [32]byte, password string) tea.Cmd {
	return func() tea.Msg {
		if err := session.UnlockPrivateGroup(id, password); err != nil {
			return gandrGroupMsg{err: err}
		}
		messages, err := session.PrivateGroupMessages(id, 200)
		groups, _ := session.PrivateGroups()
		return gandrGroupMsg{groups: groups, active: &id, messages: messages, err: err}
	}
}

// openGandrGroup opens a group that is already unlocked in this session
// (its key is cached from an earlier create/unlock), so clicking it a
// second time — or clicking any group unlocked earlier in the same run —
// does not re-prompt for the password.
func openGandrGroup(session *gandr.Session, id [32]byte) tea.Cmd {
	return func() tea.Msg {
		if !session.IsGroupUnlocked(id) {
			return gandrGroupMsg{err: fmt.Errorf("gruppen är låst")}
		}
		messages, err := session.PrivateGroupMessages(id, 200)
		groups, _ := session.PrivateGroups()
		return gandrGroupMsg{groups: groups, active: &id, messages: messages, err: err}
	}
}

func gandrGroupCommand(session *gandr.Session, active *[32]byte, command string) tea.Cmd {
	return func() tea.Msg {
		parts := strings.Fields(command)
		if len(parts) < 2 {
			return gandrGroupMsg{err: fmt.Errorf("använd: /grupp skapa, /grupp öppna, /grupp bjud eller /grupp lista")}
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
			var id [32]byte
			var password string
			switch {
			case len(parts) == 3 && gandr.IsGroupInvite(parts[2]):
				// /grupp öppna <inbjudan> — the invite string already
				// carries both the ID and the password, so there is
				// nothing left to type or look up by hand.
				decodedID, _, decodedPassword, err := gandr.DecodeGroupInvite(parts[2])
				if err != nil {
					return gandrGroupMsg{err: fmt.Errorf("ogiltig inbjudan: %w", err)}
				}
				id, password = decodedID, decodedPassword
			case len(parts) == 4:
				decoded, err := hex.DecodeString(parts[2])
				if err != nil || len(decoded) != 32 {
					return gandrGroupMsg{err: fmt.Errorf("grupp-ID ska vara 64 hextecken")}
				}
				copy(id[:], decoded)
				password = parts[3]
			default:
				return gandrGroupMsg{err: fmt.Errorf("använd: /grupp öppna ID LÖSENORD eller /grupp öppna INBJUDAN")}
			}
			if err := session.UnlockPrivateGroup(id, password); err != nil {
				return gandrGroupMsg{err: err}
			}
			messages, err := session.PrivateGroupMessages(id, 200)
			groups, _ := session.PrivateGroups()
			return gandrGroupMsg{groups: groups, active: &id, messages: messages, err: err}
		case "bjud":
			// /grupp bjud LÖSENORD — packages the *currently open* group's
			// ID, name and the given password into one shareable string.
			// The password is never stored after creation/unlock (only a
			// password-derived key is), so it has to be re-typed here; this
			// still spares the recipient from copying a 64-character ID.
			if active == nil {
				return gandrGroupMsg{err: fmt.Errorf("öppna gruppen du vill bjuda in till först")}
			}
			if len(parts) != 3 {
				return gandrGroupMsg{err: fmt.Errorf("använd: /grupp bjud LÖSENORD")}
			}
			if err := session.UnlockPrivateGroup(*active, parts[2]); err != nil {
				return gandrGroupMsg{err: fmt.Errorf("fel lösenord, ingen inbjudan skapades: %w", err)}
			}
			groups, err := session.PrivateGroups()
			if err != nil {
				return gandrGroupMsg{err: err}
			}
			name := ""
			for _, g := range groups {
				if g.ID == *active {
					name = g.Name
					break
				}
			}
			invite, err := gandr.EncodeGroupInvite(*active, name, parts[2])
			if err != nil {
				return gandrGroupMsg{err: err}
			}
			return gandrGroupMsg{groups: groups, active: active, invite: invite}
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
			return gandrMsg{err: fmt.Errorf("E2E-CHATT-gränsen saknas")}
		}
		err := subsystem.Create(passphrase)
		return gandrMsg{summary: subsystem.Summary(), err: err, created: true}
	}
}

func deleteGandr(subsystem *gandr.Subsystem) tea.Cmd {
	return func() tea.Msg {
		if subsystem == nil {
			return gandrDeleteMsg{err: fmt.Errorf("E2E-CHATT-gränsen saknas")}
		}
		return gandrDeleteMsg{err: subsystem.DeleteVault()}
	}
}

func meshTick() tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return meshTickMsg{} })
}

// locateUser resolves the machine's approximate location once, at startup,
// for the proximity alarm. It is only ever invoked when the user opted in
// via BACKFLASH_ALERT_RADIUS_KM (see New) — this is the one place in the
// app that contacts a third-party service with this machine's public IP.
func locateUser(client *geo.Client) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		loc, err := client.Locate(ctx)
		return userLocationMsg{loc: loc, err: err}
	}
}

// alertTick drives the proximity-alarm poll independently of which view is
// active (same pattern as meshTick), so a police event near the user still
// rings the alarm while they're reading a thread rather than only while
// they happen to have the police page open.
func alertTick() tea.Cmd {
	return tea.Tick(30*time.Second, func(time.Time) tea.Msg { return alertTickMsg{} })
}

// checkProximityAlerts compares a batch of police events against the
// user's resolved location and rings the alarm for any event within
// AlertRadiusKM that hasn't already been accounted for. The very first
// batch just establishes the "already seen" baseline without alerting —
// otherwise every event already sitting in the local cache from before
// this run would alarm all at once the moment a location resolves.
func (a App) checkProximityAlerts(events []external.ExternalEvent) App {
	if a.AlertRadiusKM <= 0 || a.UserLocation == nil {
		return a
	}
	if a.AlertedEventIDs == nil {
		a.AlertedEventIDs = make(map[string]bool, len(events))
	}
	firstRun := !a.AlertBaseline
	a.AlertBaseline = true
	for i := range events {
		e := events[i]
		id := e.Source + ":" + e.ExternalID
		if a.AlertedEventIDs[id] {
			continue
		}
		a.AlertedEventIDs[id] = true
		if firstRun || e.Latitude == nil || e.Longitude == nil {
			continue
		}
		dist := geo.DistanceKM(*a.UserLocation, geo.Location{Latitude: *e.Latitude, Longitude: *e.Longitude})
		if dist <= a.AlertRadiusKM {
			a.ActiveAlert = &events[i]
			a.ActiveAlertKM = dist
			a.Status = fmt.Sprintf("LARM · %s · %.1f km bort", e.LocationName, dist)
			playAlarm()
		}
	}
	return a
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
	b.WriteString("\n\n" + muted.Render("Endast publika cacheobjekt. Ingen E2E-CHATT-identitet, cookie eller läshistorik delas."))
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
				var child, browsable int
				if e := rows.Scan(&n.ID, &n.Title, &n.URL, &child, &browsable); e == nil {
					n.HasChildren = child != 0
					n.Browsable = browsable != 0
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
			_ = s.ReplaceForumSnapshot(nodes)
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
			var child, browsable int
			if e := rows.Scan(&n.ID, &n.Title, &n.URL, &child, &browsable); e == nil {
				n.HasChildren = child != 0
				n.Browsable = browsable != 0
				out = append(out, n)
			}
		}
		return dataMsg{kind: "forums", forums: out}
	}
}

// isCategory reports whether n is a local sitemap category node rather than
// a real Flashback forum. Category nodes are never browsable, so this is the
// single source of truth instead of matching on the "category:" URL sentinel
// at each call site.
func isCategory(n flashback.ForumNode) bool {
	return !n.Browsable
}

func loadForumChildren(s *store.Store, c *flashback.Client, n flashback.ForumNode) tea.Cmd {
	return func() tea.Msg {
		rows, e := s.Forums(n.ID)
		if e == nil {
			defer rows.Close()
			var out []flashback.ForumNode
			for rows.Next() {
				var child flashback.ForumNode
				var hasChildren, browsable int
				if scanErr := rows.Scan(&child.ID, &child.Title, &child.URL, &hasChildren, &browsable); scanErr == nil {
					child.HasChildren = hasChildren != 0
					child.Browsable = browsable != 0
					out = append(out, child)
				}
			}
			if len(out) > 0 {
				if isCategory(n) {
					return dataMsg{kind: "forums", forums: out}
				}
				state, _ := s.ExternalSyncState(navigationSource + ":" + n.ID)
				return dataMsg{kind: "forums", forums: out, refresh: state.LastSyncedAt.IsZero() || time.Since(state.LastSyncedAt) >= 24*time.Hour, refreshURL: n.URL, refreshParent: n.ID}
			}
		}
		// Sitemap categories are local structural nodes and intentionally have
		// no Flashback URL. If an older cache has not materialised their
		// children yet, refresh the complete sitemap and read the children back
		// from SQLite instead of sending "category:..." to net/http.
		if isCategory(n) {
			return refreshCategoryNavigation(s, c, n)()
		}
		if cached, err := cachedThreads(s, n.ID); err == nil && len(cached) > 0 {
			state, _ := s.ExternalSyncState("flashback:threads:" + n.ID)
			return dataMsg{kind: "threads", threads: cached, refresh: needsThreadMetadata(cached) || state.LastSyncedAt.IsZero() || time.Since(state.LastSyncedAt) >= 10*time.Minute, refreshURL: n.URL}
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

func needsThreadMetadata(rows []flashback.ThreadSummary) bool {
	for _, row := range rows {
		if row.PageCount < 1 {
			return true
		}
	}
	return false
}

func refreshNavigation(s *store.Store, c *flashback.Client, rawURL string) tea.Cmd {
	return func() tea.Msg {
		nodes, err := c.Forum(context.Background(), rawURL)
		if err != nil {
			return dataMsg{kind: "forums", err: err}
		}
		if err = s.ReplaceForumSnapshot(nodes); err != nil {
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

func refreshCategoryNavigation(s *store.Store, c *flashback.Client, category flashback.ForumNode) tea.Cmd {
	return func() tea.Msg {
		nodes, err := c.Forum(context.Background(), flashback.BaseURL)
		if err != nil {
			return dataMsg{kind: "forums", err: err}
		}
		if err = s.ReplaceForumSnapshot(nodes); err != nil {
			return dataMsg{kind: "forums", err: err}
		}
		rows, err := s.Forums(category.ID)
		if err != nil {
			return dataMsg{kind: "forums", err: err}
		}
		defer rows.Close()
		var children []flashback.ForumNode
		for rows.Next() {
			var child flashback.ForumNode
			var hasChildren, browsable int
			if err := rows.Scan(&child.ID, &child.Title, &child.URL, &hasChildren, &browsable); err != nil {
				return dataMsg{kind: "forums", err: err}
			}
			child.HasChildren = hasChildren != 0
			child.Browsable = browsable != 0
			children = append(children, child)
		}
		return dataMsg{kind: "forums", forums: children}
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
			var hasChildren, browsable int
			if scanErr := rows.Scan(&child.ID, &child.Title, &child.URL, &hasChildren, &browsable); scanErr != nil {
				return dataMsg{kind: "forums", err: scanErr}
			}
			child.HasChildren = hasChildren != 0
			child.Browsable = browsable != 0
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
				return dataMsg{kind: "threads", threads: out, refresh: needsThreadMetadata(out) || state.LastSyncedAt.IsZero() || time.Since(state.LastSyncedAt) >= 10*time.Minute, refreshURL: n.URL}
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

func refreshThreads(s *store.Store, c *flashback.Client, rawURL, forumID string, page int) tea.Cmd {
	return func() tea.Msg {
		forum := flashback.ForumNode{ID: forumID, URL: rawURL}
		threads, err := c.ThreadsPage(context.Background(), forum, page)
		if err != nil {
			return dataMsg{kind: "threads", err: err}
		}
		if err = s.SaveThreads(forumID, threads); err != nil {
			return dataMsg{kind: "threads", err: err}
		}
		_ = s.SetExternalSyncState(external.SyncState{Source: "flashback:threads:" + forumID, LastSyncedAt: time.Now(), Status: "ok"})
		return dataMsg{kind: "threads", threads: threads, page: page}
	}
}
func loadPosts(s *store.Store, c *flashback.Client, id string, meshRuntime *meshruntime.Runtime) tea.Cmd {
	return func() tea.Msg {
		finish := diagnostics.Start("thread.posts")
		defer finish()
		threadTitle := ""
		pageCount := 0
		threadURL := ""
		if s != nil {
			_ = s.DB.QueryRow(`SELECT title,page_count,url FROM threads WHERE id=?`, id).Scan(&threadTitle, &pageCount, &threadURL)
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
				return dataMsg{kind: "posts", posts: out, threadID: id, threadTitle: threadTitle, page: 1, pageCount: pageCount, refresh: pageCount < 1, refreshURL: threadURL}
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
			return dataMsg{kind: "posts", posts: p.Posts, threadID: id, threadTitle: firstNonEmpty(p.Title, threadTitle), page: 1, pageCount: p.MaxPage}
		}
		if meshRuntime != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if object, meshErr := meshRuntime.GetResource(ctx, "flashback", id+":1", mesh.ThreadPageSnapshot); meshErr == nil {
				var cached flashback.ParsedPage
				if decodeErr := json.Unmarshal(object.Payload, &cached); decodeErr == nil {
					_ = s.SavePage(cached)
					return dataMsg{kind: "posts", posts: cached.Posts, threadID: id, threadTitle: firstNonEmpty(cached.Title, threadTitle), page: 1, pageCount: cached.MaxPage}
				}
			}
		}
		return dataMsg{kind: "posts", posts: p.Posts, threadID: id, threadTitle: threadTitle, err: e}
	}
}

func loadThreadPage(s *store.Store, c *flashback.Client, id string, page int) tea.Cmd {
	return func() tea.Msg {
		parsed, err := c.Thread(context.Background(), id, page)
		if err != nil {
			return dataMsg{kind: "posts", threadID: id, page: page, err: err}
		}
		if s != nil {
			if err := s.SavePage(parsed); err != nil {
				return dataMsg{kind: "posts", threadID: id, page: page, err: err}
			}
		}
		return dataMsg{kind: "posts", posts: parsed.Posts, threadID: id, page: page, pageCount: parsed.MaxPage, threadTitle: parsed.Title}
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
