"""Finish a local Termux install using its own admin account and OIDC issuer."""

from __future__ import annotations

import json
import os
from pathlib import Path
import subprocess
import sys


def main() -> int:
    app = Path(__file__).resolve().parents[3]
    sys.path.insert(0, str(app / "deploy" / "compose" / "quickstart"))
    home = Path(os.environ.get("AI_PROXY_LOCAL_HOME") or Path.home() / ".local" / "share" / "ai-control-mesh")
    ports = json.loads((home / "local.json").read_text(encoding="utf-8"))
    os.environ["QUICKSTART_API"] = f"http://localhost:{ports.get('port') or 19999}"
    os.environ["QUICKSTART_ISSUER"] = f"http://localhost:{ports.get('auth_port') or 20000}"
    import machine  # type: ignore[import-not-found]

    password_file = home / "private" / "first-user-password"
    password = password_file.read_text(encoding="utf-8").strip() if password_file.is_file() else ""
    machine.sign_in(machine.credentials_path(), password)

    configuration = Path(os.environ.get("XDG_CONFIG_HOME") or Path.home() / ".config") / "agent-machine" / "machine.json"
    fresh = not configuration.exists()
    projects = Path.home() / "projects"
    projects.mkdir(exist_ok=True)
    if fresh:
        for feature in ("launches", "commands", "files", "updates", "servers"):
            subprocess.run(["agent-machine", feature, "on"], check=True, stdout=subprocess.DEVNULL)
    # PRoot cannot run Docker or Podman, even if an earlier install enabled containers.
    subprocess.run(["agent-machine", "containers", "off"], check=True, stdout=subprocess.DEVNULL)
    subprocess.run(["agent-machine", "roots", "add", str(projects)], check=True, stdout=subprocess.DEVNULL)
    subprocess.run(["agent-machine", "start"], check=True, stdout=subprocess.DEVNULL)
    print(f"Ready: {machine.API}; projects: {projects}", flush=True)
    return 0


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError, KeyError, subprocess.CalledProcessError) as error:
        print(f"Termux quickstart failed: {error}", file=sys.stderr)
        sys.exit(1)
