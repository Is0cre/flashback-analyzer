from pathlib import Path

import pytest

from flashback_analyzer.database import Database
from flashback_analyzer.parser import parse_thread_page
from flashback_analyzer.textual_app import FlashbackApp


FIXTURE = Path(__file__).parent / "fixtures" / "thread_page.html"


@pytest.mark.asyncio
async def test_textual_reader_opens_navigates_quotes_and_persists_read_state(tmp_path):
    db_path = tmp_path / "reader.sqlite3"
    with Database(db_path) as db:
        db.store_page(parse_thread_page(FIXTURE.read_text(), thread_id=999, page=1))

    app = FlashbackApp(db_path)
    async with app.run_test() as pilot:
        await pilot.pause()
        assert len(app.query_one("#thread-list").children) == 1
        await pilot.press("enter")
        await pilot.pause()
        assert app.thread_id == 999
        assert len(app.query_one("#post-list").children) == 2
        assert "QUOTED CONTENT" in str(app.query_one("#detail").render())
        await pilot.press("j")
        await pilot.pause()
        assert app.query_one("#post-list").index == 1
        with Database(db_path) as db:
            assert db.reader_position(999) == 1002
        await pilot.press("?")
        await pilot.pause()
        assert "FLASHBACK READER" in str(app.screen.query_one("Static").render())
        await pilot.press("q")
        await pilot.pause()
        await pilot.press("q")
        await pilot.pause()
        assert app.thread_id is None
