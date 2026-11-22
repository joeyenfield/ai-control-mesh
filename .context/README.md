# Project context

Design references for coding agents working on this repository, written to be read **before** changing a component. They describe how each part is built and which rules it must keep. The user documentation is `docs/`; `AGENTS.md` is the short entry point (commands, the rules every change follows, the dev machine) and sends readers here for everything component-specific, including the checklists (`server/extending-and-tests.md`).

A small component has one file. A large one has a folder: start with its `README.md` (the design, boundaries and rules, and a table of the folder's other files), then read the topic file for the part you're changing.

| Path | Component | Read it when you touch |
| --- | --- | --- |
| [overview.md](overview.md) | The whole system | Anything that crosses components, or you're new here |
| [server/](server/README.md) | `server/` (`agent-server`, Python) | API, routing, recording, accounts, the fleet registry, interception |
| [client.md](client.md) | `client/` (`agent`, `agent-proxy`, Go) | The launcher, the loopback proxy, login, terminals |
| [machine/](machine/README.md) | `machine/` (`agent-machine`, Go) | Fleet membership, model servers, containers, tunnels, MCP bridge |
| [python.md](python.md) | `python/` (`agent_py`) | The Python versions of the client and machine |
| [console/](console/README.md) | `ui/` (React) | Console pages, data layer, the Machine page, split hosting (`VITE_API_URL`, `ui/Dockerfile`, `ui/nginx.conf`) |
| [auth/](auth/README.md) | Accounts and access, across components | OIDC sign-in through the separate `agent-auth` issuer (`issuer.py`), tokens and cookies, `AuthMiddleware`, roles, sharing, machine grants, any new route (the legacy access key and pairing no longer authenticate) |
| [android.md](android.md) | `android/` (Kotlin) | The app shell around the console, and the debug-only Android Auto monitor (`app/src/debug/`) |
| [build-and-deploy/](build-and-deploy/README.md) | `tools/`, `devhelpers/` (scripts, from-source stacks), Docker and Compose (`server/Dockerfile`, `server/docker/`, `ui/Dockerfile`), `sandbox/`, `site/`, `deploy/` (published-image Compose and Helm) | Versions, builds, downloads, installers, images, the brochure site, HTTPS setup, the local quickstart stack |

## Folders

| Folder | Files |
| --- | --- |
| `server/` | `modules.md` (core module map), `features.md` (feature modules: provider accounts, usage limits, logs, retention, peering, chats), `reviews-and-costs.md`, `flows.md`, `state-and-config.md`, `rules.md` (rules for changes, security model), `processes.md` (several processes), `extending-and-tests.md` (checklists, tests, known gaps) |
| `console/` | `shell.md` (shell and routing), `data-layer.md`, `terminals.md` (viewer and Live agent view), `pages.md` (Machine page, organization, peering), `settings.md`, `workflow.md` (extending, checking visually, `docs/` pages) |
| `build-and-deploy/` | `dist.md` (`tools/dist`, handing out and installing), `images.md`, `stacks.md` (Compose and Helm), `site.md` (brochure site), `releases.md`, `scripts.md` |
| `auth/` | `credentials.md` (credentials, signed tokens, browser and native sign-in), `tenants.md` (tenants, guests of linked servers) |
| `machine/` | `packages.md` (command line, packages), `fleet-protocol.md` |

## Keeping these files true

- They describe the design, not every line. When a change alters a component's shape (a new module, message type, boundary, file on disk or invariant), update its file in the same change.
- Name real symbols and paths, so a reader can jump to them. If a name here no longer exists, fix the file.
- Keep each file focused on its own component. Cross-component flows go in `overview.md`.
- Keep each file under about 20 KB. When one grows past that, split a topic out into its own file in the component's folder (turning a single file into a folder with a `README.md` if needed), and list it in the folder's README and the table above.
