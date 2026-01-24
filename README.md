# Browser UI

Browser UI is a lightweight web application for the **Selenosis** ecosystem.  
It provides a simple HTTP server that serves a static frontend and exposes a minimal backend API for browsing sessions and connecting to VNC.

<p align="center">
  <a href="https://github.com/user-attachments/assets/b5fb55e2-2354-4dbb-8bab-25e7c78bc66c" target="_blank">
    <img src="https://github.com/user-attachments/assets/b5fb55e2-2354-4dbb-8bab-25e7c78bc66c"
         width="420"
         alt="Browser UI dashboard" />
  </a>

  <a href="https://github.com/user-attachments/assets/95eae48b-d4a6-4638-a0fc-251cdd1d2ff0" target="_blank">
    <img src="https://github.com/user-attachments/assets/95eae48b-d4a6-4638-a0fc-251cdd1d2ff0"
         width="420"
         alt="Browser UI VNC view" />
  </a>
</p>

---

## Overview

- **Frontend**: static assets built with Node and served under `/ui/`
- **Backend**: Go HTTP server providing a small API and VNC WebSocket proxy
- **Event collector**: subscribes to `browser-service` events and maintains an in-memory session store

Browser UI is intentionally stateless: Kubernetes remains the source of truth and `browser-service` is the API boundary.

---

## Responsibilities

- Serve the web UI (static frontend)
- Provide a simple JSON API for listing and inspecting sessions
- Proxy VNC traffic from the UI to the underlying browser pod
- Track sessions in memory by consuming browser events

---

## Dependency on browser-service

Browser UI **depends on browser-service** for:

- REST access to `Browser` resources
- Event stream (ADDED / MODIFIED / DELETED) used to populate the UI session store

The UI assumes that:
- `browser-service` is reachable at `BROWSER_SERVICE_URL`
- `browser-controller` and CRDs are already installed in the cluster (indirect dependency via browser-service)

---

## HTTP Endpoints

### UI

- `GET /` → redirects to `/ui/`
- `GET /ui/` → UI entrypoint (`index.html`)
- `GET /ui/*` → static assets

### API

Base path:

```
/api/v1
```

Endpoints:

- `GET /api/v1/browsers/`  
  List sessions known to the UI (in-memory view)

- `GET /api/v1/browsers/{browserId}/`  
  Get a single session by Browser ID

- `GET /api/v1/browsers/{browserId}/vnc`  
  WebSocket proxy to the browser VNC endpoint

- `GET /api/v1/browsers/{browserId}/vnc/settings`  
  Returns VNC settings (currently returns the password)

### Health

- `GET /health` → returns `{"status":"ok"}`

---

## VNC Connectivity

Browser UI exposes a WebSocket endpoint (`/vnc`) that proxies traffic to the browser pod VNC WebSocket:

- Backend target (resolved from session data):
  - `ws://<browserPodIP>:4445/selenosis/v1/vnc/<sessionId>`

The UI also exposes `/vnc/settings` for clients that need the VNC password.

---

## Configuration

Browser UI is configured using environment variables:

- `LISTEN_ADDR` — address to listen on (default `:8080`)
- `BROWSER_SERVICE_URL` — browser-service base URL (default `http://browser-service:8080`)
- `BROWSER_NAMESPACE` — namespace used for subscriptions (default `default`)
- `VNC_PASSWORD` — VNC password exposed via `/vnc/settings` (default `secret`)
- `UI_STATIC_PATH` — path to static UI assets (default `/app/static`)

---

## Build and image workflow

The project is built and packaged entirely via Docker. Local Go installation is not required for producing the final artifact.

## Build variables

The build process is controlled via the following Makefile variables:

Variable	Description
- BINARY_NAME	Name of the produced binary (browser-ui).
- REGISTRY	Docker registry prefix (default: localhost:5000).
- IMAGE_NAME	Full image name (<registry>/browser-ui).
- VERSION	Image version/tag (default: develop).
- PLATFORM	Target platform (default: linux/amd64).
- CONTAINER_TOOL docker cmd

REGISTRY, VERSION is expected to be provided externally, which allows the same Makefile to be used locally and in CI.

---

## Deployment

Minimal configuration

```yaml
apiVersion: v1
kind: Service
metadata:
  name: browser-ui-service
  namespace: default
  labels:
    app: browser-ui
spec:
  type: NodePort
  selector:
    app: browser-ui
  ports:
  - name: http
    port: 8080       
    targetPort: 8080
```

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: browser-ui
  namespace: default
spec:
  replicas: 1
  selector:
    matchLabels:
      app: browser-ui
  template:
    metadata:
      labels:
        app: browser-ui
    spec:
      containers:
      - name: browser-ui
        image: alcounit/browser-ui:latest
        imagePullPolicy: IfNotPresent
        ports:
        - containerPort: 8080
          name: http
        env:
        - name: APP_LISTEN_ADDR
          value: "8080"
        - name: BROWSER_SERVICE_URL
          value: "http://browser-service:8080"
        - name: BROWSER_NAMESPACE
          value: "default"
        - name: UI_STATIC_PATH
          value: "/app/static" 
        resources:
          requests:
            memory: "64Mi"
            cpu: "100m"
          limits:
            memory: "256Mi"
            cpu: "500m"
```
