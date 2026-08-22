from __future__ import annotations

from dataclasses import dataclass, field
from datetime import datetime


@dataclass(slots=True)
class Quote:
    author: str | None
    text: str
    post_id: int | None = None


@dataclass(slots=True)
class Post:
    post_id: int
    thread_id: int
    page: int
    ordinal: int | None
    author: str
    posted_at: datetime | None
    text: str
    raw_text: str
    quotes: list[Quote] = field(default_factory=list)
    links: list[str] = field(default_factory=list)


@dataclass(slots=True)
class ParsedPage:
    thread_id: int
    page: int
    title: str | None
    posts: list[Post]
    max_page: int
