package tui

import (
	"context"
	"embed"
	"fmt"
	"io"
	"strings"

	"github.com/backflash-cli/backflash/internal/flashback"
	"github.com/backflash-cli/backflash/internal/store"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

//go:embed assets/backflash.ans
var splashFS embed.FS

type View int
const ( ViewOverview View = iota; ViewForums; ViewThreads; ViewReader; ViewRemoteSearch )

type dataMsg struct { kind string; forums []flashback.ForumNode; threads []flashback.ThreadSummary; posts []flashback.Post; results []flashback.SearchResult; err error }
type App struct { Store *store.Store; Client *flashback.Client; CurrentView View; Width int; Height int; Forums []flashback.ForumNode; Threads []flashback.ThreadSummary; Posts []flashback.Post; Results []flashback.SearchResult; Stack []flashback.ForumNode; Cursor int; Status string; Input textinput.Model; SearchRemote bool; Query string; RemotePage int }

var ( accent = lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true); titleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("81")); muted = lipgloss.NewStyle().Foreground(lipgloss.Color("241")); selected = lipgloss.NewStyle().Reverse(true) )

func New(s *store.Store, c *flashback.Client) App { input := textinput.New(); input.Prompt = "> "; input.CharLimit = 200; return App{Store:s, Client:c, CurrentView:ViewOverview, Input:input, Status:"REDO · cache lokal", RemotePage:1} }

func Splash(w io.Writer, width int) { if width < 80 { fmt.Fprintln(w, "BACKFLASH"); return }; b, err := splashFS.ReadFile("assets/backflash.ans"); if err != nil { fmt.Fprintln(w, "BACKFLASH"); return }; if width < 120 { fmt.Fprintln(w, accent.Render("BACKFLASH // DISKURS-NOC")); return }; _, _ = w.Write(append(b, []byte("\x1b[0m\n")...)) }

func (a App) Init() tea.Cmd { return nil }
func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg: a.Width, a.Height = m.Width, m.Height
	case dataMsg: if m.err != nil { a.Status = "FEL · " + m.err.Error(); return a, nil }; a.Status = "REDO · cache lokal"; switch m.kind { case "forums": a.Forums = m.forums; case "threads": a.Threads = m.threads; a.CurrentView = ViewThreads; case "posts": a.Posts = m.posts; a.CurrentView = ViewReader; case "search": a.Results = m.results; a.CurrentView = ViewRemoteSearch } }
	case tea.KeyMsg:
		if a.Input.Focused() { if m.String() == "enter" { q := strings.TrimSpace(a.Input.Value()); a.Input.Blur(); if q != "" { a.Query = q; if a.SearchRemote { return a, remoteSearch(a.Client,q,1) }; return a, localSearch(a.Store,q,activeThread(a)) } }; if m.String() == "esc" { a.Input.Blur(); return a,nil }; var cmd tea.Cmd; a.Input, cmd = a.Input.Update(m); return a, cmd }
		switch m.String() {
		case "ctrl+c", "q": return a, tea.Quit
		case "f": a.CurrentView = ViewForums; a.Cursor = 0; return a, loadRoot(a.Store, a.Client)
		case "t": a.CurrentView = ViewOverview; a.Status = "SPARADE TRÅDAR · lokal data"; return a,nil
		case "b": if len(a.Stack)>0 { a.Stack=a.Stack[:len(a.Stack)-1]; return a,loadChildren(a.Store,a.Stack) }; a.CurrentView=ViewOverview; return a,nil
		case "j", "down": a.move(1)
		case "k", "up": a.move(-1)
		case "/": a.SearchRemote=true; a.Input.SetValue(""); a.Input.Focus(); return a,nil
		case "ctrl+f": a.SearchRemote=false; a.Input.SetValue(""); a.Input.Focus(); return a,nil
		case "enter": return a.openSelected()
		case "]": if a.CurrentView==ViewRemoteSearch { a.RemotePage++; return a,remoteSearch(a.Client,a.Query,a.RemotePage) }
		case "[": if a.CurrentView==ViewRemoteSearch && a.RemotePage>1 { a.RemotePage--; return a,remoteSearch(a.Client,a.Query,a.RemotePage) }
		case "esc": a.Input.Blur()
		}
	}
	return a,nil
}

