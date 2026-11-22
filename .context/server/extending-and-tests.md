# Server: extending and tests

Part of [Server (`server/agent_server/`)](README.md). Checklists for a new route, backend type, agent, setting or table; how the tests are built; known gaps.

## Extending

- **Route**: add it to a `routes/*.py` `register` (or a new module registered before `compat`), an `openapi.TAGS` entry, a `SITE_ADMIN_ONLY` entry if it changes the server itself (else `ADMIN_ONLY` plus an `administers` check for organization changes), the tenant check (`same_tenant`, `fleet.usable_by` for backends), the sharing check (`viewable`/`controllable`) or the feature check (`allowed`), and a line in `docs/api.md`.
- **Backend/provider type** (the places Docker Model Runner touched):
  1. `config.py`: `BACKEND_TYPES`, `NATIVE_PROTOCOLS` (if it's pass-through), `PROVIDER_NAMES`; `MACHINE_TYPES` and `fleet.PROVIDER_TYPES` if machines run it.
  2. `routes/compat.py`: `MESSAGES_BACKENDS`, path mapping in `_forward`, and `_list_models` (agents' traffic only).
  3. agent-machine: a kind in `machine/internal/providers` (status, model ops, log), its probe in `presence.DetectProviders`, Go tests, and the same in `python/` (`.context/machine/`, `.context/python.md`).
  4. `routes/management.py`: `_backend_models` shapes the machine's `status` for `/api/models`. The provider routes in `routes/machines.py` are generic; nothing per provider goes in the server.
  5. Console: `api.ts` types and `MACHINE_TYPES`, `routes.ts` `PROVIDERS`, `Settings.tsx` (`<option>`, `URL_PLACEHOLDERS`), a `<Kind>Provider` view taking a `ProviderRef` and its case in `ProviderView` (`.context/console/pages.md`).
  6. The comments in `defaults.py` where they name backend types; `docs/providers.md`, `docs/console.md`, `docs/configuration.md`, `docs/api.md`; the fake server in `devhelpers/scripts/screenshots/demo.py`.
  7. Tests: traffic in `server/tests/test_<provider>.py` (follow `test_model_runner.py`), the relay in `test_providers.py`, the machine's side in Go and Python.
- **Fleet message**: a `Fleet.ask` channel with its gate and `<channel>_result` in `Fleet.message`, a hello field in `_update`/`Machine`, then the machine side in Go and Python.
- **Agent**: an entry in `defaults.AGENTS_YAML` to ship it (`executable`, `protocol`, `env`/`args` templates, `install`, `homepage`, and `backend_type` or `intercept`; synced into every server's `agents` table on start), or one added on Server settings → Agents (`PUT /api/config/agents/{id}`); no client code. Then check `agent --list` finds it, launch it once with `--trace` and read the traces, and add it to "Supported agents" in `docs/running-agents.md`.
- **Setting**: a key in the `settings` table, read in `Settings._configure`, written by `_store_config` (code-built Settings) and seeded in `config.seed`; add it to `config_io`'s document (export, parse, apply) so `config export|import` carry it, and to `ADMIN_ONLY` if a new route changes it.
- **Table or column**: `db/schema.py` plus a new step appended to `db/migrate.py`'s `API_STEPS` (never an edit to step 1); queries in SQLAlchemy Core; if the issuer must read it, `migrate.ISSUER_READS`. Cover it in `test_db.py`, on SQLite and PostgreSQL.

## Tests (`server/tests/`)

The Go contract fixtures install a native stub on PATH when testing provider CLI
detection, so they do not depend on developer-installed Copilot. Copies used by
executable-update tests preserve the binary's executable permissions on POSIX.
Fleet fixtures reap their child while update tests wait for restart, so a POSIX
zombie cannot be mistaken for a still-running machine.

- `make_app(tmp_path, handler)`/`close_app`: `Settings(...)` built in code (which writes its values into the test's database), `create_app`, a `TestClient` (signed in: `oidc_helpers.authenticated_client`, with introspection mocked), and `runtime.client` swapped for an `httpx.MockTransport`. Assert on what upstream received too.
- `oidc_helpers.py` plays the issuer's introspection for API tests (`access_token(app, user)`, `issuer(app).revoke`); `test_oidc_issuer.py`, `test_oidc_api.py` and `test_oidc_browser.py` cover the real issuer, the API's mandatory auth and the browser sign-in; `test_quickstart.py` the quickstart stack's credentials and layout. See `.context/auth/`.
- Starlette's TestClient sends WebSockets to `ws://testserver`: pass `headers={"host": "127.0.0.1:8181"}` and `client=("127.0.0.1", ...)` to look local (`ws_connect` in `test_terminals.py`). A refused WebSocket left open inside a `with TestClient(...)` block hangs the test on exit instead of failing it; use context managers for sockets. The one pytest warning (Starlette's httpx deprecation) is expected.
- `FakeSocket` (`test_providers.py`) answers like agent-machine (`LlamaSocket` in `test_llamacpp.py` adds llama.cpp's provider and server requests); `FakeMachine` (`test_containers.py`) replaces `Fleet.ask`; `ws_connect` (`test_terminals.py`) makes WebSockets look local.
- `test_client.py` runs the real Go client and machine (and, from `python/tests`, the Python ones) on real ports and real tunnels, signed in through a real `create_auth_app` issuer, with `fake_mcp.py`, `fake_docker.py`, `fake_ollama.py`, `fake_llamacpp.py` (llama-server's router: models, downloads with `/models/sse` progress), `fake_strata.py` (`fake_systemctl.py` is for the machine's systemd tests in Go and `python/tests`). It builds the programs with the first of `BUILD_FLAGS` that builds and runs (`build_program`, version `dev` in each): on Windows, Defender's ML check (`Trojan:Win32/Bearfoos.A!ml`, a false positive) quarantines one variant of the client or another, at link, at write or at run, and which one changes with the bytes and with Defender's updates. If every variant fails with "contains a virus", `Get-MpThreatDetection` names the file, and the comment by `BUILD_FLAGS` lists what was tried; add another variant there.
- `conftest.py` (autouse `isolated_storage`) gives every test its own `AI_PROXY_LOG_DIR` (so its own SQLite file), `AI_PROXY_REMOTE_KEY_FILE` and `AI_PROXY_CA_DIR`, clears `AI_PROXY_DATABASE_URL`/`AI_PROXY_AUTH_DATABASE_URL` and the legacy `AI_PROXY_BACKENDS`/`MODELS`/`AGENTS`, and fails any test that touches the real `logs/agent-server.sqlite3` or creates secrets in `certs/` (`remote-access-key`, `container-keys.json`, `users.json`, `first-user-password`). With `AI_PROXY_TEST_DATABASE_URL` (a PostgreSQL admin URL) each test gets a fresh database there, dropped afterwards; the whole suite passes on both.
- `oidc_helpers` reads back what's stored: `stored_backends(settings)` (the backends as `config export` gives them), `database(tmp_path, separate=False)` (the test's database, or a separate SQLite one), `stored_rows(db, table)`. Each flushes the write queue first.
- `test_cluster.py` runs two apps (pods) sharing a database and a fakeredis, each with its internal port on loopback: sessions, request ids and sharing across them, a machine on one asked from the other, launch grants, usage limits, settings, a terminal viewer forwarded to its host. `test_governance.py` covers retention, holds, the audit log (chains, tampering, exports, the middleware) and blob storage; `test_jwt.py` signed access tokens against the real issuer.
- `test_db.py` covers the database itself: migrations, seeding, shipped-agent upgrades, export/import, the config routes' admin checks, request paging and retention, terminal chunks, the issuer's read-only accounts and the legacy import (issuer state included).

## Known gaps

- Peering: a linked server reachable only over its link socket is reached only from the process holding it (others answer 503); MCP servers "for our own people's launches" (scope `home`) aren't offered to guests' launches; terminal output of a launch on the other server isn't copied into the mirror session (it's viewed live, and kept there); account agents can't be launched for guests; legal holds don't cross. Long streams re-check the bridge on every message there, but don't send fresh assertions.

- `containers._answers` probes `backend.url + health_path` directly, not through the tunnel, so readiness can be wrong for a machine behind NAT.
- A settings import (`config_io.apply`: `agent-server config import`, `POST /api/config/import`) checks backend types and URLs but not `validate_spec` or the rule that local backends come only from machines; agent-machine's own spec check is what stands behind an imported container. An imported `api` backend gets `api_providers.validate`, but one naming a `machine` keeps its URL unchecked, like other local backends.
- API providers' fallback is only for connection failures; there's no fallback on 429/5xx answers, no circuit breaker and no active health probe (health is what requests and tests saw, per process).
- A machine-hosted `api` backend with a key sends that key through the machine's tunnel: its machine's owner could read it. Use `auth: none` for servers on machines unless the machine is trusted with the key.

Peering’s advertised URL (`Links.url`) uses `AI_PROXY_MESH_URL`, then `Settings.public_url`, then `Settings.proxy_url`, stripping a trailing slash. The last fallback is needed by local quickstarts with no separate public or mesh address; `Settings` has no `url` attribute. `test_peering.py` covers the links API and discovery response for all three address sources.
