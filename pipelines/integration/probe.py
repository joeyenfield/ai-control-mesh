"""Exercise a deployed distribution, its issuer and its packaged Go machine."""
from __future__ import annotations

import hashlib
from http.cookiejar import CookieJar
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import time
import unittest
from urllib.error import HTTPError
from urllib.parse import urlencode
from urllib.request import HTTPCookieProcessor, ProxyHandler, Request, build_opener

API = "http://localhost:9999"
ISSUER = "http://localhost:20000"
VERSION = os.environ["EXPECTED_VERSION"]
CLIENT = build_opener(ProxyHandler({}))


def fetch(path: str, token: str | None = None) -> dict:
    headers = {"Authorization": "Bearer " + token} if token else {}
    with CLIENT.open(Request(API + path, headers=headers), timeout=15) as response:
        return json.load(response)


class DeploymentSmoke(unittest.TestCase):
    def test_server_version(self) -> None:
        self.assertEqual(fetch("/health")["version"], VERSION)

    def test_issuer_health(self) -> None:
        with CLIENT.open(ISSUER + "/health", timeout=10) as response:
            self.assertEqual(response.status, 200)

    def test_exact_distribution_downloads(self) -> None:
        index = fetch("/downloads/index.json")
        self.assertEqual(index["server_version"], VERSION)
        self.assertEqual(index["version"], VERSION)
        self.assertTrue(index["exact"])
        self.assertTrue(index["files"])

    def test_mesh_requires_authentication(self) -> None:
        for token in (None, "invalid-ci-token"):
            with self.subTest(token="missing" if token is None else "invalid"):
                with self.assertRaises(HTTPError) as answer:
                    fetch("/api/machines", token)
                self.assertEqual(answer.exception.code, 401)


class ControlMeshIntegration(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        browser = build_opener(ProxyHandler({}), HTTPCookieProcessor(CookieJar()))

        def issuer(path: str, values: dict | None = None) -> bytes:
            request = Request(ISSUER + path,
                              data=urlencode(values).encode() if values is not None else None,
                              headers={"Origin": ISSUER})
            with browser.open(request, timeout=15) as response:
                return response.read()

        device = json.loads(issuer("/device_authorization", {
            "client_id": "mesh-cli", "scope": "openid profile offline_access mesh-api",
        }))
        page = issuer("/device").decode()
        transaction = re.search(r'name="transaction" value="([^"]+)"', page)
        if transaction is None:
            raise AssertionError("The issuer did not return its CSRF-bound device form")
        issuer("/device", {"transaction": transaction.group(1), "user_code": device["user_code"],
                           "username": "ci-admin", "password": os.environ["AI_PROXY_PASSWORD"],
                           "approve": "yes"})
        credentials = json.loads(issuer("/token", {
            "client_id": "mesh-cli", "grant_type": "urn:ietf:params:oauth:grant-type:device_code",
            "device_code": device["device_code"],
        }))
        if not credentials.get("access_token") or not credentials.get("refresh_token"):
            raise AssertionError("The real issuer did not issue OAuth credentials")
        cls.token = credentials["access_token"]
        temporary = tempfile.TemporaryDirectory(prefix="mesh-ci-")
        cls.addClassCleanup(temporary.cleanup)
        cls.root = Path(temporary.name)
        cls.workspace = cls.root / "workspace"
        (cls.workspace / "mesh-project").mkdir(parents=True)

        # Fetch the programs through the deployed server and verify its advertised checksums.
        index = fetch("/downloads/index.json")
        for program in ("agent", "agent-machine"):
            entry = next(item for item in index["files"]
                         if item["path"] == "linux-amd64/artifact/" + program)
            with CLIENT.open(API + entry["url"], timeout=30) as response:
                data = response.read()
            if hashlib.sha256(data).hexdigest() != entry["sha256"]:
                raise AssertionError("The deployed download has the wrong checksum: " + program)
            binary = cls.root / program
            binary.write_bytes(data)
            binary.chmod(0o700)
        cls.binary = str(cls.root / "agent-machine")
        cls.environment = {**os.environ, "HOME": str(cls.root),
                           "AI_PROXY_MACHINE_HOME": str(cls.root / "machine"),
                           "AI_PROXY_CREDENTIALS": str(cls.root / "credentials.json"),
                           "AI_PROXY_URL": API, "AI_PROXY_TOKEN": cls.token}
        for name in ("HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"):
            cls.environment.pop(name, None)
        for arguments in (("name", "ci-mesh-machine"), ("roots", "add", str(cls.workspace)),
                          ("servers", "off"), ("containers", "off"), ("updates", "off"),
                          ("launches", "on"), ("files", "on")):
            subprocess.run([cls.binary, *arguments], env=cls.environment,
                           check=True, capture_output=True, timeout=15)
        status = json.loads(subprocess.check_output([cls.binary, "status", "--json"],
                                                   env=cls.environment, text=True, timeout=15))
        if status["version"] != VERSION:
            raise AssertionError("The packaged machine version differs from the deployed server")
        cls.machine_id = status["id"]
        log = (cls.root / "machine.log").open("w")
        cls.addClassCleanup(log.close)
        cls.machine = subprocess.Popen([cls.binary, "run"], env=cls.environment,
                                       stdout=log, stderr=subprocess.STDOUT)

        def stop() -> None:
            if cls.machine.poll() is None:
                cls.machine.terminate()
                try:
                    cls.machine.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    cls.machine.kill()
                    cls.machine.wait(timeout=5)

        cls.addClassCleanup(stop)
        deadline = time.monotonic() + 45
        while time.monotonic() < deadline:
            if cls.machine.poll() is not None:
                raise AssertionError("The packaged machine exited before joining the fleet")
            listed = fetch("/api/machines", cls.token)["machines"]
            if any(item["id"] == cls.machine_id and item["connected"] for item in listed):
                return
            time.sleep(1)
        raise AssertionError("The packaged machine did not connect to the deployed control mesh")

    def test_device_sign_in_and_packaged_machine_join(self) -> None:
        identity = fetch("/api/auth/status", self.token)["identity"]
        self.assertEqual(identity["user"], "ci-admin")
        machine = fetch("/api/machines/" + self.machine_id, self.token)
        self.assertTrue(machine["connected"])
        self.assertEqual(machine["name"], "ci-mesh-machine")

    def test_folders_rpc_reaches_real_machine(self) -> None:
        answer = fetch("/api/machines/" + self.machine_id + "/folders?" +
                       urlencode({"path": str(self.workspace)}), self.token)
        self.assertIn("mesh-project", [item["name"] for item in answer["folders"]])

    def test_mesh_refuses_paths_outside_machine_roots(self) -> None:
        with self.assertRaises(HTTPError) as answer:
            fetch("/api/machines/" + self.machine_id + "/folders?path=/etc", self.token)
        self.assertEqual(answer.exception.code, 400)


if __name__ == "__main__":
    suite = unittest.defaultTestLoader.loadTestsFromModule(sys.modules[__name__])
    result = unittest.TextTestRunner(verbosity=2).run(suite)
    print(json.dumps({"tests_run": result.testsRun, "failures": len(result.failures),
                      "errors": len(result.errors), "skipped": len(result.skipped)}))
    sys.exit(0 if result.wasSuccessful() and not result.skipped else 1)
