"""Source-agnostic objects used by forum navigation adapters."""

from dataclasses import dataclass
from datetime import datetime


@dataclass(frozen=True)
class ForumNode:
    """A category or browsable forum in a source's navigation tree."""

    source: str
    title: str
    url: str
    parent_url: str | None = None
    sort_order: int = 0
    external_id: str | None = None
    is_browsable: bool = False


@dataclass(frozen=True)
class ThreadSummary:
    """A lightweight thread row from a forum listing."""

    source: str
    thread_id: int
    title: str
    url: str
    author: str | None = None
    reply_count: int | None = None
    view_count: int | None = None
    last_post_at: datetime | None = None
    last_post_author: str | None = None
    is_sticky: bool = False
    page_count: int | None = None
