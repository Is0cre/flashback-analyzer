from __future__ import annotations

from pathlib import Path

import typer
from rich.console import Console
from rich.table import Table

from .analysis import participation_concentration
from .database import Database
from .fetcher import Fetcher
from .parser import parse_thread_page
from .urls import parse_thread_ref

app = typer.Typer(no_args_is_help=True, help="Read-only Flashback thread analyzer.")
console = Console()
DEFAULT_DB = Path("data/flashback.sqlite3")
DEFAULT_CACHE = Path("data/cache")


@app.command()
def ingest(
    thread: str = typer.Argument(..., help="Flashback URL, e.g. https://www.flashback.org/t3322511"),
    pages: int = typer.Option(1, min=1, help="How many pages to ingest from the requested page."),
    all_pages: bool = typer.Option(False, "--all", help="Discover and ingest all pages."),
    refresh: bool = typer.Option(False, help="Ignore HTML cache."),
    db: Path = typer.Option(DEFAULT_DB, help="SQLite path."),
    cache: Path = typer.Option(DEFAULT_CACHE, help="HTML cache directory."),
) -> None:
    ref = parse_thread_ref(thread)

    with Fetcher(cache) as fetcher, Database(db) as database:
        first_html = fetcher.fetch_thread_page(ref.thread_id, ref.page, refresh=refresh)
        first = parse_thread_page(first_html, ref.thread_id, ref.page)
        count = database.store_page(first)
        console.print(f"[green]Sida {ref.page}:[/] {count} inlägg")

        if all_pages:
            page_numbers = [p for p in range(1, first.max_page + 1) if p != ref.page]
        else:
            page_numbers = list(range(ref.page + 1, ref.page + pages))

        for page in page_numbers:
            html = fetcher.fetch_thread_page(ref.thread_id, page, refresh=refresh)
            parsed = parse_thread_page(html, ref.thread_id, page)
            count = database.store_page(parsed)
            console.print(f"[green]Sida {page}:[/] {count} inlägg")

    console.print(f"\nKlar. Databas: [bold]{db}[/]")


@app.command()
def stats(
    thread: str = typer.Argument(..., help="Thread URL or t<ID>."),
    db: Path = typer.Option(DEFAULT_DB, help="SQLite path."),
) -> None:
    ref = parse_thread_ref(thread)
    with Database(db) as database:
        try:
            summary = database.thread_summary(ref.thread_id)
        except KeyError:
            raise typer.BadParameter("Tråden finns inte i databasen. Kör 'fb ingest' först.")
        concentration = participation_concentration(database.conn, ref.thread_id)

    console.print(f"[bold]{summary['title'] or f't{ref.thread_id}'}[/]")
    console.print(f"Inlägg: {summary['posts']:,}   Användare: {summary['users']:,}   Sidor sedda: {summary['last_page_seen']}")
    console.print(
        f"Top 10 står för {concentration['top_10_users_share']:.1%} av inläggen · "
        f"Gini {concentration['gini']:.3f} · HHI {concentration['hhi']:.4f}"
    )

    table = Table(title="Mest aktiva användare")
    table.add_column("Användare")
    table.add_column("Inlägg", justify="right")
    table.add_column("Andel", justify="right")
    total = int(summary["posts"])
    for row in summary["top_users"]:
        n = int(row["posts"])
        table.add_row(str(row["username"]), str(n), f"{n / total:.1%}" if total else "0%")
    console.print(table)


@app.command()
def inspect(
    thread: str = typer.Argument(...),
    db: Path = typer.Option(DEFAULT_DB),
    limit: int = typer.Option(10, min=1, max=100),
) -> None:
    """Show parsed posts to verify parser quality before adding AI."""
    ref = parse_thread_ref(thread)
    with Database(db) as database:
        rows = database.conn.execute(
            """SELECT p.post_id, p.ordinal, u.username, p.posted_at, p.text
               FROM posts p JOIN users u ON u.user_id=p.user_id
               WHERE p.thread_id=? ORDER BY COALESCE(p.ordinal, p.post_id) LIMIT ?""",
            (ref.thread_id, limit),
        ).fetchall()
    for row in rows:
        console.rule(f"#{row['ordinal'] or '?'} · {row['username']} · p{row['post_id']}")
        console.print(row["posted_at"] or "[dim]okänd tid[/]")
        console.print(row["text"])


if __name__ == "__main__":
    app()
