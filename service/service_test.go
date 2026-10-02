package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	browserv1 "github.com/alcounit/browser-controller/apis/browser/v1"
	"github.com/alcounit/browser-ui/pkg/types"
	"github.com/alcounit/seleniferous/v2/pkg/store"
	"github.com/alcounit/selenosis/v2/pkg/auth"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
)

func requestWithParam(method, path, key, value string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add(key, value)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
}

func vncWSRequest() *http.Request {
	req := requestWithParam(http.MethodGet, "/api/v1/browsers/browser-1/vnc", "browserId", "browser-1")
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	return req
}

const testHubURL = "http://selenosis:4444"

func TestGetBrowserNotFound(t *testing.T) {
	svc := NewService(testHubURL, store.NewDefaultStore[*types.Session](), store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)
	req := requestWithParam(http.MethodGet, "/browsers/missing", "browserId", "missing")
	rw := httptest.NewRecorder()

	svc.GetBrowser(rw, req)

	if rw.Code != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", rw.Code)
	}
}

type brokenWriter struct {
	*httptest.ResponseRecorder
}

func (b *brokenWriter) Write([]byte) (int, error) {
	return 0, errors.New("write error")
}

func TestGetBrowserEncodeError(t *testing.T) {
	st := store.NewDefaultStore[*types.Session]()
	st.Set("bad", &types.Session{SessionId: "bad"})
	svc := NewService(testHubURL, st, store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)
	req := requestWithParam(http.MethodGet, "/browsers/bad", "browserId", "bad")
	rec := httptest.NewRecorder()
	rw := &brokenWriter{rec}

	svc.GetBrowser(rw, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", rec.Code)
	}
}

func TestGetBrowserSuccess(t *testing.T) {
	st := store.NewDefaultStore[*types.Session]()
	st.Set("browser-1", &types.Session{
		SessionId:      "sess-1",
		BrowserId:      "browser-1",
		BrowserName:    "chrome",
		BrowserVersion: "123",
	})
	svc := NewService(testHubURL, st, store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)
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
	st := store.NewDefaultStore[*types.Session]()
	st.Set("browser-1", &types.Session{SessionId: "sess-1"})
	svc := NewService(testHubURL, st, store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)
	req := httptest.NewRequest(http.MethodGet, "/browsers", nil)
	rw := httptest.NewRecorder()

	svc.GetStatus(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rw.Code)
	}

	var got struct {
		Sessions []*types.Session        `json:"activeSessions"`
		Browsers []types.BrowserVersions `json:"supportedBrowsers"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(got.Sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(got.Sessions))
	}
}

func TestListBrowsersEncodeError(t *testing.T) {
	st := store.NewDefaultStore[*types.Session]()
	st.Set("sess-1", &types.Session{SessionId: "sess-1"})
	svc := NewService(testHubURL, st, store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)
	req := httptest.NewRequest(http.MethodGet, "/browsers", nil)
	rec := httptest.NewRecorder()
	rw := &brokenWriter{rec}

	svc.GetStatus(rw, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", rec.Code)
	}
}

func TestRouteVNCInvalidSession(t *testing.T) {
	svc := NewService(testHubURL, store.NewDefaultStore[*types.Session](), store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)
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

	st := store.NewDefaultStore[*types.Session]()
	st.Set("browser-1", &types.Session{
		SessionId: "sess-1",
		BrowserId: "browser-1",
		BrowserIP: "127.0.0.1",
	})
	svc := NewService(testHubURL, st, store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)

	req := vncWSRequest()
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

	st := store.NewDefaultStore[*types.Session]()
	st.Set("browser-1", &types.Session{
		SessionId: "sess-1",
		BrowserId: "browser-1",
		BrowserIP: "127.0.0.1",
	})
	svc := NewService(testHubURL, st, store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)

	req := vncWSRequest()
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

	st := store.NewDefaultStore[*types.Session]()
	st.Set("browser-1", &types.Session{
		SessionId: "sess-1",
		BrowserId: "browser-1",
		BrowserIP: "127.0.0.1",
	})
	svc := NewService(testHubURL, st, store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)

	req := vncWSRequest()
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

	st := store.NewDefaultStore[*types.Session]()
	st.Set("browser-1", &types.Session{
		SessionId: "sess-1",
		BrowserId: "browser-1",
		BrowserIP: "127.0.0.1",
	})
	svc := NewService(testHubURL, st, store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)

	req := vncWSRequest()
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

	st := store.NewDefaultStore[*types.Session]()
	st.Set("browser-1", &types.Session{
		SessionId: "sess-1",
		BrowserId: "browser-1",
		BrowserIP: "127.0.0.1",
	})
	svc := NewService(testHubURL, st, store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)

	req := vncWSRequest()
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

	st := store.NewDefaultStore[*types.Session]()
	st.Set("browser-1", &types.Session{
		SessionId: "sess-1",
		BrowserId: "browser-1",
		BrowserIP: "127.0.0.1",
	})
	svc := NewService(testHubURL, st, store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)

	req := vncWSRequest()
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

	st := store.NewDefaultStore[*types.Session]()
	st.Set("browser-1", &types.Session{
		SessionId: "sess-1",
		BrowserId: "browser-1",
		BrowserIP: "127.0.0.1",
	})
	svc := NewService(testHubURL, st, store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)

	req := vncWSRequest()
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

func TestDefaultWSUpgradeCheckOrigin(t *testing.T) {
	// Use a real HTTP test server so that CheckOrigin is invoked.
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		conn, err := wsUpgrade(rw, req)
		if err != nil {
			return
		}
		conn.Close()
	}))
	defer server.Close()

	wsURL := "ws" + server.URL[len("http"):]
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("expected successful ws dial, got %v", err)
	}
	conn.Close()
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

// fakeBrowserClient implements browserclient.Client for testing.
func TestGetStatusFiltersSessionsByOwner(t *testing.T) {
	st := store.NewDefaultStore[*types.Session]()
	st.Set("b-alice", &types.Session{SessionId: "s1", BrowserId: "b-alice", Owner: "alice"})
	st.Set("b-bob", &types.Session{SessionId: "s2", BrowserId: "b-bob", Owner: "bob"})
	st.Set("b-alice2", &types.Session{SessionId: "s3", BrowserId: "b-alice2", Owner: "alice"})

	svc := NewService(testHubURL, st, store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)
	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	req = req.WithContext(auth.WithOwner(req.Context(), auth.Owner{Name: "alice"}))
	rw := httptest.NewRecorder()

	svc.GetStatus(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rw.Code)
	}

	var got struct {
		Sessions []*types.Session `json:"activeSessions"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(got.Sessions) != 2 {
		t.Fatalf("expected 2 sessions for alice, got %d", len(got.Sessions))
	}
	for _, s := range got.Sessions {
		if s.Owner != "alice" {
			t.Fatalf("expected owner alice, got %s", s.Owner)
		}
	}
}

