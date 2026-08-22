from pathlib import Path

from flashback_analyzer.parser import parse_thread_page

FIXTURE = Path(__file__).parent / "fixtures" / "thread_page.html"


def test_parse_page():
    parsed = parse_thread_page(FIXTURE.read_text(), thread_id=999, page=1)
    assert parsed.title == "Testtråd"
    assert parsed.max_page == 7
    assert len(parsed.posts) == 2

    first = parsed.posts[0]
    assert first.post_id == 1001
    assert first.author == "Alice"
    assert first.ordinal == 1
    assert first.text.startswith("Jag tror att X stämmer.")
    assert "Bob Nej" not in first.text
    assert len(first.quotes) == 1
    assert first.links == ["https://example.com/source"]
