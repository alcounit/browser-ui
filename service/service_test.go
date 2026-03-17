package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	browserv1 "github.com/alcounit/browser-controller/apis/browser/v1"
	browserclient "github.com/alcounit/browser-service/pkg/client/browser"
	"github.com/alcounit/browser-service/pkg/event"
	"github.com/alcounit/browser-ui/pkg/types"
	"github.com/alcounit/seleniferous/v2/pkg/store"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func requestWithParam(method, path, key, value string) *http.Request {
	req := httptest.NewRequest(method, path, nil)
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add(key, value)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, routeCtx))
}

func TestGetBrowserNotFound(t *testing.T) {
	svc := NewService(nil, "", store.NewDefaultStore[*types.Session](), store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)
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
	svc := NewService(nil, "", st, store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)
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
	svc := NewService(nil, "", st, store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)
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
	svc := NewService(nil, "", st, store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)
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
	svc := NewService(nil, "", st, store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)
	req := httptest.NewRequest(http.MethodGet, "/browsers", nil)
	rec := httptest.NewRecorder()
	rw := &brokenWriter{rec}

	svc.GetStatus(rw, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", rec.Code)
	}
}

func TestRouteVNCInvalidSession(t *testing.T) {
	svc := NewService(nil, "", store.NewDefaultStore[*types.Session](), store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)
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
	svc := NewService(nil, "", st, store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)

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

	st := store.NewDefaultStore[*types.Session]()
	st.Set("browser-1", &types.Session{
		SessionId: "sess-1",
		BrowserId: "browser-1",
		BrowserIP: "127.0.0.1",
	})
	svc := NewService(nil, "", st, store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)

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

	st := store.NewDefaultStore[*types.Session]()
	st.Set("browser-1", &types.Session{
		SessionId: "sess-1",
		BrowserId: "browser-1",
		BrowserIP: "127.0.0.1",
	})
	svc := NewService(nil, "", st, store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)

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

	st := store.NewDefaultStore[*types.Session]()
	st.Set("browser-1", &types.Session{
		SessionId: "sess-1",
		BrowserId: "browser-1",
		BrowserIP: "127.0.0.1",
	})
	svc := NewService(nil, "", st, store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)

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

	st := store.NewDefaultStore[*types.Session]()
	st.Set("browser-1", &types.Session{
		SessionId: "sess-1",
		BrowserId: "browser-1",
		BrowserIP: "127.0.0.1",
	})
	svc := NewService(nil, "", st, store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)

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

	st := store.NewDefaultStore[*types.Session]()
	st.Set("browser-1", &types.Session{
		SessionId: "sess-1",
		BrowserId: "browser-1",
		BrowserIP: "127.0.0.1",
	})
	svc := NewService(nil, "", st, store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)

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

	st := store.NewDefaultStore[*types.Session]()
	st.Set("browser-1", &types.Session{
		SessionId: "sess-1",
		BrowserId: "browser-1",
		BrowserIP: "127.0.0.1",
	})
	svc := NewService(nil, "", st, store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)

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
type fakeBrowserClient struct {
	createErr error
	browser   *browserv1.Browser
}

func (c *fakeBrowserClient) Create(ctx context.Context, namespace string, browser *browserv1.Browser) (*browserv1.Browser, error) {
	if c.createErr != nil {
		return nil, c.createErr
	}
	if c.browser != nil {
		return c.browser, nil
	}
	// Return the browser with the same name as provided.
	return browser, nil
}

func (c *fakeBrowserClient) Get(ctx context.Context, namespace, name string) (*browserv1.Browser, error) {
	panic("not used")
}

func (c *fakeBrowserClient) Delete(ctx context.Context, namespace, name string) error {
	panic("not used")
}

func (c *fakeBrowserClient) List(ctx context.Context, namespace string) ([]*browserv1.Browser, error) {
	panic("not used")
}

func (c *fakeBrowserClient) Events(ctx context.Context, namespace string, opts ...event.EventsOption) (browserclient.EventStream, error) {
	panic("not used")
}

// mockTransport implements http.RoundTripper for mocking httpClient.
type mockTransport struct {
	resp *http.Response
	err  error
}

func (m *mockTransport) Do(req *http.Request) (*http.Response, error) {
	return m.resp, m.err
}

func TestCreateBrowserNilBody(t *testing.T) {
	svc := NewService(&fakeBrowserClient{}, "default", store.NewDefaultStore[*types.Session](), store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)
	req := httptest.NewRequest(http.MethodPost, "/browsers", nil)
	req.Body = nil
	rw := httptest.NewRecorder()

	svc.CreateBrowser(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rw.Code)
	}
}

func TestCreateBrowserInvalidJSON(t *testing.T) {
	svc := NewService(&fakeBrowserClient{}, "default", store.NewDefaultStore[*types.Session](), store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)
	req := httptest.NewRequest(http.MethodPost, "/browsers", strings.NewReader("not-json"))
	rw := httptest.NewRecorder()

	svc.CreateBrowser(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rw.Code)
	}
}

