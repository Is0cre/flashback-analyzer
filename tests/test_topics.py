from pathlib import Path

from flashback_analyzer.database import Database
from flashback_analyzer.parser import parse_thread_page
from flashback_analyzer.topics import discover_topics


FIXTURE = Path(__file__).parent / "fixtures" / "thread_page.html"


def test_topic_candidates_and_post_mappings_are_reproducible(tmp_path):
    parsed = parse_thread_page(FIXTURE.read_text(), thread_id=999, page=1)
    parsed.posts[0].text = "polisen tidslinje och bevis"
    parsed.posts[1].text = "polisen bevis motsäger tidslinje"
    with Database(tmp_path / "test.sqlite3") as db:
        db.store_page(parsed)
        first = discover_topics(db.conn, 999, limit=2, min_post_count=2)
        second = discover_topics(db.conn, 999, limit=2, min_post_count=2)

        assert [(row.label, row.post_count) for row in first] == [("bevis", 2), ("polisen", 2)]
        assert [(row.label, row.post_count) for row in second] == [("bevis", 2), ("polisen", 2)]
        assert db.conn.execute("SELECT COUNT(*) FROM topics WHERE thread_id=999").fetchone()[0] == 2
        assert db.conn.execute("SELECT COUNT(*) FROM post_topics").fetchone()[0] == 4
        assert db.conn.execute("SELECT MAX(version) FROM schema_version").fetchone()[0] == 10


def test_topics_reject_invalid_limits(tmp_path):
    with Database(tmp_path / "test.sqlite3") as db:
        try:
            discover_topics(db.conn, 999, limit=0)
        except ValueError:
            pass
        else:
            raise AssertionError("expected ValueError")
