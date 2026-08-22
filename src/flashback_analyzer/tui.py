from __future__ import annotations

import sqlite3

from rich.console import Console
from rich.prompt import Prompt
from rich.table import Table


def _overview(console: Console, conn: sqlite3.Connection, thread_id: int) -> None:
    row = conn.execute("SELECT title, page_count, post_count FROM threads WHERE thread_id=?", (thread_id,)).fetchone()
    if not row:
        console.print("[red]Tråden finns inte i databasen.[/]")
        return
    users = conn.execute("SELECT COUNT(DISTINCT user_id) FROM posts WHERE thread_id=?", (thread_id,)).fetchone()[0]
    console.print(f"[bold]{row['title'] or f't{thread_id}'}[/]")
    console.print(f"Inlägg: {row['post_count']:,}   Användare: {users:,}   Sidor: {row['page_count']}")


def _topics(console: Console, conn: sqlite3.Connection, thread_id: int) -> None:
    rows = conn.execute("""SELECT label, COUNT(pt.post_id) AS posts, confidence
        FROM topics t LEFT JOIN post_topics pt ON pt.topic_id=t.topic_id
        WHERE t.thread_id=? GROUP BY t.topic_id ORDER BY posts DESC, label LIMIT 20""", (thread_id,)).fetchall()
    table = Table(title="Ämneskandidater")
    table.add_column("Ämne")
    table.add_column("Inlägg", justify="right")
    table.add_column("Konfidens", justify="right")
    for row in rows:
        table.add_row(row["label"], str(row["posts"]), f"{row['confidence']:.1%}")
    console.print(table if rows else "[dim]Kör fb topics först för att upptäcka ämnen.[/]")


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


def run_tui(conn: sqlite3.Connection, thread_id: int, *, console: Console | None = None) -> None:
    """Run a small read-only terminal browser over stored analysis data."""
    output = console or Console()
    while True:
        output.clear()
        _overview(output, conn, thread_id)
        output.print("\n[bold]1[/] Översikt  [bold]2[/] Ämnen  [bold]3[/] Segment  [bold]4[/] Länkar  [bold]q[/] Avsluta")
        choice = Prompt.ask("Val", choices=["1", "2", "3", "4", "q"], default="1", console=output)
        if choice == "q":
            return
        output.clear()
        if choice == "2":
            _topics(output, conn, thread_id)
        elif choice == "3":
            _segments(output, conn, thread_id)
        elif choice == "4":
            _links(output, conn, thread_id)
        else:
            _overview(output, conn, thread_id)
        if Prompt.ask("Tryck Enter för menyn", default="", console=output) == "__quit__":
            return
