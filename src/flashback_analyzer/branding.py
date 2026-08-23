"""Terminal-native BACKFLASH identity and fallback artwork."""

from __future__ import annotations

from .ui_strings import text


_MEDIUM_MARK = "BACKFLASH // DISKURSÖVERVAKNING"
_FULL_MARK = """┌─┐┌─┐┌─┬┐┌─┐┌─┐┬  ┌─┐┌─┐┬ ┬
├─┤├─┘│ ││├┤ ├─┤│  ├─┤├─┘├─┤
┴ ┴┴  ┴ ┴└─┘┴ ┴┴─┘┴ ┴┴  ┴ ┴"""

_DOG = r"""      __
  ___ /  \__
 /   \     _`-.
|  -  |   (o) |
| ___ |  .---.|
 \___/   `---'/
  `-.___.-.-'
      /| |\
     /_/_\_\\
"""


def render_wordmark(width: int, theme: str = "default") -> str:
    """Return a width-safe wordmark; theme styling belongs to the TUI CSS."""

    if width < 60:
        return "BACKFLASH"
    if width < 100:
        return _MEDIUM_MARK
    return _FULL_MARK


def render_empty_state(width: int, height: int, theme: str = "default") -> str:
    """Return restrained empty-state copy with an original tired-dog mark."""

    if width < 60 or height < 8:
        return "BACKFLASH\n\n" + text("empty")
    return f"{_DOG}\n\nBACKFLASH\n{text('cache_warm')}\n\n{text('empty')}"


def render_overview() -> str:
    """Compact local-first landing state shown before a thread is selected."""
    return "BACKFLASH // ÖVERSIKT\n\n" + text("select_thread")
