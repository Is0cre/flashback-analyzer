from __future__ import annotations

from dataclasses import dataclass
from datetime import date
import re

from bs4 import BeautifulSoup

from .fetcher import Fetcher
from .urls import thread_page_url


FEED_PATHS = {
    "aktuella": "aktuella-amnen",
    "populära": "populara-amnen",
    "nya ämnen": "nya-amnen",
    "nya inlägg": "nya-inlagg",
}
THREAD_LINK_RE = re.compile(r"/t(?P<thread>\d+)(?:p\d+)?(?:$|[/?#])", re.I)
STATS_RE = re.compile(r"([\d\s]+)\s+visningar\s+•\s+([\d\s]+)\s+läsare\s+•\s+([\d\s]+)\s+svar", re.I)


@dataclass(frozen=True, slots=True)
class DiscoveryItem:
    feed: str
    thread_id: int
    title: str
    views: int | None = None
    readers: int | None = None
    replies: int | None = None

    @property
    def url(self) -> str:
        return thread_page_url(self.thread_id, 1)


def feed_urls(day: date | None = None) -> dict[str, str]:
    current = (day or date.today()).isoformat()
    return {name: f"https://www.flashback.org/{path}/{current}" for name, path in FEED_PATHS.items()}


def _number(value: str) -> int:
    return int(value.replace(" ", ""))


def parse_discovery_page(html: str, feed: str) -> list[DiscoveryItem]:
    """Extract thread candidates from one Flashback discovery page."""
    soup = BeautifulSoup(html, "html.parser")
    items: list[DiscoveryItem] = []
    seen: set[int] = set()
    for anchor in soup.select("a[href]"):
        match = THREAD_LINK_RE.search(str(anchor.get("href", "")))
        title = anchor.get_text(" ", strip=True)
        if not match or not title:
            continue
        thread_id = int(match["thread"])
        if thread_id in seen:
            continue
        seen.add(thread_id)
        container = anchor.find_parent(["li", "article", "tr", "div"]) or anchor.parent
        context = container.get_text(" ", strip=True) if container else title
        stats = STATS_RE.search(context)
        items.append(DiscoveryItem(
            feed=feed,
            thread_id=thread_id,
            title=title,
            views=_number(stats.group(1)) if stats else None,
            readers=_number(stats.group(2)) if stats else None,
            replies=_number(stats.group(3)) if stats else None,
        ))
    return items


def fetch_discovery_items(fetcher: Fetcher, *, refresh: bool = False) -> list[DiscoveryItem]:
    """Fetch all four dated discovery feeds, using cache and polite pacing."""
    items: list[DiscoveryItem] = []
    seen: set[int] = set()
    for feed, url in feed_urls().items():
        try:
            html = fetcher.fetch_url(url, refresh=refresh)
        except Exception:
            # One unavailable feed should not hide the other discovery lists.
            continue
        for item in parse_discovery_page(html, feed):
            if item.thread_id not in seen:
                seen.add(item.thread_id)
                items.append(item)
    return items
