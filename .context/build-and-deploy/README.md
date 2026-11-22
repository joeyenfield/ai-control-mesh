# Build and deploy (`tools/`, `devhelpers/`, `server/Dockerfile`, `server/docker/`, `ui/Dockerfile`, `site/`, `sandbox/`, `deploy/`)

How the components are versioned, built for every platform, handed out by the server, installed and run in containers. The static brochure site in `site/` is separate from the application console in `ui/`; Compose serves it privately on port 8080 for the public reverse proxy.

## Files in this folder

| File | Covers |
| --- | --- |
| [README.md](README.md) | This page: the design, boundaries and rules |
| [dist.md](dist.md) | `tools/dist`, and how the server hands out builds and installers. |
| [images.md](images.md) | The container images: the server, the sandbox and the all-in-one image. |
| [stacks.md](stacks.md) | The Compose projects and Helm charts, published (`deploy/`) and from source (`devhelpers/`). |
| [site.md](site.md) | The static public site in `site/`. |
| [releases.md](releases.md) | Local release builds: images, installers and the tray. |
| [scripts.md](scripts.md) | The developer and release scripts, in `.sh`/`.ps1` pairs. |

## Design in one paragraph

There is one hand-written version (`VERSION`) and one compatibility number (`PROTOCOL`). `tools/dist` builds every Go program for every platform, plus packages, the Python wheel and the Android APK, into `dist/<version>/` with a `SHA256SUMS`. The server hands out exactly those files (`/downloads/`), and every installer and self-update checks them against that list. So a computer only ever installs what its server built.

## Jenkins pipelines

`pipelines/Jenkinsfile.build` and `Jenkinsfile.verify` are read from Git by
multibranch Jenkins jobs. They use `checkout scm` for the indexed revision.
Verification discovers branches and PR heads, including forks with target-branch
pipeline trust. Distribution builds discover origin branches; only `master`
builds automatically. Both folders are indexed every five minutes.
`Jenkinsfile.test` and `Jenkinsfile.distribute` remain parameterized SCM pipelines
for exact built versions. Release requires the matching deployment-test result.
The release job's native VERSION dropdown is maintained by
`pipelines/refresh-versions.py` on the trusted CI host. It lists matching passed
deployment tests newest first, refreshes after tests and job installation, and
preserves the destination parameter. Its parameters must not be redeclared
statically in `Jenkinsfile.distribute`, which would erase the refreshed choices.
`pipelines/configure.groovy` migrates the controller while preserving legacy job
history. The definitions call installed host runners through `MESH_CI_HOME`;
credentials and artifacts stay outside Git. See `docs/pipelines.md` for setup and
the deployment and control mesh integration coverage. The seven checks in
`pipelines/integration/` are archived and checksummed with each distribution.
They run the packaged Go machine against real PostgreSQL/server/issuer services,
cover OAuth device sign-in, fleet enrollment, a folders RPC and path confinement.
Promotion rejects failed or skipped deployment tests. Installer URLs point to
the versioned GitHub release, where the installer files are published alongside
the full distribution archive.

## Versions

`Jenkinsfile.verify` runs the host runner's stages: `tools`, `client`, `machine`, `vet` (gofmt, `go vet` per shipped system), then in parallel `server` (SQLite), `server-postgres` (a private pod with `postgres:17`), `python` (the whole `python/` suite, the client contract and the install tests among it) and `helm` (lint and kubeconform per `ci/` values file, CRDs from the CRDs catalog), then `ui`. The trusted host's `verify.py` must support every stage a branch's Jenkinsfile names (it keeps `ollama-setup` for branches from before `python` replaced it); containers disable inherited proxy settings so host agent credentials cannot leak into builds.

The distribution build has a mandatory `Android runtime regressions` stage after platform packaging. `pipelines/android-runtime/run.py` compiles Go tests for Android x86-64 and runs them through a temporary APK in a fresh emulator's app domain, where raw app-storage exec is denied. Thirteen required checks cover ARM64 loader/libc DNS linkage, both modules' native/script execution and cancellation, PTY I/O, machine-to-agent shell launch, and native Ollama/llama.cpp start/stop with the correct child executable identity. The runner creates its own AVD, ADB server and test keys and removes them afterwards. JUnit and logs are archived in Jenkins and checksummed under `android-runtime/` in the distribution. A missing, failed, skipped, incomplete or other-commit report blocks release. PR verification still runs its disposable Go/server/UI containers; host Android tooling is used only by trusted origin distribution builds.

The server Dockerfile applies `AI_PROXY_VERSION` after installing dependencies.
Changing a CI snapshot version therefore preserves the dependency build cache.