func TestGetStatusNoFilterWithoutOwner(t *testing.T) {
	st := store.NewDefaultStore[*types.Session]()
	st.Set("b-alice", &types.Session{SessionId: "s1", BrowserId: "b-alice", Owner: "alice"})
	st.Set("b-bob", &types.Session{SessionId: "s2", BrowserId: "b-bob", Owner: "bob"})

	svc := NewService(testHubURL, st, store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)
	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	rw := httptest.NewRecorder()

	svc.GetStatus(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rw.Code)
	}

	var got struct {
		Sessions []*types.Session `json:"activeSessions"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(got.Sessions) != 2 {
		t.Fatalf("expected 2 sessions (no filter), got %d", len(got.Sessions))
	}
}

type mockTransport struct {
	resp *http.Response
	err  error

	gotReq  *http.Request
	gotBody []byte
}

func (m *mockTransport) Do(req *http.Request) (*http.Response, error) {
	m.gotReq = req
	if req.Body != nil {
		m.gotBody, _ = io.ReadAll(req.Body)
	}
	return m.resp, m.err
}

func useTransport(t *testing.T, m *mockTransport) *mockTransport {
	t.Helper()
	prev := httpClient
	httpClient = m
	t.Cleanup(func() { httpClient = prev })
	return m
}

func hubResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body))}
}

func createdSessionBody(sessionId string) string {
	return fmt.Sprintf(`{"value":{"sessionId":%q}}`, sessionId)
}

func createBrowserRequest(t *testing.T, body string) *http.Request {
	t.Helper()
	return httptest.NewRequest(http.MethodPost, "/browsers", strings.NewReader(body))
}

func seededStore(t *testing.T, sessions ...*types.Session) store.Store[*types.Session] {
	t.Helper()
	st := store.NewDefaultStore[*types.Session]()
	for _, sess := range sessions {
		st.Set(sess.BrowserId, sess)
	}
	return st
}

func decodeSelenosisOptions(t *testing.T, body []byte) map[string]any {
	t.Helper()

	var sent struct {
		Capabilities struct {
			AlwaysMatch map[string]any `json:"alwaysMatch"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatalf("failed to decode sent body %q: %v", body, err)
	}

	opts, ok := sent.Capabilities.AlwaysMatch["selenosis:options"].(map[string]any)
	if !ok {
		t.Fatalf("expected selenosis:options in sent body, got %q", body)
	}
	return opts
}

