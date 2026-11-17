"""Fixed source-build recipe for Qualcomm OpenCL. Embedded verbatim by the Go machine.

The caller verifies the source archive and supplies the pinned vendor-loader patch.
No model, listener, or arbitrary compiler arguments come from the server request.
"""
from __future__ import annotations

import argparse
import hashlib
import os
from pathlib import Path
import subprocess

PATCH_SHA = "dc21507d755969fbfad95b6493a78ffdb30892a52a5d26cb1f10dc2a7af4a483"


def commands(source: Path, build: Path, prefix: Path, patch: Path):
    return [
        [str(prefix / "bin/patch"), "--batch", "--forward", "-p1", "-d", str(source), "-i", str(patch)],
        [str(prefix / "bin/cmake"), "-S", str(source), "-B", str(build), "-G", "Ninja",
         "-DCMAKE_BUILD_TYPE=Release", "-DCMAKE_C_COMPILER=" + str(prefix / "bin/clang"),
         "-DCMAKE_CXX_COMPILER=" + str(prefix / "bin/clang++"), "-DBUILD_SHARED_LIBS=OFF",
         "-DCMAKE_C_FLAGS=-DSUBPROCESS_SPAWN_VIA_FORK=1", "-DCMAKE_CXX_FLAGS=-DSUBPROCESS_SPAWN_VIA_FORK=1",
         "-DGGML_OPENMP=OFF", "-DGGML_OPENCL=ON", "-DGGML_OPENCL_USE_SPHAL=ON",
         "-DGGML_OPENCL_TARGET_VERSION=300", "-DGGML_OPENCL_USE_ADRENO_KERNELS=OFF",
         "-DGGML_OPENCL_USE_ADRENO_BIN_KERNELS=OFF", "-DGGML_OPENCL_EMBED_KERNELS=ON",
         "-DGGML_BACKEND_DL=OFF", "-DOpenCL_INCLUDE_DIRS=" + str(prefix / "include"),
         "-DLLAMA_OPENSSL=ON", "-DOPENSSL_ROOT_DIR=" + str(prefix), "-DCMAKE_PREFIX_PATH=" + str(prefix),
         "-DLLAMA_BUILD_UI=OFF", "-DLLAMA_USE_PREBUILT_UI=OFF",
         "-DLLAMA_BUILD_APP=OFF", "-DLLAMA_BUILD_SERVER=ON", "-DLLAMA_BUILD_TESTS=OFF",
         "-DLLAMA_SUBPROCESS=ON", "-DLLAMA_BUILD_NUMBER=11340", "-DLLAMA_BUILD_COMMIT=e358d5917"],
        [str(prefix / "bin/cmake"), "--build", str(build), "--target", "llama-server", "-j", "2"],
    ]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ("source", "build", "prefix", "patch"):
        parser.add_argument("--" + name, type=Path, required=True)
    args = parser.parse_args()
    if hashlib.sha256(args.patch.read_bytes()).hexdigest() != PATCH_SHA:
        raise RuntimeError("vendor loader patch checksum mismatch")
    env = dict(os.environ)
    for name in ("CFLAGS", "CXXFLAGS", "CPPFLAGS", "LDFLAGS"):
        env.pop(name, None)
    for command in commands(args.source, args.build, args.prefix, args.patch):
        print("# " + " ".join(command), flush=True)
        subprocess.run(command, check=True, env=env)
    strip = args.prefix / "bin/llvm-strip"
    if strip.is_file():
        subprocess.run([str(strip), str(args.build / "bin/llama-server")], check=True)


if __name__ == "__main__":
    main()
