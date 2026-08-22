from __future__ import annotations

import curses
import sqlite3
from pathlib import Path
import sys

from rich.console import Console
from rich.prompt import Confirm, Prompt
from rich.table import Table
from rich.text import Text

from .database import Database
from .discovery import DiscoveryItem, fetch_discovery_items
from .fetcher import Fetcher
from .parser import parse_thread_page
from .questions import discover_questions
from .segmentation import build_segments
from .topics import discover_topics
from .urls import parse_thread_ref


def _logo() -> Text | None:
    candidates = (Path("assets/ansi-art.utf.ans"), Path("ansi-art.utf.ans"), Path(__file__).resolve().parents[2] / "assets" / "ansi-art.utf.ans")
    for path in candidates:
        if path.is_file():
            return Text.from_ansi(path.read_text(encoding="utf-8"))
    return None


def _clear_header(console: Console, logo: Text | None, title: str) -> None:
    console.clear()
    if logo is not None:
        console.print(logo, crop=True, overflow="crop")
    console.rule(f"[bold]{title}[/]")


def _overview(console: Console, conn: sqlite3.Connection, thread_id: int) -> None:
    row = conn.execute("SELECT title, page_count, post_count FROM threads WHERE thread_id=?", (thread_id,)).fetchone()
    if not row:
        console.print("[red]Tråden finns inte i databasen.[/]")
        return
    users = conn.execute("SELECT COUNT(DISTINCT user_id) FROM posts WHERE thread_id=?", (thread_id,)).fetchone()[0]
    console.print(f"[bold]{row['title'] or f't{thread_id}'}[/]")
    console.print(f"Tråd: t{thread_id}   Inlägg: {row['post_count']:,}   Användare: {users:,}   Sidor: {row['page_count']}")


def _topics(console: Console, conn: sqlite3.Connection, thread_id: int) -> None:
    rows = conn.execute("""SELECT label, COUNT(pt.post_id) AS posts, confidence FROM topics t
        LEFT JOIN post_topics pt ON pt.topic_id=t.topic_id WHERE t.thread_id=?
        GROUP BY t.topic_id ORDER BY posts DESC, label LIMIT 20""", (thread_id,)).fetchall()
    table = Table(title="Ämneskandidater")
    table.add_column("Ämne")
    table.add_column("Inlägg", justify="right")
    table.add_column("Konfidens", justify="right")
    for row in rows:
        table.add_row(row["label"], str(row["posts"]), f"{row['confidence']:.1%}")
    console.print(table if rows else "[dim]Kör fb topics först för att upptäcka ämnen.[/]")


def _questions(console: Console, conn: sqlite3.Connection, thread_id: int) -> None:
    rows = conn.execute("""SELECT q.question, COUNT(pq.post_id) AS posts, q.confidence FROM questions q
        LEFT JOIN post_questions pq ON pq.question_id=q.question_id WHERE q.thread_id=?
        GROUP BY q.question_id ORDER BY posts DESC, q.question LIMIT 20""", (thread_id,)).fetchall()
    table = Table(title="Frågekandidater")
    table.add_column("Fråga")
    table.add_column("Inlägg", justify="right")
    table.add_column("Konfidens", justify="right")
    for row in rows:
        table.add_row(row["question"], str(row["posts"]), f"{row['confidence']:.1%}")
    console.print(table if rows else "[dim]Kör fb questions först för att upptäcka frågor.[/]")


def _segments(console: Console, conn: sqlite3.Connection, thread_id: int) -> None:
    rows = conn.execute("""SELECT segment_number, first_post_id, last_post_id, post_count, start_time, end_time
        FROM segments WHERE thread_id=? ORDER BY segment_number""", (thread_id,)).fetchall()
    table = Table(title="Segment")
    for name in ("Nr", "Första", "Sista", "Inlägg", "Start", "Slut"):
        table.add_column(name, justify="right" if name in {"Nr", "Första", "Sista", "Inlägg"} else "left")
    for row in rows:
        table.add_row(str(row["segment_number"]), str(row["first_post_id"]), str(row["last_post_id"]), str(row["post_count"]), row["start_time"] or "?", row["end_time"] or "?")
    console.print(table if rows else "[dim]Kör fb segments först för att skapa segment.[/]")


