"""Semantic parsers for Flashback navigation and forum listings."""

from __future__ import annotations

import re
from datetime import datetime
from urllib.parse import parse_qs, urljoin, urlparse, urlunparse

from bs4 import BeautifulSoup, Tag

from ...navigation import ForumNode, ThreadSummary

SOURCE = "flashback"
BASE_URL = "https://www.flashback.org/"
_FORUM_ID_RE = re.compile(r"/(?:f|forum|forums?)[/-]?(\d+)(?:[^/]*)?(?:/|$)", re.I)
_THREAD_RE = re.compile(r"/(?:t|threads?)[/-]?(\d+)(?:p\d+)?(?:-[^/]*)?(?:/|$)", re.I)
_NUMBER_RE = re.compile(r"\d[\d\s\u00a0.,]*")


def normalize_url(href: str, base_url: str = BASE_URL) -> str:
    """Return a canonical Flashback URL without tracking/query noise."""

    absolute = urljoin(base_url, href.strip())
    parsed = urlparse(absolute)
    query = "" if parsed.netloc.lower().endswith("flashback.org") else parsed.query
    path = parsed.path or "/"
    if path != "/":
        path = path.rstrip("/")
    return urlunparse((parsed.scheme.lower(), parsed.netloc.lower(), path, "", query, ""))


def _forum_id(url: str) -> str | None:
    match = _FORUM_ID_RE.search(urlparse(url).path)
    if match:
        return match.group(1)
    query = parse_qs(urlparse(url).query)
    return (query.get("f") or [None])[0]


def _thread_id(url: str) -> int | None:
    match = _THREAD_RE.search(urlparse(url).path)
    return int(match.group(1)) if match else None


def _is_forum_url(url: str) -> bool:
    # Flashback user/profile links can contain words such as "forum" and
    # must never become navigation nodes. The numeric forum identifier is the
    # reliable discriminator for the current adapter.
    return _forum_id(url) is not None


def _text(node: Tag | None) -> str:
    return " ".join(node.get_text(" ", strip=True).split()) if node else ""


def parse_navbar(html: str, source_url: str = BASE_URL) -> list[ForumNode]:
    """Extract forum links and their DOM-derived parent relationships.

    The parser uses semantic links and ``li`` ancestry. It does not depend on
    menu coordinates or a fixed number of hierarchy levels.
    """

    soup = BeautifulSoup(html, "html.parser")
    anchors = [a for a in soup.select("nav a[href], a[href]") if _text(a)]
    result: list[ForumNode] = []
    seen: set[str] = set()
    for order, anchor in enumerate(anchors):
        href = anchor.get("href")
        if not isinstance(href, str):
            continue
        url = normalize_url(href, source_url)
        if url in seen or not _is_forum_url(url):
            continue
        seen.add(url)
        parent_url = None
        own_li = anchor.find_parent("li")
        if own_li:
            parent_li = own_li.find_parent("li")
            while parent_li and parent_url is None:
                parent_anchor = parent_li.find("a", href=True, recursive=False)
                if parent_anchor:
                    candidate = normalize_url(str(parent_anchor["href"]), source_url)
                    if candidate != url and _is_forum_url(candidate):
                        parent_url = candidate
                parent_li = parent_li.find_parent("li")
        result.append(
            ForumNode(
                source=SOURCE,
                title=_text(anchor),
                url=url,
                parent_url=parent_url,
                sort_order=order,
                external_id=_forum_id(url),
                is_browsable=True,
            )
        )
    return result


def _parse_number(value: str) -> int | None:
    match = _NUMBER_RE.search(value)
    if not match:
        return None
    digits = re.sub(r"\D", "", match.group(0))
    return int(digits) if digits else None


def _parse_datetime(value: str) -> datetime | None:
    value = " ".join(value.split())
    if value.endswith("Z"):
        value = value[:-1]
    if "T" in value:
        value = value.replace("T", " ")
    for fmt in ("%Y-%m-%d %H:%M", "%Y-%m-%d %H:%M:%S", "%d.%m.%Y %H:%M"):
        try:
            return datetime.strptime(value[: len(datetime.now().strftime(fmt))], fmt)
        except ValueError:
            continue
    return None


def _row_for(anchor: Tag) -> Tag:
    for name in ("tr", "article", "li"):
        row = anchor.find_parent(name)
        if row:
            return row
    return anchor.parent if isinstance(anchor.parent, Tag) else anchor


def parse_forum_listing(
    html: str,
    source_url: str,
    *,
    source: str = SOURCE,
) -> list[ThreadSummary]:
    """Parse thread rows while tolerating absent optional listing metadata."""

    soup = BeautifulSoup(html, "html.parser")
    result: list[ThreadSummary] = []
    seen: set[int] = set()
    for anchor in soup.select("a[href]"):
        href = str(anchor.get("href", ""))
        absolute = normalize_url(href, source_url)
        thread_id = _thread_id(absolute)
        if thread_id is None or thread_id in seen:
            continue
        seen.add(thread_id)
        row = _row_for(anchor)
        row_text = _text(row)
        classes = " ".join(row.get("class", [])) if isinstance(row, Tag) else ""
        sticky = bool(re.search(r"sticky|announcement|pinned|klistrad", f"{classes} {row_text}", re.I))
        reply_match = re.search(r"([\d\s\u00a0.,]+?)\s*(?:svar|repl(?:ies|y))\b", row_text, re.I)
        view_match = re.search(r"([\d\s\u00a0.,]+?)\s*(?:visningar|views?)\b", row_text, re.I)
        author_node = row.select_one("[data-author], .author, .username, .author a")
        time_node = row.select_one("time[datetime], time, [data-timestamp], .date, .last-post")
        timestamp_value = str(time_node.get("datetime", "")) if time_node else ""
        timestamp_value = timestamp_value or _text(time_node)
        last_author_node = row.select_one(".last-post-author, [data-last-author]")
        pages = [int(m.group(1)) for m in re.finditer(r"[?&/]p(?:age)?[=/]?(\d+)", str(row), re.I)]
        result.append(
            ThreadSummary(
                source=source,
                thread_id=thread_id,
                title=_text(anchor),
                url=absolute,
                author=_text(author_node) or None,
                reply_count=_parse_number(reply_match.group(1)) if reply_match else None,
                view_count=_parse_number(view_match.group(1)) if view_match else None,
                last_post_at=_parse_datetime(timestamp_value),
                last_post_author=_text(last_author_node) or None,
                is_sticky=sticky,
                page_count=max(pages) if pages else None,
            )
        )
    return result
