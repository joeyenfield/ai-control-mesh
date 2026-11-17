"""Ollama setup regressions shared in behavior with the Go machine tests."""
from __future__ import annotations

import json
import sys
import time
from pathlib import Path

import pytest

from agent_py.machine import ollama_install, servers, settings


@pytest.fixture
def machine(tmp_path, monkeypatch):
    monkeypatch.setenv("AI_PROXY_MACHINE_HOME", str(tmp_path / "home"))
    prefix = tmp_path / "prefix"
    prefix.mkdir()
    monkeypatch.setenv("PREFIX", str(prefix))
    monkeypatch.setenv("TERMUX_VERSION", "test")
    monkeypatch.setenv(servers.SYSTEMCTL_ENV, "[]")
    monkeypatch.setenv(servers.OLLAMA_ENV, "[]")
    monkeypatch.setenv(ollama_install.APT_ENV, json.dumps([sys.executable, str(Path(__file__).parents[2] / "server/tests/fake_ollama_apt.py")]))
    manager = servers.Manager(machine=lambda: settings.Machine(servers=True), port=1, llama_port=2)
    monkeypatch.setattr(manager, "ollama_version", lambda: "")
    return manager, prefix


def wait():
    for _ in range(500):
        job = ollama_install.progress()
        if job and job["state"] != "installing":
            return job
        time.sleep(.01)
    pytest.fail("installation did not finish")


def test_ollama_setup_installs_matching_packages_and_preserves_models(machine):
    manager, prefix = machine
    servers.save_config({"models_dir": "existing", "keep_alive": "30m", "extra_env": "OLLAMA_VULKAN=0\nOLLAMA_CONTEXT_LENGTH=65536\nOLLAMA_DEBUG=1"})
    options = manager.handle({"kind": "ollama", "op": "install_options"})["ollama_install"]
    assert len(options["variants"]) == 3 and options["source"] == "termux"
    for variant in ("vulkan-adreno", "vulkan", "cpu"):
        assert manager.handle({"kind": "ollama", "op": "install", "variant": variant})["ok"]
        assert wait()["state"] == "installed"
        config = servers.load_config()
        assert config["models_dir"] == "existing" and config["keep_alive"] == "30m"
        assert config["extra_env"] == "OLLAMA_DEBUG=1"
        if variant == "cpu":
            assert not config["vulkan"] and config["gpus"] == "-1" and config["llm_library"] == "cpu"
        else:
            assert config["vulkan"] and config["gpus"] == "" and config["kv_cache_type"] == "f16"
            assert not config["flash_attention"] and config["context_length"] == 8192
    calls = [json.loads(line) for line in (prefix / "apt-calls.jsonl").read_text().splitlines()]
    assert calls[1][-3:] == ["ollama", "vulkan-loader-generic", "mesa-vulkan-icd-freedreno"]
    assert calls[3][-2:] == ["ollama", "vulkan-loader-android"]
    assert not manager.handle({"kind": "ollama", "op": "install", "variant": "vulkan; touch /tmp/x"})["ok"]


@pytest.mark.parametrize("failure", ["FAKE_OLLAMA_APT_FAIL", "FAKE_OLLAMA_APT_MISSING"])
def test_ollama_setup_failure_preserves_settings(machine, monkeypatch, failure):
    manager, _ = machine
    servers.save_config({"models_dir": "existing", "context_length": 32768})
    monkeypatch.setenv(failure, "1")
    assert manager.handle({"kind": "ollama", "op": "install", "variant": "vulkan-adreno"})["ok"]
    job = wait()
    assert job["state"] == "failed" and job["error"]
    config = servers.load_config()
    assert config["models_dir"] == "existing" and config["context_length"] == 32768 and not config["vulkan"]


def test_ollama_setup_blocks_changes_during_install_and_when_off(machine, monkeypatch):
    manager, _ = machine
    monkeypatch.setenv("FAKE_OLLAMA_APT_DELAY", ".15")
    assert manager.handle({"kind": "ollama", "op": "install", "variant": "cpu"})["ok"]
    for op in ("start", "restart", "configure", "install"):
        answer = manager.handle({"kind": "ollama", "op": op, "variant": "cpu"})
        assert not answer["ok"] and "being installed" in answer["error"]
    assert wait()["state"] == "installed"
    manager.machine = settings.Machine
    assert not manager.handle({"kind": "ollama", "op": "install", "variant": "cpu"})["ok"]


def test_ollama_setup_refuses_running_external_server(machine, monkeypatch):
    manager, prefix = machine
    monkeypatch.setattr(manager, "ollama_version", lambda: "1.0")
    answer = manager.handle({"kind": "ollama", "op": "install", "variant": "cpu"})
    assert not answer["ok"] and "stop Ollama" in answer["error"]
    assert not (prefix / "apt-calls.jsonl").exists()
