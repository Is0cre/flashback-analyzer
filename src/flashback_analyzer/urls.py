from __future__ import annotations

import re
from dataclasses import dataclass

THREAD_RE = re.compile(r"(?:https?://(?:www\.)?flashback\.org/)?t(?P<thread>\d+)(?:p(?P<page>\d+))?", re.I)


@dataclass(frozen=True, slots=True)
class ThreadRef:
    thread_id: int
    page: int = 1

    @property
    def canonical_url(self) -> str:
        suffix = "" if self.page == 1 else f"p{self.page}"
        return f"https://www.flashback.org/t{self.thread_id}{suffix}"


def parse_thread_ref(value: str) -> ThreadRef:
    match = THREAD_RE.search(value.strip())
    if not match:
        raise ValueError(f"Inte en Flashback-tråd-URL eller tråd-ID: {value!r}")
    return ThreadRef(
        thread_id=int(match.group("thread")),
        page=int(match.group("page") or 1),
    )


def thread_page_url(thread_id: int, page: int) -> str:
    suffix = "" if page == 1 else f"p{page}"
    return f"https://www.flashback.org/t{thread_id}{suffix}"
