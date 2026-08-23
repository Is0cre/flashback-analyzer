from __future__ import annotations

from pathlib import Path

import typer
from rich.console import Console
from rich.table import Table

from .analysis import participation_concentration
from .database import Database
from .fetcher import Fetcher
from .parser import parse_thread_page
from .segmentation import build_segments
from .topics import discover_topics
from .textual_app import launch_textual_tui
from .questions import discover_questions
from .search import SearchService
from .urls import parse_thread_ref
from .adapters.flashback.navigation import diagnose_nav

app = typer.Typer(no_args_is_help=False, help="Read-only Flashback thread analyzer.")
console = Console()
DEFAULT_DB = Path("data/flashback.sqlite3")
DEFAULT_CACHE = Path("data/cache")


@app.callback(invoke_without_command=True)
def main(ctx: typer.Context) -> None:
    """Open the local reader when no subcommand is provided."""
    if ctx.invoked_subcommand is None:
        launch_textual_tui(DEFAULT_DB)


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
        first = parse_thread_page(first_html, ref.thread_id, ref.page, source_url=ref.canonical_url)
        count = database.store_page(first)
        console.print(f"[green]Sida {ref.page}:[/] {count} inlägg")

        if all_pages:
            page_numbers = [p for p in range(1, first.max_page + 1) if p != ref.page]
        else:
            page_numbers = list(range(ref.page + 1, ref.page + pages))

        for page in page_numbers:
            html = fetcher.fetch_thread_page(ref.thread_id, page, refresh=refresh)
            parsed = parse_thread_page(html, ref.thread_id, page, source_url=f"https://www.flashback.org/t{ref.thread_id}{f'p{page}' if page != 1 else ''}")
            count = database.store_page(parsed)
            console.print(f"[green]Sida {page}:[/] {count} inlägg")

    console.print(f"\nKlar. Databas: [bold]{db}[/]")


@app.command("debug-nav")
def debug_nav(
    url: str = typer.Argument("https://www.flashback.org/", help="Flashback page to inspect."),
    refresh: bool = typer.Option(False, help="Ignore the HTML cache."),
    cache: Path = typer.Option(DEFAULT_CACHE, help="HTML cache directory."),
) -> None:
    """Show accepted/rejected forum-navigation links and their URL classes."""
    with Fetcher(cache) as fetcher:
        html = fetcher.fetch_url(url, refresh=refresh)
    for decision, kind, href, title in diagnose_nav(html, url):
        console.print(f"{decision} {kind.value.upper()}\n{href}\n{title}\n", markup=False)


@app.command()
def sync(
    thread: str = typer.Argument(..., help="Thread URL or t<ID>."),
    db: Path = typer.Option(DEFAULT_DB, help="SQLite path."),
    cache: Path = typer.Option(DEFAULT_CACHE, help="HTML cache directory."),
) -> None:
    """Refresh only the current tail and pages discovered after it."""
    ref = parse_thread_ref(thread)
    with Database(db) as database:
        try:
            existing = database.conn.execute(
                """SELECT t.thread_id, MAX(p.page) AS latest_page FROM threads t
                   LEFT JOIN posts p ON p.thread_id=t.thread_id WHERE t.thread_id=? GROUP BY t.thread_id""",
                (ref.thread_id,),
            ).fetchone()
        except Exception as exc:
            raise typer.BadParameter(f"Kunde inte läsa tråden: {exc}") from exc
        if existing is None:
            raise typer.BadParameter("Tråden finns inte i databasen. Kör 'fb ingest' först.")
        last_page = max(1, int(existing["latest_page"] or 1))
        with Fetcher(cache) as fetcher:
            # The last known page can receive new posts; a refresh is required here.
            html = fetcher.fetch_thread_page(ref.thread_id, last_page, refresh=True)
            parsed = parse_thread_page(html, ref.thread_id, last_page, source_url=f"https://www.flashback.org/t{ref.thread_id}{f'p{last_page}' if last_page != 1 else ''}")
            database.store_page(parsed)
            new_pages = range(last_page + 1, parsed.max_page + 1)
            for page in new_pages:
                html = fetcher.fetch_thread_page(ref.thread_id, page)
                parsed = parse_thread_page(html, ref.thread_id, page, source_url=f"https://www.flashback.org/t{ref.thread_id}p{page}")
                database.store_page(parsed)
    console.print(f"[green]Synkroniserad:[/] sida {last_page} och {max(0, parsed.max_page - last_page)} nya sidor")


@app.command("thread-info")
def thread_info(thread: str = typer.Argument(...), db: Path = typer.Option(DEFAULT_DB)) -> None:
    """Show stored thread metadata."""
    ref = parse_thread_ref(thread)
    with Database(db) as database:
        try:
            summary = database.thread_summary(ref.thread_id)
        except KeyError:
            raise typer.BadParameter("Tråden finns inte i databasen. Kör 'fb ingest' först.")
    for key in ("thread_id", "title", "url", "first_seen_at", "last_fetched_at", "first_post", "last_post", "page_count", "posts", "users"):
        console.print(f"{key}: {summary.get(key)}")


