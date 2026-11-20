"""Recreate docs/screenshots in a disposable container, from a checkout.

The container has the browser Playwright pins, Node and Python, and its own network namespace, so
the demo's ports (8199, 18200 and the fake servers) never meet anything on the host. It builds the
console from the checkout, installs the server, runs ``demo.py --capture`` and copies the PNGs back
into the checkout's docs/screenshots. Jenkins (``Jenkinsfile.screenshots``) runs it; it works the same
by hand on any computer with Podman:

    python3 pipelines/screenshots/run.py .                   # every screenshot
    python3 pipelines/screenshots/run.py . --names terminals # only some (capture.mjs names, comma separated)
"""

from __future__ import annotations

import argparse
import shutil
import subprocess
import sys
import tarfile
import tempfile
from dataclasses import dataclass
from pathlib import Path

# Pinned together: the image carries the browsers that exact playwright-core release drives.
PLAYWRIGHT = "1.59.1"
IMAGE = f"mcr.microsoft.com/playwright:v{PLAYWRIGHT}-noble"
# Left out of the copy given to the container: builds, installs and local state it remakes or must not see.
SKIP = {".git", "node_modules", "dist", "logs", "certs", "bin", "__pycache__", ".venv", "venv", "output"}
SKIP_PREFIXES = (".screenshot-demo", ".test-", ".tmp-")

# What runs inside the container, in /work (the copied checkout); /out is where the PNGs go.
INSIDE = """
set -eu
# The image's Python has no ensurepip.
apt-get update -qq >/dev/null
DEBIAN_FRONTEND=noninteractive apt-get install -y -qq python3-venv >/dev/null
python3 -m venv /venv
/venv/bin/pip install -q --disable-pip-version-check -e /work/server
cd /work/ui
npm ci --no-audit --no-fund --loglevel=error
npm run build
cd /work
npm install --no-save --no-audit --no-fund --loglevel=error playwright-core@{playwright}
/venv/bin/python devhelpers/scripts/screenshots/demo.py --capture "$NAMES"
cp docs/screenshots/*.png /out/
"""


@dataclass
class Run:
    checkout: Path
    names: str
    image: str


def copy_checkout(checkout: Path, archive: Path) -> None:
    """The working tree as a tar, so uncommitted changes are captured too and the checkout stays untouched."""

    def keep(info: tarfile.TarInfo) -> tarfile.TarInfo | None:
        name = Path(info.name).name
        if name in SKIP or name.startswith(SKIP_PREFIXES):
            return None
        return info

    with tarfile.open(archive, "w") as tar:
        tar.add(checkout, arcname="work", filter=keep)


def capture(run: Run) -> int:
    shots = run.checkout / "docs" / "screenshots"
    if not shots.is_dir():
        print(f"{shots} doesn't exist: pass the repository's root", file=sys.stderr)
        return 2
    if not shutil.which("podman"):
        print("Podman is needed to run the screenshots' container", file=sys.stderr)
        return 2
    with tempfile.TemporaryDirectory(prefix="mesh-screenshots-") as temp:
        root = Path(temp)
        archive, out = root / "checkout.tar", root / "out"
        out.mkdir()
        copy_checkout(run.checkout, archive)
        script = INSIDE.format(playwright=PLAYWRIGHT)
        command = [
            "podman", "run", "--rm", "--http-proxy=false",
            "-e", f"NAMES={run.names}",
            "-v", f"{archive}:/checkout.tar:ro,Z",
            "-v", f"{out}:/out:Z",
            run.image, "sh", "-c", "tar -xf /checkout.tar -C / && " + script,
        ]
        result = subprocess.run(command)
        if result.returncode:
            return result.returncode
        pictures = sorted(out.glob("*.png"))
        if not pictures:
            print("The container took no screenshots", file=sys.stderr)
            return 1
        for picture in pictures:
            shutil.copy2(picture, shots / picture.name)
        print(f"Copied {len(pictures)} screenshots into {shots}")
    return 0


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("checkout", type=Path, help="the repository's root")
    parser.add_argument("--names", default="", help="only these screenshots (capture.mjs names, comma separated)")
    parser.add_argument("--image", default=IMAGE, help=f"the container image (default {IMAGE})")
    args = parser.parse_args()
    return capture(Run(args.checkout.resolve(), args.names.strip(), args.image))


if __name__ == "__main__":
    sys.exit(main())
