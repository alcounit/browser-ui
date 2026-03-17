package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	logctx "github.com/alcounit/browser-controller/pkg/log"
	"github.com/alcounit/browser-ui/pkg/types"
	"github.com/alcounit/seleniferous/v2/pkg/store"
	"github.com/alcounit/selenosis/v2/pkg/selenium"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"

	"github.com/go-chi/chi/v5"

	browserv1 "github.com/alcounit/browser-controller/apis/browser/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	browserclient "github.com/alcounit/browser-service/pkg/client/browser"
)

type Service struct {
	namespace           string
	client              browserclient.Client
	sessionStore        store.Store[*types.Session]
	configStore         store.Store[types.BrowserVersions]
	browserStartTimeout time.Duration
}

type wsConn interface {
	ReadMessage() (int, []byte, error)
	WriteMessage(int, []byte) error
	Close() error
}

var wsUpgrade = func(rw http.ResponseWriter, req *http.Request) (wsConn, error) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	return upgrader.Upgrade(rw, req, nil)
}

var wsDial = func(target string) (wsConn, error) {
	dialer := websocket.Dialer{}
	conn, _, err := dialer.Dial(target, nil)
	return conn, err
}

var httpClient interface {
	Do(*http.Request) (*http.Response, error)
} = http.DefaultClient

func NewService(client browserclient.Client, namespace string, sessionStore store.Store[*types.Session], configStore store.Store[types.BrowserVersions], browserStartTimeout time.Duration) *Service {
	return &Service{
		namespace:           namespace,
		client:              client,
		sessionStore:        sessionStore,
		configStore:         configStore,
		browserStartTimeout: browserStartTimeout,
	}
}

func (s Service) GetBrowser(rw http.ResponseWriter, req *http.Request) {
	log := logctx.FromContext(req.Context())

	browserId := chi.URLParam(req, "browserId")
	session, ok := s.sessionStore.Get(browserId)
	if !ok {
		log.Error().Str("browserId", browserId).Msgf("unknown browserId")
		http.Error(rw, "session not found", http.StatusNotFound)
		return

	}

	rw.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(rw).Encode(session); err != nil {
		log.Error().Err(err).Msg("failed to encode session response")
		http.Error(rw, "failed to encode response", http.StatusInternalServerError)
		return
	}
	log.Info().Str("browserId", browserId).Msg("session retrived")
}

func (s *Service) GetStatus(rw http.ResponseWriter, req *http.Request) {
	log := logctx.FromContext(req.Context())

	activeSessions := s.sessionStore.List()
	supportedBrowsers := s.configStore.List()

	response := struct {
		ActiveSessions    []*types.Session        `json:"activeSessions"`
		SupportedBrowsers []types.BrowserVersions `json:"supportedBrowsers"`
	}{
		ActiveSessions:    activeSessions,
		SupportedBrowsers: supportedBrowsers,
	}

	rw.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(rw).Encode(&response); err != nil {
		log.Error().Err(err).Msg("failed to encode sessions response")
		http.Error(rw, "failed to encode response", http.StatusInternalServerError)
		return
	}
	log.Info().Int("activeSessions", len(activeSessions)).Int("supportedBrowsers", len(supportedBrowsers)).Msg("session list retrieved")
}

