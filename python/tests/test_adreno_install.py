"""Adreno installer parity: verified build, GPU gate, profile and failure recovery."""
from __future__ import annotations

import hashlib
import json
import os
from pathlib import Path

import pytest

from agent_py.machine import adreno_assets, adreno_builder, adreno_install as adreno, llamacpp, servers
from test_llamacpp import archive
from test_providers import Upstream, send
from test_servers import fake


@pytest.fixture
def setup(tmp_path, monkeypatch):
    monkeypatch.setenv("AI_PROXY_MACHINE_HOME", str(tmp_path / "home"))
    monkeypatch.setenv("AI_PROXY_MACHINE_DATA", "")
    prefix = tmp_path / "prefix"
    prefix.mkdir()
    monkeypatch.setenv("PREFIX", str(prefix))
    monkeypatch.setenv("TERMUX_VERSION", "test")
    monkeypatch.setenv(llamacpp.LLAMA_ENV, "[]")
    monkeypatch.setenv(llamacpp.APT_ENV, fake("fake_ollama_apt.py"))
    monkeypatch.setenv(adreno.BUILDER_ENV, fake("fake_adreno_builder.py"))
    monkeypatch.setattr(llamacpp, "_job", None)
    data = archive({"llama-source/CMakeLists.txt": "fixture", "llama-source/LICENSE": "MIT fixture"})
    upstream = Upstream({})
    upstream.routes["GET /source"] = lambda handler: send(handler, 200, data, "application/octet-stream")
    try:
        url = f"http://127.0.0.1:{upstream.port}/source"
        monkeypatch.setenv(llamacpp.RELEASES_ENV, url)
        monkeypatch.setenv(adreno.SOURCE_ENV, json.dumps({"browser_download_url": url, "digest": "sha256:" + hashlib.sha256(data).hexdigest()}))
        yield prefix
    finally:
        upstream.close()


@pytest.mark.skipif(os.name == "nt", reason="Termux source installation")
def test_adreno_install_checks_gpu_applies_profile_and_reuses_verified_build(setup):
    old = llamacpp.TABLE.save({"ctx_size": 65536, "models_dir": "/existing-models", "jinja": False,
                               "extra_env": "LLAMA_ARG_CTX_SIZE=65536\nGGML_VK_VISIBLE_DEVICES=0"})
    assert adreno.options()[0]["id"] == "opencl-adreno" and adreno.options()[0]["recommended"]
    for _ in range(2):
        adreno.install()
        record = llamacpp.load_install()
        assert record["source"] == "source" and record["variant"] == "opencl-adreno"
        config = llamacpp.TABLE.load()
        assert config["ctx_size"] == 8192 and config["device"] == "GPUOpenCL"
        assert config["cache_type_v"] == "f16" and config["flash_attn"] == "off"
        assert config["models_dir"] == old["models_dir"] and config["jinja"] is False
        assert config["extra_env"] == "GGML_VK_VISIBLE_DEVICES=0"
        assert all((Path(record["dir"]) / name).is_file() for name in ("LICENSE.vendor-loader", "LICENSE.llama.cpp", "build.json"))
    assert len((setup / "adreno-builds.jsonl").read_text().splitlines()) == 1
    apt = (setup / "apt-calls.jsonl").read_text()
    assert len(apt.splitlines()) == 2 and "opencl-headers" in apt


@pytest.mark.skipif(os.name == "nt", reason="Termux source installation")
@pytest.mark.parametrize("failure", ["checksum", "compile", "gpu"])
def test_adreno_failures_preserve_selection_and_settings(setup, monkeypatch, failure):
    if failure == "checksum":
        asset = json.loads(os.environ[adreno.SOURCE_ENV])
        asset["digest"] = "sha256:" + "0" * 64
        monkeypatch.setenv(adreno.SOURCE_ENV, json.dumps(asset))
    else:
        monkeypatch.setenv("FAKE_ADRENO_BUILD_FAIL" if failure == "compile" else "FAKE_ADRENO_NO_GPU", "1")
    previous = {"source": "found", "binary": "/existing/llama-server", "variant": "cpu", "installed_at": ""}
    llamacpp.save_install(previous)
    old = llamacpp.TABLE.save({"ctx_size": 32768})
    with pytest.raises(servers.Refused):
        adreno.install()
    assert llamacpp.load_install() == previous and llamacpp.TABLE.load() == old


def test_adreno_embedded_assets_and_recipe_are_pinned():
    root = Path(__file__).resolve().parents[2] / "machine/internal/servers/adreno"
    assert adreno_assets.PATCH.encode() == (root / "loader.patch").read_bytes()
    assert adreno_assets.LICENSE.encode() == (root / "LICENSE").read_bytes()
    assert hashlib.sha256(adreno_assets.PATCH.encode()).hexdigest() == adreno.PATCH_SHA
    assert Path(adreno_builder.__file__).read_bytes().replace(b"\r\n", b"\n") == (root / "build.py").read_bytes()
    # Test the choices responsible for Android vendor access and safe decoding.
    command = adreno_builder.commands(Path("src"), Path("build"), Path("prefix"), Path("patch"))[1]
    assert "-DGGML_OPENCL_USE_SPHAL=ON" in command
    assert "-DGGML_OPENCL_USE_ADRENO_KERNELS=OFF" in command
    assert "-DLLAMA_OPENSSL=ON" in command
    assert "-DCMAKE_CXX_FLAGS=-DSUBPROCESS_SPAWN_VIA_FORK=1" in command
