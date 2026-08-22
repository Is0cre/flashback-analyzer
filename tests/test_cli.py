from typer.testing import CliRunner
from pathlib import Path

import flashback_analyzer.cli as cli


def test_no_subcommand_launches_textual_app(monkeypatch):
    launched: list[object] = []
    monkeypatch.setattr(cli, "launch_textual_tui", lambda *args, **kwargs: launched.append(args))
    result = CliRunner().invoke(cli.app, [])
    assert result.exit_code == 0
    assert launched == [(Path("data/flashback.sqlite3"),)]
