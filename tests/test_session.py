import httpx

from flashback_analyzer.fetcher import Fetcher
from flashback_analyzer.session import AnonymousSessionProvider, CookieSessionProvider
from flashback_analyzer.write import ReadOnlyWriteClient, ReplyDraft


def test_anonymous_fetcher_does_not_add_session_material(tmp_path):
    with Fetcher(tmp_path, min_delay_seconds=0) as fetcher:
        assert fetcher.session_provider.is_authenticated() is False
        assert fetcher.client.cookies == httpx.Cookies()


def test_cookie_session_is_explicit_and_not_logged():
    session = CookieSessionProvider({"session": "opaque"})
    assert session.is_authenticated() is True
    assert session.cookies() == {"session": "opaque"}
    assert AnonymousSessionProvider().is_authenticated() is False


def test_write_is_guarded_until_authenticated_client_exists():
    try:
        ReadOnlyWriteClient().publish_reply(ReplyDraft(1, "hello"))
    except RuntimeError as exc:
        assert "not configured" in str(exc)
    else:
        raise AssertionError("read-only writer unexpectedly posted")
