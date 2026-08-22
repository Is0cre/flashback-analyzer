from datetime import date

from flashback_analyzer.discovery import feed_urls, parse_discovery_page
from flashback_analyzer.database import Database


HTML = """
<html><body>
<section><a href="/t1234567">Ny tråd om test</a>
<span>1 234 visningar • 56 läsare • 78 svar</span></section>
<section><a href="/t1234567p2">Duplicerad sidlänk</a></section>
<section><a href="/t7654321">Andra tråden</a></section>
</body></html>
"""


def test_parse_discovery_page_extracts_threads_and_stats():
    items = parse_discovery_page(HTML, "aktuella")
    assert [(item.thread_id, item.title) for item in items] == [(1234567, "Ny tråd om test"), (7654321, "Andra tråden")]
    assert items[0].views == 1234
    assert items[0].readers == 56
    assert items[0].replies == 78


def test_feed_urls_are_date_scoped():
    urls = feed_urls(date(2026, 8, 22))
    assert urls["aktuella"].endswith("/aktuella-amnen/2026-08-22")
    assert urls["nya inlägg"].endswith("/nya-inlagg/2026-08-22")


def test_discovery_items_are_cached_in_sqlite(tmp_path):
    items = parse_discovery_page(HTML, "aktuella")
    with Database(tmp_path / "test.sqlite3") as db:
        assert db.store_discovery_items(items) == 2
        cached = db.cached_discovery_items()
        assert [item.thread_id for item in cached] == [1234567, 7654321]
        assert cached[0].replies == 78
