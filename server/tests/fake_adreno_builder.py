"""Offline source-build fixture shared by Go, Python and fleet contracts."""
from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import sys

parser = argparse.ArgumentParser()
parser.add_argument("recipe", type=Path)
for name in ("source", "build", "prefix", "patch"):
    parser.add_argument("--" + name, type=Path, required=True)
args = parser.parse_args()
assert hashlib.sha256(args.patch.read_bytes()).hexdigest() == "dc21507d755969fbfad95b6493a78ffdb30892a52a5d26cb1f10dc2a7af4a483"
assert (args.source / "CMakeLists.txt").is_file()
with (args.prefix / "adreno-builds.jsonl").open("a") as calls:
    calls.write(json.dumps(sys.argv[1:]) + "\n")
if os.environ.get("FAKE_ADRENO_BUILD_FAIL"):
    sys.exit("fixture compiler failed")
binary = args.build / "bin/llama-server"
binary.parent.mkdir(parents=True)
binary.write_text("#!" + sys.executable + "\nimport os, sys\n"
                  "if '--list-devices' in sys.argv:\n"
                  "    print('CPU' if os.environ.get('FAKE_ADRENO_NO_GPU') else 'GPUOpenCL: QUALCOMM Adreno(TM) 750')\n"
                  "else:\n    print('version: 11340 (e358d5917)')\n")
binary.chmod(0o700)
