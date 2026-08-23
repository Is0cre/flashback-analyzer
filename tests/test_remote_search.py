from pathlib import Path

from flashback_analyzer.adapters.flashback.search import FlashbackSearchAdapter
from flashback_analyzer.search import SearchResult


def test_flashback_search_results_parse_thread_post_author_forum_and_snippet():
    html = (Path(__file__).parent / "fixtures" / "search_results.html").read_text()
    results = FlashbackSearchAdapter.parse_results(html)
    assert [(r.post_id, r.thread_id) for r in results] == [(9001, 7001), (9002, 7002)]
    assert results[0].title == "Linuxtråden"
    assert results[0].author == "PeterNoster"
    assert results[0].forum == "Linux"
    assert "kör Linux" in results[0].snippet


def test_remote_search_builds_observed_flashback_get_parameters():
    class FakeFetcher:
        def __init__(self):
            self.url = None

        def fetch_url(self, url):
            self.url = url
            return "<div id='posts'></div>"

    fetcher = FakeFetcher()
    FlashbackSearchAdapter(fetcher).search("linux", scope=21, page=2)
    assert fetcher.url == "https://www.flashback.org/sok/?so=pd&query=linux&sp=1&search_post=1&f=21&page=2"
