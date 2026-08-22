from datetime import date

from flashback_analyzer.discovery import feed_urls, parse_discovery_page


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