def _links(console: Console, conn: sqlite3.Connection, thread_id: int) -> None:
    rows = conn.execute("""SELECT l.domain, COUNT(*) AS links, COUNT(DISTINCT l.url) AS urls,
        COUNT(DISTINCT p.user_id) AS users FROM links l JOIN posts p ON p.post_id=l.post_id
        WHERE p.thread_id=? GROUP BY l.domain ORDER BY links DESC, l.domain LIMIT 20""", (thread_id,)).fetchall()
    table = Table(title="Länkkällor")
    for name in ("Domän", "Länkar", "URL:er", "Användare"):
        table.add_column(name, justify="right" if name != "Domän" else "left")
    for row in rows:
        table.add_row(row["domain"], str(row["links"]), str(row["urls"]), str(row["users"]))
    console.print(table if rows else "[dim]Inga länkar lagrade.[/]")


def _pause(console: Console) -> None:
    Prompt.ask("Enter för att återgå", default="", console=console)


def _select_option(console: Console, title: str, options: list[tuple[str, object]]) -> object:
    """Select an option with arrows and Enter; retain a piped-input fallback."""
    if not sys.stdin.isatty():
        labels = [label for label, _ in options]
        choice = Prompt.ask(title, choices=[str(index) for index in range(1, len(labels) + 1)] + ["q"], console=console)
        if choice == "q":
            return "quit"
        return options[int(choice) - 1][1]

    def screen(stdscr: object) -> object:
        window = stdscr  # type: ignore[assignment]
        curses.curs_set(0)
        selected = 0
        while True:
            window.erase()
            height, width = window.getmaxyx()
            window.addnstr(1, 2, title, max(1, width - 4), curses.A_BOLD)
            for index, (label, _) in enumerate(options):
                if index + 3 >= height - 1:
                    break
                prefix = "▶ " if index == selected else "  "
                attr = curses.A_REVERSE if index == selected else curses.A_NORMAL
                window.addnstr(index + 3, 2, prefix + label, max(1, width - 4), attr)
            window.addnstr(max(1, height - 2), 2, "↑/↓ navigera · Enter välj · q avsluta", max(1, width - 4))
            window.refresh()
            key = window.getch()
            if key in (curses.KEY_UP, ord("k")):
                selected = (selected - 1) % len(options)
            elif key in (curses.KEY_DOWN, ord("j")):
                selected = (selected + 1) % len(options)
            elif key in (curses.KEY_ENTER, 10, 13):
                return options[selected][1]
            elif key in (27, ord("q")):
                return "quit"

    try:
        return curses.wrapper(screen)
    except curses.error:
        labels = [label for label, _ in options]
        choice = Prompt.ask(title, choices=[str(index) for index in range(1, len(labels) + 1)] + ["q"], console=console)
        if choice == "q":
            return "quit"
        return options[int(choice) - 1][1]


def _page_options(console: Console, *, default_pages: int = 1, reply_count: int | None = None) -> tuple[int, bool]:
    estimate = f" · cirka {(reply_count + 1 + 19) // 20} sidor" if reply_count is not None else ""
    reply_note = f" ({reply_count:,} svar{estimate})" if reply_count is not None else ""
    while True:
        raw = Prompt.ask(f"Antal sidor att hämta{reply_note}", default=str(default_pages), console=console)
        try:
            pages = int(raw)
            if pages < 1:
                raise ValueError
            break
        except ValueError:
            console.print("[red]Ange ett positivt heltal.[/]")
    all_pages = Confirm.ask("Upptäck och hämta alla sidor?", default=False, console=console)
    if all_pages and reply_count is not None and reply_count >= 1000:
        all_pages = Confirm.ask(f"Tråden har {reply_count:,} svar. Hämta verkligen hela tråden?", default=False, console=console)
    return pages, all_pages


def _ingest(database: Database, cache_dir: Path, value: str, console: Console, *, reply_count: int | None = None) -> int:
    ref = parse_thread_ref(value)
    pages, all_pages = _page_options(console, reply_count=reply_count)
    stored = 0
    with Fetcher(cache_dir) as fetcher:
        first_html = fetcher.fetch_thread_page(ref.thread_id, ref.page)
        first = parse_thread_page(first_html, ref.thread_id, ref.page, source_url=ref.canonical_url)
        stored += database.store_page(first)
        page_numbers = [p for p in range(1, first.max_page + 1) if p != ref.page] if all_pages else range(ref.page + 1, ref.page + pages)
        for page in page_numbers:
            html = fetcher.fetch_thread_page(ref.thread_id, page)
            parsed = parse_thread_page(html, ref.thread_id, page, source_url=f"https://www.flashback.org/t{ref.thread_id}{f'p{page}' if page != 1 else ''}")
            stored += database.store_page(parsed)
    console.print(f"[green]Hämtat:[/] {stored} inlägg")
    return ref.thread_id


