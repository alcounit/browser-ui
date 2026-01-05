# browser-ui

browser-ui is a small web UI service for browsing active browser sessions and opening a VNC connection. It serves a React/Vite static UI and a minimal JSON API backed by the Browser Service event stream.

https://github.com/user-attachments/assets/33abd7d7-e7d3-48f7-9b9f-d6dbe72e4f32

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
| `VNC_PASSWORD` | `secret` | VNC password for connection. |
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


## Build and image workflow

The project is built and packaged entirely via Docker. Local Go installation is not required for producing the final artifact.

## Build variables

The build process is controlled via the following Makefile variables:

Variable	Description
- BINARY_NAME	Name of the produced binary (browser-ui).
- DOCKER_REGISTRY	Docker registry prefix (passed via environment).
- IMAGE_NAME	Full image name (<registry>/browser-ui).
- VERSION	Image version/tag (default: :v0.0.1).
- PLATFORM	Target platform (default: linux/amd64).

DOCKER_REGISTRY is expected to be provided externally, which allows the same Makefile to be used locally and in CI.

## Deployment

To be added....

## Notes
- The VNC proxy uses WebSocket and expects the sidecar to be available at port `4445` inside the browser pod.
- `UI_STATIC_PATH` must point to the built assets directory (Vite output).
