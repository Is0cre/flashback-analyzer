"""Cached forum navigation service shared by CLI and TUI presentations."""

from __future__ import annotations

from datetime import datetime, timedelta, timezone
from pathlib import Path

from .adapters.flashback.navigation import BASE_URL, parse_forum_listing, parse_navbar
from .database import Database
from .fetcher import Fetcher


class NavigationService:
    """Load, cache and refresh a source's forum hierarchy and listings."""

    def __init__(self, database: Database, cache_dir: Path = Path("data/cache"), ttl_hours: int = 24) -> None:
        self.database = database
        self.cache_dir = cache_dir
        self.ttl = timedelta(hours=ttl_hours)

    def list_root(self) -> list[object]:
        return self.database.forum_roots()

    def list_children(self, section_id: int) -> list[object]:
        return self.database.forum_children(section_id)

    def list_threads(self, section_id: int) -> list[object]:
        return self.database.forum_thread_rows(section_id)

    def is_stale(self) -> bool:
        row = self.database.navigation_cache()
        if not row:
            return True
        expires = datetime.fromisoformat(str(row["expires_at"]).replace("Z", "+00:00"))
        if expires.tzinfo is None:
            expires = expires.replace(tzinfo=timezone.utc)
        return expires <= datetime.now(timezone.utc)

    def refresh(self, *, force: bool = False) -> int:
        """Refresh the navbar using the normal polite fetcher and return rows stored."""

        if not force and not self.is_stale():
            return 0
        with Fetcher(self.cache_dir) as fetcher:
            html = fetcher.fetch_url(BASE_URL, refresh=force)
        nodes = parse_navbar(html, BASE_URL)
        stored = self.database.store_forum_nodes(nodes)
        expires = datetime.now(timezone.utc) + self.ttl
        self.database.set_navigation_cache("flashback", "navbar", BASE_URL, expires.isoformat())
        return stored

    def refresh_forum(self, section_id: int, *, force: bool = False) -> int:
        """Refresh a forum listing; cached rows remain available on failure."""

        section = self.database.forum_section(section_id)
        if not section or not section["is_browsable"]:
            return 0
        with Fetcher(self.cache_dir) as fetcher:
            html = fetcher.fetch_url(str(section["url"]), refresh=force)
        summaries = parse_forum_listing(html, str(section["url"]))
        return self.database.store_forum_thread_summaries(section_id, summaries)