func (s *Service) CreateBrowser(rw http.ResponseWriter, req *http.Request) {
	log := logctx.FromContext(req.Context())

	if req.Body == nil {
		log.Error().Msg("request body is required")
		http.Error(rw, "request body is required", http.StatusBadRequest)
		return
	}
	defer req.Body.Close()

	var request struct {
		BrowserName      string         `json:"browserName"`
		BrowserVersion   string         `json:"browserVersion"`
		SelenosisOptions map[string]any `json:"selenosisOptions"`
	}

	if err := json.NewDecoder(req.Body).Decode(&request); err != nil {
		log.Error().Err(err).Msg("failed to decode create browser request")
		http.Error(rw, "invalid request body", http.StatusBadRequest)
		return
	}

	if request.BrowserName == "" || request.BrowserVersion == "" {
		log.Error().Msg("browserName and browserVersion are required")
		http.Error(rw, "browserName and browserVersion are required", http.StatusBadRequest)
		return
	}

	template := browserv1.Browser{
		ObjectMeta: metav1.ObjectMeta{
			Name: uuid.NewString(),
			Annotations: map[string]string{
				"startedManually": "true",
			},
		},
		Spec: browserv1.BrowserSpec{
			BrowserName:    request.BrowserName,
			BrowserVersion: request.BrowserVersion,
		},
	}

	var err error
	template.ObjectMeta.Annotations, err = setSelenosisOptions(template.ObjectMeta.Annotations, request.SelenosisOptions)
	if err != nil {
		log.Error().Err(err).Msg("failed to set selenosis options annotation")
		http.Error(rw, "invalid selenosis options", http.StatusBadRequest)
		return
	}

	browser, err := s.client.Create(req.Context(), s.namespace, &template)
	if err != nil {
		log.Error().Err(err).Msg("failed to create browser")
		http.Error(rw, "failed to create browser", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(req.Context(), s.browserStartTimeout)
	defer cancel()

	session, err := waitForSession(ctx, browser.GetName(), s.sessionStore)
	if err != nil {
		log.Error().Err(err).Str("browserName", request.BrowserName).Msg("session did not become available in time")
		http.Error(rw, "session did not become available in time", http.StatusInternalServerError)
		return
	}

	createReq := selenium.CreateSessionRequest{
		Capabilities: map[string]selenium.Capabilities{
			"alwaysMatch": {
				"browserName":    request.BrowserName,
				"browserVersion": request.BrowserVersion,
			},
		},
	}

	raw, err := json.Marshal(createReq)
	if err != nil {
		log.Error().Err(err).Msg("failed to marshal create session request")
		http.Error(rw, "failed to create browser", http.StatusInternalServerError)
		return
	}

	reqBody := bytes.NewBuffer(raw)

	reqUrl := &url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(session.BrowserIP, "4445"),
		Path:   "/session",
	}

	innerReq, err := http.NewRequestWithContext(req.Context(), http.MethodPost, reqUrl.String(), reqBody)
	if err != nil {
		log.Error().Err(err).Msg("failed to build create session request")
		http.Error(rw, "failed to create browser", http.StatusInternalServerError)
		return
	}

	innerReq.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(innerReq)
	if err != nil {
		log.Error().Err(err).Msg("failed to post create session request")
		http.Error(rw, "failed to create browser", http.StatusInternalServerError)
		return
	}
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		log.Error().Str("status", resp.Status).Msg("create session request failed")
		http.Error(rw, "failed to create browser", http.StatusInternalServerError)
		return
	}

	rw.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(rw).Encode(session); err != nil {
		log.Error().Err(err).Msg("failed to encode session response")
		http.Error(rw, "failed to encode response", http.StatusInternalServerError)
		return
	}

	log.Info().Str("browserName", request.BrowserName).Str("browserVersion", request.BrowserVersion).Msg("browser created")

}

func (s *Service) DeleteBrowser(rw http.ResponseWriter, req *http.Request) {
	log := logctx.FromContext(req.Context())

	browserId := chi.URLParam(req, "browserId")

	session, ok := s.sessionStore.Get(browserId)
	if !ok {
		log.Error().Str("browserId", browserId).Msgf("unknown browserId")
		http.Error(rw, "session not found", http.StatusNotFound)
		return

	}

	if !session.StartedManually {
		log.Error().Str("browserId", browserId).Str("sessionId", session.SessionId).Msgf("cannot delete session that was not started manually")
		http.Error(rw, "cannot delete session that was not started manually", http.StatusBadRequest)
		return
	}

	reqUrl := &url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(session.BrowserIP, "4445"),
		Path:   fmt.Sprintf("/session/%s", session.SessionId),
	}

	innerReq, err := http.NewRequestWithContext(req.Context(), http.MethodDelete, reqUrl.String(), nil)
	if err != nil {
		log.Error().Err(err).Msg("failed to build delete session request")
		http.Error(rw, "failed to delete browser", http.StatusInternalServerError)
		return
	}

	resp, err := httpClient.Do(innerReq)
	if err != nil {
		log.Error().Err(err).Msg("failed to post create session request")
		http.Error(rw, "failed to create browser", http.StatusInternalServerError)
		return
	}
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		log.Error().Str("status", resp.Status).Msg("delete session request failed")
		http.Error(rw, "failed to delete browser", http.StatusInternalServerError)
		return
	}

	rw.WriteHeader(http.StatusOK)
}

