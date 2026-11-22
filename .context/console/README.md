# Console (`ui/`)

The web console: React 19, TypeScript (strict), Vite, TanStack Query, react-router 7. It is a pure client of the server's API. The server serves the build from `ui/dist` and rebuilds it on start when sources are newer (`server/agent_server/ui_build.py`).

## Files in this folder

| File | Covers |
| --- | --- |
| [README.md](README.md) | This page: the design, boundaries and rules |
| [shell.md](shell.md) | The app shell, navigation and routes. |
| [data-layer.md](data-layer.md) | `api.ts`, query keys, polling, live updates, and page-specific data rules. |
| [terminals.md](terminals.md) | The terminal viewer and the Live agent view (terminal grid). |
| [pages.md](pages.md) | The Machine page, the organization workspace and the peering pages. |
| [settings.md](settings.md) | The server settings pages. |
| [workflow.md](workflow.md) | Adding to the console, checking changes visually, and writing `docs/` pages. |

## Design in one paragraph

Every byte the console shows comes from the server over HTTP, server-sent events (`/api/live`) or WebSockets (`/api/terminals/{id}/view`). `VITE_API_URL` chooses the API origin at build time; unset means same-origin. `api.ts` owns `API_ORIGIN`, `apiUrl`, credentialed fetches and the socket URL; SSE uses `withCredentials`. Documentation, downloads and installer/login commands use the API origin too. All server state is TanStack Query cache, keyed by short arrays and refreshed by polling or invalidation. Navigation is data (`navigation.ts`), and the layout is one stylesheet (`styles.css`) with CSS variables for light and dark themes: the tokens at its top (`--paper`, `--ground`, `--rail`, `--hover`, `--ink`, `--steel`, `--faint`, `--line`, `--accent`, `--radius`, `--shadow`, ...), then the controls, the shell and the shared surfaces (panels, cards and tables share one look: hairline border, 14 px radius, soft shadow). Page tabs are underlined (`.tabs.page-tabs`, `.machine-nav`); second-level tabs are segmented controls (`.tabs.segmented`), so a level never reads as another row of the same tabs, and a machine's providers are a side list (`.provider-side`), never tabs. Anything about a model server lives on its machine's page. The console never works out routing decisions itself: which backends an agent can use comes from `/api/agents`.

## Boundaries

llama.cpp's Termux install chooser can suggest `opencl-adreno` with explicit source-build/profile help. Owned source builds report `source: source` and can be removed like GitHub builds. Installation requires saved/reverted edits and disables settings during the job; completion refreshes the form even on a fast cache hit so stale settings cannot overwrite the hardware profile. The demo provides this choice; `docs/screenshots/llamacpp-adreno.png` shows it.

The standalone nginx image and server-hosted SPA send `frame-ancestors 'none'`, `X-Frame-Options: DENY`, `nosniff`, `same-origin` referrer policy, and object/base-URL restrictions. These do not change page layout. nginx location caching must preserve the server-level security headers.

- Only the server's API. No imports from other components, and no logic the server owns (backend choice, agent templates).
- The server's SPA fallback returns `index.html` for unknown paths but 404 under `api/`, `v1/`, `session/`, `anthropic/`, `downloads/`, `help-files/`. So the SPA must not use `/downloads/`, `/android` (files) or `/docs` (OpenAPI). That's why Downloads is `/get` and docs are `/help`.
- Every build replaces the lazy chunks (`mermaid.js`, the terminal, containers). So the server sends `index.html` and the docs with `Cache-Control: no-cache` and `/assets/` as immutable, `main.tsx` reloads the page once on `vite:preloadError` (a console from before an update), and the Docs page says so on each diagram if `mermaid.js` still won't load, trying again next time.
- Standalone: `ui/Dockerfile` builds a static nginx image; `ui/nginx.conf` caches hashed assets, revalidates the SPA and refuses API paths. Compose's `console` profile exposes it on loopback :8081. The API must trust its exact origin through `AI_PROXY_CONSOLE_ORIGINS`; both hosts must be HTTPS subdomains of the same site for SameSite=Lax cookies.
- Dev: `npm run dev` on :5173 proxies `/api` (with `ws: true`), `/health`, the API reference (`/docs`, `/redoc`, `/openapi.json`), `/help-files`, `/downloads`, the installers and `/android` to `AI_PROXY_URL` (default `http://127.0.0.1:9999`). With `VITE_API_URL`, it calls that origin directly instead.

## Module map (where things go)

- `src/`: `api.ts` (calls and types), `hooks.tsx` (shared queries, theme, toasts, clipboard), `live.ts`, `traffic.ts` (host and path classification), `format.ts`, `routes.ts` (`ROUTES`, `PROVIDERS`, `providerLabel()`), `navigation.ts`.
- `pages/`: one file per page. `Docs.tsx` renders `docs/*.md` from `/help-files/` with marked, and loads mermaid lazily. `markdown.ts` holds what it shares with the file viewer: `escapeHtml`, `slug`, `mermaidBlock`, `renderDiagrams`/`watchDiagrams`.
- `components/`: reuse before writing new ones: `Tabs` (`useTabParam`, `TabBar`, `TabPanel`, which unmounts inactive panels and so stops their polling), `CodeLine`, `ConfirmDialog`, `ModelSearch` (`flavor="llamacpp"`: Hugging Face only, names `user/repo:quant`, a pull method beside Pull and the **By hand** tab, `ManualDownload`/`llamaCurlCommands`, over `api.llamaFiles`/`api.llamaReload`)/`usePull(target, label, onPulled)` (`start(ref, method?)`), `ProviderState`/`StateDot`, `LogPanel`, `RouteBadge`, `AddBackendForm`, `Sharing`, `McpChoice`, `FolderPicker`, `Attention` (`AttentionAlerts`, `waitingAgents`, `NotifyToggle`), `Restart` (`RestartProvider`, `RestartButton`), `Icon` (inline SVG only), `DetailView` (`useDetailView`, `DetailBack`: details that take the page from their list on a narrow screen).

`components/AgentPicker.tsx` owns Start an agent's dropdown for both commands and machines. It always groups by the server's route (`ROUTE_ORDER`/`ROUTES`); a machine's missing agents are struck through and marked Not installed, still selectable for install instructions. The button/listbox supports arrow keys, Home/End, first-letter navigation, Enter, Escape, Tab and outside-click dismissal. Machine availability never changes the route grouping.

## Styling

`styles.css` is the only stylesheet, with no framework. Tokens are on `:root` and overridden on `:root[data-theme="dark"]` (`--paper`, `--ground`, `--ink`, `--accent` indigo, `--shadow*`, `--route-*`). The fonts are Inter and JetBrains Mono. An inline script in `index.html` sets the theme before React loads. Check dark mode and a ~412 px viewport (the Android app) for every layout change.

## Gotchas

- The auth gate and public pages work from `window.location`; add a route to `PublicDocs`/`PublicDownloads` only if it must work signed out.
- A poll whose `refetchInterval` is a function returning `false` on error stops for good after one failure (a lapsed sign-in, a dropped network); use `pollEvery` instead, which only stops on 403.
- UI text: plain sentence case, providers named through `providerLabel()`.

Ask a model scopes asynchronous session creation and reply updates to a conversation generation. Reset and unmount invalidate the previous generation; a session created after cancellation is ended immediately. Old requests cannot change the new conversation or clear its busy state.

AI review setup uses a fieldset of native radio choices, each showing the server-provided title and about text. A separate setup panel groups the model, optional question, selected focus and start action. Both grids stack below 700 px; prompt definitions and routing remain server-owned.
