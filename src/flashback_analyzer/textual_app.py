from __future__ import annotations

from pathlib import Path

from rich.text import Text
from textual.app import App, ComposeResult
from textual.binding import Binding
from textual.containers import Horizontal, Vertical
from textual.screen import Screen
from textual.widgets import Footer, Header, Input, Label, ListItem, ListView, Static

from .database import Database
from .fetcher import Fetcher
from .navigation_service import NavigationService
from .parser import parse_thread_page


class ThreadItem(ListItem):
    def __init__(self, thread_id: int, title: str, unread: int, posts: int) -> None:
        self.thread_id = thread_id
        text = f"{'+' + str(unread) if unread else '  '}  {title or f't{thread_id}'}  ({posts})"
        super().__init__(Label(text, markup=False))


class PostItem(ListItem):
    def __init__(self, post_id: int, username: str, timestamp: str | None, preview: str, unread: bool) -> None:
        self.post_id = post_id
        marker = "● " if unread else "  "
        time = (timestamp or "okänd tid")[:16]
        super().__init__(Label(f"{marker}#{post_id} {username} · {time}\n    {preview[:140]}", markup=False))


class ForumItem(ListItem):
    def __init__(self, section_id: int, title: str, has_children: bool) -> None:
        self.section_id = section_id
        label = f"{title}{'  ›' if has_children else ''}"
        super().__init__(Label(Text(label, overflow="ellipsis", no_wrap=True), markup=False))


class ForumBackItem(ListItem):
    """The explicit parent entry in a forum navigation level."""

    def __init__(self) -> None:
        super().__init__(Label("..", markup=False))


class ForumThreadItem(ListItem):
    def __init__(self, thread_id: int, title: str, replies: int | None, sticky: bool) -> None:
        self.thread_id = thread_id
        suffix = f" · {replies} svar" if replies is not None else ""
        super().__init__(Label(f"{'📌 ' if sticky else ''}{title or f't{thread_id}'}{suffix}", markup=False))


