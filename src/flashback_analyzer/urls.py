from __future__ import annotations

import re
from dataclasses import dataclass
from urllib.parse import parse_qsl, urlencode, urlsplit, urlunsplit

# Flashback links are often copied with a trailing ``C`` marker. It is not
# part of the numeric thread ID and is accepted for convenient CLI use.
THREAD_RE = re.compile(r"(?:https?://(?:www\.)?flashback\.org/)?t(?P<thread>\d+)(?:c)?(?:p(?P<page>\d+))?", re.I)


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


def normalize_url(value: str) -> str:
    """Return a stable URL suitable for deduplication and source statistics."""
    parts = urlsplit(value.strip())
    if not parts.scheme or not parts.netloc:
        return value.strip()
    hostname = (parts.hostname or "").lower()
    if hostname.startswith("www."):
        hostname = hostname[4:]
    netloc = hostname
    if parts.port and not ((parts.scheme.lower() == "http" and parts.port == 80) or
                           (parts.scheme.lower() == "https" and parts.port == 443)):
        netloc = f"{hostname}:{parts.port}"
    tracking = {"utm_source", "utm_medium", "utm_campaign", "utm_term", "utm_content",
                "gclid", "fbclid", "mc_cid", "mc_eid"}
    query = urlencode([(key, val) for key, val in parse_qsl(parts.query, keep_blank_values=True)
                       if key.lower() not in tracking])
    path = parts.path or "/"
    if path != "/":
        path = path.rstrip("/")
    return urlunsplit((parts.scheme.lower(), netloc, path, query, ""))


def url_domain(value: str) -> str:
    """Return the lower-case hostname, without a leading ``www.``."""
    hostname = (urlsplit(value).hostname or "").lower()
    return hostname[4:] if hostname.startswith("www.") else hostname
