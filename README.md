# browser-ui

browser-ui is a small web UI service for browsing active browser sessions and opening a VNC connection. It serves a React/Vite static UI and a minimal JSON API backed by the Browser Service event stream.

## What it does
- Builds the frontend from `src/` (Vite + React).
- Serves the UI at `/ui` and redirects `/` to `/ui/`.
- Exposes an API for listing sessions and reading session details.
- Proxies VNC WebSocket connections to the sidecar in the browser pod.

## Requirements
- Browser Service running and reachable via `BROWSER_SERVICE_URL`.
- Browser Controller CRDs installed in the cluster.
- Browser pods run a `seleniferous` sidecar on port `4445` (used for VNC proxy).

## Environment variables
| Variable | Default | Description |
| --- | --- | --- |
| `LISTEN_ADDR` | `:8080` | HTTP listen address. |
| `BROWSER_SERVICE_URL` | `http://browser-service:8080` | Browser Service API base URL. |
| `BROWSER_NAMESPACE` | `default` | Namespace to watch for `Browser` resources. |
| `UI_STATIC_PATH` | `/app/static` | Path to built UI assets. |

## HTTP endpoints

UI:
- `GET /ui` and `GET /ui/` serve `index.html`.
- `GET /ui/*` serves static assets.
- `GET /` redirects to `/ui/`.

API:
- `GET /api/v1/browsers/` returns the list of active sessions.
- `GET /api/v1/browsers/{browserId}/` returns a single session.
- `GET /api/v1/browsers/{browserId}/vnc` upgrades to a WebSocket and proxies VNC.

Health:
- `GET /health` returns `{"status":"ok"}`.

## How it works
- At startup, the service starts a collector that subscribes to the Browser Service `/events` stream for the configured namespace.
- It stores sessions in an in-memory store keyed by `browserId`.
- The UI reads data from the API and opens VNC via the WebSocket endpoint.

## Build and run (local)

Frontend build:
```bash
cd src
npm ci
npm run build
```

Backend build:
```bash
go mod download
go build -o bin/browser-ui ./cmd/browser-ui
```

Run:
```bash
UI_STATIC_PATH=./src/dist \
BROWSER_SERVICE_URL=http://localhost:8080 \
BROWSER_NAMESPACE=default \
./bin/browser-ui
```

## Docker
The Dockerfile builds the UI and the Go server in separate stages and produces a distroless image.

```bash
docker build -t browser-ui:local .

docker run --rm -p 8080:8080 \
  -e BROWSER_SERVICE_URL=http://browser-service:8080 \
  -e BROWSER_NAMESPACE=default \
  browser-ui:local
```

## Makefile
Useful targets:
- `make docker-build`
- `make docker-push`
- `make deploy`

## Notes
- The VNC proxy uses WebSocket and expects the sidecar to be available at port `4445` inside the browser pod.
- `UI_STATIC_PATH` must point to the built assets directory (Vite output).
