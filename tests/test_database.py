from pathlib import Path

from flashback_analyzer.database import Database
from flashback_analyzer.parser import parse_thread_page

FIXTURE = Path(__file__).parent / "fixtures" / "thread_page.html"


def test_store_and_summary(tmp_path):
    parsed = parse_thread_page(FIXTURE.read_text(), thread_id=999, page=1)
    with Database(tmp_path / "test.sqlite3") as db:
        assert db.store_page(parsed) == 2
        summary = db.thread_summary(999)
        assert summary["posts"] == 2
        assert summary["users"] == 2


def test_storage_keeps_provenance_and_normalizes_links(tmp_path):
    parsed = parse_thread_page(FIXTURE.read_text(), thread_id=999, page=1, source_url="https://www.flashback.org/t999")
    with Database(tmp_path / "test.sqlite3") as db:
        db.store_page(parsed)
        post = db.conn.execute("SELECT position_on_page, source_url, content_hash FROM posts WHERE post_id=1001").fetchone()
        assert post["position_on_page"] == 1
        assert post["source_url"].endswith("/t999")
        assert len(post["content_hash"]) == 64
        raw = db.conn.execute("SELECT content_hash, raw_html FROM raw_pages WHERE thread_id=999 AND page=1").fetchone()
        assert raw["raw_html"].startswith("<!doctype html>")
        assert len(raw["content_hash"]) == 64
        link = db.conn.execute("SELECT domain, author FROM links WHERE post_id=1001").fetchone()
        assert link["domain"] == "example.com"
        assert link["author"] == "Alice"


def test_store_page_is_idempotent(tmp_path):
    parsed = parse_thread_page(FIXTURE.read_text(), thread_id=999, page=1)
    with Database(tmp_path / "test.sqlite3") as db:
        assert db.store_page(parsed) == 2
        assert db.store_page(parsed) == 2
        assert db.conn.execute("SELECT COUNT(*) FROM posts WHERE thread_id=999").fetchone()[0] == 2
        assert db.conn.execute("SELECT COUNT(*) FROM quotes WHERE post_id=1001").fetchone()[0] == 1
