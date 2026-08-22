from flashback_analyzer.urls import parse_thread_ref, thread_page_url


def test_parse_thread_url():
    ref = parse_thread_ref("https://www.flashback.org/t3322511p2")
    assert ref.thread_id == 3322511
    assert ref.page == 2


def test_page_url():
    assert thread_page_url(123, 1) == "https://www.flashback.org/t123"
    assert thread_page_url(123, 4) == "https://www.flashback.org/t123p4"