func TestCreateBrowserEmptyBrowserName(t *testing.T) {
	svc := NewService(&fakeBrowserClient{}, "default", store.NewDefaultStore[*types.Session](), store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)
	body := `{"browserName":"","browserVersion":"123"}`
	req := httptest.NewRequest(http.MethodPost, "/browsers", strings.NewReader(body))
	rw := httptest.NewRecorder()

	svc.CreateBrowser(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rw.Code)
	}
}

func TestCreateBrowserEmptyBrowserVersion(t *testing.T) {
	svc := NewService(&fakeBrowserClient{}, "default", store.NewDefaultStore[*types.Session](), store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)
	body := `{"browserName":"chrome","browserVersion":""}`
	req := httptest.NewRequest(http.MethodPost, "/browsers", strings.NewReader(body))
	rw := httptest.NewRecorder()

	svc.CreateBrowser(rw, req)

	if rw.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", rw.Code)
	}
}

func TestCreateBrowserClientCreateError(t *testing.T) {
	cl := &fakeBrowserClient{createErr: errors.New("create failed")}
	svc := NewService(cl, "default", store.NewDefaultStore[*types.Session](), store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)
	body := `{"browserName":"chrome","browserVersion":"123"}`
	req := httptest.NewRequest(http.MethodPost, "/browsers", strings.NewReader(body))
	rw := httptest.NewRecorder()

	svc.CreateBrowser(rw, req)

	if rw.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", rw.Code)
	}
}

func TestCreateBrowserWaitForSessionTimeout(t *testing.T) {
	// Use a very short timeout so waitForSession times out immediately.
	// The session store is empty so waitForSession will never find the session.
	cl := &fakeBrowserClient{}
	svc := NewService(cl, "default", store.NewDefaultStore[*types.Session](), store.NewDefaultStore[types.BrowserVersions](), 1*time.Millisecond)
	body := `{"browserName":"chrome","browserVersion":"123"}`
	req := httptest.NewRequest(http.MethodPost, "/browsers", strings.NewReader(body))
	rw := httptest.NewRecorder()

	svc.CreateBrowser(rw, req)

	if rw.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", rw.Code)
	}
}

func TestCreateBrowserBuildRequestError(t *testing.T) {
	// Use a BrowserIP with an invalid character so that http.NewRequestWithContext
	// fails when parsing the constructed URL (url.URL.String() percent-encodes
	// control characters, producing an invalid escape sequence).
	st := store.NewDefaultStore[*types.Session]()

	now := metav1.Now()
	returnedBrowser := &browserv1.Browser{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "browser-bad-ip",
			CreationTimestamp: now,
		},
	}

	// BrowserIP with control char → url.URL.String() → invalid URL escape
	st.Set("browser-bad-ip", &types.Session{
		SessionId: "sess-bad-ip",
		BrowserId: "browser-bad-ip",
		BrowserIP: "127.0.0.1\x01",
	})

	cl := &fakeBrowserClient{browser: returnedBrowser}
	svc := NewService(cl, "default", st, store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)

	body := `{"browserName":"chrome","browserVersion":"123"}`
	req := httptest.NewRequest(http.MethodPost, "/browsers", strings.NewReader(body))
	rw := httptest.NewRecorder()

	svc.CreateBrowser(rw, req)

	if rw.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", rw.Code)
	}
}

func TestCreateBrowserHTTPClientError(t *testing.T) {
	// Pre-populate the session store so waitForSession succeeds immediately when
	// the fake client returns a browser with a known name.
	st := store.NewDefaultStore[*types.Session]()

	now := metav1.Now()
	returnedBrowser := &browserv1.Browser{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "browser-xyz",
			CreationTimestamp: now,
		},
	}

	st.Set("browser-xyz", &types.Session{
		SessionId: "sess-xyz",
		BrowserId: "browser-xyz",
		BrowserIP: "127.0.0.1",
	})

	cl := &fakeBrowserClient{browser: returnedBrowser}
	svc := NewService(cl, "default", st, store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)

	prevHTTPClient := httpClient
	httpClient = &mockTransport{err: errors.New("http error")}
	defer func() { httpClient = prevHTTPClient }()

	body := `{"browserName":"chrome","browserVersion":"123"}`
	req := httptest.NewRequest(http.MethodPost, "/browsers", strings.NewReader(body))
	rw := httptest.NewRecorder()

	svc.CreateBrowser(rw, req)

	if rw.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", rw.Code)
	}
}

func TestCreateBrowserSeleniferous500(t *testing.T) {
	st := store.NewDefaultStore[*types.Session]()

	now := metav1.Now()
	returnedBrowser := &browserv1.Browser{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "browser-abc",
			CreationTimestamp: now,
		},
	}

	st.Set("browser-abc", &types.Session{
		SessionId: "sess-abc",
		BrowserId: "browser-abc",
		BrowserIP: "127.0.0.1",
	})

	cl := &fakeBrowserClient{browser: returnedBrowser}
	svc := NewService(cl, "default", st, store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)

	prevHTTPClient := httpClient
	httpClient = &mockTransport{resp: &http.Response{
		StatusCode: http.StatusInternalServerError,
		Body:       io.NopCloser(strings.NewReader("")),
	}}
	defer func() { httpClient = prevHTTPClient }()

	body := `{"browserName":"chrome","browserVersion":"123"}`
	req := httptest.NewRequest(http.MethodPost, "/browsers", strings.NewReader(body))
	rw := httptest.NewRecorder()

	svc.CreateBrowser(rw, req)

	if rw.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", rw.Code)
	}
}

