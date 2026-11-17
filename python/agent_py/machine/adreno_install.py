"""Pinned Qualcomm OpenCL source installation, mirroring Go's adreno_install.go."""
from __future__ import annotations

import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import uuid

from . import adreno_assets, adreno_builder, llamacpp, servers
from .servers import Refused

SOURCE_ENV = "AI_PROXY_LLAMACPP_ADRENO_SOURCE"
BUILDER_ENV = "AI_PROXY_LLAMACPP_ADRENO_BUILDER"
COMMIT = "e358d59178377be4c58ba567925e05faadbccb57"
SOURCE_SHA = "bb1c3b0467180a700ae2b27f067b126e22700c860afbfb7900cfaf9a4738b3a2"
PATCH_SHA = "dc21507d755969fbfad95b6493a78ffdb30892a52a5d26cb1f10dc2a7af4a483"
VARIANT = {"id": "opencl-adreno", "label": "OpenCL (Adreno, source build)",
           "help": "Qualcomm vendor-driver access, tested on Samsung S24 Ultra. Builds a pinned version on this phone; the first install can take several minutes. Applies an 8K context and F16 cache. Test a model after restarting."}


def available() -> bool:
    if os.environ.get(SOURCE_ENV) and os.environ.get(BUILDER_ENV):
        return True
    return llamacpp.go_arch() == "arm64" and all(Path(p).exists() for p in ("/vendor/lib64/libOpenCL.so", "/vendor/lib64/libOpenCL_adreno.so"))


def options():
    variants = [{k: v for k, v in item.items() if k != "packages"} for item in llamacpp.TERMUX_VARIANTS]
    for variant in variants:
        variant["recommended"] = variant["id"] == "cpu"
    if available():
        for variant in variants:
            variant["recommended"] = False
        variants.insert(0, {**VARIANT, "recommended": True})
    return variants


