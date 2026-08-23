"""Legitimate HTTP session boundaries for future authenticated features.

This module deliberately does not implement login, password hashing, or
credential discovery. Applications may inject cookies obtained through a
user-approved session mechanism.
"""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Mapping, Protocol


class SessionProvider(Protocol):
    def headers(self) -> Mapping[str, str]: ...
    def cookies(self) -> Mapping[str, str]: ...
    def is_authenticated(self) -> bool: ...


@dataclass(frozen=True)
class AnonymousSessionProvider:
    def headers(self) -> Mapping[str, str]:
        return {}

    def cookies(self) -> Mapping[str, str]:
        return {}

    def is_authenticated(self) -> bool:
        return False


@dataclass(frozen=True)
class CookieSessionProvider:
    """Use cookies supplied explicitly by the user or an OS session store."""

    _cookies: Mapping[str, str] = field(default_factory=dict, repr=False)
    _headers: Mapping[str, str] = field(default_factory=dict, repr=False)

    def headers(self) -> Mapping[str, str]:
        return dict(self._headers)

    def cookies(self) -> Mapping[str, str]:
        return dict(self._cookies)

    def is_authenticated(self) -> bool:
        return bool(self._cookies)
