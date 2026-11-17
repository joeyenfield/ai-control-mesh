"""Ollama's Termux package installer and explicit CPU/GPU setup, mirroring Go."""
from __future__ import annotations

import os
import subprocess
import threading
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Dict

from . import settings, servers, llamacpp
from .servers import Refused

APT_ENV = "AI_PROXY_OLLAMA_APT"
VARIANTS = [
    {"id": "cpu", "label": "CPU only", "help": "Most compatible. Disables GPU backends.", "recommended": True,
     "packages": ["ollama"]},
    {"id": "vulkan", "label": "Vulkan (Android driver)", "recommended": False,
     "help": "Experimental phone GPU support using Android's driver. Replaces an existing generic Vulkan loader.",
     "packages": ["ollama", "vulkan-loader-android"]},
    {"id": "vulkan-adreno", "label": "Vulkan (Adreno / Turnip)", "recommended": False,
     "help": "Experimental Snapdragon GPU support using Mesa Turnip. Replaces Android's Vulkan loader. Some devices or models produce incorrect replies; test before relying on it.",
     "packages": ["ollama", "vulkan-loader-generic", "mesa-vulkan-icd-freedreno"]},
]
_lock = threading.Lock()
_job = None


def now() -> str:
    return datetime.now(timezone.utc).isoformat().replace("+00:00", "Z")


def install_options() -> Dict[str, Any]:
    if not llamacpp.termux_prefix():
        return {"variants": [], "error": "Automatic Ollama installation is available in Termux. On this platform install Ollama from https://ollama.com/download, then configure it here."}
    return {"source": "termux", "variants": [{k: v for k, v in item.items() if k != "packages"} for item in VARIANTS]}


def progress():
    with _lock:
        return dict(_job) if _job else None


def installing() -> bool:
    return (progress() or {}).get("state") == "installing"


def log_path() -> Path:
    return settings.home() / "ollama-install.log"


def start(manager, variant: str) -> None:
    global _job
    if not llamacpp.termux_prefix():
        raise Refused("automatic Ollama installation requires Termux")
    chosen = next((item for item in VARIANTS if item["id"] == variant), None)
    if chosen is None:
        raise Refused("pick an Ollama install option: cpu, vulkan or vulkan-adreno")
    if manager.ollama_version() or servers.managed_pid():
        raise Refused("stop Ollama before installing or changing its GPU setup")
    with _lock:
        if _job and _job["state"] == "installing":
            raise Refused("Ollama is already being installed here")
        _job = {"state": "installing", "variant": variant, "step": "starting", "at": now(), "done": 0, "total": 0}

    def run():
        try:
            install_termux(manager, chosen)
            update = {"state": "installed", "step": "Configured. Start Ollama to use these settings; GPU inference has not been verified."}
            if variant == "cpu":
                update["step"] = "Configured for CPU. Start Ollama to use these settings."
        except Exception as error:
            update = {"state": "failed", "error": str(error)}
        with _lock:
            _job.update(**update, at=now())

    threading.Thread(target=run, name="ollama-install", daemon=True).start()


def install_termux(manager, chosen: Dict[str, Any]) -> None:
    prefix = Path(llamacpp.termux_prefix())
    apt = servers.command(APT_ENV, lambda: str(prefix / "bin" / "apt-get"))
    if not apt:
        raise Refused("Termux's apt-get isn't here")
    settings.home().mkdir(parents=True, exist_ok=True, mode=0o700)
    fd = os.open(str(log_path()), os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(fd, "ab") as log:
        for args in (["update"], ["install", "-y", "--no-install-recommends", "-o", "Dpkg::Options::=--force-confnew", *chosen["packages"]]):
            with _lock:
                _job["step"] = "apt-get " + args[0]
            log.write(("# apt-get " + " ".join(args) + "\n").encode())
            log.flush()
            try:
                result = subprocess.run([*apt, *args], stdin=subprocess.DEVNULL, stdout=log, stderr=log,
                                        timeout=20 * 60, env={**os.environ, "DEBIAN_FRONTEND": "noninteractive"}, **servers.QUIET)
                if result.returncode:
                    raise Refused(f"exit status {result.returncode}")
            except (OSError, subprocess.TimeoutExpired, Refused) as error:
                tail = log_path().read_bytes()[-1200:].decode(errors="replace")
                raise Refused(f"apt-get {args[0]}: {error} (see {log_path()}): {tail}") from error
    paths = [prefix / "bin" / "ollama"]
    if chosen["id"] != "cpu":
        paths += [prefix / "lib" / "ollama" / "vulkan" / "libggml-vulkan.so", prefix / "lib" / "libvulkan.so"]
    for path in paths:
        if not path.exists():
            raise Refused(f"packages installed, but required file {path} is missing; settings were not changed")
    with manager.lock:
        if not manager.machine().servers:
            raise Refused("server management was switched off; settings were not changed")
        if manager.ollama_version() or servers.managed_pid():
            raise Refused("Ollama was started during installation; stop it and run setup again to apply the hardware settings")
        servers.save_config(setup_config(servers.load_config(), chosen["id"]))


def setup_config(config: Dict[str, Any], variant: str) -> Dict[str, Any]:
    config.update(vulkan=variant != "cpu", gpus="", llm_library="")
    profile = {"OLLAMA_VULKAN", "OLLAMA_LLM_LIBRARY"}
    if variant == "cpu":
        config.update(gpus="-1", llm_library="cpu")
    else:
        config.update(kv_cache_type="f16", flash_attention=False, context_length=8192, num_parallel=1, max_loaded_models=1, new_engine=False)
        profile.update({"OLLAMA_KV_CACHE_TYPE", "OLLAMA_FLASH_ATTENTION", "OLLAMA_CONTEXT_LENGTH", "OLLAMA_NUM_PARALLEL", "OLLAMA_MAX_LOADED_MODELS", "OLLAMA_NEW_ENGINE"})
    config["extra_env"] = "\n".join(line for line in config.get("extra_env", "").split("\n") if line.strip().split("=", 1)[0].strip() not in profile)
    return config
