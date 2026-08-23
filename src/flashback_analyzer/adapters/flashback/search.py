"""Flashback's normal public keyword-search adapter."""

from __future__ import annotations

import re
from urllib.parse import urlencode

from bs4 import BeautifulSoup, Tag

from ...search import SearchResult
from .navigation import BASE_URL, normalize_url


class FlashbackSearchAdapter:
    """Build and parse the GET form used by Flashback's ``/sok/`` page."""

    endpoint = "https://www.flashback.org/sok/"

    def __init__(self, fetcher: object) -> None:
        self.fetcher = fetcher

    def search(self, query: str, scope: int | None = None, page: int = 1) -> list[SearchResult]:
        params = {"so": "pd", "query": query, "sp": "1", "search_post": "1"}
        if scope is not None:
            params["f"] = str(scope)
        if page > 1:
            params["page"] = str(page)
        url = f"{self.endpoint}?{urlencode(params)}"
        html = self.fetcher.fetch_url(url)
        return self.parse_results(html, url)

    @staticmethod
    def parse_results(html: str, source_url: str = BASE_URL) -> list[SearchResult]:
        soup = BeautifulSoup(html, "html.parser")
        results: list[SearchResult] = []
        for post in soup.select("#posts > div.post.post-small"):
            post_id = _post_id(post)
            if post_id is None:
                continue
            thread_match = re.search(r"/t(\d+)", str(post))
            thread_id = int(thread_match.group(1)) if thread_match else None
            title_link = next((a for a in post.select(".post-body a[href]") if str(a.get("href", "")).startswith("/p")), None)
            title = _text(title_link)
            author_link = next((a for a in post.select(".post-body a[href]") if str(a.get("href", "")).startswith("/u")), None)
            forum_link = next((a for a in post.select(".post-heading a[href]") if str(a.get("href", "")).startswith("/f")), None)
            message = post.select_one(".post_message")
            result_url = normalize_url(str(title_link.get("href")) if title_link else f"/p{post_id}", source_url)
            results.append(SearchResult(
                result_type="post", thread_id=thread_id, post_id=post_id,
                title=title or f"Post #{post_id}", author=_text(author_link) or None,
                forum=_text(forum_link) or None, timestamp=None,
                snippet=_text(message)[:400], url=result_url,
            ))
        return results


def _post_id(node: Tag) -> int | None:
    match = re.search(r"post(\d+)", str(node.get("id", "")))
    if match:
        return int(match.group(1))
    match = re.search(r"/p(\d+)", str(node))
    return int(match.group(1)) if match else None


def _text(node: Tag | None) -> str:
    return " ".join(node.get_text(" ", strip=True).split()) if node else ""
