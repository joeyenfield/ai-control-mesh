"""Tray dependency setup uses fake package and desktop commands, never the real desktop."""

from __future__ import annotations

import subprocess

import pytest

from agent_py import tray, tray_setup


@pytest.fixture
def desktop(monkeypatch):
    calls = []
    monkeypatch.setattr(tray_setup.sys, "platform", "linux")
    monkeypatch.setattr(tray_setup.Path, "is_file", lambda path: True)
    monkeypatch.setattr(tray_setup.shutil, "which", lambda name: "/usr/bin/" + name)
    monkeypatch.setattr(tray_setup.os, "geteuid", lambda: 1000, raising=False)
    monkeypatch.setenv("XDG_CURRENT_DESKTOP", "XFCE")
    monkeypatch.setattr(tray_setup.time, "sleep", lambda seconds: None)

    def run(command, **kwargs):
        calls.append(command)
        assert kwargs["stdin"] is subprocess.DEVNULL
        return subprocess.CompletedProcess(command, 0, stdout="install ok installed", stderr="")

    monkeypatch.setattr(tray_setup.subprocess, "run", run)
    return calls


def test_installed_dependencies_need_no_sudo_or_apt(desktop):
    tray_setup.prepare()
    assert len(desktop) == len(tray_setup.PACKAGES)
    assert all(command[0] == "dpkg-query" for command in desktop)


@pytest.mark.parametrize("root", [False, True])
def test_missing_dependencies_are_installed_once(desktop, monkeypatch, root):
    monkeypatch.setattr(tray_setup.os, "geteuid", lambda: 0 if root else 1000)
    monkeypatch.setattr(tray_setup, "probe", lambda command: subprocess.CompletedProcess(command, 1, stdout="", stderr=""))
    tray_setup.prepare()
    prefix = [] if root else ["sudo"]
    assert desktop == [[*prefix, "apt-get", "update"],
                       [*prefix, "apt-get", "install", "-y", *tray_setup.PACKAGES]]


def test_failed_package_install_is_reported(desktop, monkeypatch):
    monkeypatch.setattr(tray_setup, "probe", lambda command: subprocess.CompletedProcess(command, 1, stdout="", stderr=""))
    monkeypatch.setattr(tray_setup.subprocess, "run", lambda command, **kwargs: subprocess.CompletedProcess(command, 100))
    with pytest.raises(RuntimeError, match="apt/sudo error"):
        tray_setup.prepare()


def test_missing_sudo_does_not_attempt_an_install(desktop, monkeypatch):
    monkeypatch.setattr(tray_setup.shutil, "which", lambda name: None if name == "sudo" else "/usr/bin/" + name)
    monkeypatch.setattr(tray_setup, "probe", lambda command: subprocess.CompletedProcess(command, 1, stdout="", stderr=""))
    with pytest.raises(RuntimeError, match="needs sudo"):
        tray_setup.prepare()
    assert not desktop


def test_gnome_enables_the_installed_extension_and_verifies_the_watcher(desktop, monkeypatch):
    monkeypatch.setenv("XDG_CURRENT_DESKTOP", "ubuntu:GNOME")
    ready = iter([False, False, True])
    monkeypatch.setattr(tray_setup, "watcher_ready", lambda: next(ready))
    run = tray_setup.subprocess.run

    def commands(command, **kwargs):
        if command[:2] == ["gnome-extensions", "list"]:
            desktop.append(command)
            return subprocess.CompletedProcess(command, 0, stdout=tray_setup.INDICATORS[0], stderr="")
        return run(command, **kwargs)

    monkeypatch.setattr(tray_setup.subprocess, "run", commands)
    tray_setup.prepare()
    assert ["gnome-extensions", "enable", tray_setup.INDICATORS[0]] in desktop
    assert any(command[-1] == "gnome-shell-extension-appindicator" for command in desktop)


def test_new_gnome_extension_reports_when_a_new_session_is_needed(desktop, monkeypatch):
    monkeypatch.setenv("XDG_CURRENT_DESKTOP", "GNOME")
    monkeypatch.setattr(tray_setup, "watcher_ready", lambda: False)
    with pytest.raises(RuntimeError, match="Sign out and back in"):
        tray_setup.prepare()


def test_working_gnome_host_is_preserved(desktop, monkeypatch):
    monkeypatch.setenv("XDG_CURRENT_DESKTOP", "GNOME")
    monkeypatch.setattr(tray_setup, "watcher_ready", lambda: True)
    tray_setup.prepare()
    assert all(command[0] == "dpkg-query" for command in desktop)


def test_non_linux_setup_never_runs_package_commands(desktop, monkeypatch):
    monkeypatch.setattr(tray_setup.sys, "platform", "win32")
    tray_setup.prepare()
    assert not desktop


def test_setup_checks_desktop_before_installing_and_reports_failure(desktop, monkeypatch, capsys):
    monkeypatch.setattr(tray, "desktop_available", lambda: False)
    assert tray.main(["--setup"]) == 1
    assert not desktop
    monkeypatch.setattr(tray, "desktop_available", lambda: True)

    def failed_setup():
        raise RuntimeError("missing desktop dependency")

    monkeypatch.setattr(tray_setup, "prepare", failed_setup)
    assert tray.main(["--setup"]) == 1
    assert "missing desktop dependency" in capsys.readouterr().err
