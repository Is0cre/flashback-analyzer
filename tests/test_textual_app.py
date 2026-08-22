from pathlib import Path

import pytest

from flashback_analyzer.database import Database
from flashback_analyzer.adapters.flashback.navigation import parse_forum_listing, parse_navbar
from flashback_analyzer.parser import parse_thread_page
from flashback_analyzer.textual_app import FlashbackApp


FIXTURE = Path(__file__).parent / "fixtures" / "thread_page.html"
NAV_FIXTURE = Path(__file__).parent / "fixtures" / "navigation_root.html"
LISTING_FIXTURE = Path(__file__).parent / "fixtures" / "forum_listing.html"


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


@pytest.mark.asyncio
async def test_textual_forum_mode_browses_cached_hierarchy_and_listing(tmp_path):
    db_path = tmp_path / "forums.sqlite3"
    with Database(db_path) as db:
        db.store_forum_nodes(parse_navbar(NAV_FIXTURE.read_text()))
        root = db.forum_roots()[0]
        politics = db.forum_children(root["id"])[0]
        db.store_forum_thread_summaries(
            politics["id"],
            parse_forum_listing(LISTING_FIXTURE.read_text(), "https://www.flashback.org/f11"),
        )
        db.set_navigation_cache("flashback", "navbar", "https://www.flashback.org/", "2999-01-01T00:00:00+00:00")

    app = FlashbackApp(db_path)
    async with app.run_test() as pilot:
        await pilot.press("f")
        await pilot.pause()
        assert "FORUMS" in str(app.query_one("#left-title").render())
        await pilot.press("enter")
        await pilot.pause()
        await pilot.press("enter")
        await pilot.pause()
        assert len(app.query_one("#post-list").children) == 3
        assert app.query_one("#post-list").children[0].thread_id == 1001
        await pilot.press("b")
        await pilot.pause()
        assert app.forum_stack == [root["id"]]


@pytest.mark.asyncio
async def test_textual_renders_flashback_bbcode_as_literal_text(tmp_path):
    parsed = parse_thread_page(FIXTURE.read_text(), thread_id=1000, page=1)
    parsed.title = "[MOD] Tråd med markup"
    parsed.posts[0].author = "moderator[/MOD]"
    parsed.posts[0].text = "Text med en avslutande tagg [/MOD]"
    db_path = tmp_path / "markup.sqlite3"
    with Database(db_path) as db:
        db.store_page(parsed)

    app = FlashbackApp(db_path)
    async with app.run_test() as pilot:
        await pilot.press("enter")
        await pilot.pause()
        assert app.thread_id == 1000
        assert "[/MOD]" in str(app.query_one("#detail").render())
