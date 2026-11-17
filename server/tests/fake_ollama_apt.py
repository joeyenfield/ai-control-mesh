"""Termux apt fake shared by Go, Python and fleet installation tests. Never installs packages."""
from __future__ import annotations

import json
import os
from pathlib import Path
import sys
import time

prefix = Path(os.environ["PREFIX"])
with (prefix / "apt-calls.jsonl").open("a") as calls:
    calls.write(json.dumps(sys.argv[1:]) + "\n")
time.sleep(float(os.environ.get("FAKE_OLLAMA_APT_DELAY", "0")))
if os.environ.get("FAKE_OLLAMA_APT_FAIL"):
    print("package repository unavailable", flush=True)
    sys.exit(42)
if sys.argv[1] == "install":
    paths = ["bin/ollama"]
    if "vulkan-loader-android" in sys.argv or "vulkan-loader-generic" in sys.argv:
        paths += ["lib/ollama/vulkan/libggml-vulkan.so", "lib/libvulkan.so"]
    if not os.environ.get("FAKE_OLLAMA_APT_MISSING"):
        for name in paths:
            path = prefix / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.touch()
