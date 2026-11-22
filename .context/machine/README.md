# Machine (`machine/`)

`agent-machine`: the Go module `ai-control-mesh/machine`. It keeps a computer in a server's **fleet** and supplies every local LLM provider: the model servers it runs, and containers it runs for the server.

The executable dispatches through `settings.CommandArgs()`. On Android this removes an extra absolute self-path inserted by Termux's linker before the subcommand; other arguments stay intact. `settings/args_test.go` mirrors the client's cases, and Python uses `common.command_args()`. `settings.Executable()` uses argv to identify the native binary when Android's `/proc/self/exe` identifies the system linker. Startup, remote updates and update location discovery use it. `process.Command()` starts our binaries through Android's linker explicitly because Go's raw exec syscall bypasses Termux's libc hook. The client mirrors both helpers; Python uses its interpreter and libc subprocess handling instead. Background startup redirects stdout/stderr to the private machine log before spawning and reaps the child with `Wait` so a zombie cannot look like a successful startup. Python mirrors startup logging and already uses `Popen.poll()` to detect exits.

## Files in this folder

| File | Covers |
| --- | --- |
| [README.md](README.md) | This page: the design, boundaries and rules |
| [packages.md](packages.md) | The `agent-machine` subcommands and the Go packages behind them. |
| [fleet-protocol.md](fleet-protocol.md) | The fleet protocol as the machine sees it: the connection, requests and replies. |

## Design in one paragraph

The machine only dials out. It holds one WebSocket to the server (`/api/machines/connect`), describes itself in a hello and then every minute, and answers requests that arrive on that socket. Every request is checked again on the machine, against a freshly loaded `machine.json`, whatever the server allowed. Everything about a model server (models, logs, load, settings, start/stop) is done here against `127.0.0.1:<port>`, so the server never needs a route to it. Agents' traffic reaches the model servers through tunnels the machine dials back on request. Launches and shells run the client's `agent` program as a separate process; the two share no code.

## Boundaries

All child process call sites use `process.Command` / `CommandContext`: headless agents and terminals, updates, model servers and package tools, account detection, MCP and containers. The helpers mirror the client's ELF/shebang handling and preserve the separate argument list and cancellation. Android regression tests cover the machine-to-agent session handshake (`presence/native_test.go`), native/scripts/contexts (`process/command_test.go`), and Ollama's lifecycle (`servers/native_test.go`) in Jenkins' required fresh app sandbox.

- Imports nothing from `client/`, `server/` or `tools/`. Dependencies: gorilla/websocket and x/sys (`go.mod` also lists x/term, which no code imports).
- No inbound listeners except the MCP bridge on `127.0.0.1:0` and the servers it starts on request: llama-server on `127.0.0.1:9931`, and Ollama on the bind its settings name (loopback by default; the console can set `0.0.0.0`). Outbound only otherwise: the server, loopback model servers, model catalogues, and GitHub's llama.cpp releases when the console installs llama.cpp.
- `Proxy: nil` on every client (`settings.Transport`). Server calls carry the version, protocol and token headers (`settings.ServerClient`, `Identify`).
- One sign-in and selected server shared with `agent` for this OS user (`agent login` and `agent-machine login` are equivalent for authentication).
- Device authorization and rotating OAuth credentials mirror the client in this separate Go module (`internal/settings/oauth.go`: `DeviceLogin`, `Upstream.AccessToken`, `RejectAccessToken`, `LockCredentials`, `Revoke`). Automation sets the shared `AI_PROXY_TOKEN`; `AI_PROXY_MACHINE_TOKEN` is an alias accepted by both programs when the shared variable is absent. Every request and socket dial (`presence.Dial`) gets a fresh access token; a close 4410 calls `RejectAccessToken` and the reconnect refreshes. `login` refuses `--user`/`--key`. Never send passwords to the API or fall back to an old key. See `.context/auth/credentials.md`.

## State (`settings.Home()`: `AI_PROXY_MACHINE_HOME`, `%APPDATA%\agent-machine`, `~/.config/agent-machine`)

`servers/adreno_install.go` adds Termux's `opencl-adreno` install choice on ARM64 machines with Qualcomm vendor OpenCL libraries. It verifies the pinned source archive and embedded MIT loader patch (`adreno/loader.patch`, license beside it), installs fixed build dependencies, then executes embedded `adreno/build.py`. The recipe is byte-identical to Python's `adreno_builder.py`; keep both in step. The owned build lives under `llamacpp/adreno-<commit>-<recipe-hash>/`, with `build.json` hashing source, patch, recipe, native binary and launcher. Valid caches skip apt/download/compile, but always probe `--list-devices` for Adreno before selecting. The launcher resets Termux executable identity to the native child and disables warmup. Successful installs merge the tested 8K/F16/off/one-slot/four-thread/GPUOpenCL profile and save `source: source`; failures preserve selection/config. Model folders and unrelated settings are preserved. The ordinary uninstall removes owned builds, leaving apt packages. `AI_PROXY_LLAMACPP_ADRENO_SOURCE` (asset JSON) and `AI_PROXY_LLAMACPP_ADRENO_BUILDER` (command JSON) are local fixture overrides, never server-request fields. `adreno_install_test.go` covers hashes, GPU refusal, failure recovery and cache reuse; the fleet contract runs against both implementations.