def _sync(database: Database, cache_dir: Path, thread_id: int, console: Console) -> None:
    row = database.conn.execute("SELECT MAX(page) AS page FROM posts WHERE thread_id=?", (thread_id,)).fetchone()
    last_page = max(1, int(row["page"] or 1))
    with Fetcher(cache_dir) as fetcher:
        html = fetcher.fetch_thread_page(thread_id, last_page, refresh=True)
        parsed = parse_thread_page(html, thread_id, last_page, source_url=f"https://www.flashback.org/t{thread_id}{f'p{last_page}' if last_page != 1 else ''}")
        database.store_page(parsed)
        for page in range(last_page + 1, parsed.max_page + 1):
            html = fetcher.fetch_thread_page(thread_id, page)
            database.store_page(parse_thread_page(html, thread_id, page, source_url=f"https://www.flashback.org/t{thread_id}p{page}"))
    console.print(f"[green]Synkroniserad:[/] från sida {last_page}")


def _workspace_menu(console: Console, database: Database, logo: Text | None, live_items: list[DiscoveryItem]) -> int | None | str:
    _clear_header(console, logo, "Flashback Analyzer · Arbetsyta")
    saved_count = database.conn.execute("SELECT COUNT(*) FROM threads").fetchone()[0]
    console.print(f"Live-kandidater: [bold]{len(live_items)}[/]   Sparade trådar: [bold]{saved_count}[/]\n")
    choices = [
        (f"Live från Flashback · {len(live_items)} kandidater", "live_menu"),
        (f"Sparade trådar · {saved_count} analyserade", "saved_menu"),
        ("Lägg till tråd manuellt", "add"),
        ("Uppdatera live-listan", "refresh"),
        ("Avsluta", "quit"),
    ]
    return _select_option(console, "Arbetsyta · välj med piltangenter", choices)


def _live_menu(console: Console, logo: Text | None, live_items: list[DiscoveryItem]) -> DiscoveryItem | str | None:
    _clear_header(console, logo, "Flashback Analyzer · Live från Flashback")
    choices: list[tuple[str, object]] = []
    for index, item in enumerate(live_items[:20], start=1):
        replies = f" · {item.replies:,} svar" if item.replies is not None else ""
        readers = f" · {item.readers:,} läsare" if item.readers is not None else ""
        choices.append((f"{index}. {item.feed} · t{item.thread_id} · {item.title[:72]}{replies}{readers}", item))
    choices.append(("Tillbaka till arbetsytan", "back"))
    choice = _select_option(console, "Live-lista · välj tråd", choices)
    return None if choice == "back" else choice


def _saved_menu(console: Console, database: Database, logo: Text | None) -> int | str | None:
    _clear_header(console, logo, "Flashback Analyzer · Sparade trådar")
    rows = database.conn.execute("""SELECT t.thread_id, t.title, t.post_count, t.last_fetched_at,
        COUNT(DISTINCT p.user_id) AS users FROM threads t LEFT JOIN posts p ON p.thread_id=t.thread_id
        GROUP BY t.thread_id ORDER BY t.last_fetched_at DESC, t.thread_id""").fetchall()
    choices: list[tuple[str, object]] = []
    for index, row in enumerate(rows, start=1):
        fetched = (row["last_fetched_at"] or "okänd tid").replace("T", " ")[:16]
        choices.append((f"{index}. t{row['thread_id']} · {(row['title'] or 'utan titel')[:72]} · {row['post_count']:,} inlägg · {row['users']:,} användare · {fetched}", int(row["thread_id"])))
    choices.append(("Tillbaka till arbetsytan", "back"))
    choice = _select_option(console, "Sparade trådar · välj tråd", choices)
    return None if choice == "back" else choice


