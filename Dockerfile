FROM node:22-alpine AS ui-builder
WORKDIR /src
COPY src/package*.json ./src/
RUN npm ci --prefix ./src
COPY src/ ./src/
RUN npm run build --prefix ./src

FROM golang:1.24.4 AS go-builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64
RUN go build -trimpath -ldflags="-s -w" -o /out/browser-ui ./cmd/browser-ui

FROM gcr.io/distroless/static:nonroot
WORKDIR /app
COPY --from=go-builder /out/browser-ui /app/browser-ui
COPY --from=ui-builder --chown=65532:65532 /src/static/ /app/static/
USER 65532:65532
ENV STATIC_PATH=/app/static \
    PORT=8080 \
    NAMESPACE=default \
    BROWSER_SERVICE_URL=http://browser-service:8080
EXPOSE 8080
ENTRYPOINT ["/app/browser-ui"]
