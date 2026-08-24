// Package cacheui is the small operator console for a BACKFLASH cache peer.
// It has no Gandr dependency and displays only public mesh/runtime metadata.
package cacheui

import (
	"fmt"
	"strings"
	"time"

	"github.com/backflash-cli/backflash/internal/mesh"
	meshruntime "github.com/backflash-cli/backflash/internal/mesh/runtime"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type tickMsg time.Time

type Model struct {
	Runtime   *meshruntime.Runtime
	Config    mesh.Config
	Width     int
	Height    int
	Pulse     int
	ShowKey   bool
	Invite    string
	Status    string
	StartedAt time.Time
}

var (
	brand  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81"))
	accent = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	ok     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42"))
	warn   = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	dim    = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	box    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("238")).Padding(0, 1)
)

func New(runtime *meshruntime.Runtime, cfg mesh.Config) Model {
	return Model{Runtime: runtime, Config: cfg, Status: "operatörskonsol redo"}
}

func (m Model) Init() tea.Cmd { return tick() }

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(now time.Time) tea.Msg { return tickMsg(now) })
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch value := msg.(type) {
	case tea.WindowSizeMsg:
		m.Width, m.Height = value.Width, value.Height
	case tickMsg:
		m.Pulse++
		return m, tick()
	case tea.KeyMsg:
		switch value.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "k":
			m.ShowKey = !m.ShowKey
			m.Status = "publik nyckel " + map[bool]string{true: "visas", false: "dold"}[m.ShowKey]
		case "i":
			invite, err := m.Runtime.Invite()
			if err != nil {
				m.Status = "inbjudan kunde inte skapas: " + err.Error()
			} else {
				m.Invite = invite
				m.Status = "inbjudan skapad · kopiera raden nedan"
			}
		case "r":
			m.Status = "status uppdaterad"
		}
	}
	return m, nil
}

func (m Model) View() string {
	snapshot := meshruntime.Snapshot{}
	if m.Runtime != nil {
		snapshot = m.Runtime.Snapshot()
	}
	state := string(snapshot.State)
	stateView := warn.Render(state)
	if snapshot.State == meshruntime.Running {
		stateView = ok.Render(state)
	}
	var b strings.Builder
	b.WriteString(brand.Render("BACKFLASH CACHE // NÄTVERKSKONSOL"))
	b.WriteString("  " + dim.Render("publik cache-peer · ingen GANDR-data"))
	b.WriteString("\n\n")
	status := []string{
		"STATUS       " + stateView,
		fmt.Sprintf("HEALTH       %s", health(snapshot)),
		fmt.Sprintf("PEERS        %d", snapshot.Peers),
		fmt.Sprintf("OBJEKT       %d", snapshot.Objects),
		fmt.Sprintf("RX / TX      %s / %s", formatBytes(snapshot.BytesRecv), formatBytes(snapshot.BytesSent)),
		"DELNING      " + map[bool]string{true: ok.Render("PÅ"), false: dim.Render("AV")}[snapshot.ShareCache],
	}
	b.WriteString(box.Render(strings.Join(status, "\n")))
	b.WriteString("\n\n" + renderFlow(snapshot, m.Pulse))

	key := snapshot.Identity
	if m.ShowKey && m.Runtime != nil {
		key = m.Runtime.PublicKeyString()
	}
	b.WriteString("\n\n" + box.Render("IDENTITET\n"+key+"\n"+dim.Render("k · visa/dölj full publik nyckel")))
	if m.Invite != "" {
		b.WriteString("\n\n" + box.Render("NÄTVERKSINBJUDAN\n"+m.Invite+"\n"+dim.Render("Endast publik peer-information · i · skapa igen")))
	}
	if snapshot.LastError != "" {
		b.WriteString("\n\n" + warn.Render("FEL · "+snapshot.LastError))
	}
	b.WriteString("\n\n" + dim.Render(m.Status+" · r uppdatera · i inbjudan · k nyckel · q avsluta"))
	return b.String()
}

func renderFlow(snapshot meshruntime.Snapshot, pulse int) string {
	packet := []string{"◇", "◆", "◇", "·"}[pulse%4]
	if snapshot.Peers == 0 {
		return box.Render("PAKETFLÖDE\n\nPEER  " + dim.Render("· väntar på ansluten peer") + "\n\nCACHE " + dim.Render("· redo för publika objekt"))
	}
	return box.Render("PAKETFLÖDE\n\nPEER  " + accent.Render(packet+" HAVE · publikt objekt") + "  ───────▶  CACHE\nCACHE " + ok.Render("◇ GET / OBJECT") + "  ◀───────  PEER\n\n" + dim.Render("hashprefix och storlek visas aldrig som innehåll"))
}

func health(snapshot meshruntime.Snapshot) string {
	switch snapshot.State {
	case meshruntime.Running:
		return ok.Render("OK")
	case meshruntime.Degraded:
		return warn.Render("DEGRADED")
	case meshruntime.Error:
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196")).Render("FEL")
	default:
		return dim.Render("—")
	}
}

func formatBytes(value uint64) string {
	if value < 1024 {
		return fmt.Sprintf("%d B", value)
	}
	if value < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(value)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(value)/(1024*1024))
}

func Run(runtime *meshruntime.Runtime, cfg mesh.Config) error {
	model := New(runtime, cfg)
	program := tea.NewProgram(model, tea.WithAltScreen())
	_, err := program.Run()
	return err
}