`settings.Data()` (`AI_PROXY_MACHINE_DATA`, else `Home()`) holds the large files: `llamacpp/` (installed builds) and `llamacpp-models/` (the default GGUF folder). Only when the variable is set (`settings.DataSet`) does an Ollama agent-machine starts get `OLLAMA_MODELS=<data>/ollama-models` (`ollamaDataModels`, unless the models_dir setting names one), so existing machines keep Ollama's own folder. Python: `settings.data()`/`data_set()`, `servers.ollama_data_models`. Credentials, settings, logs and install records stay in `Home()`.

`machine.json` (id, name, toggles, roots, address, CLI), `status.json` (pid, state, server, at, server_version, started: background, service or terminal), `mcp.json`, `servers.json` (the Ollama and llama-server it started; Ollama's saves keep llama-server's fields and the other way round), `ollama.json`, `llamacpp.json`, `llamacpp-install.json` (what the console installed and where its `llama-server` is), `llamacpp/` (installed builds), `agent-machine.log`, `ollama.log`, `llamacpp.log`, `llamacpp-install.log`. Files are 0600 in a 0700 folder, written atomically.

## Invariants

1. Every toggle defaults to off and is re-checked per request from `machine.json`.
1. File contents leave the machine only through the `files` request, only with `files on`, only inside the roots (checked on the path as asked and once links are resolved). Supported raster images up to 10 MiB are the only binary files sent; an incomplete image sends no bytes. Hidden names (.env, .git) are shown: the `files` grant is the boundary.
2. Roots are checked after resolving links (`Machine.Within`). The server checks too (`within_roots`); keep both.
3. Container specs come only from the engine table, with each engine's own options, plain images, `gpus` all/none/numbers, and a volume name or absolute folder. **Never add free-form docker arguments**: `docker run` is root on that machine. Podman gets GPUs as CDI devices.
4. Tunnels reach only reported provider ports, containers' ports and the bridge. A container's port counts only from a spec that passed `Validate` (on a start, restart or status about it); removing the container or turning containers off closes it (`containers.Manager.Ports`).
5. Provider operations call fixed loopback paths. Ollama stop only stops the Ollama it started, and the same for llama-server, which always listens on loopback only. llama.cpp is only installed from Termux's packages or a GitHub release whose SHA-256 matches GitHub's digest, unpacked inside its own folder; its settings never turn on llama-server's tools, MCP or agent mode.
6. Strata keys are redacted in logs. MCP commands, env and headers never leave the machine (the hello carries names). The bridge checks its per-run key in constant time and re-reads `mcp.json` on every request.
7. A launch is refused when this computer's agent-proxy carries traffic to another server.
8. Updates install only the server's build, verified by SHA-256.

## Extending and testing

Native model-server commands apply `process.ExecutableEnvironment` after assembling their environment. Android's direct system-linker launch bypasses Termux libc's exec hook, so its inherited `TERMUX_EXEC__PROC_SELF_EXE` must identify the child binary, not agent-machine. Otherwise llama.cpp's libc executable lookup respawns agent-machine as a model worker. The helper preserves the rest of the environment and leaves other platforms unchanged. Native Ollama and llama.cpp tests inject a stale parent identity and validate the child identity, readiness and stop; both run in Jenkins's Android APK harness. Python subprocess already uses the Termux libc hook.

`servers/ollama_install.go` handles Ollama `install_options` and `install`: Termux-only fixed package lists for CPU, Android Vulkan or Mesa Turnip. `install_available` and a bounded install job travel in Ollama's state. A running Ollama must be stopped first; changing/starting/configuring Ollama is refused during installation. `ollama-install.log` records apt output privately. Settings are merged over the saved config only after packages and required backend files succeed, and server management is checked again before saving. CPU disables GPU devices; Vulkan uses F16, no Flash Attention, 8K context and one slot. Profile-related extra variables are removed so they cannot defeat the choice. Installation never claims verified GPU inference. Python mirrors this in `machine/ollama_install.py`. Shared fake apt and fleet contracts cover both implementations without touching real packages.

Ollama startup uses `process.Command()` so Android executes the native Termux binary through its system linker rather than a raw exec syscall into app storage. Python's `subprocess.Popen` already goes through libc and Termux's execution hook. `servers/native_test.go` starts the test executable as a native Ollama stand-in, checks its HTTP readiness and saved process id, then stops it; this also runs as a compiled Android test on the emulator.

- **Provider kind**: a case in `providers.handle` (status, model operations, log), a probe in presence, Go tests, then the server and console sides (the checklist in `.context/server/extending-and-tests.md`).
- **Message type**: a branch in `presence.session` answering `<type>_result` with its own toggle check; a new toggle goes into `settings.Machine`, `configure`, `show` and `describe`. Mirror it in `python/agent_py/machine/presence.py` and the server's `Fleet`.
- `go test ./...`, and vet for Windows and Linux (the `process_*.go` files differ). End to end: `server/tests/test_client.py::fleet_machine` with `fake_docker.py`, `fake_strata.py`, `fake_llamacpp.py`, `fake_mcp.py`. The installer's tests play GitHub with `httptest` (`AI_PROXY_LLAMACPP_RELEASES`) and Termux's apt-get with a script (`AI_PROXY_LLAMACPP_APT`).
- Don't test as the Ubuntu WSL user: it runs a real agent-machine in the real fleet.
- For a real container runtime, Podman works as the machine's CLI (`agent-machine containers cli podman`). On the dev computer that's `podman-machine-default`, normally stopped: start it, test, then clean up and stop it again. Docker isn't installed there, so Docker Model Runner support is tested against mocks and the demo's fake only.
