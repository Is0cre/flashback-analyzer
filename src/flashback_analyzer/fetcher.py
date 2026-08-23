from __future__ import annotations

import hashlib
import time
from pathlib import Path

import httpx

from .urls import thread_page_url
from .session import AnonymousSessionProvider, SessionProvider


class Fetcher:
    """Polite, read-only HTTP fetcher with an on-disk cache."""

    def __init__(
        self,
        cache_dir: Path,
        min_delay_seconds: float = 5.2,
        timeout_seconds: float = 30.0,
        user_agent: str = "flashback-analyzer/0.1 (+read-only research tool)",
        session_provider: SessionProvider | None = None,
    ) -> None:
        self.cache_dir = cache_dir
        self.cache_dir.mkdir(parents=True, exist_ok=True)
        self.min_delay_seconds = min_delay_seconds
        self.session_provider = session_provider or AnonymousSessionProvider()
        self._last_request_at = 0.0
        headers = {
            "User-Agent": user_agent,
            "Accept": "text/html,application/xhtml+xml",
            "Accept-Language": "sv-SE,sv;q=0.9,en;q=0.7",
        }
        headers.update(self.session_provider.headers())
        self.client = httpx.Client(
            timeout=timeout_seconds,
            follow_redirects=True,
            headers=headers,
            cookies=dict(self.session_provider.cookies()),
        )

    def close(self) -> None:
        self.client.close()

    def __enter__(self) -> "Fetcher":
        return self

    def __exit__(self, *_: object) -> None:
        self.close()

    def fetch_thread_page(self, thread_id: int, page: int, *, refresh: bool = False) -> str:
        url = thread_page_url(thread_id, page)
        return self.fetch_url(url, refresh=refresh)

    def fetch_url(self, url: str, *, refresh: bool = False) -> str:
        """Fetch any read-only Flashback HTML page through the same cache/pacing."""
        cache_path = self._cache_path(url)

        if cache_path.exists() and not refresh:
            return cache_path.read_text(encoding="utf-8")

        elapsed = time.monotonic() - self._last_request_at
        if self._last_request_at and elapsed < self.min_delay_seconds:
            time.sleep(self.min_delay_seconds - elapsed)

        response = self.client.get(url)
        self._last_request_at = time.monotonic()
        response.raise_for_status()

        html = response.text
        cache_path.write_text(html, encoding="utf-8")
        return html

    def _cache_path(self, url: str) -> Path:
        digest = hashlib.sha256(url.encode()).hexdigest()[:16]
        return self.cache_dir / f"{digest}.html"
