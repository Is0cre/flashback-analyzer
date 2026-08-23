from pathlib import Path

from flashback_analyzer.adapters.flashback.navigation import parse_forum_listing, parse_navbar
from flashback_analyzer.database import Database


FIXTURES = Path(__file__).parent / "fixtures"


def test_navbar_preserves_arbitrary_hierarchy_and_normalizes_urls():
    nodes = parse_navbar((FIXTURES / "navigation_root.html").read_text())
    by_title = {node.title: node for node in nodes}
    assert by_title["Samhälle"].parent_url is None
    assert by_title["Politik"].parent_url.endswith("/f10")
    assert by_title["Brott"].parent_url.endswith("/f12")
    assert by_title["Dator & IT"].url == "https://www.flashback.org/f20"
    assert all(node.is_browsable for node in nodes)


def test_navbar_ignores_profile_and_non_forum_links():
    nodes = parse_navbar("""<nav>
        <a href='/f10'>Samhälle</a>
        <a href='/u42'>moderator[/MOD]</a>
        <a href='/forums/moderator'>moderator</a>
    </nav>""")
    assert [node.title for node in nodes] == ["Samhälle"]


def test_forum_listing_tolerates_missing_optional_fields():
    rows = parse_forum_listing((FIXTURES / "forum_listing.html").read_text(), "https://www.flashback.org/f11")
    assert [row.thread_id for row in rows] == [1001, 1002, 1003]
    assert rows[0].is_sticky is True
    assert rows[0].reply_count == 12
    assert rows[0].view_count == 1204
    assert rows[0].last_post_at is not None
    assert rows[2].reply_count is None


def test_navigation_storage_resolves_parents_and_maps_threads(tmp_path):
    nodes = parse_navbar((FIXTURES / "navigation_root.html").read_text())
    rows = parse_forum_listing((FIXTURES / "forum_listing.html").read_text(), "https://www.flashback.org/f11")
    with Database(tmp_path / "navigation.sqlite3") as db:
        db.store_forum_nodes(nodes)
        root = db.forum_roots()
        assert [row["title"] for row in root] == ["Samhälle", "Dator & IT"]
        politics = db.forum_children(root[0]["id"])[0]
        db.store_forum_thread_summaries(politics["id"], rows)
        assert [row["thread_id"] for row in db.forum_thread_rows(politics["id"])] == [1001, 1002, 1003]
        assert db.tracked_thread_rows() == []


def test_replacing_navigation_removes_stale_nodes_without_touching_threads(tmp_path):
    nodes = parse_navbar((FIXTURES / "navigation_root.html").read_text())
    with Database(tmp_path / "navigation.sqlite3") as db:
        db.store_forum_nodes(nodes)
        db.replace_forum_nodes([nodes[0]])
        assert [row["title"] for row in db.forum_roots()] == ["Samhälle"]
