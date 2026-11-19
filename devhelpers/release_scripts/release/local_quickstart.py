"""Finish a one-command local install: sign agent in, and join this computer to the fleet.

The web installer runs it in the server's own environment, after agent-server local start:

    python local_quickstart.py signin <instance folder>
    python local_quickstart.py machine <agent-machine program>

signin approves the device code as admin with the first account's password, the way a browser would
(deploy/compose/quickstart/machine.py), so nothing waits for a browser. It saves the sign-in that
agent and agent-machine share, beside any other servers already in it. Exit codes: 0 signed in (or
already), 2 no first-user password to sign in with (changed and the file deleted: the installer
falls back to agent login), 1 any other failure.

machine turns on, for a machine that has no settings yet, what the console's tour needs: launches,
terminals, files and model servers, in ~/projects. A machine set up before keeps its choices
(agent-machine config changes them). Then it (re)starts agent-machine, so an update runs the new one.
"""

from __future__ import annotations

import json
import os
from pathlib import Path
import subprocess
import sys

FEATURES = ("launches", "commands", "files", "servers")


def config_dir() -> Path:
    """As agent and agent-machine find it (settings.configDir)."""
    if sys.platform == "win32":
        return Path(os.environ.get("APPDATA") or Path.home() / "AppData" / "Roaming")
    return Path(os.environ.get("XDG_CONFIG_HOME") or Path.home() / ".config")


def sign_in(home: Path) -> int:
    app = Path(__file__).resolve().parents[3]
    ports = json.loads((home / "local.json").read_text(encoding="utf-8"))
    os.environ["QUICKSTART_API"] = f"http://localhost:{ports.get('port') or 19999}"
    os.environ["QUICKSTART_ISSUER"] = f"http://localhost:{ports.get('auth_port') or 20000}"
    os.environ["QUICKSTART_USER"] = "admin"  # the first account agent-server local makes
    sys.path.insert(0, str(app / "deploy" / "compose" / "quickstart"))
    import machine  # type: ignore[import-not-found]

    password_file = home / "private" / "first-user-password"
    password = password_file.read_text(encoding="utf-8").strip() if password_file.is_file() else ""
    path = machine.credentials_path()
    if not password and not path.exists():
        return 2
    try:
        machine.sign_in(path, password)
    except Exception as error:  # noqa: BLE001 - any failure falls back to the browser sign-in
        if not password:
            return 2
        print(f"Couldn't sign in by itself: {error}", file=sys.stderr)
        return 1
    return 0


def join(program: str) -> int:
    home = Path(os.environ["AI_PROXY_MACHINE_HOME"]).expanduser() if os.environ.get("AI_PROXY_MACHINE_HOME") else config_dir() / "agent-machine"
    fresh = not (home / "machine.json").exists()
    quiet = {"stdout": subprocess.DEVNULL}
    try:
        if fresh:
            projects = Path.home() / "projects"
            projects.mkdir(exist_ok=True)
            for feature in FEATURES:
                subprocess.run([program, feature, "on"], check=True, **quiet)
            subprocess.run([program, "roots", "add", str(projects)], check=True, **quiet)
            print(f"This computer joins the fleet with {', '.join(FEATURES)} on, in {projects}", flush=True)
        else:
            print("This computer keeps its machine settings (agent-machine config changes them)", flush=True)
        subprocess.run([program, "stop"], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        subprocess.run([program, "start"], check=True)
    except (OSError, subprocess.CalledProcessError) as error:
        print(f"Couldn't start agent-machine: {error}", file=sys.stderr)
        return 1
    return 0


def main(arguments: list[str]) -> int:
    if len(arguments) == 2 and arguments[0] == "signin":
        return sign_in(Path(arguments[1]))
    if len(arguments) == 2 and arguments[0] == "machine":
        return join(arguments[1])
    print("usage: local_quickstart.py signin <instance folder> | machine <agent-machine program>", file=sys.stderr)
    return 1


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