func (a *App) move(delta int) { n:=a.itemCount(); if n==0{return}; a.Cursor=(a.Cursor+delta+n)%n }
func (a App) itemCount() int { switch a.CurrentView { case ViewForums:return len(a.Forums); case ViewThreads:return len(a.Threads); case ViewReader:return len(a.Posts); case ViewRemoteSearch:return len(a.Results) }; return 0 }
func activeThread(a App) string { if len(a.Threads)>0 && a.Cursor<len(a.Threads) { return a.Threads[a.Cursor].ID }; return "" }
func (a App) openSelected() (tea.Model,tea.Cmd) { switch a.CurrentView { case ViewForums: if a.Cursor<len(a.Forums) { n:=a.Forums[a.Cursor]; a.Stack=append(a.Stack,n); a.Cursor=0; if n.HasChildren { return a,loadForumChildren(a.Store,a.Client,n) }; a.CurrentView=ViewThreads; return a,loadForum(a.Store,a.Client,n) }; case ViewThreads: if a.Cursor<len(a.Threads) { a.CurrentView=ViewReader; a.Cursor=0; return a,loadPosts(a.Store,a.Client,a.Threads[a.Cursor].ID) }; case ViewRemoteSearch: if a.Cursor<len(a.Results) { r:=a.Results[a.Cursor]; a.CurrentView=ViewReader; a.Status="TRÅD HÄMTAS…"; return a,loadRemoteThread(a.Store,a.Client,r) } }; return a,nil }

func (a App) View() string { var b strings.Builder; b.WriteString(accent.Render("BACKFLASH // DISKURS-NOC")); b.WriteString("\n\n"); switch a.CurrentView { case ViewOverview: b.WriteString(titleStyle.Render("ÖVERSIKT")); b.WriteString("\n\nSTATUS\n  DB          REDO\n  NÄTVERK     "+a.Status+"\n  SESSION     "+map[bool]string{true:"AKTIV",false:"ANONYM"}[a.Client.Session.Authenticated()]+"\n\nSkriv f för forum, / för fjärrsökning eller Ctrl+F för lokal sökning."); case ViewForums: b.WriteString(titleStyle.Render("FORUM · "+a.breadcrumb())); b.WriteString("\n\n"); b.WriteString(renderNodes(a.Forums,a.Cursor)); case ViewThreads: b.WriteString(titleStyle.Render("TRÅDAR · "+a.breadcrumb())); b.WriteString("\n\n"); b.WriteString(renderThreads(a.Threads,a.Cursor)); case ViewReader: b.WriteString(titleStyle.Render("INLÄGG")); b.WriteString("\n\n"); b.WriteString(renderPosts(a.Posts,a.Cursor)); case ViewRemoteSearch: b.WriteString(titleStyle.Render("SÖK PÅ FLASHBACK: "+a.Query)); b.WriteString("\n\n"); b.WriteString(renderResults(a.Results,a.Cursor)) }; if a.Input.Focused() { b.WriteString("\n\n"+a.Input.View()) }; b.WriteString("\n\n"+muted.Render("j/k flytta · Enter öppna · f forum · / fjärrsök · Ctrl+F lokalt · b tillbaka · q avsluta")); return b.String() }
func (a App) breadcrumb() string { p:=[]string{"FLASHBACK"}; for _,n:=range a.Stack {p=append(p,n.Title)}; return strings.Join(p," > ") }
func renderNodes(xs []flashback.ForumNode,c int)string{var b strings.Builder;for i,n:=range xs{line:=n.Title;if n.HasChildren{line+="  ›"};if i==c{line=selected.Render(line)};b.WriteString(line+"\n")};return b.String()}
func renderThreads(xs []flashback.ThreadSummary,c int)string{var b strings.Builder;for i,n:=range xs{line:=fmt.Sprintf("%s%s · %d svar",map[bool]string{true:"📌 ",false:""}[n.Sticky],n.Title,n.Replies);if i==c{line=selected.Render(line)};b.WriteString(line+"\n")};return b.String()}
func renderPosts(xs []flashback.Post,c int)string{var b strings.Builder;for i,n:=range xs{line:=fmt.Sprintf("#%s  %s  %s\n    %s",n.ID,n.Author,n.Timestamp.Format("2006-01-02 15:04"),n.Text);if i==c{line=selected.Render(line)};b.WriteString(line+"\n")};return b.String()}
func renderResults(xs []flashback.SearchResult,c int)string{var b strings.Builder;for i,n:=range xs{line:=fmt.Sprintf("#%s  %s · %s\n    %s",n.PostID,n.Title,n.Author,n.Snippet);if i==c{line=selected.Render(line)};b.WriteString(line+"\n")};return b.String()}

