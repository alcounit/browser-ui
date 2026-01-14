package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alcounit/browser-ui/pkg/types"
	"github.com/alcounit/seleniferous/v2/pkg/store"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
)

func requestWithParam(method, path, key, value string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add(key, value)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
}

func TestGetBrowserNotFound(t *testing.T) {
	svc := NewService(store.NewDefaultStore())
	req := requestWithParam(http.MethodGet, "/browsers/missing", "browserId", "missing")
	rw := httptest.NewRecorder()

	svc.GetBrowser(rw, req)

	if rw.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", rw.Code)
	}
}

func TestGetBrowserEncodeError(t *testing.T) {
	st := store.NewDefaultStore()
	st.Set("bad", func() {})
	svc := NewService(st)
	req := requestWithParam(http.MethodGet, "/browsers/bad", "browserId", "bad")
	rw := httptest.NewRecorder()

	svc.GetBrowser(rw, req)

	if rw.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", rw.Code)
	}
}

func TestGetBrowserSuccess(t *testing.T) {
	st := store.NewDefaultStore()
	st.Set("browser-1", &types.Session{
		SessionId:      "sess-1",
		BrowserId:      "browser-1",
		BrowserName:    "chrome",
		BrowserVersion: "123",
	})
	svc := NewService(st)
	req := requestWithParam(http.MethodGet, "/browsers/browser-1", "browserId", "browser-1")
	rw := httptest.NewRecorder()

	svc.GetBrowser(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rw.Code)
	}

	var got types.Session
	if err := json.Unmarshal(rw.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if got.SessionId != "sess-1" {
		t.Fatalf("expected sessionId sess-1, got %s", got.SessionId)
	}
}

func TestListBrowsersSuccess(t *testing.T) {
	st := store.NewDefaultStore()
	st.Set("browser-1", &types.Session{SessionId: "sess-1"})
	svc := NewService(st)
	req := httptest.NewRequest(http.MethodGet, "/browsers", nil)
	rw := httptest.NewRecorder()

	svc.ListBrowsers(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rw.Code)
	}

	var got []types.Session
	if err := json.Unmarshal(rw.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 session, got %d", len(got))
	}
}

func TestListBrowsersEncodeError(t *testing.T) {
	st := store.NewDefaultStore()
	st.Set("bad", func() {})
	svc := NewService(st)
	req := httptest.NewRequest(http.MethodGet, "/browsers", nil)
	rw := httptest.NewRecorder()

	svc.ListBrowsers(rw, req)

	if rw.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", rw.Code)
	}
}

func TestRouteVNCInvalidSession(t *testing.T) {
	svc := NewService(store.NewDefaultStore())
	req := requestWithParam(http.MethodGet, "/vnc/unknown", "browserId", "unknown")
	rw := httptest.NewRecorder()

	svc.RouteVNC(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rw.Code)
	}
}

type fakeWSMessage struct {
	mt   int
	data []byte
	err  error
}

type fakeWSConn struct {
	readCh   chan fakeWSMessage
	writeCh  chan fakeWSMessage
	closed   bool
	writeErr error
}

func newFakeWSConn() *fakeWSConn {
	return &fakeWSConn{
		readCh:  make(chan fakeWSMessage, 4),
		writeCh: make(chan fakeWSMessage, 4),
	}
}

func (c *fakeWSConn) ReadMessage() (int, []byte, error) {
	msg, ok := <-c.readCh
	if !ok {
		return 0, nil, io.EOF
	}
	if msg.err != nil {
		return 0, nil, msg.err
	}
	return msg.mt, msg.data, nil
}

func (c *fakeWSConn) WriteMessage(mt int, data []byte) error {
	if c.writeErr != nil {
		return c.writeErr
	}
	c.writeCh <- fakeWSMessage{mt: mt, data: data}
	return nil
}

func (c *fakeWSConn) Close() error {
	c.closed = true
	return nil
}

