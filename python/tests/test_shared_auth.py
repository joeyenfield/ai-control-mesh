"""The shared auth profile, migration and server selection, mirrored from Go's settings tests."""

from __future__ import annotations

import errno
import time

import httpx
import pytest
from starlette.requests import Request

from agent_py import common, login, oauth
from agent_py.client import settings as client
from agent_py.client.localproxy import AgentProxy
from agent_py.machine import settings as machine


def test_both_programs_use_the_same_auth_profile(tmp_path, monkeypatch):
    expected = tmp_path / "config" / "agent-proxy" / "credentials.json"
    assert client.credentials_path() == machine.credentials_path() == expected
    monkeypatch.setenv("AI_PROXY_MACHINE_HOME", str(tmp_path / "machine"))
    assert client.credentials_path() == machine.credentials_path() == expected
    monkeypatch.setenv("AI_PROXY_CREDENTIALS", str(tmp_path / "explicit.json"))
    assert client.credentials_path() == machine.credentials_path() == tmp_path / "explicit.json"


@pytest.mark.parametrize("cross_volume", [False, True])
def test_machine_only_login_moves_under_the_shared_lock(tmp_path, monkeypatch, cross_volume):
    server = "https://mesh.example"
    entry = common.ServerEntry(server, user="alice", access_token="access", refresh_token="refresh",
                               expires_at=int(time.time()) + 3600, issuer=server, client_id="mesh-cli")
    legacy = tmp_path / "config" / "agent-machine" / "credentials.json"
    if cross_volume:
        monkeypatch.setenv("AI_PROXY_MACHINE_HOME", str(tmp_path / "custom-machine-home"))
        legacy = tmp_path / "custom-machine-home" / "credentials.json"
    common.Credentials(legacy, {server: entry}, server).save()
    if cross_volume:
        original = common.replace_file

        def move(source, target):
            if source == legacy:
                raise OSError(errno.EXDEV, "Cross-device link")
            return original(source, target)

        monkeypatch.setattr(common, "replace_file", move)
    assert client.upstream().server() == machine.upstream().server() == server
    assert client.upstream().token(server) == "access"
    assert not legacy.exists()
    assert machine.upstream().credentials().default_entry().refresh_token == "refresh"
    common.Credentials(common.credentials_path()).save()
    common.Credentials(legacy, {server: entry}, server).save()
    assert machine.upstream().credentials().default_entry() is None


def test_shared_selection_wins_and_other_servers_cannot_use_credentials(monkeypatch):
    server, other = "https://mesh.example", "https://other.example"
    common.Credentials(common.credentials_path(), {server: common.ServerEntry(server),
                       other: common.ServerEntry(other, access_token="old-other-token")}, server).save()
    monkeypatch.setenv("AI_PROXY_URL", other)
    monkeypatch.setenv("AI_PROXY_TOKEN", "shared-token")
    monkeypatch.setenv("AI_PROXY_MACHINE_TOKEN", "legacy-token")
    assert client.upstream().server() == machine.upstream().server() == server
    for upstream in (client.upstream(), machine.upstream()):
        with pytest.raises(common.ServerChangedError):
            upstream.token(other)
        with pytest.raises(common.ServerChangedError):
            upstream.token(server)
    monkeypatch.setenv("AI_PROXY_URL", server)
    for upstream in (client.upstream(), machine.upstream()):
        assert upstream.token(server) == "shared-token"
    monkeypatch.delenv("AI_PROXY_TOKEN")
    assert common.environment_token() == "legacy-token"


def test_an_explicit_profile_never_imports_another_profiles_login(tmp_path, monkeypatch):
    legacy = tmp_path / "config" / "agent-machine" / "credentials.json"
    common.Credentials(legacy, {"https://old.example": common.ServerEntry("https://old.example")}, "https://old.example").save()
    monkeypatch.setenv("AI_PROXY_CREDENTIALS", str(tmp_path / "isolated" / "credentials.json"))
    with oauth.credential_lock(common.credentials_path()):
        assert common.Credentials.load(common.credentials_path()).default_entry() is None
    assert legacy.exists()


def test_active_logout_cannot_restore_a_historical_server(monkeypatch):
    active, other = "https://mesh.example", "https://old.example"
    common.Credentials(common.credentials_path(), {active: common.ServerEntry(active, refresh_token="active"),
                       other: common.ServerEntry(other, access_token="old-access", refresh_token="old-refresh")}, active).save()
    revoked = []
    monkeypatch.setattr(oauth, "revoke", lambda entry: revoked.append(entry.refresh_token))
    assert login.logout([], program="agent", credentials_path=client.credentials_path) == 0
    assert revoked == ["active"]
    assert machine.upstream().credentials().servers == {}


@pytest.mark.asyncio
async def test_login_switch_cannot_send_a_token_to_the_new_server(monkeypatch):
    old, other = "https://old.example", "https://other.example"
    path = common.credentials_path()
    entry = common.ServerEntry(old, access_token="old-access", expires_at=int(time.time()) + 3600,
                               issuer=old, client_id="mesh-cli")
    common.Credentials(path, {old: entry}, old).save()
    upstream = client.upstream()
    token = upstream.token

    def switch_after_signing(server):
        result = token(server)
        common.Credentials(path, {other: common.ServerEntry(other)}, other).save()
        return result

    monkeypatch.setattr(upstream, "token", switch_after_signing)

    def answer(request):
        assert str(request.url) == old + "/v1/models"
        assert request.headers[common.TOKEN_HEADER] == "old-access"
        return httpx.Response(200, stream=httpx.ByteStream(b"old server"))

    proxy = AgentProxy(upstream, 18183)
    await proxy.http.aclose()
    async with httpx.AsyncClient(transport=httpx.MockTransport(answer)) as http:
        proxy.http = http
        request = Request({"type": "http", "method": "GET", "path": "/v1/models", "query_string": b"",
                           "scheme": "http", "server": ("127.0.0.1", 8184), "headers": []})
        response = await proxy.agent(request)
        body = b"".join([chunk async for chunk in response.body_iterator])
        assert response.status_code == 200 and body == b"old server"
