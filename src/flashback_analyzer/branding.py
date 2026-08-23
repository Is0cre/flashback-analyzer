"""Terminal-native BACKFLASH identity and fallback artwork."""

from __future__ import annotations


_MEDIUM_MARK = "BACKFLASH // DISCOURSE OPS"
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
        return "BACKFLASH\n\nnothing selected.\nhumanity pending."
    return f"{_DOG}\n\nBACKFLASH\nDOG AWAKE // CACHE WARM\n\nnothing selected.\nhumanity pending."