func TestCreateBrowserNilBody(t *testing.T) {
	svc := NewService(testHubURL, store.NewDefaultStore[*types.Session](), store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)

	req := httptest.NewRequest(http.MethodPost, "/browsers", nil)
	req.Body = nil
	rw := httptest.NewRecorder()

	svc.CreateBrowser(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rw.Code)
	}
}

func TestCreateBrowserInvalidRequest(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "invalid json", body: "{"},
		{name: "empty browser name", body: `{"browserVersion":"120"}`},
		{name: "empty browser version", body: `{"browserName":"chrome"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewService(testHubURL, store.NewDefaultStore[*types.Session](), store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)
			rw := httptest.NewRecorder()

			svc.CreateBrowser(rw, createBrowserRequest(t, tt.body))

			if rw.Code != http.StatusBadRequest {
				t.Fatalf("expected status 400, got %d", rw.Code)
			}
		})
	}
}

func TestCreateBrowserPostsToHub(t *testing.T) {
	m := useTransport(t, &mockTransport{resp: hubResponse(http.StatusOK, createdSessionBody("s1"))})

	st := seededStore(t, &types.Session{BrowserId: "b1", SessionId: "s1"})
	svc := NewService(testHubURL, st, store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)
	rw := httptest.NewRecorder()

	svc.CreateBrowser(rw, createBrowserRequest(t, `{"browserName":"chrome","browserVersion":"120"}`))

	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rw.Code, rw.Body.String())
	}
	if m.gotReq == nil {
		t.Fatal("expected a request to the hub")
	}
	if got, want := m.gotReq.URL.String(), testHubURL+wdHubSessionPath; got != want {
		t.Fatalf("target = %q, want %q", got, want)
	}
	if m.gotReq.Method != http.MethodPost {
		t.Fatalf("method = %q, want POST", m.gotReq.Method)
	}
	if got := m.gotReq.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q", got)
	}

	var decoded types.Session
	if err := json.Unmarshal(rw.Body.Bytes(), &decoded); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if decoded.BrowserId != "b1" {
		t.Fatalf("browserId = %q, want b1", decoded.BrowserId)
	}
}

func TestCreateBrowserSendsStartedManuallyAnnotation(t *testing.T) {
	m := useTransport(t, &mockTransport{resp: hubResponse(http.StatusOK, createdSessionBody("s1"))})

	st := seededStore(t, &types.Session{BrowserId: "b1", SessionId: "s1"})
	svc := NewService(testHubURL, st, store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)
	rw := httptest.NewRecorder()

	svc.CreateBrowser(rw, createBrowserRequest(t, `{"browserName":"chrome","browserVersion":"120"}`))

	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rw.Code)
	}

	opts := decodeSelenosisOptions(t, m.gotBody)
	annotations, ok := opts["annotations"].(map[string]any)
	if !ok {
		t.Fatalf("expected annotations in options, got %#v", opts)
	}
	if annotations["startedManually"] != "true" {
		t.Fatalf("startedManually = %#v, want \"true\"", annotations["startedManually"])
	}
}

func TestCreateBrowserKeepsCallerOptions(t *testing.T) {
	m := useTransport(t, &mockTransport{resp: hubResponse(http.StatusOK, createdSessionBody("s1"))})

	st := seededStore(t, &types.Session{BrowserId: "b1", SessionId: "s1"})
	svc := NewService(testHubURL, st, store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)
	rw := httptest.NewRecorder()

	body := `{"browserName":"chrome","browserVersion":"120","selenosisOptions":{` +
		`"annotations":{"team":"qa"},` +
		`"containers":{"browser":{"env":{"LOG_LEVEL":"debug"}}}}}`
	svc.CreateBrowser(rw, createBrowserRequest(t, body))

	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rw.Code)
	}

	opts := decodeSelenosisOptions(t, m.gotBody)
	annotations := opts["annotations"].(map[string]any)
	if annotations["team"] != "qa" {
		t.Fatalf("caller annotation lost: %#v", annotations)
	}
	if annotations["startedManually"] != "true" {
		t.Fatalf("startedManually must still be set: %#v", annotations)
	}
	if _, ok := opts["containers"]; !ok {
		t.Fatalf("caller containers lost: %#v", opts)
	}
}

func TestCreateBrowserSendsOwnerLabel(t *testing.T) {
	m := useTransport(t, &mockTransport{resp: hubResponse(http.StatusOK, createdSessionBody("s1"))})

	st := seededStore(t, &types.Session{BrowserId: "b1", SessionId: "s1"})
	svc := NewService(testHubURL, st, store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)
	rw := httptest.NewRecorder()

	req := createBrowserRequest(t, `{"browserName":"chrome","browserVersion":"120"}`)
	req = req.WithContext(auth.WithOwner(req.Context(), auth.Owner{Name: "ui-user"}))

	svc.CreateBrowser(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rw.Code)
	}

	opts := decodeSelenosisOptions(t, m.gotBody)
	labels, ok := opts["labels"].(map[string]any)
	if !ok {
		t.Fatalf("expected labels in options, got %#v", opts)
	}
	if labels[browserv1.SelenosisOwnerLabelKey] != "ui-user" {
		t.Fatalf("owner label = %#v", labels[browserv1.SelenosisOwnerLabelKey])
	}
}

func TestCreateBrowserCallerCannotOverrideOwnerLabel(t *testing.T) {
	m := useTransport(t, &mockTransport{resp: hubResponse(http.StatusOK, createdSessionBody("s1"))})

	st := seededStore(t, &types.Session{BrowserId: "b1", SessionId: "s1"})
	svc := NewService(testHubURL, st, store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)
	rw := httptest.NewRecorder()

	body := `{"browserName":"chrome","browserVersion":"120","selenosisOptions":{"labels":{"` +
		browserv1.SelenosisOwnerLabelKey + `":"someone-else","team":"qa"}}}`
	req := createBrowserRequest(t, body)
	req = req.WithContext(auth.WithOwner(req.Context(), auth.Owner{Name: "ui-user"}))

	svc.CreateBrowser(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rw.Code)
	}

	labels := decodeSelenosisOptions(t, m.gotBody)["labels"].(map[string]any)
	if labels[browserv1.SelenosisOwnerLabelKey] != "ui-user" {
		t.Fatalf("owner label = %#v, want ui-user", labels[browserv1.SelenosisOwnerLabelKey])
	}
	if labels["team"] != "qa" {
		t.Fatalf("other caller labels must survive: %#v", labels)
	}
}

func TestCreateBrowserNoOwnerLabelWithoutOwner(t *testing.T) {
	m := useTransport(t, &mockTransport{resp: hubResponse(http.StatusOK, createdSessionBody("s1"))})

	st := seededStore(t, &types.Session{BrowserId: "b1", SessionId: "s1"})
	svc := NewService(testHubURL, st, store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)
	rw := httptest.NewRecorder()

	svc.CreateBrowser(rw, createBrowserRequest(t, `{"browserName":"chrome","browserVersion":"120"}`))

	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rw.Code)
	}

	if _, ok := decodeSelenosisOptions(t, m.gotBody)["labels"]; ok {
		t.Fatal("labels must not be sent when there is no owner")
	}
}

