from __future__ import annotations

import sqlite3
from pathlib import Path

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
    _clear_header(console, logo, "Flashback Analyzer · Trådar")
    choices: list[tuple[str, int | str]] = []
    if live_items:
        table = Table(title="Live från Flashback")
        table.add_column("Val", justify="right")
        table.add_column("Källa")
        table.add_column("Tråd")
        table.add_column("Visningar", justify="right")
        table.add_column("Läsare", justify="right")
        table.add_column("Svar", justify="right")
        for index, item in enumerate(live_items[:40], start=1):
            table.add_row(str(index), item.feed, f"t{item.thread_id} · {item.title[:90]}", *(str(value) if value is not None else "?" for value in (item.views, item.readers, item.replies)))
            choices.append((str(index), item))
        console.print(table)
    rows = database.conn.execute("SELECT thread_id, title, post_count FROM threads ORDER BY last_fetched_at DESC, thread_id").fetchall()
    if rows:
        table = Table(title="Sparade trådar")
        table.add_column("Val", justify="right")
        table.add_column("Tråd")
        table.add_column("Inlägg", justify="right")
        offset = len(choices)
        for index, row in enumerate(rows, start=offset + 1):
            table.add_row(str(index), f"t{row['thread_id']} · {row['title'] or 'utan titel'}", f"{row['post_count']:,}")
            choices.append((str(index), int(row["thread_id"])))
        console.print(table)
    else:
        console.print("[dim]Inga trådar ännu.[/]")
    choice = Prompt.ask("[a] Lägg till tråd · [r] Uppdatera live · [q] Avsluta", choices=[value for value, _ in choices] + ["a", "r", "q"], console=console)
    if choice == "a":
        return "add"
    if choice == "q":
        return "quit"
    if choice == "r":
        return "refresh"
    return dict(choices)[choice]


def _thread_menu(console: Console, database: Database, cache_dir: Path, logo: Text | None, thread_id: int) -> int | None | str:
    _clear_header(console, logo, f"Flashback Analyzer · t{thread_id}")
    _overview(console, database.conn, thread_id)
    console.print("\n[bold]1[/] Översikt  [bold]2[/] Ämnen  [bold]3[/] Frågor  [bold]4[/] Segment  [bold]5[/] Länkar")
    console.print("[bold]6[/] Hämta sidor  [bold]7[/] Synka svansen  [bold]a[/] Lägg till tråd  [bold]b[/] Trådlista  [bold]q[/] Avsluta")
    choice = Prompt.ask("Val", choices=["1", "2", "3", "4", "5", "6", "7", "a", "b", "q"], default="1", console=console)
    if choice == "q":
        return "quit"
    if choice == "b":
        return None
    if choice == "a":
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
        output.print("[dim]Läser aktuella Flashback-trådar...[/]")
        try:
            with Fetcher(cache_dir) as fetcher:
                live_items = fetch_discovery_items(fetcher)
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
                    live_items = fetch_discovery_items(fetcher, refresh=True)
            except Exception as exc:
                output.print(f"[yellow]Live-listan kunde inte hämtas: {exc}[/]")
                _pause(output)
            selected = None
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
