"""Restricted host probes must leave the rest of a server stats sample intact."""

from __future__ import annotations

from types import SimpleNamespace

import pytest

from agent_server import stats


@pytest.mark.parametrize("probe, missing", [
    ("cpu_percent", ("cpu", "percent")),
    ("virtual_memory", ("memory", "total")),
    ("swap_memory", ("swap", "total")),
    ("boot_time", ("boot_time",)),
    ("Process.memory_info", ("process", "rss")),
    ("Process.cpu_percent", ("process", "cpu_percent")),
    ("Process.num_threads", ("process", "threads")),
    ("Process.create_time", ("process", "started_at")),
    ("Process.net_connections", ("process", "connections")),
])
def test_one_denied_probe_leaves_other_stats_available(tmp_path, monkeypatch, probe, missing):
    sampler = stats.ServerStats(SimpleNamespace(log_dir=tmp_path, dist_dir=tmp_path))

    def denied(*args, **kwargs):
        raise PermissionError(13, "Permission denied", "/proc/stat")

    monkeypatch.setattr("agent_server.stats.psutil." + probe, denied)
    sample = sampler.sample()
    value = sample
    for key in missing:
        value = value[key]
    assert value is None
    assert sample["process"]["pid"] > 0 and sample["disks"]
    if missing[0] != "memory":
        assert sample["memory"]["total"] > 0
    if missing != ("process", "rss"):
        assert sample["process"]["rss"] > 0


def test_unreadable_folder_does_not_stall_size_measurement(tmp_path, monkeypatch):
    sampler = stats.ServerStats(SimpleNamespace(log_dir=tmp_path, dist_dir=tmp_path))

    def denied(path):
        raise PermissionError(13, "Permission denied", str(path))

    monkeypatch.setattr(stats.Path, "is_dir", denied)
    sampler._measuring = True
    sampler._measure()
    assert not sampler._measuring and sampler._measured_at is not None
    assert sampler._sizes[str(tmp_path)] is None
