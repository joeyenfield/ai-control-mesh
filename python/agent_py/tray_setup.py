"""Desktop dependencies installed by the tray's --setup command, never at normal startup."""

from __future__ import annotations

import os
import shutil
import subprocess
import sys
import time
from pathlib import Path
from typing import List

PACKAGES = ("python3-gi", "gir1.2-gtk-3.0", "gir1.2-ayatanaappindicator3-0.1")
INDICATORS = ("ubuntu-appindicators@ubuntu.com", "appindicatorsupport@rgcjonas.gmail.com")


def probe(command: List[str]) -> subprocess.CompletedProcess:
    """Queries must not consume the installer being piped through stdin."""
    return subprocess.run(command, stdin=subprocess.DEVNULL, capture_output=True, text=True, timeout=20)


def prepare() -> None:
    """Install Debian/Ubuntu libraries and enable GNOME's indicator extension as the desktop user."""
    if not sys.platform.startswith("linux") or not Path("/etc/debian_version").is_file():
        return
    if not shutil.which("apt-get"):
        return
    desktop = os.environ.get("XDG_CURRENT_DESKTOP", "").lower().split(":")
    gnome = any(name in ("gnome", "ubuntu") for name in desktop)
    packages = [*PACKAGES]
    if gnome:
        packages.extend(("gnome-shell-extension-appindicator", "libglib2.0-bin"))
    missing = []
    for package in packages:
        result = probe(["dpkg-query", "-W", "-f=${Status}", package])
        if result.returncode or result.stdout.strip() != "install ok installed":
            missing.append(package)
    if missing:
        prefix = []
        if os.geteuid() != 0:
            if not shutil.which("sudo"):
                raise RuntimeError("Installing tray dependencies needs sudo: apt-get install " + " ".join(missing))
            prefix = ["sudo"]
        print("Installing tray desktop dependencies: " + " ".join(missing), flush=True)
        # sudo uses /dev/tty itself when a password is needed. apt must not read
        # the remaining lines of a curl | sh installer from its standard input.
        for command in ([*prefix, "apt-get", "update"], [*prefix, "apt-get", "install", "-y", *missing]):
            result = subprocess.run(command, stdin=subprocess.DEVNULL)
            if result.returncode:
                raise RuntimeError("Couldn't install tray desktop dependencies; check the apt/sudo error above and rerun the installer.")
    if not gnome:
        return
    # A working watcher also covers a compatible extension installed by the user.
    if watcher_ready():
        return
    if not shutil.which("gnome-extensions"):
        raise RuntimeError("GNOME's tray extension tools are missing; sign out and back in, then run agent-tray --setup.")
    listed = probe(["gnome-extensions", "list"])
    extension = next((name for name in INDICATORS if name in listed.stdout.splitlines()), None)
    if extension:
        enabled = probe(["gnome-extensions", "enable", extension])
        if enabled.returncode:
            raise RuntimeError("Couldn't enable GNOME's tray extension: " + enabled.stderr.strip())
    if extension:
        # Enabling the extension can return before it acquires its D-Bus name.
        for _ in range(10):
            if watcher_ready():
                return
            time.sleep(0.2)
    raise RuntimeError(
        "Tray libraries are installed, but GNOME hasn't loaded its AppIndicator extension. "
        "Sign out and back in, then rerun the installer or agent-tray --setup."
    )


def watcher_ready() -> bool:
    """GNOME must expose an indicator host, not merely have its extension package on disk."""
    result = probe([
        "gdbus", "call", "--session", "--dest", "org.freedesktop.DBus",
        "--object-path", "/org/freedesktop/DBus", "--method",
        "org.freedesktop.DBus.NameHasOwner", "org.kde.StatusNotifierWatcher",
    ])
    return result.returncode == 0 and result.stdout.strip() == "(true,)"
