from pathlib import Path

from flashback_analyzer.adapters.flashback.navigation import (
    FlashbackLinkType,
    classify_flashback_url,
    diagnose_nav,
    parse_forum_listing,
    parse_navbar,
)
from flashback_analyzer.database import Database


FIXTURES = Path(__file__).parent / "fixtures"


def test_navbar_preserves_arbitrary_hierarchy_and_normalizes_urls():
    nodes = parse_navbar((FIXTURES / "navigation_root.html").read_text())
    by_title = {node.title: node for node in nodes}
    assert by_title["Samhälle"].parent_url is None
    assert by_title["Politik"].parent_url.endswith("/f10-samhalle-60823")
    assert by_title["Dator och IT"].url == "https://www.flashback.org/f20-dator-och-it-60823"
    nested = parse_navbar((FIXTURES / "navigation_nested.html").read_text())
    nested_by_title = {node.title: node for node in nested}
    assert nested_by_title["Brott"].parent_url.endswith("/f12-juridik-60823")
    assert all(node.is_browsable for node in nodes)


def test_navbar_ignores_profile_and_non_forum_links():
    nodes = parse_navbar("""<nav>
        <a href='/f10'>Samhälle</a>
        <a href='/u42'>moderator[/MOD]</a>
        <a href='/forums/moderator'>moderator</a>
    </nav>""")
    assert nodes == []


def test_flashback_url_classifier_distinguishes_forums_from_last_post_users():
    assert classify_flashback_url("/f555-ai-artificiell-intelligens-60823") is FlashbackLinkType.FORUM
    assert classify_flashback_url("/f555lp") is FlashbackLinkType.USER
    assert classify_flashback_url("/u123456") is FlashbackLinkType.USER
    assert classify_flashback_url("/t1234567n") is FlashbackLinkType.THREAD
    assert classify_flashback_url("/p1234567") is FlashbackLinkType.POST


def test_nav_diagnostics_reports_structural_acceptance_and_rejections():
    diagnostics = diagnose_nav((FIXTURES / "navigation_root.html").read_text())
    by_title = {title: (decision, kind) for decision, kind, _href, title in diagnostics}
    assert by_title["Samhälle"] == ("ACCEPT", FlashbackLinkType.FORUM)
    assert by_title["PeterNoster"] == ("REJECT", FlashbackLinkType.USER)
    assert by_title["Fanten"] == ("REJECT", FlashbackLinkType.THREAD)
    assert by_title["Cpt.Pepper"] == ("REJECT", FlashbackLinkType.USER)


def test_forum_tree_excludes_regression_usernames_from_other_cells_and_nav():
    names = {node.title for node in parse_navbar((FIXTURES / "navigation_root.html").read_text())}
    for username in ("PeterNoster", "Fl1pst3r", "WeirdRaccoon", "Alibabbla", "Cpt.Pepper", "SC430", "Fanten", "Svartkatt13"):
        assert username not in names
    assert "Felix042" in names  # valid forum-like title is retained by structure


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
        assert [row["title"] for row in root] == ["Samhälle", "Dator och IT"]
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