def _thread_menu(console: Console, database: Database, cache_dir: Path, logo: Text | None, thread_id: int) -> int | None | str:
    _clear_header(console, logo, f"Flashback Analyzer · t{thread_id}")
    _overview(console, database.conn, thread_id)
    choices = [
        ("Översikt", "1"), ("Ämnen", "2"), ("Frågor", "3"), ("Segment", "4"), ("Länkar", "5"),
        ("Hämta sidor", "6"), ("Synka svansen", "7"), ("Lägg till tråd", "add"),
        ("Tillbaka till trådlistan", "back"), ("Avsluta", "quit"),
    ]
    choice = _select_option(console, "Trådmeny · välj med piltangenter", choices)
    if choice == "quit":
        return "quit"
    if choice == "back":
        return None
    if choice == "add":
        return "add"
    if choice == "6":
        _ingest(database, cache_dir, f"t{thread_id}", console)
        _pause(console)
        return thread_id
    if choice == "7":
        _sync(database, cache_dir, thread_id, console)
        _pause(console)
        return thread_id
    console.clear()
    if choice == "2":
        discover_topics(database.conn, thread_id)
        _topics(console, database.conn, thread_id)
    elif choice == "3":
        discover_questions(database.conn, thread_id)
        _questions(console, database.conn, thread_id)
    elif choice == "4":
        build_segments(database.conn, thread_id)
        _segments(console, database.conn, thread_id)
    elif choice == "5":
        _links(console, database.conn, thread_id)
    else:
        _overview(console, database.conn, thread_id)
    _pause(console)
    return thread_id


def run_tui(database: Database, thread_id: int | None = None, *, cache_dir: Path = Path("data/cache"), show_logo: bool = False, console: Console | None = None) -> None:
    """Run a clear, navigable terminal browser with thread management."""
    output = console or Console()
    logo = _logo() if show_logo else None
    live_items: list[DiscoveryItem] = []
    if thread_id is None:
        live_items = database.cached_discovery_items()
        output.print("[dim]Läser aktuella Flashback-trådar...[/]")
        try:
            with Fetcher(cache_dir) as fetcher:
                fetched_items = fetch_discovery_items(fetcher)
                if fetched_items:
                    database.store_discovery_items(fetched_items)
                    live_items = fetched_items
        except Exception as exc:
            output.print(f"[yellow]Live-listan kunde inte hämtas: {exc}[/]")
    selected: int | None | str = thread_id
    while True:
        if selected == "quit":
            return
        if selected == "add":
            _clear_header(output, logo, "Flashback Analyzer · Lägg till tråd")
            value = Prompt.ask("Tråd (t<ID> eller URL)", console=output)
            try:
                selected = _ingest(database, cache_dir, value, output)
            except (ValueError, OSError) as exc:
                output.print(f"[red]Kunde inte hämta tråden: {exc}[/]")
                _pause(output)
            continue
        if selected == "refresh":
            output.print("[dim]Uppdaterar live-listan...[/]")
            try:
                with Fetcher(cache_dir) as fetcher:
                    fetched_items = fetch_discovery_items(fetcher, refresh=True)
                    if fetched_items:
                        database.store_discovery_items(fetched_items)
                        live_items = fetched_items
            except Exception as exc:
                output.print(f"[yellow]Live-listan kunde inte hämtas: {exc}[/]")
                _pause(output)
            selected = None
            continue
        if selected == "live_menu":
            selected = _live_menu(output, logo, live_items)
            continue
        if selected == "saved_menu":
            selected = _saved_menu(output, database, logo)
            continue
        if isinstance(selected, DiscoveryItem):
            _clear_header(output, logo, "Flashback Analyzer · Lägg till tråd")
            output.print(f"[bold]t{selected.thread_id} · {selected.title}[/]")
            output.print(f"Visningar: {selected.views or '?'}   Läsare: {selected.readers or '?'}   Svar: {selected.replies or '?'}")
            try:
                selected = _ingest(database, cache_dir, f"t{selected.thread_id}", output, reply_count=selected.replies)
            except (ValueError, OSError) as exc:
                output.print(f"[red]Kunde inte hämta tråden: {exc}[/]")
                _pause(output)
            continue
        if selected is None:
            selected = _workspace_menu(output, database, logo, live_items)
        else:
            selected = _thread_menu(output, database, cache_dir, logo, selected)
