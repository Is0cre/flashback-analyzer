from pathlib import Path

from flashback_analyzer.database import Database
from flashback_analyzer.parser import parse_thread_page
from flashback_analyzer.segmentation import build_segments


FIXTURE = Path(__file__).parent / "fixtures" / "thread_page.html"


def test_segments_are_rebuilt_idempotently_with_membership(tmp_path):
    parsed = parse_thread_page(FIXTURE.read_text(), thread_id=999, page=1)
    with Database(tmp_path / "test.sqlite3") as db:
        db.store_page(parsed)
        first = build_segments(db.conn, 999, max_posts=1, gap_hours=24)
        second = build_segments(db.conn, 999, max_posts=1, gap_hours=24)

        assert [(row.first_post_id, row.last_post_id) for row in first] == [(1001, 1001), (1002, 1002)]
        assert second == first
        assert db.conn.execute("SELECT COUNT(*) FROM segments WHERE thread_id=999").fetchone()[0] == 2
        assert db.conn.execute("SELECT COUNT(*) FROM segment_posts").fetchone()[0] == 2
        assert db.conn.execute("SELECT MAX(version) FROM schema_version").fetchone()[0] == 3


def test_invalid_segment_size_is_rejected(tmp_path):
    with Database(tmp_path / "test.sqlite3") as db:
        try:
            build_segments(db.conn, 999, max_posts=0)
        except ValueError as exc:
            assert "max_posts" in str(exc)
        else:
            raise AssertionError("expected ValueError")