func TestCreateBrowserEncodeError(t *testing.T) {
	st := store.NewDefaultStore[*types.Session]()

	now := metav1.Now()
	returnedBrowser := &browserv1.Browser{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "browser-enc",
			CreationTimestamp: now,
		},
	}

	st.Set("browser-enc", &types.Session{
		SessionId: "sess-enc",
		BrowserId: "browser-enc",
		BrowserIP: "127.0.0.1",
	})

	cl := &fakeBrowserClient{browser: returnedBrowser}
	svc := NewService(cl, "default", st, store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)

	prevHTTPClient := httpClient
	httpClient = &mockTransport{resp: &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("")),
	}}
	defer func() { httpClient = prevHTTPClient }()

	body := `{"browserName":"chrome","browserVersion":"123"}`
	req := httptest.NewRequest(http.MethodPost, "/browsers", strings.NewReader(body))
	rec := httptest.NewRecorder()
	rw := &brokenWriter{rec}

	svc.CreateBrowser(rw, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", rec.Code)
	}
}

func TestCreateBrowserSuccess(t *testing.T) {
	st := store.NewDefaultStore[*types.Session]()

	now := metav1.Now()
	returnedBrowser := &browserv1.Browser{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "browser-ok",
			CreationTimestamp: now,
		},
	}

	st.Set("browser-ok", &types.Session{
		SessionId: "sess-ok",
		BrowserId: "browser-ok",
		BrowserIP: "127.0.0.1",
	})

	cl := &fakeBrowserClient{browser: returnedBrowser}
	svc := NewService(cl, "default", st, store.NewDefaultStore[types.BrowserVersions](), 5*time.Second)

	prevHTTPClient := httpClient
	httpClient = &mockTransport{resp: &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("")),
	}}
	defer func() { httpClient = prevHTTPClient }()

	body := `{"browserName":"chrome","browserVersion":"123"}`
	req := httptest.NewRequest(http.MethodPost, "/browsers", strings.NewReader(body))
	rw := httptest.NewRecorder()

	svc.CreateBrowser(rw, req)

	if rw.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rw.Code)
	}

	var got types.Session
	if err := json.Unmarshal(rw.Body.Bytes(), &got); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if got.SessionId != "sess-ok" {
		t.Fatalf("expected sessionId sess-ok, got %s", got.SessionId)
	}
}

func TestWaitForSessionAlreadyInStore(t *testing.T) {
	st := store.NewDefaultStore[*types.Session]()
	st.Set("browser-1", &types.Session{SessionId: "sess-1"})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sess, err := waitForSession(ctx, "browser-1", st)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if sess.SessionId != "sess-1" {
		t.Fatalf("expected sessionId sess-1, got %s", sess.SessionId)
	}
}

func TestWaitForSessionContextCancelled(t *testing.T) {
	st := store.NewDefaultStore[*types.Session]()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := waitForSession(ctx, "browser-missing", st)
	if err == nil {
		t.Fatalf("expected error from cancelled context")
	}
}

func TestSetSelenosisOptionsEmpty(t *testing.T) {
	ann, err := setSelenosisOptions(nil, nil)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if ann != nil {
		t.Fatalf("expected nil annotations for empty opts")
	}
}

func TestSetSelenosisOptionsWithOpts(t *testing.T) {
	opts := map[string]any{"key": "value"}
	ann, err := setSelenosisOptions(nil, opts)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if ann == nil {
		t.Fatalf("expected non-nil annotations")
	}
	if _, ok := ann[browserv1.SelenosisOptionsAnnotationKey]; !ok {
		t.Fatalf("expected SelenosisOptionsAnnotationKey to be set")
	}
}

func TestSetSelenosisOptionsExistingAnnotations(t *testing.T) {
	existing := map[string]string{"existing-key": "existing-value"}
	opts := map[string]any{"key": "value"}
	ann, err := setSelenosisOptions(existing, opts)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if ann == nil {
		t.Fatalf("expected non-nil annotations")
	}
	if ann["existing-key"] != "existing-value" {
		t.Fatalf("expected existing-key to be preserved")
	}
	if _, ok := ann[browserv1.SelenosisOptionsAnnotationKey]; !ok {
		t.Fatalf("expected SelenosisOptionsAnnotationKey to be set")
	}
}

func TestSetSelenosisOptionsMarshalError(t *testing.T) {
	// Use a channel value inside opts to trigger json.Marshal error.
	opts := map[string]any{"key": make(chan int)}
	_, err := setSelenosisOptions(nil, opts)
	if err == nil {
		t.Fatalf("expected marshal error, got nil")
	}
}