func TestCreateBrowserHubFailures(t *testing.T) {
	tests := []struct {
		name       string
		m          *mockTransport
		wantStatus int
		wantReason string
	}{
		{
			name:       "transport error is a bad gateway",
			m:          &mockTransport{err: errors.New("boom")},
			wantStatus: http.StatusBadGateway,
			wantReason: "boom",
		},
		{
			name: "hub 500 passes through with its message",
			m: &mockTransport{resp: hubResponse(http.StatusInternalServerError,
				`{"value":{"error":"unknown error","message":"browser did not become ready"}}`)},
			wantStatus: http.StatusInternalServerError,
			wantReason: "browser did not become ready",
		},
		{
			name:       "hub 404 passes through with a plain body",
			m:          &mockTransport{resp: hubResponse(http.StatusNotFound, "404 page not found")},
			wantStatus: http.StatusNotFound,
			wantReason: "404 page not found",
		},
		{
			name:       "unparsable body is a bad gateway",
			m:          &mockTransport{resp: hubResponse(http.StatusOK, "not-json")},
			wantStatus: http.StatusBadGateway,
			wantReason: "not-json",
		},
		{
			name:       "no session id is a bad gateway",
			m:          &mockTransport{resp: hubResponse(http.StatusOK, `{"value":{}}`)},
			wantStatus: http.StatusBadGateway,
			wantReason: "hub response carries no session id",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useTransport(t, tt.m)

			svc := NewService(testHubURL, store.NewDefaultStore[*types.Session](), store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)
			rw := httptest.NewRecorder()

			svc.CreateBrowser(rw, createBrowserRequest(t, `{"browserName":"chrome","browserVersion":"120"}`))

			if rw.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d", tt.wantStatus, rw.Code)
			}

			var got struct {
				Error  string `json:"error"`
				Reason string `json:"reason"`
			}
			if err := json.Unmarshal(rw.Body.Bytes(), &got); err != nil {
				t.Fatalf("failed to decode error response: %v (body %q)", err, rw.Body.String())
			}
			if got.Error != "failed to create browser" {
				t.Fatalf("error = %q, want %q", got.Error, "failed to create browser")
			}
			if !strings.Contains(got.Reason, tt.wantReason) {
				t.Fatalf("reason = %q, want it to contain %q", got.Reason, tt.wantReason)
			}
		})
	}
}

