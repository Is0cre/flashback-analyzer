from __future__ import annotations

import re
from datetime import datetime
from urllib.parse import urljoin

from bs4 import BeautifulSoup, Tag

from .models import ParsedPage, Post, Quote

POST_MESSAGE_ID_RE = re.compile(r"post_message_(\d+)")
POST_ID_RE = re.compile(r"(?:post|p)(?:_|-)?(\d+)", re.I)
PAGE_LINK_RE = re.compile(r"/t(\d+)p(\d+)(?:$|[?#/])")
POST_NUMBER_RE = re.compile(r"#\s*(\d+)")
DATE_RE = re.compile(r"(20\d{2}-\d{2}-\d{2}),?\s+(\d{2}:\d{2})")
QUOTE_HEADER_RE = re.compile(r"Ursprungligen\s+postat\s+av\s+(.+)", re.I)


def _text(node: Tag | None) -> str:
    return node.get_text(" ", strip=True) if node else ""


def _extract_post_id(message: Tag) -> int | None:
    if message.get("id"):
        m = POST_MESSAGE_ID_RE.search(str(message["id"]))
        if m:
            return int(m.group(1))

    ancestor = message.find_parent(id=POST_ID_RE)
    if ancestor and ancestor.get("id"):
        m = POST_ID_RE.search(str(ancestor["id"]))
        if m:
            return int(m.group(1))
    return None


def _find_container(message: Tag) -> Tag:
    # Prefer the nearest wrapper that also contains author/date metadata.
    for parent in message.parents:
        if not isinstance(parent, Tag):
            continue
        classes = set(parent.get("class", []))
        ident = str(parent.get("id", ""))
        if {"post", "post-wrapper", "post-row"} & classes or POST_ID_RE.fullmatch(ident):
            return parent
    return message.parent if isinstance(message.parent, Tag) else message


def _extract_author(container: Tag) -> str:
    selectors = [
        ".post-user-username",
        ".username",
        ".bigusername",
        "a[href*='/u']",
        "a[href*='member.php']",
    ]
    for selector in selectors:
        node = container.select_one(selector)
        value = _text(node)
        if value:
            return value
    return "[okänd]"


def _extract_datetime(container: Tag) -> datetime | None:
    time_node = container.find("time")
    if time_node:
        value = time_node.get("datetime") or _text(time_node)
        if value:
            try:
                return datetime.fromisoformat(str(value).replace("Z", "+00:00"))
            except ValueError:
                pass

    m = DATE_RE.search(_text(container))
    if m:
        return datetime.strptime(f"{m.group(1)} {m.group(2)}", "%Y-%m-%d %H:%M")
    return None


def _extract_ordinal(container: Tag) -> int | None:
    # Typical visible marker: "#13".
    for node in container.find_all(["a", "span"], limit=50):
        m = POST_NUMBER_RE.fullmatch(_text(node))
        if m:
            return int(m.group(1))
    return None


def _extract_quotes(message: Tag) -> list[Quote]:
    quotes: list[Quote] = []
    seen: set[str] = set()
    selectors = [".quote", ".post-bbcode-quote", "blockquote"]
    for selector in selectors:
        for node in message.select(selector):
            text = _text(node)
            if not text or text in seen:
                continue
            seen.add(text)
            author = None
            m = QUOTE_HEADER_RE.search(text)
            if m:
                author = m.group(1).split(" ", 1)[0].strip()
            post_id = None
            for link in node.select("a[href]"):
                hm = re.search(r"(?:/p|#p?)(\d+)", link.get("href", ""))
                if hm:
                    post_id = int(hm.group(1))
                    break
            quotes.append(Quote(author=author, text=text, post_id=post_id))
    return quotes


def _extract_clean_text(message: Tag) -> tuple[str, str]:
    raw = _text(message)
    clone = BeautifulSoup(str(message), "html.parser")
    root = clone.find()
    if root:
        for selector in [".quote", ".post-bbcode-quote", "blockquote"]:
            for node in root.select(selector):
                node.decompose()
        clean = root.get_text(" ", strip=True)
    else:
        clean = raw
    return clean, raw


def _extract_links(message: Tag) -> list[str]:
    links: list[str] = []
    seen: set[str] = set()
    for a in message.select("a[href]"):
        href = str(a.get("href", "")).strip()
        if not href or href.startswith(("javascript:", "mailto:")):
            continue
        absolute = urljoin("https://www.flashback.org/", href)
        if absolute not in seen:
            seen.add(absolute)
            links.append(absolute)
    return links


def parse_thread_page(html: str, thread_id: int, page: int) -> ParsedPage:
    soup = BeautifulSoup(html, "html.parser")
    title = _text(soup.find("h1")) or (soup.title.string.strip() if soup.title and soup.title.string else None)

    messages = soup.select("div.post_message[id^='post_message_']")
    if not messages:
        messages = soup.select("[id^='post_message_']")

    posts: list[Post] = []
    for message in messages:
        if not isinstance(message, Tag):
            continue
        post_id = _extract_post_id(message)
        if post_id is None:
            continue
        container = _find_container(message)
        clean_text, raw_text = _extract_clean_text(message)
        posts.append(
            Post(
                post_id=post_id,
                thread_id=thread_id,
                page=page,
                ordinal=_extract_ordinal(container),
                author=_extract_author(container),
                posted_at=_extract_datetime(container),
                text=clean_text,
                raw_text=raw_text,
                quotes=_extract_quotes(message),
                links=_extract_links(message),
            )
        )

    max_page = max(1, page)
    for a in soup.select("a[href]"):
        href = str(a.get("href", ""))
        m = PAGE_LINK_RE.search(href)
        if m and int(m.group(1)) == thread_id:
            max_page = max(max_page, int(m.group(2)))

    return ParsedPage(
        thread_id=thread_id,
        page=page,
        title=title,
        posts=posts,
        max_page=max_page,
    )