@app.command("paths")
def paths() -> None:
    """Show the exact persistent paths used by this checkout."""
    console.print(f"Database:\n{DEFAULT_DB.resolve()}", markup=False)
    console.print(f"HTTP cache:\n{DEFAULT_CACHE.resolve()}", markup=False)
    console.print(f"Log:\n{(Path('data') / 'app.log').resolve()}", markup=False)


@app.command("search")
def search(thread: str = typer.Argument(...), query: str = typer.Argument(...), db: Path = typer.Option(DEFAULT_DB)) -> None:
    """Search the local canonical post dataset."""
    ref = parse_thread_ref(thread)
    with Database(db) as database:
        results = SearchService(database).search_posts(query, thread_id=ref.thread_id)
    console.print(f"{len(results)} resultat")
    for result in results:
        console.print(f"#{result.post_id} {result.author or '?'}")
        console.print(result.snippet[:240], markup=False)
        console.print()


@app.command()
def links(thread: str = typer.Argument(...), db: Path = typer.Option(DEFAULT_DB)) -> None:
    """Show normalized domains, link counts, unique URLs, and linking users."""
    ref = parse_thread_ref(thread)
    with Database(db) as database:
        rows = database.link_statistics(ref.thread_id)
    table = Table(title="Länkkällor")
    table.add_column("Domän")
    table.add_column("Länkar", justify="right")
    table.add_column("Unika URL:er", justify="right")
    table.add_column("Unika användare", justify="right")
    for row in rows:
        table.add_row(str(row["domain"]), str(row["links"]), str(row["unique_urls"]), str(row["unique_users"]))
    console.print(table)


@app.command()
def segments(
    thread: str = typer.Argument(...),
    size: int = typer.Option(75, min=1, help="Target maximum posts per segment."),
    gap_hours: float = typer.Option(24.0, min=0.01, help="Split after this inactivity gap when a segment is large enough."),
    db: Path = typer.Option(DEFAULT_DB),
) -> None:
    """Build and display chronological analysis segments."""
    ref = parse_thread_ref(thread)
    with Database(db) as database:
        try:
            rows = build_segments(database.conn, ref.thread_id, max_posts=size, gap_hours=gap_hours)
        except KeyError:
            raise typer.BadParameter("Tråden finns inte i databasen. Kör 'fb ingest' först.")
    table = Table(title="Kronologiska segment")
    for name in ("Segment", "Första post", "Sista post", "Start", "Slut", "Inlägg"):
        table.add_column(name, justify="right" if name in {"Segment", "Första post", "Sista post", "Inlägg"} else "left")
    for row in rows:
        table.add_row(str(row.number), str(row.first_post_id), str(row.last_post_id), row.start_time or "?", row.end_time or "?", str(row.post_count))
    console.print(table)


@app.command()
def topics(
    thread: str = typer.Argument(...),
    limit: int = typer.Option(10, min=1, max=100),
    min_posts: int = typer.Option(2, min=1, help="Minimum number of posts containing a topic term."),
    db: Path = typer.Option(DEFAULT_DB),
) -> None:
    """Discover recurring lexical topic candidates and their post counts."""
    ref = parse_thread_ref(thread)
    with Database(db) as database:
        try:
            rows = discover_topics(database.conn, ref.thread_id, limit=limit, min_post_count=min_posts)
        except KeyError:
            raise typer.BadParameter("Tråden finns inte i databasen. Kör 'fb ingest' först.")
    table = Table(title="Diskussionsämnen (lexikal baslinje)")
    table.add_column("Ämne")
    table.add_column("Inlägg", justify="right")
    table.add_column("Konfidens", justify="right")
    for row in rows:
        table.add_row(row.label, str(row.post_count), f"{row.confidence:.1%}")
    console.print(table)


@app.command()
def questions(
    thread: str = typer.Argument(...),
    limit: int = typer.Option(20, min=1, max=100),
    min_posts: int = typer.Option(1, min=1, help="Minimum number of posts containing the question."),
    db: Path = typer.Option(DEFAULT_DB),
) -> None:
    """Discover explicit question candidates and their post counts."""
    ref = parse_thread_ref(thread)
    with Database(db) as database:
        try:
            rows = discover_questions(database.conn, ref.thread_id, limit=limit, min_post_count=min_posts)
        except KeyError:
            raise typer.BadParameter("Tråden finns inte i databasen. Kör 'fb ingest' först.")
    table = Table(title="Frågekandidater (explicit text)")
    table.add_column("Fråga")
    table.add_column("Inlägg", justify="right")
    table.add_column("Konfidens", justify="right")
    for row in rows:
        table.add_row(row.question, str(row.post_count), f"{row.confidence:.1%}")
    console.print(table)


@app.command()
def tui(thread: str | None = typer.Argument(None, help="Kompakt trådref, t.ex. t3742384C."), db: Path = typer.Option(DEFAULT_DB)) -> None:
    """Open the Textual terminal reader."""
    ref = parse_thread_ref(thread) if thread else None
    with Database(db) as database:
        if ref and not database.conn.execute("SELECT 1 FROM threads WHERE thread_id=?", (ref.thread_id,)).fetchone():
            raise typer.BadParameter("Tråden finns inte i databasen. Kör 'fb ingest' först.")
    launch_textual_tui(db, initial_thread=ref.thread_id if ref else None)


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