class FlashbackApp(App[None]):
    CSS = """
    Screen { background: $surface; }
    #body { height: 1fr; }
    #threads-panel { width: 34; min-width: 28; border: solid $primary; }
    #posts-panel { width: 2fr; border: solid $primary; }
    #detail-panel { width: 1fr; min-width: 34; border: solid $primary; padding: 1; overflow-y: auto; }
    .panel-title { height: 1; background: $primary; color: $text; padding: 0 1; }
    ListView { height: 1fr; }
    #detail { height: 1fr; overflow-y: auto; }
    Footer { dock: bottom; }
    """

    BINDINGS = [
        Binding("j", "next", "Next", show=False), Binding("k", "previous", "Previous", show=False),
        Binding("J", "next_unread", "Next unread"), Binding("K", "previous_unread", "Previous unread"),
        Binding("g", "first", "First", show=False), Binding("G", "last", "Last", show=False),
        Binding("n", "toggle_unread", "Unread only"), Binding("enter", "detail", "Open"),
        Binding("/", "search", "Search"),
        Binding("f", "forums", "Forums"), Binding("t", "tracked", "Tracked"),
        Binding("b", "up", "Up"), Binding("r", "refresh", "Refresh"),
        Binding("q", "back", "Back / quit"), Binding("?", "help", "Help"),
    ]

    def __init__(self, db_path: Path, initial_thread: int | None = None) -> None:
        super().__init__()
        self.db_path = db_path
        self.initial_thread = initial_thread
        self.database: Database | None = None
        self.thread_id: int | None = None
        self.posts: list[object] = []
        self.visible_posts: list[object] = []
        self.unread_only = False
        self.navigation: NavigationService | None = None
        self.forum_mode = False
        self.forum_stack: list[int] = []
        self.search_active = False

    def compose(self) -> ComposeResult:
        yield Header(show_clock=False)
        with Horizontal(id="body"):
            with Vertical(id="threads-panel"):
                yield Label("THREADS", id="left-title", classes="panel-title")
                yield ListView(id="thread-list")
            with Vertical(id="posts-panel"):
                yield Label("POSTS", id="center-title", classes="panel-title")
                yield ListView(id="post-list")
            with Vertical(id="detail-panel"):
                yield Label("DETAIL", classes="panel-title")
                yield Static("Select a thread to begin.", id="detail", markup=False)
        yield Footer()

    def on_mount(self) -> None:
        self.database = Database(self.db_path)
        self.navigation = NavigationService(self.database, self.db_path.parent / "cache")
        self._load_threads()
        if self.initial_thread is not None:
            self._open_thread(self.initial_thread)

    def on_unmount(self) -> None:
        if self.database is not None:
            self.database.close()

    def _load_threads(self) -> None:
        if self.database is None:
            return
        view = self.query_one("#thread-list", ListView)
        view.clear()
        rows = self.database.tracked_thread_rows()
        for row in rows:
            view.append(ThreadItem(int(row["thread_id"]), str(row["title"] or "untitled"), int(row["unread_count"]), int(row["post_count"])))
        if rows:
            view.index = 0

    def _set_tracked_mode(self) -> None:
        self.forum_mode = False
        self.forum_stack.clear()
        self.query_one("#left-title", Label).update("THREADS")
        self.query_one("#center-title", Label).update("POSTS")
        self._load_threads()
        self.query_one("#thread-list", ListView).focus()

    def _forum_breadcrumb(self) -> str:
        if self.database is None:
            return "FLASHBACK"
        titles = []
        for section_id in self.forum_stack:
            row = self.database.forum_section(section_id)
            if row:
                titles.append(str(row["title"]))
        return "FLASHBACK" + (" > " + " > ".join(titles) if titles else "")

    def _load_forum_level(self) -> None:
        if self.database is None or self.navigation is None:
            return
        self.forum_mode = True
        current_id = self.forum_stack[-1] if self.forum_stack else None
        rows = self.navigation.list_root() if current_id is None else self.navigation.list_children(current_id)
        left = self.query_one("#thread-list", ListView)
        left.clear()
        if current_id is not None:
            left.append(ForumBackItem())
        for row in rows:
            left.append(ForumItem(int(row["id"]), str(row["title"]), bool(row["has_children"])))
        if left.children:
            # Keep the parent entry visible, but start on the first real item.
            left.index = 1 if current_id is not None and len(left.children) > 1 else 0
        self.query_one("#left-title", Label).update(f"FORUMS · {self._forum_breadcrumb()}")
        self.query_one("#center-title", Label).update("THREADS")
        center = self.query_one("#post-list", ListView)
        center.clear()
        if current_id is not None:
            for row in self.navigation.list_threads(current_id):
                center.append(ForumThreadItem(int(row["thread_id"]), str(row["title"] or ""), row["reply_count"], bool(row["is_sticky"])))
        left.focus()

    def _start_navigation_refresh(self, section_id: int | None = None, *, force: bool = False) -> None:
        if self.navigation is None:
            return
        if not force and section_id is None and not self.navigation.is_stale():
            return
        self.run_worker(lambda: self._refresh_navigation(section_id, force), thread=True, exclusive=True)

    def _refresh_navigation(self, section_id: int | None, force: bool = False) -> None:
        try:
            # SQLite connections are thread-affine. The Textual worker must
            # use its own connection; only the completion callback touches
            # the UI-owned database/service again.
            with Database(self.db_path) as database:
                service = NavigationService(database, self.db_path.parent / "cache")
                if section_id is None:
                    service.refresh(force=force)
                else:
                    service.refresh_forum(section_id, force=True)
            self.call_from_thread(self._load_forum_level)
        except Exception as exc:
            self.call_from_thread(self.notify, f"Navigation refresh failed; cached data remains available: {exc}", severity="warning")

    def _open_forum(self, section_id: int) -> None:
        self.forum_stack.append(section_id)
        self._load_forum_level()
        if self.navigation and not self.navigation.list_children(section_id) and not self.navigation.list_threads(section_id):
            self.notify("Loading forum listing…")
            self._start_navigation_refresh(section_id)

    def _open_forum_thread(self, thread_id: int) -> None:
        if self.database is None:
            return
        if self.database.thread_posts(thread_id):
            self._open_thread(thread_id)
            return
        row = self.database.conn.execute("SELECT url FROM threads WHERE thread_id=?", (thread_id,)).fetchone()
        if not row:
            self.notify("Thread listing has no usable URL.", severity="warning")
            return
        self.notify("Ingesting thread…")
        self.run_worker(lambda: self._ingest_thread(thread_id, str(row["url"])), thread=True, exclusive=True)

    def _ingest_thread(self, thread_id: int, url: str) -> None:
        try:
            with Fetcher(self.db_path.parent / "cache") as fetcher:
                html = fetcher.fetch_url(url)
            parsed = parse_thread_page(html, thread_id=thread_id, page=1, source_url=url)
            with Database(self.db_path) as database:
                database.store_page(parsed)
            self.call_from_thread(self._ingestion_finished, thread_id)
        except Exception as exc:
            self.call_from_thread(self.notify, f"Could not ingest thread: {exc}", severity="error")

    def _ingestion_finished(self, thread_id: int) -> None:
        self.notify("Thread ready")
        self._open_thread(thread_id)

    def _open_thread(self, thread_id: int) -> None:
        if self.database is None:
            return
        self.thread_id = thread_id
        self.posts = self.database.thread_posts(thread_id)
        self.unread_only = False
        self._render_posts()
        self.query_one("#post-list", ListView).focus()

    def _render_posts(self) -> None:
        if self.database is None or self.thread_id is None:
            return
        position = self.database.reader_position(self.thread_id)
        self.visible_posts = [row for row in self.posts if not self.unread_only or position is None or int(row["post_id"]) > position]
        view = self.query_one("#post-list", ListView)
        view.clear()
        for row in self.visible_posts:
            unread = position is None or int(row["post_id"]) > position
            view.append(PostItem(int(row["post_id"]), str(row["username"]), row["posted_at"], str(row["text"]), unread))
        if self.visible_posts:
            view.index = 0
            self._show_post(self.visible_posts[0])
        else:
            self.query_one("#detail", Static).update("No posts match this filter.")

    def _show_post(self, row: object) -> None:
        if self.database is None:
            return
        lines = [f"POST #{row['post_id']}", str(row["username"]), str(row["posted_at"] or "unknown time"), "", "ORIGINAL TEXT", "─" * 24, str(row["text"])]
        quotes = self.database.post_quotes(int(row["post_id"]))
        if quotes:
            lines.extend(["", "QUOTED CONTENT", "─" * 24])
            for quote in quotes:
                lines.extend([f"> {quote['quoted_author'] or 'unknown user'}", f"> {quote['quote_text']}"])
        lines.extend(["", f"SOURCE PAGE: {row['page']}", f"POSITION: {row['position_on_page'] or '?'}"])
        self.query_one("#detail", Static).update("\n".join(lines))

    def _mark_seen(self, row: object) -> None:
        if self.database is not None and self.thread_id is not None:
            self.database.mark_post_seen(self.thread_id, int(row["post_id"]))

    def _move(self, delta: int) -> None:
        view = self.screen.focused if isinstance(self.screen.focused, ListView) else self.query_one("#post-list", ListView)
        if view.index is None:
            view.index = 0
        else:
            view.index = max(0, min(len(view.children) - 1, view.index + delta))
        if view.id == "post-list" and self.visible_posts and view.index is not None:
            self._show_post(self.visible_posts[view.index])

    def action_next(self) -> None: self._move(1)
    def action_previous(self) -> None: self._move(-1)

    def _jump_unread(self, delta: int) -> None:
        if self.database is None or self.thread_id is None:
            return
        position = self.database.reader_position(self.thread_id)
        candidates = [index for index, row in enumerate(self.posts) if position is None or int(row["post_id"]) > position]
        if not candidates:
            return
        view = self.query_one("#post-list", ListView)
        current = view.index or 0
        target = next((index for index in candidates if index > current), candidates[-1]) if delta > 0 else next((index for index in reversed(candidates) if index < current), candidates[0])
        if self.unread_only:
            view.index = max(0, min(len(self.visible_posts) - 1, candidates.index(target)))
        else:
            view.index = target
        self._show_post(self.posts[target])

    def action_next_unread(self) -> None: self._jump_unread(1)
    def action_previous_unread(self) -> None: self._jump_unread(-1)

    def action_first(self) -> None:
        view = self.query_one("#post-list", ListView); view.index = 0
        if self.visible_posts: self._show_post(self.visible_posts[0])

    def action_last(self) -> None:
        view = self.query_one("#post-list", ListView)
        if self.visible_posts:
            view.index = len(self.visible_posts) - 1; self._show_post(self.visible_posts[-1])

    def action_toggle_unread(self) -> None:
        if self.thread_id is not None:
            self.unread_only = not self.unread_only; self._render_posts()

    def action_search(self) -> None:
        self.push_screen(SearchScreen(), self.action_search_results)

    def action_search_results(self, query: str | None) -> None:
        if self.database is None or not query:
            return
        results = self.database.search_posts(query, thread_id=self.thread_id)
        self.search_active = True
        self.visible_posts = results
        view = self.query_one("#post-list", ListView)
        view.clear()
        for row in results:
            view.append(PostItem(int(row["post_id"]), str(row["username"]), row["posted_at"], str(row["text"]), False))
        if results:
            view.index = 0
            self._show_post(results[0])
        else:
            self.query_one("#detail", Static).update(f"No cached posts matched: {query}")

    def action_forums(self) -> None:
        self.thread_id = None
        self._load_forum_level()
        self._start_navigation_refresh()

    def action_tracked(self) -> None:
        self.thread_id = None
        self._set_tracked_mode()

    def action_up(self) -> None:
        if self.thread_id is not None:
            self.action_back()
        elif self.forum_mode:
            if self.forum_stack:
                self.forum_stack.pop()
                self._load_forum_level()
            else:
                self._set_tracked_mode()

    def action_refresh(self) -> None:
        if self.forum_mode:
            section_id = self.forum_stack[-1] if self.forum_stack else None
            self._start_navigation_refresh(section_id, force=True)
        elif self.thread_id is not None:
            self.notify("Use fb sync for thread synchronization.")

    def action_detail(self) -> None:
        view = self.query_one("#post-list", ListView)
        if view.index is not None and self.visible_posts:
            row = self.visible_posts[view.index]; self._show_post(row)
            self._mark_seen(row); self._load_threads()

    def action_back(self) -> None:
        if self.search_active:
            self.search_active = False
            self.unread_only = False
            self._render_posts()
            return
        if self.thread_id is not None:
            self.thread_id = None; self.posts = []; self.visible_posts = []
            self.query_one("#post-list", ListView).clear(); self.query_one("#detail", Static).update("Select a thread to begin.")
            if self.forum_mode:
                self._load_forum_level()
            else:
                self.query_one("#thread-list", ListView).focus()
        elif self.forum_mode:
            self.action_up()
        else:
            self.exit()

    def action_help(self) -> None:
        self.push_screen(HelpScreen())

    def on_list_view_selected(self, event: ListView.Selected) -> None:
        if isinstance(event.item, ThreadItem):
            self._open_thread(event.item.thread_id)
        elif isinstance(event.item, ForumItem):
            self._open_forum(event.item.section_id)
        elif isinstance(event.item, ForumBackItem):
            self.action_up()
        elif isinstance(event.item, ForumThreadItem):
            self._open_forum_thread(event.item.thread_id)
        elif isinstance(event.item, PostItem) and self.visible_posts:
            row = next((row for row in self.visible_posts if int(row["post_id"]) == event.item.post_id), self.visible_posts[0]); self._show_post(row); self._mark_seen(row)

    def on_list_view_highlighted(self, event: ListView.Highlighted) -> None:
        if isinstance(event.item, PostItem) and self.visible_posts:
            row = next((row for row in self.visible_posts if int(row["post_id"]) == event.item.post_id), None)
            if row is not None:
                self._show_post(row)
                self._mark_seen(row)


