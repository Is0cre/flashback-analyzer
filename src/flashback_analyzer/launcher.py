"""Minimal console launcher: show the local splash before heavy imports."""

from __future__ import annotations

import shutil
import sys
from pathlib import Path


def _asset_path() -> Path:
    candidates = (
        Path.cwd() / "backflash.ans",
        Path(__file__).with_name("backflash.ans"),
        Path(__file__).resolve().parents[2] / "backflash.ans",
    )
    return next((path for path in candidates if path.is_file()), candidates[0])


def show_early_splash() -> None:
    if not sys.stdout.isatty():
        return
    width = shutil.get_terminal_size((80, 24)).columns
    if width < 80:
        output = "BACKFLASH\n"
    elif width < 120:
        output = "BACKFLASH // DISKURSÖVERVAKNING\n"
    else:
        try:
            output = _asset_path().read_text(encoding="utf-8")
        except (OSError, UnicodeError):
            output = "BACKFLASH\n"
    from .ui_strings import startup_quote
    sys.stdout.write(output.rstrip("\n") + "\x1b[0m\n" + startup_quote() + "\n")
    sys.stdout.flush()


def main() -> None:
    if len(sys.argv) == 1:
        show_early_splash()
    from .cli import app
    app()
