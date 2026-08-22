from pathlib import Path

from flashback_analyzer.database import Database
from flashback_analyzer.parser import parse_thread_page
from flashback_analyzer.questions import discover_questions


FIXTURE = Path(__file__).parent / "fixtures" / "thread_page.html"


def test_explicit_questions_keep_evidence_mappings(tmp_path):
    parsed = parse_thread_page(FIXTURE.read_text(), thread_id=999, page=1)
    parsed.posts[0].text = "Är polisens tidslinje rimlig? Jag tror det."
    parsed.posts[1].text = "Är polisens tidslinje rimlig? Jag tror inte det."
    with Database(tmp_path / "test.sqlite3") as db:
        db.store_page(parsed)
        rows = discover_questions(db.conn, 999)
        assert len(rows) == 1
        assert rows[0].question == "Är polisens tidslinje rimlig?"
        assert rows[0].post_count == 2
        assert db.conn.execute("SELECT COUNT(*) FROM post_questions").fetchone()[0] == 2
        assert db.conn.execute("SELECT MAX(version) FROM schema_version").fetchone()[0] == 5


def test_questions_do_not_exist_for_unknown_thread(tmp_path):
    with Database(tmp_path / "test.sqlite3") as db:
        try:
            discover_questions(db.conn, 999)
        except KeyError as exc:
            assert exc.args == (999,)
        else:
            raise AssertionError("expected KeyError")