class HelpScreen(Screen[None]):
    def compose(self) -> ComposeResult:
        yield Static("FLASHBACK READER\n\n↑/↓ or j/k   move\nEnter         open / mark read\nJ/K           next / previous unread\ng/G           first / last\nn              unread-only\nq              back / quit\n?              this help\n\nPress q to close.")

    def on_key(self, event: object) -> None:
        if getattr(event, "key", None) in {"q", "escape"}:
            self.app.pop_screen()


class SearchScreen(Screen[str | None]):
    """Small local-search prompt; it never contacts Flashback."""

    CSS = """
    SearchScreen { align: center middle; }
    #search-box { width: 70; height: auto; border: solid $primary; padding: 1; }
    """

    def compose(self) -> ComposeResult:
        with Vertical(id="search-box"):
            yield Label("SEARCH CACHED POSTS")
            yield Input(placeholder="text or username", id="search-input")
            yield Label("Enter search · Escape cancel", classes="panel-title")

    def on_mount(self) -> None:
        self.query_one("#search-input", Input).focus()

    def on_input_submitted(self, event: Input.Submitted) -> None:
        self.dismiss(event.value)

    def on_key(self, event: object) -> None:
        if getattr(event, "key", None) in {"escape", "q"}:
            self.dismiss(None)


def launch_textual_tui(db_path: Path, initial_thread: int | None = None) -> None:
    FlashbackApp(db_path, initial_thread=initial_thread).run()
