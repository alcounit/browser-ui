package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

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
}