func TestCreateBrowserTruncatesLongHubBody(t *testing.T) {
	long := strings.Repeat("x", maxHubErrorReason*2)
	useTransport(t, &mockTransport{resp: hubResponse(http.StatusInternalServerError, long)})

	svc := NewService(testHubURL, store.NewDefaultStore[*types.Session](), store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)
	rw := httptest.NewRecorder()

	svc.CreateBrowser(rw, createBrowserRequest(t, `{"browserName":"chrome","browserVersion":"120"}`))

	var got struct {
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if len(got.Reason) != maxHubErrorReason {
		t.Fatalf("reason length = %d, want %d", len(got.Reason), maxHubErrorReason)
	}
}

func TestCreateBrowserBuildRequestError(t *testing.T) {
	useTransport(t, &mockTransport{resp: hubResponse(http.StatusOK, createdSessionBody("s1"))})

	svc := NewService("http://selenosis:4444\x01", store.NewDefaultStore[*types.Session](), store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)
	rw := httptest.NewRecorder()

	svc.CreateBrowser(rw, createBrowserRequest(t, `{"browserName":"chrome","browserVersion":"120"}`))

	if rw.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", rw.Code)
	}
}

func TestCreateBrowserSessionNeverAppears(t *testing.T) {
	useTransport(t, &mockTransport{resp: hubResponse(http.StatusOK, createdSessionBody("missing"))})

	svc := NewService(testHubURL, store.NewDefaultStore[*types.Session](), store.NewDefaultStore[types.BrowserVersions](), 10*time.Millisecond)
	rw := httptest.NewRecorder()

	svc.CreateBrowser(rw, createBrowserRequest(t, `{"browserName":"chrome","browserVersion":"120"}`))

	if rw.Code != http.StatusGatewayTimeout {
		t.Fatalf("expected status 504, got %d", rw.Code)
	}
}

func TestCreateBrowserEncodeError(t *testing.T) {
	useTransport(t, &mockTransport{resp: hubResponse(http.StatusOK, createdSessionBody("s1"))})

	st := seededStore(t, &types.Session{BrowserId: "b1", SessionId: "s1"})
	svc := NewService(testHubURL, st, store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)
	rw := &brokenWriter{ResponseRecorder: httptest.NewRecorder()}

	svc.CreateBrowser(rw, createBrowserRequest(t, `{"browserName":"chrome","browserVersion":"120"}`))

	if rw.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", rw.Code)
	}
}

func TestWaitForSessionIdAlreadyInStore(t *testing.T) {
	st := seededStore(t, &types.Session{BrowserId: "b1", SessionId: "s1"})

	start := time.Now()
	session, err := waitForSessionId(context.Background(), "s1", st)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if session.BrowserId != "b1" {
		t.Fatalf("browserId = %q", session.BrowserId)
	}
	if elapsed := time.Since(start); elapsed > 400*time.Millisecond {
		t.Fatalf("expected an immediate hit, took %s", elapsed)
	}
}

func TestWaitForSessionIdContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := waitForSessionId(ctx, "s1", store.NewDefaultStore[*types.Session]()); err == nil {
		t.Fatal("expected error")
	}
}

func TestFindSessionIdMiss(t *testing.T) {
	st := seededStore(t, &types.Session{BrowserId: "b1", SessionId: "s1"})

	if _, ok := findSessionId("nope", st); ok {
		t.Fatal("expected no match")
	}
}

