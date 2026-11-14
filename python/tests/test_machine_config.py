"""Machine permission selection, mirroring command/config_test.go in the Go machine."""

from __future__ import annotations

from io import StringIO

import pytest

from agent_py.machine import settings
from agent_py.machine.config import configure_features


def test_selection_preserves_machine_and_only_enables_chosen_functions(tmp_path, monkeypatch):
    monkeypatch.setenv("AI_PROXY_MACHINE_HOME", str(tmp_path))
    machine = settings.Machine.load()
    machine.name, machine.roots, machine.container_cli = "workstation", [str(tmp_path)], "podman"
    machine.containers = True
    machine.save()
    out = StringIO()
    assert configure_features([], StringIO("1,3,terminals\n"), out) == 0
    got = settings.Machine.load()
    assert got.launches and got.files and got.commands
    assert not got.servers and not got.containers and not got.updates
    assert (got.id, got.name, got.roots, got.container_cli) == (machine.id, machine.name, machine.roots, machine.container_cli)
    for detail in ("hidden files", "root privileges", "shell commands can access", "within a minute"):
        assert detail in out.getvalue()


@pytest.mark.parametrize(("text", "code", "disabled"), [
    ("\n", 0, False), ("", 1, False), ("files", 1, False),
    ("1,unknown\n", 1, False), (",\nnone\n", 0, True), ("7\nnone\n", 0, True),
])
def test_keep_cancel_retry_and_disable(tmp_path, monkeypatch, text, code, disabled):
    monkeypatch.setenv("AI_PROXY_MACHINE_HOME", str(tmp_path))
    machine = settings.Machine.load()
    machine.servers = True
    machine.save()
    before = settings.machine_path().read_bytes()
    assert configure_features([], StringIO(text), StringIO()) == code
    if disabled:
        assert not any(getattr(settings.Machine.load(), name) for name in settings.TOGGLES)
    else:
        assert settings.machine_path().read_bytes() == before
