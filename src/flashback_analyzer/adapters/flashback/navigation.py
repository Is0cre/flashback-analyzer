"""Semantic parsers for Flashback navigation and forum listings."""

from __future__ import annotations

import re
from datetime import datetime
from dataclasses import replace
from enum import Enum
from urllib.parse import parse_qs, urljoin, urlparse, urlunparse

from bs4 import BeautifulSoup, Tag

from ...navigation import ForumNode, ThreadSummary

SOURCE = "flashback"
BASE_URL = "https://www.flashback.org/"
_FORUM_PATH_RE = re.compile(r"^/f(\d+)(?:-[^/]*)?$", re.I)
_FORUM_LAST_POST_RE = re.compile(r"^/f\d+lp$", re.I)
_THREAD_PATH_RE = re.compile(r"^/t(\d+)(?:p\d+)?n?(?:-[^/]*)?$", re.I)
_POST_PATH_RE = re.compile(r"^/p\d+$", re.I)
_NUMBER_RE = re.compile(r"\d[\d\s\u00a0.,]*")


class FlashbackLinkType(Enum):
    FORUM = "forum"
    THREAD = "thread"
    POST = "post"
    USER = "user"
    OTHER = "other"


def normalize_url(href: str, base_url: str = BASE_URL) -> str:
    """Return a canonical Flashback URL without tracking/query noise."""

    absolute = urljoin(base_url, href.strip())
    parsed = urlparse(absolute)
    query = "" if parsed.netloc.lower().endswith("flashback.org") else parsed.query
    path = parsed.path or "/"
    if path != "/":
        path = path.rstrip("/")
    return urlunparse((parsed.scheme.lower(), parsed.netloc.lower(), path, "", query, ""))


def classify_flashback_url(href: str, base_url: str = BASE_URL) -> FlashbackLinkType:
    """Classify observed Flashback URL forms without looking at link text.

    Forum rows use ``/f<ID>-slug``. Last-post author links use the deceptively
    similar ``/f<ID>lp`` form, which is why a loose ``/f<ID>`` regex is unsafe.
    """

    url = normalize_url(href, base_url)
    parsed = urlparse(url)
    path = parsed.path
    if _FORUM_PATH_RE.fullmatch(path) or (parsed.query and parse_qs(parsed.query).get("f")):
        return FlashbackLinkType.FORUM
    if _FORUM_LAST_POST_RE.fullmatch(path) or re.fullmatch(r"/u\d+", path, re.I):
        return FlashbackLinkType.USER
    if _THREAD_PATH_RE.fullmatch(path):
        return FlashbackLinkType.THREAD
    if _POST_PATH_RE.fullmatch(path) or path.endswith("/showpost.php") and "p" in parse_qs(parsed.query):
        return FlashbackLinkType.POST
    return FlashbackLinkType.OTHER


def _forum_id(url: str) -> str | None:
    match = _FORUM_PATH_RE.fullmatch(urlparse(url).path)
    if match:
        return match.group(1)
    query = parse_qs(urlparse(url).query)
    return (query.get("f") or [None])[0]


def _thread_id(url: str) -> int | None:
    match = _THREAD_PATH_RE.fullmatch(urlparse(url).path)
    return int(match.group(1)) if match else None


def _text(node: Tag | None) -> str:
    return " ".join(node.get_text(" ", strip=True).split()) if node else ""


def parse_navbar(html: str, source_url: str = BASE_URL) -> list[ForumNode]:
    """Extract only forum cells from Flashback's actual forum-list tables.

    On the live site, homepage/forum navigation is represented by a
    ``table.forumslist``. Forum links are in ``td.td_forum``; the adjacent
    last-post cell contains thread and author links, including ``/f<ID>lp``
    author links. Restricting extraction to the forum cell plus strict URL
    classification excludes those links structurally and independently.
    """

    soup = BeautifulSoup(html, "html.parser")
    result: list[ForumNode] = []
    by_url: dict[str, int] = {}

    def add(anchor: Tag, parent_url: str | None, order: int, has_children: bool) -> None:
        href = anchor.get("href")
        if not isinstance(href, str):
            return
        url = normalize_url(href, source_url)
        if classify_flashback_url(url) is not FlashbackLinkType.FORUM:
            return
        title = _text(anchor)
        if not title:
            return
        node = ForumNode(SOURCE, title, url, parent_url, order, _forum_id(url), True, has_children)
        existing = by_url.get(url)
        if existing is None:
            by_url[url] = len(result)
            result.append(node)
        elif has_children and not result[existing].has_children:
            result[existing] = replace(result[existing], has_children=True)

    order = 0
    for table in soup.select("table.forumslist"):
        parent_anchor = None
        category = table.find_previous("div", class_="navbar-forum")
        if category:
            parent_anchor = category.select_one("a.forum-title[href]")
        if parent_anchor is None:
            breadcrumb = table.find_previous("div", class_="list-forum-title")
            if breadcrumb:
                parent_anchor = breadcrumb.select_one("ol.breadcrumb a[href]")
        parent_url = None
        if parent_anchor:
            parent_url = normalize_url(str(parent_anchor["href"]), source_url)
            add(parent_anchor, None, order, True)
            order += 1
        forum_cells = table.select("td.td_forum")
        for cell in forum_cells:
            anchor = cell.select_one("a[href]")
            if anchor is None:
                continue
            add(anchor, parent_url, order, bool(cell.select_one("table.forumslist")))
            order += 1
    return result


def diagnose_nav(html: str, source_url: str = BASE_URL) -> list[tuple[str, FlashbackLinkType, str, str]]:
    """Return developer diagnostics for links in known forum-list context."""

    soup = BeautifulSoup(html, "html.parser")
    rows: list[tuple[str, FlashbackLinkType, str, str]] = []
    for anchor in soup.select("table.forumslist a[href], div.navbar-forum a[href], div.list-forum-title a[href], nav a[href]"):
        href = str(anchor.get("href", ""))
        kind = classify_flashback_url(href, source_url)
        in_forum_cell = anchor.find_parent("td", class_="td_forum") is not None
        is_category = "forum-title" in (anchor.get("class") or []) or anchor.find_parent("div", class_="list-forum-title") is not None
        accepted = kind is FlashbackLinkType.FORUM and (in_forum_cell or is_category)
        rows.append(("ACCEPT" if accepted else "REJECT", kind, normalize_url(href, source_url), _text(anchor)))
    return rows


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