func TestRouteVNCSuccessProxy(t *testing.T) {
	clientConn := newFakeWSConn()
	backendConn := newFakeWSConn()

	prevUpgrade := wsUpgrade
	prevDial := wsDial
	wsUpgrade = func(rw http.ResponseWriter, req *http.Request) (wsConn, error) {
		return clientConn, nil
	}
	wsDial = func(target string) (wsConn, error) {
		return backendConn, nil
	}
	defer func() {
		wsUpgrade = prevUpgrade
		wsDial = prevDial
	}()

	st := store.NewDefaultStore()
	st.Set("browser-1", &types.Session{
		SessionId: "sess-1",
		BrowserId: "browser-1",
		BrowserIP: "127.0.0.1",
	})
	svc := NewService(st)

	req := requestWithParam(http.MethodGet, "/api/v1/browsers/browser-1/vnc", "browserId", "browser-1")
	rw := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		svc.RouteVNC(rw, req)
		close(done)
	}()

	clientConn.readCh <- fakeWSMessage{mt: websocket.TextMessage, data: []byte("ping")}
	backendConn.readCh <- fakeWSMessage{mt: websocket.TextMessage, data: []byte("pong")}
	close(clientConn.readCh)
	close(backendConn.readCh)

	select {
	case msg := <-backendConn.writeCh:
		if string(msg.data) != "ping" {
			t.Fatalf("expected backend to receive ping, got %s", string(msg.data))
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timeout waiting for backend write")
	}

	select {
	case msg := <-clientConn.writeCh:
		if string(msg.data) != "pong" {
			t.Fatalf("expected client to receive pong, got %s", string(msg.data))
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timeout waiting for client write")
	}

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timeout waiting for handler to finish")
	}
}

func TestRouteVNCClientReadError(t *testing.T) {
	clientConn := newFakeWSConn()
	backendConn := newFakeWSConn()

	prevUpgrade := wsUpgrade
	prevDial := wsDial
	wsUpgrade = func(rw http.ResponseWriter, req *http.Request) (wsConn, error) {
		return clientConn, nil
	}
	wsDial = func(target string) (wsConn, error) {
		return backendConn, nil
	}
	defer func() {
		wsUpgrade = prevUpgrade
		wsDial = prevDial
	}()

	st := store.NewDefaultStore()
	st.Set("browser-1", &types.Session{
		SessionId: "sess-1",
		BrowserId: "browser-1",
		BrowserIP: "127.0.0.1",
	})
	svc := NewService(st)

	req := requestWithParam(http.MethodGet, "/api/v1/browsers/browser-1/vnc", "browserId", "browser-1")
	rw := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		svc.RouteVNC(rw, req)
		close(done)
	}()

	clientConn.readCh <- fakeWSMessage{err: errors.New("read failed")}
	close(backendConn.readCh)

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timeout waiting for handler to finish")
	}
}

func TestRouteVNCBackendReadError(t *testing.T) {
	clientConn := newFakeWSConn()
	backendConn := newFakeWSConn()

	prevUpgrade := wsUpgrade
	prevDial := wsDial
	wsUpgrade = func(rw http.ResponseWriter, req *http.Request) (wsConn, error) {
		return clientConn, nil
	}
	wsDial = func(target string) (wsConn, error) {
		return backendConn, nil
	}
	defer func() {
		wsUpgrade = prevUpgrade
		wsDial = prevDial
	}()

	st := store.NewDefaultStore()
	st.Set("browser-1", &types.Session{
		SessionId: "sess-1",
		BrowserId: "browser-1",
		BrowserIP: "127.0.0.1",
	})
	svc := NewService(st)

	req := requestWithParam(http.MethodGet, "/api/v1/browsers/browser-1/vnc", "browserId", "browser-1")
	rw := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		svc.RouteVNC(rw, req)
		close(done)
	}()

	backendConn.readCh <- fakeWSMessage{err: errors.New("read failed")}
	close(clientConn.readCh)

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timeout waiting for handler to finish")
	}
}

func TestRouteVNCBackendWriteError(t *testing.T) {
	clientConn := newFakeWSConn()
	backendConn := newFakeWSConn()
	backendConn.writeErr = errors.New("write failed")

	prevUpgrade := wsUpgrade
	prevDial := wsDial
	wsUpgrade = func(rw http.ResponseWriter, req *http.Request) (wsConn, error) {
		return clientConn, nil
	}
	wsDial = func(target string) (wsConn, error) {
		return backendConn, nil
	}
	defer func() {
		wsUpgrade = prevUpgrade
		wsDial = prevDial
	}()

	st := store.NewDefaultStore()
	st.Set("browser-1", &types.Session{
		SessionId: "sess-1",
		BrowserId: "browser-1",
		BrowserIP: "127.0.0.1",
	})
	svc := NewService(st)

	req := requestWithParam(http.MethodGet, "/api/v1/browsers/browser-1/vnc", "browserId", "browser-1")
	rw := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		svc.RouteVNC(rw, req)
		close(done)
	}()

	clientConn.readCh <- fakeWSMessage{mt: websocket.TextMessage, data: []byte("ping")}
	close(clientConn.readCh)
	close(backendConn.readCh)

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timeout waiting for handler to finish")
	}
}