def sha(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def setup_config(config):
    fields = {"ctx_size": 8192, "cache_type_k": "f16", "cache_type_v": "f16", "flash_attn": "off",
              "cache_ram": 0, "parallel": 1, "models_max": 1, "threads": 4, "gpu_layers": "all", "device": "GPUOpenCL"}
    config.update(fields)
    managed = {"LLAMA_ARG_" + name for name in ("CTX_SIZE", "CACHE_TYPE_K", "CACHE_TYPE_V", "FLASH_ATTN", "CACHE_RAM", "N_PARALLEL", "MODELS_MAX", "THREADS", "N_GPU_LAYERS", "DEVICE")}
    config["extra_env"] = "\n".join(line for line in config.get("extra_env", "").split("\n") if line.strip().split("=", 1)[0].strip() not in managed)
    return config


def install() -> None:
    if not available():
        raise Refused("this build requires ARM64 Termux and Qualcomm's vendor OpenCL driver")
    patch_bytes = adreno_assets.PATCH.encode()
    if sha(patch_bytes) != PATCH_SHA:
        raise Refused("embedded Adreno patch checksum mismatch")
    recipe = Path(adreno_builder.__file__).read_bytes().replace(b"\r\n", b"\n")
    root = llamacpp.install_root()
    root.mkdir(parents=True, exist_ok=True, mode=0o700)
    final = root / ("adreno-" + COMMIT[:12] + "-" + sha(recipe)[:12])
    asset = {"name": "adreno-source.tar.gz", "browser_download_url": "https://codeload.github.com/ggml-org/llama.cpp/tar.gz/" + COMMIT, "digest": "sha256:" + SOURCE_SHA}
    if os.environ.get(SOURCE_ENV):
        try:
            asset.update(json.loads(os.environ[SOURCE_ENV]))
            asset["name"] = "adreno-source.tar.gz"
        except (TypeError, ValueError) as error:
            raise Refused(f"invalid Adreno source override: {error}") from error
    manifest = {"Source": asset["digest"].removeprefix("sha256:"), "Patch": PATCH_SHA, "Recipe": sha(recipe)}
    try:
        cached = json.loads((final / "build.json").read_text())
        valid = all(cached.get(k) == v for k, v in manifest.items()) and cached.get("Binary") == sha((final / "native/llama-server").read_bytes()) and cached.get("Launcher") == sha((final / "llama-server").read_bytes())
    except (OSError, ValueError):
        valid = False
    if not valid:
        if final.exists():
            final = final.with_name(final.name + "-" + uuid.uuid4().hex[:12])
        with tempfile.TemporaryDirectory(prefix=".adreno-build-", dir=root) as directory:
            work = Path(directory)
            llamacpp._set(step="installing source-build tools", release=COMMIT[:12])
            prefix = Path(llamacpp.termux_prefix())
            apt = servers.command(llamacpp.APT_ENV, lambda: str(prefix / "bin/apt-get"))
            if not apt:
                raise Refused("Termux's apt-get isn't here")
            for args in (["update"], ["install", "-y", "--no-install-recommends", "clang", "cmake", "ninja", "python", "patch", "opencl-headers", "openssl", "pkg-config"]):
                error = llamacpp.run_logged(20 * 60, apt, args)
                if error:
                    raise Refused(f"installing Adreno build tools: {error} (see {llamacpp.install_log()})")
            llamacpp._set(step="downloading verified llama.cpp source")
            archive = llamacpp.download(asset, lambda n: llamacpp._set(done=n))
            try:
                llamacpp.unpack(archive, asset["name"], work / "source")
            finally:
                Path(archive).unlink(missing_ok=True)
            roots = list((work / "source").glob("*/CMakeLists.txt"))
            if len(roots) != 1:
                raise Refused("source archive does not contain one llama.cpp source tree")
            patch = work / "loader.patch"
            patch.write_bytes(patch_bytes)
            script = work / "build.py"
            script.write_bytes(recipe)
            builder = servers.command(BUILDER_ENV, lambda: str(prefix / "bin/python"))
            if not builder:
                raise Refused("Python is required to build the Adreno backend")
            llamacpp._set(step="compiling Qualcomm OpenCL (first install takes several minutes)")
            error = llamacpp.run_logged(60 * 60, builder, [str(script), "--source", str(roots[0].parent), "--build", str(work / "build"), "--prefix", str(prefix), "--patch", str(patch)])
            if error:
                tail = llamacpp.install_log().read_bytes()[-700:].decode(errors="replace")
                raise Refused(f"Adreno build failed: {error} (see {llamacpp.install_log()}): {tail}")
            installed = work / "installed"
            native = installed / "native/llama-server"
            native.parent.mkdir(parents=True, mode=0o700)
            installed.chmod(0o700)
            shutil.copyfile(work / "build/bin/llama-server", native)
            native.chmod(0o700)
            manifest["Binary"] = sha(native.read_bytes())
            import shlex
            binary = shlex.quote(str(final / "native/llama-server"))
            wrapper = ("#!" + str(prefix / "bin/sh") + "\nexport TERMUX_EXEC__PROC_SELF_EXE=" + binary + "\nexec " + binary + " --no-warmup \"$@\"\n").encode()
            (installed / "llama-server").write_bytes(wrapper)
            (installed / "llama-server").chmod(0o700)
            manifest["Launcher"] = sha(wrapper)
            (installed / "build.json").write_text(json.dumps(manifest, indent=2))
            (installed / "LICENSE.vendor-loader").write_text(adreno_assets.LICENSE)
            if (roots[0].parent / "LICENSE").exists():
                shutil.copyfile(roots[0].parent / "LICENSE", installed / "LICENSE.llama.cpp")
            for name in ("build.json", "LICENSE.vendor-loader", "LICENSE.llama.cpp"):
                path = installed / name
                if path.exists():
                    path.chmod(0o600)
            os.replace(installed, final)
    llamacpp._set(step="checking Qualcomm GPU access", release=COMMIT[:12])
    try:
        probe = subprocess.run([str(final / "native/llama-server"), "--list-devices"], capture_output=True, text=True, timeout=45,
                               env=llamacpp.environment(dict(os.environ), llamacpp.TABLE.defaults()), **servers.QUIET)
    except (OSError, subprocess.TimeoutExpired) as error:
        raise Refused(f"built backend cannot access the Adreno GPU; previous selection is unchanged: {error}") from error
    output = probe.stdout + probe.stderr
    if probe.returncode or "GPUOpenCL" not in output or "adreno" not in output.lower():
        raise Refused(f"built backend cannot access the Adreno GPU; previous selection is unchanged: {output.strip()}")
    previous = llamacpp.load_install()
    old = llamacpp.TABLE.load()
    llamacpp.TABLE.save(setup_config(llamacpp.TABLE.load()))
    try:
        llamacpp.save_install({"source": "source", "release": COMMIT[:12], "variant": "opencl-adreno", "binary": str(final / "llama-server"), "dir": str(final), "installed_at": llamacpp.now()})
    except OSError:
        llamacpp.TABLE.save(old)
        raise
    llamacpp.prune(str(final), previous.get("dir") or "")
