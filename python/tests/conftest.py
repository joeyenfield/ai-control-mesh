"""Runs the Python programs the way the server's contract tests run the Go ones.

server/tests/test_client.py starts `agent` and `agent-machine` as programs against a real
agent-server. The fixtures here stand the Python versions in for the Go builds: real
executables (distlib's launchers, as pip makes for console scripts) running this package.
"""

from __future__ import annotations

import os
import site
import sys
from pathlib import Path

import pytest

PYTHON = Path(__file__).resolve().parents[1]
SERVER_TESTS = PYTHON.parent / "server" / "tests"
sys.path[:0] = [str(PYTHON), str(SERVER_TESTS)]
# The programs' own children (agent-proxy, launches) run `python -m agent_py...` and need to find it too.
os.environ["PYTHONPATH"] = os.pathsep.join([str(PYTHON), site.getusersitepackages(), *filter(None, [os.environ.get("PYTHONPATH")])])
# This computer may have a real Strata, llama-server or a systemd ollama.service: the programs under
# test find none (an empty command), unless a test names a fake.
os.environ.setdefault("AI_PROXY_STRATA_COMMAND", "[]")
os.environ.setdefault("AI_PROXY_LLAMACPP_COMMAND", "[]")
os.environ.setdefault("AI_PROXY_SYSTEMCTL_COMMAND", "[]")


@pytest.fixture(autouse=True)
def isolated_auth_profile(tmp_path, monkeypatch):
    """Both programs now read the same profile; tests must never discover the real login."""
    monkeypatch.setenv("APPDATA", str(tmp_path / "config"))
    monkeypatch.setenv("XDG_CONFIG_HOME", str(tmp_path / "config"))
    for name in ("AI_PROXY_CREDENTIALS", "AI_PROXY_MACHINE_HOME", "AI_PROXY_TOKEN", "AI_PROXY_MACHINE_TOKEN", "AI_PROXY_URL",
                 "AI_PROXY_SETUP_CODE", "AI_PROXY_MACHINE_SETUP_CODE", "AI_PROXY_MACHINE_ENABLE"):
        monkeypatch.delenv(name, raising=False)


def script(folder: Path, name: str, entry: str, python: str = sys.executable) -> Path:
    from pip._vendor.distlib.scripts import ScriptMaker

    maker = ScriptMaker(None, str(folder))
    maker.executable = python
    maker.variants = {""}
    [made] = maker.make(f"{name} = {entry}")
    return Path(made)


@pytest.fixture(scope="session")
def agent_binary(tmp_path_factory) -> Path:
    return script(tmp_path_factory.mktemp("client"), "agent", "agent_py.client:main")


@pytest.fixture(scope="session")
def machine_binary(tmp_path_factory) -> Path:
    return script(tmp_path_factory.mktemp("machine"), "agent-machine", "agent_py.machine:main")
