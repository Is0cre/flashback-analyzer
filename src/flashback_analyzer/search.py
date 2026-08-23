"""Search service boundaries for local and remote search."""

from __future__ import annotations

from dataclasses import dataclass
import re
from typing import Protocol

from .database import Database


@dataclass(frozen=True)
class SearchResult:
    result_type: str
    thread_id: int | None
    post_id: int | None
    title: str
    author: str | None
    forum: str | None
    timestamp: str | None
    snippet: str
    url: str


class RemoteSearchProvider(Protocol):
    def search(self, query: str, scope: int | None = None, page: int = 1) -> list[SearchResult]: ...


class SearchService:
    """Local canonical-dataset search used by CLI and TUI."""

    def __init__(self, database: Database) -> None:
        self.database = database

    def search_posts(self, query: str, thread_id: int | None = None, limit: int = 100) -> list[SearchResult]:
        post_match = re.fullmatch(r"#?(\d+)", query.strip())
        rows = self.database.search_posts(query, thread_id=thread_id, limit=limit)
        if post_match:
            params: list[object] = [int(post_match.group(1))]
            clause = ""
            if thread_id is not None:
                clause = " AND p.thread_id=?"
                params.append(thread_id)
            rows = self.database.conn.execute(f"""SELECT p.post_id, p.ordinal, p.page, p.position_on_page,
                p.posted_at, p.text, u.username FROM posts p JOIN users u ON u.user_id=p.user_id
                WHERE p.post_id=?{clause} LIMIT ?""", [*params, limit]).fetchall()
        return [
            SearchResult(
                result_type="post", thread_id=thread_id, post_id=int(row["post_id"]),
                title="", author=str(row["username"]), forum=None,
                timestamp=row["posted_at"], snippet=_snippet(str(row["text"]), query),
                url=f"https://www.flashback.org/p{row['post_id']}#p{row['post_id']}",
            )
            for row in self.database.search_posts(query, thread_id=thread_id, limit=limit)
        ]


def _snippet(text: str, query: str, width: int = 240) -> str:
    """Return a compact context window around the first plain-text term."""
    terms = [term.lstrip("#") for term in query.split() if term and not term.startswith("user:")]
    lower = text.casefold()
    index = min((lower.find(term.casefold()) for term in terms if lower.find(term.casefold()) >= 0), default=0)
    start = max(0, index - width // 3)
    end = min(len(text), start + width)
    snippet = text[start:end].strip()
    return ("…" if start else "") + snippet + ("…" if end < len(text) else "")