func loadRoot(s *store.Store,c *flashback.Client) tea.Cmd{return func()tea.Msg{rows,err:=s.Forums("");var out []flashback.ForumNode; if err==nil{defer rows.Close();for rows.Next(){var n flashback.ForumNode;var child int;if e:=rows.Scan(&n.ID,&n.Title,&n.URL,&child);e==nil{n.HasChildren=child!=0;out=append(out,n)}}};if len(out)>0{return dataMsg{kind:"forums",forums:out}};nodes,e:=c.Forum(context.Background(),flashback.BaseURL);if e==nil{_ = s.SaveForums(nodes)};return dataMsg{kind:"forums",forums:nodes,err:e}}}
func loadChildren(s *store.Store,stack []flashback.ForumNode)tea.Cmd{return func()tea.Msg{parent:="";if len(stack)>0{parent=stack[len(stack)-1].ID};rows,e:=s.Forums(parent);if e!=nil{return dataMsg{kind:"forums",err:e}};defer rows.Close();var out []flashback.ForumNode;for rows.Next(){var n flashback.ForumNode;var child int;if e:=rows.Scan(&n.ID,&n.Title,&n.URL,&child);e==nil{n.HasChildren=child!=0;out=append(out,n)}};return dataMsg{kind:"forums",forums:out}}}
func loadForumChildren(s *store.Store,c *flashback.Client,n flashback.ForumNode)tea.Cmd{return func()tea.Msg{rows,e:=s.Forums(n.ID);if e==nil{defer rows.Close();var out []flashback.ForumNode;for rows.Next(){var child flashback.ForumNode;var hasChildren int;if scanErr:=rows.Scan(&child.ID,&child.Title,&child.URL,&hasChildren);scanErr==nil{child.HasChildren=hasChildren!=0;out=append(out,child)}};if len(out)>0{return dataMsg{kind:"forums",forums:out}}};out,e:=c.Forum(context.Background(),n.URL);if e==nil{_ = s.SaveForums(out)};return dataMsg{kind:"forums",forums:out,err:e}}}
func loadForum(s *store.Store,c *flashback.Client,n flashback.ForumNode)tea.Cmd{return func()tea.Msg{rows,e:=s.DB.Query(`SELECT id,title,url,replies,views,last_post_at,last_post_author,sticky,page_count FROM threads WHERE forum_id=? ORDER BY last_seen_at DESC`,n.ID);if e==nil{defer rows.Close();var out []flashback.ThreadSummary;for rows.Next(){var t flashback.ThreadSummary;var sticky int;if e:=rows.Scan(&t.ID,&t.Title,&t.URL,&t.Replies,&t.Views,&t.LastPostAt,&t.LastPostAuthor,&sticky,&t.PageCount);e==nil{t.Sticky=sticky!=0;out=append(out,t)}};if len(out)>0{return dataMsg{kind:"threads",threads:out}}};rows,e=c.Threads(context.Background(),n);if e==nil{_ = s.SaveThreads(n.ID,rows)};return dataMsg{kind:"threads",threads:rows,err:e}}}
func loadPosts(s *store.Store,c *flashback.Client,id string)tea.Cmd{return func()tea.Msg{rows,e:=s.Posts(id);if e==nil{defer rows.Close();var out []flashback.Post;for rows.Next(){var p flashback.Post;if e:=rows.Scan(&p.ID,&p.Author,&p.Timestamp,&p.Text);e==nil{p.ThreadID=id;out=append(out,p)}};if len(out)>0{return dataMsg{kind:"posts",posts:out}}};p,e:=c.Thread(context.Background(),id,1);if e==nil{_ = s.SavePage(p)};return dataMsg{kind:"posts",posts:p.Posts,err:e}}}
func loadRemoteThread(s *store.Store,c *flashback.Client,r flashback.SearchResult)tea.Cmd{return loadPosts(s,c,r.ThreadID)}
func remoteSearch(c *flashback.Client,q string,page int)tea.Cmd{return func()tea.Msg{r,e:=c.Search(context.Background(),q,page);return dataMsg{kind:"search",results:r,err:e}}}
func localSearch(s *store.Store,q,id string)tea.Cmd{return func()tea.Msg{rows,e:=s.DB.Query(`SELECT post_id,thread_id,author,snippet(post_search,3,'','…',12) FROM post_search WHERE post_search MATCH ? AND (?='' OR thread_id=?) LIMIT 100`,q,id,id);if e!=nil{return dataMsg{kind:"search",err:e}};defer rows.Close();var r []flashback.SearchResult;for rows.Next(){var x flashback.SearchResult;x.ResultType="post";if e:=rows.Scan(&x.PostID,&x.ThreadID,&x.Author,&x.Snippet);e==nil{r=append(r,x)}};return dataMsg{kind:"search",results:r}}}