- `VERSION` = `X.Y.Z`. The build version is `X.Y.Z` on its tag, else `X.Y.Z+<commit>[.dirty]`. `.dirty` means uncommitted changes to any tracked file (no exclusions: the server's settings used to be saved in `config/`, which was exempt, but they live in its database now); there's no `.git` → `+unknown`. `AI_PROXY_VERSION` overrides it.
- It is computed in three places that must agree: `tools/dist/version.go` (`BuildVersion`), `server/agent_server/version.py` (`build_version`) and `python/agent_py/common.py` (`checkout_version`).
- `PROTOCOL`/`MIN_PROTOCOL` live in `version.py`, `settings.Protocol` in client/ and machine/, and `common.PROTOCOL`; `test_client.py` checks they agree. Raise `PROTOCOL` for changes older programs can't follow, and `MIN_PROTOCOL` only when dropping the old one. Outside the range the server answers 426 (WebSocket close 4426).

## Rules

The Jenkins verification runner's Python programs stage includes the Ollama/Adreno installer units, llama.cpp units (including unloaded/split model sizes, duplicate roots and weight symlinks), both installation fleet contracts and the llama.cpp model-serving contract. The latter hides unloaded models' file paths to check sizes through the real fleet API. Go's full machine suite and server contracts cover the same behavior; the Android runtime stage separately covers native executable identity. Qualcomm device inference remains a real-device check, not an emulator claim. The source installer embeds the vendor patch/license and Python build recipe; `.gitattributes` forces LF for their hashed bytes. Python assets are modules so both pip and the Go distribution wheel writer carry them.

The standalone console and brochure nginx images hide version strings and send framing, object/base-URL, sniffing and referrer protection headers. Keep nginx header inheritance intact when changing location-specific caching. Helm trusts forwarded headers from loopback by default: operators must set ingress/gateway CIDRs, or restrict `networkPolicy.publicFrom` before trusting `*`.

- Keep `go.mod` at Go 1.24 and build with `GOTOOLCHAIN=local`.
- Every build in `dist/<version>/` is ~450 MB and nothing prunes them: delete old `dist/<version>+<commit>/` folders by hand.
- Podman on the dev computer (`podman-machine-default`, WSL-based, no localhost forwarding) builds and runs `server/Dockerfile`: pass `--http-proxy=false` to `podman build`, and reach a published port at the VM's address (`ip -4 addr` inside `wsl -d podman-machine-default`).
- Don't commit `dist/`, `ui/dist`, `logs/`, `certs/` or the `bin/` folders.
- Linux package versions are `X.Y.Z-<release>`. `PackageRelease` makes the release the commits since `vX.Y.Z` plus one, so each untagged build upgrades the one before. Keep the release rising if the version scheme changes.

The brochure’s `coaching.html` page is a short, high-level overview with diagrams: the review flow (session, checks, score, AI review), the five scored areas (Context, Tools, How it went, Reliability, Cost) beside an example scorecard, the ten shipped review topics as chips, and what is and isn't sent to the reviewing model. Detail belongs in `docs/session-review.md`, which it links to; keep its few numbers (27 checks, 16,000 characters) in step with that page and `server/agent_server/review.py`. It is linked in every page’s navigation, included in the public preview, sitemap, image and page checks, and Docker image. Product captures are refreshed from the current isolated demo; `session-review` uses synthetic data and a simulated review response. The real-agent terminals come from an isolated all-in-one container; the 39 walkthrough images come from fresh Basic, Compose and Kubernetes setups. The quickstart capture reloads the provider after pulling a model and asks a short question without tool use. New public pages also need the deployment’s front nginx allowlist updated.

The marketing layout uses compact section spacing, a connected four-benefit strip, a collapsible architecture diagram, a coaching preview beside the findings, and a two-column product gallery with a leading live-session image. Full screenshots remain available from every image. The homepage has one closing quickstart panel; its former closing CTA anchor remains for existing links. The shared shell and detail pages use the same tighter spacing and typography.

The quickstart’s “See what it sent” step uses a shared inspector example from a real, payload-captured Qwen Code call in the one-container demo, labelled as an example in all three guides. It shows the selected request in full screen with the Conversation tab open, rather than an unselected request list. The capture script enables payload capture before launch, filters requests to the new session, requires a successful captured call and opens its inspector before saving. Existing screenshot filenames are retained.

Published-image quickstart commands use `run --pull=always` for Docker/Podman containers and `compose up -d --pull always` for Compose, including CPU, GPU and PowerShell variants. This checks for the newest image at the configured tag; mesh images default to the last promoted `latest` build. From-source `--build` examples keep their existing behavior. The Kubernetes quickstart already sets the server image pull policy to `Always`.

The quickstart's fifth level, **Agent only** (`#lvl-agent-only`, a card across the row under the four server levels; the guide is `#agent-only`, so existing links still open it), is for computers connecting to an existing server: install, sign in, `agent --list`, start an agent, then its own Stop or remove it section. A terminal switch picks the Unix or PowerShell command; both select only `agent` and skip the optional tray. The homepage and README link to it. `check.mjs` runs it through the same level loop as the server guides.