func TestHubError(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		status string
		want   string
	}{
		{name: "selenium envelope", body: `{"value":{"message":"browser failed to start"}}`, status: "500 Internal Server Error", want: "browser failed to start"},
		{name: "plain text falls back to body", body: "404 page not found", status: "404 Not Found", want: "404 page not found"},
		{name: "json without message falls back to body", body: `{"value":{}}`, status: "500 Internal Server Error", want: `{"value":{}}`},
		{name: "empty body falls back to status", body: "", status: "502 Bad Gateway", want: "502 Bad Gateway"},
		{name: "blank body falls back to status", body: "   \n", status: "500 Internal Server Error", want: "500 Internal Server Error"},
		{name: "body is trimmed", body: "  boom\n", status: "500 Internal Server Error", want: "boom"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hubError([]byte(tt.body), tt.status); got != tt.want {
				t.Fatalf("hubError = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestUpstreamStatus(t *testing.T) {
	tests := []struct {
		name string
		in   int
		want int
	}{
		{name: "client error passes through", in: http.StatusBadRequest, want: http.StatusBadRequest},
		{name: "server error passes through", in: http.StatusInternalServerError, want: http.StatusInternalServerError},
		{name: "upper bound passes through", in: 599, want: 599},
		{name: "success becomes bad gateway", in: http.StatusOK, want: http.StatusBadGateway},
		{name: "redirect becomes bad gateway", in: http.StatusFound, want: http.StatusBadGateway},
		{name: "out of range becomes bad gateway", in: 600, want: http.StatusBadGateway},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := upstreamStatus(tt.in); got != tt.want {
				t.Fatalf("upstreamStatus(%d) = %d, want %d", tt.in, got, tt.want)
			}
		})
	}
}

func TestDeleteBrowserRejected(t *testing.T) {
	tests := []struct {
		name     string
		sessions []*types.Session
		want     int
	}{
		{name: "unknown browser", sessions: nil, want: http.StatusNotFound},
		{
			name:     "not started manually",
			sessions: []*types.Session{{BrowserId: "b1", SessionId: "s1"}},
			want:     http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewService(testHubURL, seededStore(t, tt.sessions...), store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)
			rw := httptest.NewRecorder()

			svc.DeleteBrowser(rw, requestWithParam(http.MethodDelete, "/browsers/b1", "browserId", "b1"))

			if rw.Code != tt.want {
				t.Fatalf("expected status %d, got %d", tt.want, rw.Code)
			}
		})
	}
}

func TestDeleteBrowserGoesThroughHub(t *testing.T) {
	m := useTransport(t, &mockTransport{resp: hubResponse(http.StatusOK, "")})

	st := seededStore(t, &types.Session{BrowserId: "b1", SessionId: "s1", StartedManually: true})
	svc := NewService(testHubURL, st, store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)
	rw := httptest.NewRecorder()

	svc.DeleteBrowser(rw, requestWithParam(http.MethodDelete, "/browsers/b1", "browserId", "b1"))

	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rw.Code)
	}
	if got, want := m.gotReq.URL.String(), testHubURL+wdHubSessionPath+"/s1"; got != want {
		t.Fatalf("target = %q, want %q", got, want)
	}
	if m.gotReq.Method != http.MethodDelete {
		t.Fatalf("method = %q, want DELETE", m.gotReq.Method)
	}
}

func TestDeleteBrowserHubFailures(t *testing.T) {
	tests := []struct {
		name string
		m    *mockTransport
	}{
		{name: "transport error", m: &mockTransport{err: errors.New("boom")}},
		{name: "hub 500", m: &mockTransport{resp: hubResponse(http.StatusInternalServerError, "")}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useTransport(t, tt.m)

			st := seededStore(t, &types.Session{BrowserId: "b1", SessionId: "s1", StartedManually: true})
			svc := NewService(testHubURL, st, store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)
			rw := httptest.NewRecorder()

			svc.DeleteBrowser(rw, requestWithParam(http.MethodDelete, "/browsers/b1", "browserId", "b1"))

			if rw.Code != http.StatusInternalServerError {
				t.Fatalf("expected status 500, got %d", rw.Code)
			}
		})
	}
}

func TestDeleteBrowserBuildRequestError(t *testing.T) {
	st := seededStore(t, &types.Session{BrowserId: "b1", SessionId: "s1", StartedManually: true})
	svc := NewService("http://selenosis:4444\x01", st, store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)
	rw := httptest.NewRecorder()

	svc.DeleteBrowser(rw, requestWithParam(http.MethodDelete, "/browsers/b1", "browserId", "b1"))

	if rw.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", rw.Code)
	}
}