func (s *Service) RouteVNC(rw http.ResponseWriter, req *http.Request) {
	log := logctx.FromContext(req.Context())

	browserId := chi.URLParam(req, "browserId")

	session, ok := s.sessionStore.Get(browserId)
	if !ok {
		log.Error().Str("browserId", browserId).Msgf("unknown browserId")
		http.Error(rw, "invalid session", http.StatusBadRequest)
		return
	}

	client, err := wsUpgrade(rw, req)
	if err != nil {
		log.Err(err).Str("browserId", browserId).Msg("client ws upgrade failed")
		return
	}
	defer client.Close()

	targetURL := url.URL{
		Scheme: "ws",
		Host:   net.JoinHostPort(session.BrowserIP, "4445"),
		Path:   fmt.Sprintf("/selenosis/v1/vnc/%s", session.SessionId),
	}

	backend, err := wsDial(targetURL.String())
	if err != nil {
		log.Err(err).Str("browserId", browserId).Str("url", targetURL.String()).Msg("backend ws dial failed")
		return
	}
	defer backend.Close()

	log.Info().Str("browserId", browserId).Msg("ws connection established")

	errCh := make(chan error, 2)

	go func() {
		for {
			mt, data, err := client.ReadMessage()
			if err != nil {
				if isNormalWSDisconnect(err) {
					errCh <- nil
					return
				}
				errCh <- err
				return
			}

			if err := backend.WriteMessage(mt, data); err != nil {
				errCh <- err
				return
			}
		}
	}()

	go func() {
		for {
			mt, data, err := backend.ReadMessage()
			if err != nil {

				if isNormalWSDisconnect(err) {
					errCh <- nil
					return
				}
				errCh <- err
				return
			}

			if err := client.WriteMessage(mt, data); err != nil {
				errCh <- err
				return
			}
		}
	}()

	err = <-errCh

	switch err {
	case nil:
		log.Info().
			Str("browserId", browserId).
			Msg("vnc connection closed")

	default:
		log.Error().
			Err(err).
			Str("browserId", browserId).
			Msg("vnc connection terminated with error")
	}
}

func isNormalWSDisconnect(err error) bool {
	if err == nil {
		return false
	}

	if websocket.IsCloseError(
		err,
		websocket.CloseNormalClosure,
		websocket.CloseGoingAway,
		websocket.CloseNoStatusReceived,
	) {
		return true
	}

	if errors.Is(err, io.EOF) {
		return true
	}

	return false
}

func setSelenosisOptions(ann map[string]string, opts map[string]any) (map[string]string, error) {
	if len(opts) == 0 {
		return ann, nil
	}

	b, err := json.Marshal(opts)
	if err != nil {
		return ann, fmt.Errorf("marshal selenosis options: %w", err)
	}

	if ann == nil {
		ann = map[string]string{}
	}

	ann[browserv1.SelenosisOptionsAnnotationKey] = string(b)
	return ann, nil
}

func waitForSession(ctx context.Context, browserName string, store store.Store[*types.Session]) (*types.Session, error) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("timeout waiting for session: %s", browserName)
		case <-ticker.C:
			if session, ok := store.Get(browserName); ok {
				return session, nil
			}
		}
	}
}
