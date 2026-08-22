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