func TestWaitForSessionIdAppearsAfterTick(t *testing.T) {
	st := store.NewDefaultStore[*types.Session]()

	go func() {
		time.Sleep(50 * time.Millisecond)
		st.Set("b1", &types.Session{BrowserId: "b1", SessionId: "s1"})
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	session, err := waitForSessionId(ctx, "s1", st)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if session.BrowserId != "b1" {
		t.Fatalf("browserId = %q", session.BrowserId)
	}
}

func TestGetStatusGroupsBrowsersBySessionType(t *testing.T) {
	cfgStore := store.NewDefaultStore[types.BrowserVersions]()
	cfgStore.Set("cfg-selenium", types.BrowserVersions{
		"selenium": {"chrome": {"120"}},
	})
	cfgStore.Set("cfg-playwright", types.BrowserVersions{
		"playwright":             {"playwright-chromium": {"1.59.1"}},
		types.SessionTypeUnknown: {"firefox": {"140"}},
	})

	svc := NewService(testHubURL, store.NewDefaultStore[*types.Session](), cfgStore, 5*time.Second)
	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	rw := httptest.NewRecorder()

	svc.GetStatus(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rw.Code)
	}

	var got struct {
		Browsers []map[string]map[string][]string `json:"supportedBrowsers"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(got.Browsers) != 2 {
		t.Fatalf("expected 2 config entries, got %d", len(got.Browsers))
	}

	seen := map[string][]string{}
	for _, cfg := range got.Browsers {
		for sessionType, browsers := range cfg {
			for name, versions := range browsers {
				seen[sessionType+"/"+name] = versions
			}
		}
	}

	for key, want := range map[string]string{
		"selenium/chrome":                     "120",
		"playwright/playwright-chromium":      "1.59.1",
		types.SessionTypeUnknown + "/firefox": "140",
	} {
		versions, ok := seen[key]
		if !ok {
			t.Fatalf("missing %q in %v", key, seen)
		}
		if len(versions) != 1 || versions[0] != want {
			t.Fatalf("versions for %q = %v, want [%s]", key, versions, want)
		}
	}
}

func TestGetStatusEmptyConfigStore(t *testing.T) {
	svc := NewService(testHubURL, store.NewDefaultStore[*types.Session](), store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)
	req := httptest.NewRequest(http.MethodGet, "/status", nil)
	rw := httptest.NewRecorder()

	svc.GetStatus(rw, req)

	var got struct {
		Browsers []types.BrowserVersions `json:"supportedBrowsers"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(got.Browsers) != 0 {
		t.Fatalf("expected no supported browsers, got %v", got.Browsers)
	}
}

type failingBody struct{}

func (failingBody) Read([]byte) (int, error) { return 0, errors.New("read error") }
func (failingBody) Close() error             { return nil }

func TestCreateBrowserUnreadableHubBody(t *testing.T) {
	useTransport(t, &mockTransport{resp: &http.Response{StatusCode: http.StatusOK, Body: failingBody{}}})

	svc := NewService(testHubURL, store.NewDefaultStore[*types.Session](), store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)
	rw := httptest.NewRecorder()

	svc.CreateBrowser(rw, createBrowserRequest(t, `{"browserName":"chrome","browserVersion":"120"}`))

	if rw.Code != http.StatusBadGateway {
		t.Fatalf("expected status 502, got %d", rw.Code)
	}

	var got struct {
		Error  string `json:"error"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(rw.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to decode error response: %v", err)
	}
	if got.Reason != "read error" {
		t.Fatalf("reason = %q, want %q", got.Reason, "read error")
	}
}

func TestCreateBrowserLargeSuccessBody(t *testing.T) {
	caps := strings.Repeat("a", maxHubErrorReason*8)
	body := fmt.Sprintf(`{"value":{"sessionId":%q,"capabilities":{"padding":%q}}}`, "00000000-0000-0000-0000-ffff0a2a0581", caps)
	if len(body) <= maxHubErrorReason {
		t.Fatalf("test body must exceed the reason limit, got %d", len(body))
	}

	useTransport(t, &mockTransport{resp: hubResponse(http.StatusOK, body)})

	st := store.NewDefaultStore[*types.Session]()
	st.Set("browser-1", &types.Session{
		SessionId:   "00000000-0000-0000-0000-ffff0a2a0581",
		BrowserId:   "browser-1",
		BrowserName: "chrome",
	})

	svc := NewService(testHubURL, st, store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)
	rw := httptest.NewRecorder()

	svc.CreateBrowser(rw, createBrowserRequest(t, `{"browserName":"chrome","browserVersion":"120"}`))

	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d (body %q)", rw.Code, rw.Body.String())
	}

	var got types.Session
	if err := json.Unmarshal(rw.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if got.BrowserId != "browser-1" {
		t.Fatalf("browserId = %q, want %q", got.BrowserId, "browser-1")
	}
}

func TestTruncateReason(t *testing.T) {
	short := "browser did not become ready"
	if got := truncateReason(short); got != short {
		t.Fatalf("truncateReason(%q) = %q, want it unchanged", short, got)
	}

	long := strings.Repeat("x", maxHubErrorReason*2)
	if got := truncateReason(long); len(got) != maxHubErrorReason {
		t.Fatalf("length = %d, want %d", len(got), maxHubErrorReason)
	}

	multibyte := strings.Repeat("\u2026", maxHubErrorReason)
	got := truncateReason(multibyte)
	if len(got) >= maxHubErrorReason {
		t.Fatalf("length = %d, want < %d", len(got), maxHubErrorReason)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("truncated reason is not valid UTF-8: %q", got)
	}
}

func vncProbeService(t *testing.T, dial func(target string) (wsConn, error)) *Service {
	t.Helper()

	prevDial := wsDial
	wsDial = dial
	t.Cleanup(func() { wsDial = prevDial })

	st := store.NewDefaultStore[*types.Session]()
	st.Set("browser-1", &types.Session{
		SessionId: "sess-1",
		BrowserId: "browser-1",
		BrowserIP: "127.0.0.1",
	})
	return NewService(testHubURL, st, store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)
}

func probeRequest() *http.Request {
	return requestWithParam(http.MethodGet, "/api/v1/browsers/browser-1/vnc", "browserId", "browser-1")
}

func TestRouteVNCProbeGreeted(t *testing.T) {
	backend := newFakeWSConn()
	backend.readCh <- fakeWSMessage{mt: websocket.BinaryMessage, data: []byte("RFB 003.008\n")}

	var dialed string
	svc := vncProbeService(t, func(target string) (wsConn, error) {
		dialed = target
		return backend, nil
	})

	rw := httptest.NewRecorder()
	svc.RouteVNC(rw, probeRequest())

	if rw.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", rw.Code)
	}
	if dialed != "ws://127.0.0.1:4445/selenosis/v1/vnc/sess-1" {
		t.Fatalf("unexpected probe target %q", dialed)
	}
	if !backend.closed {
		t.Fatal("expected the probe connection to be closed")
	}
}

func TestRouteVNCProbeUnavailable(t *testing.T) {
	tests := []struct {
		name string
		dial func(target string) (wsConn, error)
	}{
		{
			name: "dial fails",
			dial: func(string) (wsConn, error) { return nil, errors.New("dial failed") },
		},
		{
			name: "sidecar closes without a greeting",
			dial: func(string) (wsConn, error) {
				backend := newFakeWSConn()
				close(backend.readCh)
				return backend, nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := vncProbeService(t, tt.dial)

			rw := httptest.NewRecorder()
			svc.RouteVNC(rw, probeRequest())

			if rw.Code != http.StatusServiceUnavailable {
				t.Fatalf("expected status 503, got %d", rw.Code)
			}
		})
	}
}

func TestRouteVNCProbeTimesOut(t *testing.T) {
	prevTimeout := vncProbeTimeout
	vncProbeTimeout = 50 * time.Millisecond
	t.Cleanup(func() { vncProbeTimeout = prevTimeout })

	backend := newFakeWSConn()
	t.Cleanup(func() { close(backend.readCh) })

	svc := vncProbeService(t, func(string) (wsConn, error) { return backend, nil })

	rw := httptest.NewRecorder()
	svc.RouteVNC(rw, probeRequest())

	if rw.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected status 503, got %d", rw.Code)
	}
	if !backend.closed {
		t.Fatal("expected the probe connection to be closed")
	}
}

func TestRouteVNCProbeUnknownSession(t *testing.T) {
	svc := vncProbeService(t, func(string) (wsConn, error) {
		t.Fatal("probe must not dial for an unknown session")
		return nil, nil
	})

	rw := httptest.NewRecorder()
	svc.RouteVNC(rw, requestWithParam(http.MethodGet, "/api/v1/browsers/missing/vnc", "browserId", "missing"))

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rw.Code)
	}
}

func TestGetBrowserExposesVNCFlag(t *testing.T) {
	st := store.NewDefaultStore[*types.Session]()
	st.Set("browser-1", &types.Session{SessionId: "sess-1", BrowserId: "browser-1", VNC: false})
	svc := NewService(testHubURL, st, store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)

	rw := httptest.NewRecorder()
	svc.GetBrowser(rw, requestWithParam(http.MethodGet, "/api/v1/browsers/browser-1", "browserId", "browser-1"))

	var got map[string]any
	if err := json.Unmarshal(rw.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if vnc, ok := got["vnc"]; !ok || vnc != false {
		t.Fatalf("expected vnc=false in the response, got %v (present=%v)", vnc, ok)
	}
}