func TestRouteVNCClientWriteError(t *testing.T) {
	clientConn := newFakeWSConn()
	clientConn.writeErr = errors.New("write failed")
	backendConn := newFakeWSConn()

	prevUpgrade := wsUpgrade
	prevDial := wsDial
	wsUpgrade = func(rw http.ResponseWriter, req *http.Request) (wsConn, error) {
		return clientConn, nil
	}
	wsDial = func(target string) (wsConn, error) {
		return backendConn, nil
	}
	defer func() {
		wsUpgrade = prevUpgrade
		wsDial = prevDial
	}()

	st := store.NewDefaultStore()
	st.Set("browser-1", &types.Session{
		SessionId: "sess-1",
		BrowserId: "browser-1",
		BrowserIP: "127.0.0.1",
	})
	svc := NewService(st)

	req := requestWithParam(http.MethodGet, "/api/v1/browsers/browser-1/vnc", "browserId", "browser-1")
	rw := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		svc.RouteVNC(rw, req)
		close(done)
	}()

	backendConn.readCh <- fakeWSMessage{mt: websocket.TextMessage, data: []byte("pong")}
	close(backendConn.readCh)
	close(clientConn.readCh)

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("timeout waiting for handler to finish")
	}
}

func TestRouteVNCUpgradeFailure(t *testing.T) {
	prevUpgrade := wsUpgrade
	wsUpgrade = func(rw http.ResponseWriter, req *http.Request) (wsConn, error) {
		return nil, errors.New("upgrade failed")
	}
	defer func() {
		wsUpgrade = prevUpgrade
	}()

	st := store.NewDefaultStore()
	st.Set("browser-1", &types.Session{
		SessionId: "sess-1",
		BrowserId: "browser-1",
		BrowserIP: "127.0.0.1",
	})
	svc := NewService(st)

	req := requestWithParam(http.MethodGet, "/api/v1/browsers/browser-1/vnc", "browserId", "browser-1")
	rw := httptest.NewRecorder()

	svc.RouteVNC(rw, req)
}

func TestRouteVNCBackendDialFailure(t *testing.T) {
	clientConn := newFakeWSConn()

	prevUpgrade := wsUpgrade
	prevDial := wsDial
	wsUpgrade = func(rw http.ResponseWriter, req *http.Request) (wsConn, error) {
		return clientConn, nil
	}
	wsDial = func(target string) (wsConn, error) {
		return nil, errors.New("dial failed")
	}
	defer func() {
		wsUpgrade = prevUpgrade
		wsDial = prevDial
	}()

	st := store.NewDefaultStore()
	st.Set("browser-1", &types.Session{
		SessionId: "sess-1",
		BrowserId: "browser-1",
		BrowserIP: "127.0.0.1",
	})
	svc := NewService(st)

	req := requestWithParam(http.MethodGet, "/api/v1/browsers/browser-1/vnc", "browserId", "browser-1")
	rw := httptest.NewRecorder()

	svc.RouteVNC(rw, req)
	if !clientConn.closed {
		t.Fatalf("expected client connection to be closed")
	}
}

func TestDefaultWSUpgradeFailure(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/ws", nil)
	rw := httptest.NewRecorder()

	if _, err := wsUpgrade(rw, req); err == nil {
		t.Fatalf("expected upgrade error")
	}
}

func TestDefaultWSDialFailure(t *testing.T) {
	if _, err := wsDial("ws://127.0.0.1:0"); err == nil {
		t.Fatalf("expected dial error")
	}
}

func TestIsNormalWSDisconnect(t *testing.T) {
	if isNormalWSDisconnect(nil) {
		t.Fatalf("expected nil error to be false")
	}

	closeErr := &websocket.CloseError{Code: websocket.CloseGoingAway}
	if !isNormalWSDisconnect(closeErr) {
		t.Fatalf("expected close error to be true")
	}

	if !isNormalWSDisconnect(io.EOF) {
		t.Fatalf("expected EOF to be true")
	}

	if isNormalWSDisconnect(errors.New("other")) {
		t.Fatalf("expected non-normal error to be false")
	}
}
