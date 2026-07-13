# browser-ui

**Web dashboard and live VNC viewer for the [Selenosis](https://github.com/alcounit/selenosis) ecosystem.**
A stateless Go server that serves a React frontend, lists live browser sessions, and proxies VNC to the browser pods — Kubernetes stays the source of truth.

[![GitHub release](https://img.shields.io/github/v/release/alcounit/browser-ui)](https://github.com/alcounit/browser-ui/releases)
[![Go Reference](https://pkg.go.dev/badge/github.com/alcounit/browser-ui.svg)](https://pkg.go.dev/github.com/alcounit/browser-ui)
[![Docker Pulls](https://img.shields.io/docker/pulls/alcounit/browser-ui.svg)](https://hub.docker.com/r/alcounit/browser-ui)
[![License](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](./LICENSE)

<p align="center">
  <a href="https://github.com/user-attachments/assets/b5fb55e2-2354-4dbb-8bab-25e7c78bc66c" target="_blank">
    <img src="https://github.com/user-attachments/assets/b5fb55e2-2354-4dbb-8bab-25e7c78bc66c" width="420" alt="Browser UI dashboard" />
  </a>
  <a href="https://github.com/user-attachments/assets/95eae48b-d4a6-4638-a0fc-251cdd1d2ff0" target="_blank">
    <img src="https://github.com/user-attachments/assets/95eae48b-d4a6-4638-a0fc-251cdd1d2ff0" width="420" alt="Browser UI VNC view" />
  </a>
</p>

---

## How it fits

browser-ui is the dashboard of a small Kubernetes-native platform. It never talks to the cluster directly — it consumes **browser-service** and proxies VNC to the pods.

| Component | Role |
| --- | --- |
| **[selenosis](https://github.com/alcounit/selenosis)** | Stateless Selenium / Playwright / MCP hub. |
| **[seleniferous](https://github.com/alcounit/seleniferous)** | Sidecar proxy inside each browser pod (incl. the VNC endpoint). |
| **[browser-controller](https://github.com/alcounit/browser-controller)** | Operator reconciling `Browser` / `BrowserConfig` CRDs into pods. |
| **[browser-service](https://github.com/alcounit/browser-service)** | REST + SSE facade over the CRDs. **browser-ui talks only to this.** |
| **browser-ui** (this repo) | Web dashboard, live session list, in-browser VNC viewer. |
| **[selenosis-deploy](https://github.com/alcounit/selenosis-deploy)** | Helm chart that deploys the whole stack. |

---

## How it works

- **Frontend** — React/TypeScript (noVNC, TanStack Query), built with Vite and served under `/ui/`.
- **Backend** — Go HTTP server (chi/v5, zerolog) exposing a small JSON API and a VNC WebSocket proxy.
- **Event collector** — subscribes to the `browser-service` SSE stream (ADDED / MODIFIED / DELETED) and keeps an **in-memory** session store derived from `Browser` resources.

browser-ui is stateless: restart it freely, run multiple replicas. It depends on `browser-service` being reachable at `BROWSER_SERVICE_URL` (and, indirectly, on the controller and CRDs being installed).

---

## VNC viewer

The viewer (`GET /api/v1/browsers/{id}/vnc`) is a WebSocket proxy to the session pod's seleniferous VNC endpoint. The browser image's VNC server is password-protected, and vendors set that password differently (some bake it into the image), so there is **no global server-side password** — the user supplies it in the UI and it is resolved on the client from what was saved before:

1. password saved for this browser **name + version** (`localStorage`, e.g. `chrome@146.0`),
2. password saved for this browser **name** (`localStorage`, any version).

**On first use (nothing saved) or when the password doesn't match**, the viewer shows an inline prompt so the user types the password. After a successful connect it offers to remember it:

- **For all browsers of this name** — reused for every version of that browser (e.g. all `chrome`).
- **Only this browser version** — reused only for that exact `name + version` (e.g. `chrome 146.0`).
- **Don't save** — used once, nothing stored.

Both scopes persist in `localStorage` (the session id is random and dies with the pod, so it is never used as a key). The version-scoped password takes priority over the name-scoped one.

Wrong passwords surface a clear `securityfailure` message and re-prompt; a hard attempt cap prevents retry loops. This keeps a single deployment usable across mixed browser vendors without forcing one shared VNC password.

---

## Configuration

Configured via environment variables (read in `cmd/browser-ui/main.go`):

| Variable | Default | Description |
| --- | --- | --- |
| `LISTEN_ADDR` | `:8080` | HTTP listen address. |
| `BROWSER_SERVICE_URL` | `http://browser-service:8080` | `browser-service` base URL. |
| `BROWSER_NAMESPACE` | `default` | Namespace for session subscriptions. |
| `BROWSER_STARTUP_TIMEOUT` | `3m` | Max wait for a manually started browser to become ready. |
| `UI_STATIC_PATH` | `/app/static` | Path to the built frontend assets. |
| `BASIC_AUTH_FILE` | | Path to a JSON users file; when set, the UI requires login. |

Basic Auth is optional. When `BASIC_AUTH_FILE` is set, the UI gates the API behind a login (`/auth/login` issues an HttpOnly cookie) and the file is watched for hot reload.

---

## Endpoints

<details>
<summary><b>UI, API, auth, and health routes</b></summary>

**UI**
- `GET /` → redirects to `/ui/`
- `GET /ui/`, `GET /ui/*` → frontend entrypoint and static assets

**Auth**
- `GET /api/v1/auth/config` → whether auth is enabled
- `POST /api/v1/auth/login` / `POST /api/v1/auth/logout`

**Sessions** (under `/api/v1`, auth-gated when enabled)
- `GET /status/` → active sessions + supported browsers from the in-memory store
- `POST /browsers/` → create/start a session — body `{"browserName":"chrome","browserVersion":"146.0","selenosisOptions":{}}`
- `GET /browsers/{browserId}/` → single session
- `DELETE /browsers/{browserId}/` → delete a manually started session
- `GET /browsers/{browserId}/vnc` → VNC WebSocket proxy to the pod

**Health**
- `GET /health` → `{"status":"ok"}`

</details>

---

## Build

The project builds and packages entirely via Docker (multi-stage: Node for the frontend, Go for the backend) — a local Go/Node install is not required for the final image.

```bash
make test       # go tests
make test-ui    # frontend unit tests (vitest)
make docker-build
```

<details>
<summary><b>Makefile variables</b></summary>

| Variable | Description |
| --- | --- |
| `BINARY_NAME` | Produced binary name (fixed: `browser-ui`). |
| `REGISTRY` | Docker registry prefix (default `localhost:5000`). |
| `IMAGE_NAME` | Full image name, `$(REGISTRY)/$(BINARY_NAME)`. |
| `VERSION` | Image tag (default `develop`). |
| `EXTRA_TAGS` | Additional `-t` tags for `docker-push`. |
| `PLATFORM` | Target platform (default `linux/amd64`). |
| `CONTAINER_TOOL` | Container build tool (default `docker`). |
| `NPM` / `UI_DIR` | npm binary and frontend directory for `test-ui` (defaults `npm` / `src`). |

`REGISTRY` and `VERSION` are expected to be supplied externally so the same Makefile works locally and in CI.

</details>

---

## Deployment

Deployed as part of the full stack via the [selenosis-deploy](https://github.com/alcounit/selenosis-deploy) Helm chart.

## License

[Apache-2.0](./LICENSE)
