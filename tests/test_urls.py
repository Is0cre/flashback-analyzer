from flashback_analyzer.urls import normalize_url, parse_thread_ref, thread_page_url, url_domain


def test_parse_thread_url():
    ref = parse_thread_ref("https://www.flashback.org/t3322511p2")
    assert ref.thread_id == 3322511
    assert ref.page == 2


def test_parse_compact_thread_ref_with_copy_marker():
    ref = parse_thread_ref("t3742384C")
    assert ref.thread_id == 3742384
    assert ref.page == 1


def test_page_url():
    assert thread_page_url(123, 1) == "https://www.flashback.org/t123"
    assert thread_page_url(123, 4) == "https://www.flashback.org/t123p4"


def test_normalize_url_removes_tracking_and_normalizes_host():
    value = normalize_url("HTTPS://WWW.Example.COM/story/?utm_source=x&b=2#section")
    assert value == "https://example.com/story?b=2"
    assert url_domain(value) == "example.com"
