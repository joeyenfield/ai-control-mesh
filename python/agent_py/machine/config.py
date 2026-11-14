"""Interactive machine permissions, mirroring machine/internal/command/config.go."""

from __future__ import annotations

from typing import List, TextIO

from . import settings

DESCRIPTIONS = (
    "Launch coding agents here as your user, in allowed project folders.",
    "Open remote terminals as your user. The starting folder is restricted; shell commands can access anything your user can.",
    "Browse and read files in allowed project folders, including hidden files. File contents are sent to the server.",
    "Let the machine owner or an admin replace agent-machine and agent with the server's verified build.",
    "Manage local model servers: install llama.cpp, start and stop servers, change settings, download and delete models.",
    "Run supported model-server containers with Docker or Podman. Docker access can grant root privileges on this computer.",
)


def configure_features(args: List[str], source: TextIO, out: TextIO) -> int:
    if args in (["--help"], ["-h"]):
        print("Usage: agent-machine config\nChoose multiple machine functions by number or name. Enter keeps current settings; none switches every function off.", file=out)
        return 0
    if args:
        from . import fail
        return fail("config takes no arguments (use --help)")
    machine = settings.Machine.load()
    print("Choose what this machine allows through AI Control Mesh.", file=out)
    print("These permissions use programs installed here; selecting them does not install model servers or a container runtime.", file=out)
    for number, (name, description) in enumerate(zip(settings.TOGGLES, DESCRIPTIONS), 1):
        state = "on" if getattr(machine, name) else "off"
        print(f"  {number}. {name} [{state}]\n     {description}", file=out)
    print("Launches, file viewing and terminal starting folders use roots set with: agent-machine roots add <folder>", file=out)
    print("Server permissions and sharing rules still apply. Changes reach the server within a minute.", file=out)
    print("A selection enables only the listed functions and switches all others off.", file=out)
    while True:
        print("Enable functions (comma-separated numbers or names, e.g. 1,3; none = all off; Enter = keep current): ", file=out, end="", flush=True)
        line = source.readline()
        if not line or not line.endswith("\n"):
            print("\nConfiguration cancelled; permissions unchanged. Run agent-machine config from a terminal to choose functions.", file=out)
            return 1
        line = line.strip().lower()
        if not line:
            print("Keeping current machine permissions.", file=out)
            return 0
        names = []
        if line != "none":
            parts = line.split(",")
            for i, part in enumerate(parts):
                try:
                    number = int(part.strip())
                except ValueError:
                    continue
                if 1 <= number <= len(settings.TOGGLES):
                    parts[i] = settings.TOGGLES[number - 1]
            try:
                names = settings.parse_toggles(",".join(parts))
            except ValueError:
                names = []
            if not names:
                print("Choose listed numbers or names, or none. No permissions have changed.", file=out)
                continue
        machine = settings.Machine.load()
        for name in settings.TOGGLES:
            machine.set_toggle(name, name in names)
        try:
            machine.save()
        except OSError as error:
            print(f"Couldn't save machine permissions: {error}", file=out)
            return 1
        print(f"Enabled: {', '.join(names) or 'none'}. All other machine functions are off.", file=out)
        for name in names:
            for note in machine.toggle_notes(name):
                print(note, file=out)
        print("Change these choices later with: agent-machine config", file=out)
        return 0
