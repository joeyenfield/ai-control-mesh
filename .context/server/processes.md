# Server: several processes

Part of [Server (`server/agent_server/`)](README.md). Running the API as more than one process: what each kind of state does to stay correct.

## Several processes

`agent-server` is one program; `AI_PROXY_ROLES` (`roles.py`) says what a process serves, and `AI_PROXY_REDIS_URL` makes processes one server (`cluster.py`). Without Redis, everything stays as described above (memory is authoritative), and `app.single_node` holds a PostgreSQL advisory lock (`SINGLE_NODE_LOCK`) so a second such process refuses to start; roles other than `all` and `AI_PROXY_WORKERS>1` are refused (`roles.check`). With Redis (`cluster.scaled`):

| What | How |
| --- | --- |
| Sessions (`SessionStore`) | Created with a direct write and announced; `get` falls back to the database; document changes are `_change` (read, change, write in one transaction with `with_for_update`, then `session.changed`), applied in place in every process (`_refresh`, `listeners`: terminal viewers drop); request ids from shared counters (`session:<id>:seq`/`:count`, seeded from the row); counters are columns only ever raised (`_write_counters`); `list()` reads the last `LIST_DAYS` from the database. |
| Machines (`Fleet`) | The holding process announces `fleet.machine` (state, keys sealed); others keep `owners`; `ask`/`launch`/`shell`/`send_tunnel` go through `_where` to `_remote` (the owner's `/internal/fleet/...`), whose `FleetError`/`MachineGone` come back as such; `fleet.removed` closes and refuses it everywhere. `_save(machine)` writes only the changed machine. |
| Tunnels (`TunnelHub`) | The asking process keeps its loopback listener and records `tunnel:<id>` → itself; a dial-back reaching another process is `relay`ed to `/internal/tunnels/<id>` (binary frames only; the machine's token renewals are taken where its WebSocket is). |
| Terminals | `terminal:<session>` → the hosting process and its `public()`, refreshed by `flush_quiet`; `TerminalForward` proxies `/api/terminals/<id>/view` there; `summaries` uses the snapshot. |
| Live (`LiveHub`) | Events batched to `live.events` every `BATCH_SECONDS` only while another process has subscribers (its beacon `live`); others mirror entries without payloads; details are asked of the process (`elsewhere`, `/internal/live`). |
| Usage limits | `usage:<tenant>:<user>:<provider>` hash per hour, seeded once from the database (max per hour); `usage.limits` reloads limits. |
| Provider accounts | `Grants` in shared keys, `claim` (`SET NX`) for the one-time token; `provider_accounts.changed` carries rows. |
| Settings, container keys, API provider keys | `Settings.changed` / `KeyStore.listeners` / `ApiKeys.listeners` → `config.changed` → `Settings.reload`, `KeyStore.reload` and `ApiKeys.reload`. API providers' health and discovered models (`runtime.api_health`, `runtime.api_discovery`) are each process's own observations and aren't shared: the console shows what the process answering it saw. |
| Jobs | Only the `worker` role runs them, each under a lease: retention (`prune_daily`), `_audit_checkpoints`, `_audit_exports`. |
| Auth | `Auth.revocations` (`Revocations`): signed access tokens checked locally (`verify_jwt`) while revocations are fresh; see `.context/auth/`. |
| Links (`links.py`) | One `fleet` process dials each link (lease `link-dial:<id>`); the process holding a socket says so in `link:socket:<id>`. Nonces are cluster keys, so a replay is refused anywhere. Changes are `bridge.changed` messages (`Links._heard` reloads links, keys and bridges). Signed requests to a reachable peer go from any process; one only reachable over its socket is reached only from the holding process (a known gap). Guests' launches: `fleet:guest-launch:<id>`; the internal launch route passes `guest` on. |

A new route belongs to a role by the module that registers it (`roles.MODULE_ROLES`, `PATH_ROLES`): add it there, and to the chart's route table (`deploy/helm/agent-mesh/templates/_helpers.tpl`) when it isn't the api's. Jobs one process should run take `cluster.leader(name)` and run in the `worker` role. `test_cluster.py` shows two processes in one test (`RedisCluster("fakeredis://<uuid>", ...)`, `create_app(..., cluster=, internal_port=0)`).

**Must never** with several processes: put a machine's keys, an MCP key or a token in Redis in the clear (seal them); serve `/internal/*` on the public port or without the token (`/metrics` is the only exception); treat an internal call as authentication (launches and shells name the user, whose permissions are checked again; forwarded viewers carry their own credentials); keep a new kind of per-process state without saying how another process finds it.
