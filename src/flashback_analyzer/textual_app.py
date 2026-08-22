from __future__ import annotations

from pathlib import Path

from textual.app import App, ComposeResult
from textual.binding import Binding
from textual.containers import Horizontal, Vertical
from textual.screen import Screen
from textual.widgets import Footer, Header, Label, ListItem, ListView, Static

from .database import Database


class ThreadItem(ListItem):
    def __init__(self, thread_id: int, title: str, unread: int, posts: int) -> None:
        self.thread_id = thread_id
        text = f"{'+' + str(unread) if unread else '  '}  {title or f't{thread_id}'}  ({posts})"
        super().__init__(Label(text))


class PostItem(ListItem):
    def __init__(self, post_id: int, username: str, timestamp: str | None, preview: str, unread: bool) -> None:
        self.post_id = post_id
        marker = "● " if unread else "  "
        time = (timestamp or "okänd tid")[:16]
        super().__init__(Label(f"{marker}#{post_id} {username} · {time}\n    {preview[:140]}"))


class FlashbackApp(App[None]):
    CSS = """
    Screen { background: $surface; }
    #body { height: 1fr; }
    #threads-panel { width: 30; min-width: 24; border: solid $primary; }
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

    def compose(self) -> ComposeResult:
        yield Header(show_clock=False)
        with Horizontal(id="body"):
            with Vertical(id="threads-panel"):
                yield Label("THREADS", classes="panel-title")
                yield ListView(id="thread-list")
            with Vertical(id="posts-panel"):
                yield Label("POSTS", classes="panel-title")
                yield ListView(id="post-list")
            with Vertical(id="detail-panel"):
                yield Label("DETAIL", classes="panel-title")
                yield Static("Select a thread to begin.", id="detail")
        yield Footer()

    def on_mount(self) -> None:
        self.database = Database(self.db_path)
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

    def action_detail(self) -> None:
        view = self.query_one("#post-list", ListView)
        if view.index is not None and self.visible_posts:
            row = self.visible_posts[view.index]; self._show_post(row)
            self._mark_seen(row); self._load_threads()

    def action_back(self) -> None:
        if self.thread_id is not None:
            self.thread_id = None; self.posts = []; self.visible_posts = []
            self.query_one("#post-list", ListView).clear(); self.query_one("#detail", Static).update("Select a thread to begin.")
            self.query_one("#thread-list", ListView).focus()
        else:
            self.exit()

    def action_help(self) -> None:
        self.push_screen(HelpScreen())

    def on_list_view_selected(self, event: ListView.Selected) -> None:
        if isinstance(event.item, ThreadItem):
            self._open_thread(event.item.thread_id)
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


def launch_textual_tui(db_path: Path, initial_thread: int | None = None) -> None:
    FlashbackApp(db_path, initial_thread=initial_thread).run()
