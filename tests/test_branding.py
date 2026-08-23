from flashback_analyzer.branding import render_empty_state, render_wordmark


def test_wordmark_degrades_by_terminal_width():
    assert render_wordmark(40) == "BACKFLASH"
    assert "BACKFLASH" in render_wordmark(80)
    assert "┌" in render_wordmark(120)


def test_empty_state_has_compact_and_mascot_forms():
    assert "humanity pending" in render_empty_state(40, 20)
    assert "DOG AWAKE" in render_empty_state(80, 20)
