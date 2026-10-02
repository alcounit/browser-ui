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
	"strings"
	"time"
	"unicode/utf8"

	logctx "github.com/alcounit/browser-controller/pkg/log"
	"github.com/alcounit/browser-ui/pkg/types"
	"github.com/alcounit/seleniferous/v2/pkg/store"
	"github.com/alcounit/selenosis/v2/pkg/auth"
	"github.com/alcounit/selenosis/v2/pkg/selenium"
	"github.com/gorilla/websocket"

	"github.com/go-chi/chi/v5"

	browserv1 "github.com/alcounit/browser-controller/apis/browser/v1"
)

type Service struct {
	selenosisURL        string
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

const wdHubSessionPath = "/session"

var vncProbeTimeout = 5 * time.Second

const (
	maxHubResponseBody = 1 << 20
	maxHubErrorReason  = 512
)

var httpClient interface {
	Do(*http.Request) (*http.Response, error)
} = http.DefaultClient

func NewService(selenosisURL string, sessionStore store.Store[*types.Session], configStore store.Store[types.BrowserVersions], browserStartTimeout time.Duration) *Service {
	return &Service{
		selenosisURL:        selenosisURL,
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
	log.Info().Str("browserId", browserId).Str("browserName", session.BrowserName).Str("browserVersion", session.BrowserVersion).Msg("session retrived")
}

func (s *Service) GetStatus(rw http.ResponseWriter, req *http.Request) {
	log := logctx.FromContext(req.Context())

	activeSessions := s.sessionStore.List()
	if owner, ok := auth.OwnerFrom(req.Context()); ok {
		filtered := make([]*types.Session, 0, len(activeSessions))
		for _, sess := range activeSessions {
			if sess.Owner == owner.Name {
				filtered = append(filtered, sess)
			}
		}
		activeSessions = filtered
	}
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
		writeBrowserError(rw, http.StatusBadRequest, "request body is required", "")
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
		writeBrowserError(rw, http.StatusBadRequest, "invalid request body", err.Error())
		return
	}

	if request.BrowserName == "" || request.BrowserVersion == "" {
		log.Error().Msg("browserName and browserVersion are required")
		writeBrowserError(rw, http.StatusBadRequest, "browserName and browserVersion are required", "")
		return
	}

	opts := map[string]any{}
	for k, v := range request.SelenosisOptions {
		opts[k] = v
	}

	annotations := map[string]string{"startedManually": "true"}
	if raw, ok := opts["annotations"].(map[string]any); ok {
		for k, v := range raw {
			if s, ok := v.(string); ok {
				annotations[k] = s
			}
		}
	}
	opts["annotations"] = annotations

	if owner, ok := auth.OwnerFrom(req.Context()); ok {
		labels := map[string]string{browserv1.SelenosisOwnerLabelKey: owner.Name}
		if raw, ok := opts["labels"].(map[string]any); ok {
			for k, v := range raw {
				if s, ok := v.(string); ok && k != browserv1.SelenosisOwnerLabelKey {
					labels[k] = s
				}
			}
		}
		opts["labels"] = labels
	}

	createReq := selenium.CreateSessionRequest{
		Capabilities: map[string]selenium.Capabilities{
			"alwaysMatch": {
				"browserName":       request.BrowserName,
				"browserVersion":    request.BrowserVersion,
				"selenosis:options": opts,
			},
		},
	}

	raw, err := json.Marshal(createReq)
	if err != nil {
		log.Error().Err(err).Msg("failed to marshal create session request")
		writeBrowserError(rw, http.StatusInternalServerError, "failed to create browser", err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(req.Context(), s.browserStartTimeout)
	defer cancel()

	url := s.selenosisURL + wdHubSessionPath
	innerReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(raw))
	if err != nil {
		log.Error().Err(err).Msg("failed to build create session request")
		writeBrowserError(rw, http.StatusInternalServerError, "failed to create browser", err.Error())
		return
	}

	innerReq.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(innerReq)
	if err != nil {
		log.Error().Err(err).Msg("failed to post create session request")
		writeBrowserError(rw, http.StatusBadGateway, "failed to create browser", err.Error())
		return
	}
	defer func() {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxHubResponseBody))
	if err != nil {
		log.Error().Err(err).Msg("failed to read create session response")
		writeBrowserError(rw, http.StatusBadGateway, "failed to create browser", err.Error())
		return
	}

	if resp.StatusCode != http.StatusOK {
		reason := hubError(body, resp.Status)
		log.Error().Str("status", resp.Status).Str("reason", reason).Msg("create session request failed")
		writeBrowserError(rw, upstreamStatus(resp.StatusCode), "failed to create browser", reason)
		return
	}

	var payload selenium.Payload
	if err := json.Unmarshal(body, &payload); err != nil {
		log.Error().Err(err).Msg("failed to decode create session response")
		writeBrowserError(rw, http.StatusBadGateway, "failed to create browser", hubError(body, resp.Status))
		return
	}

	sessionId, ok := payload.GetSessionId()
	if !ok {
		log.Error().Msg("create session response carries no session id")
		writeBrowserError(rw, http.StatusBadGateway, "failed to create browser", "hub response carries no session id")
		return
	}

	session, err := waitForSessionId(ctx, sessionId, s.sessionStore)
	if err != nil {
		log.Error().Err(err).Str("browserName", request.BrowserName).Msg("session did not become available in time")
		writeBrowserError(rw, http.StatusGatewayTimeout, "session did not become available in time", err.Error())
		return
	}

	rw.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(rw).Encode(session); err != nil {
		log.Error().Err(err).Msg("failed to encode session response")
		http.Error(rw, "failed to encode response", http.StatusInternalServerError)
		return
	}

	log.Info().Str("browserId", session.BrowserId).Str("browserName", request.BrowserName).Str("browserVersion", request.BrowserVersion).Msg("browser created")

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

	log = log.With().Str("browserId", browserId).Str("browserName", session.BrowserName).Str("browserVersion", session.BrowserVersion).Logger()

	target := fmt.Sprintf("%s%s/%s", s.selenosisURL, wdHubSessionPath, session.SessionId)

	innerReq, err := http.NewRequestWithContext(req.Context(), http.MethodDelete, target, nil)
	if err != nil {
		log.Error().Err(err).Msg("failed to build delete session request")
		http.Error(rw, "failed to delete browser", http.StatusInternalServerError)
		return
	}

	resp, err := httpClient.Do(innerReq)
	if err != nil {
		log.Error().Err(err).Msg("failed to send delete session request")
		http.Error(rw, "failed to delete browser", http.StatusInternalServerError)
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
	log.Info().Msg("browser deleted")
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

	targetURL := url.URL{
		Scheme: "ws",
		Host:   net.JoinHostPort(session.BrowserIP, "4445"),
		Path:   fmt.Sprintf("/selenosis/v1/vnc/%s", session.SessionId),
	}

	if !websocket.IsWebSocketUpgrade(req) {
		probeVNC(rw, req, browserId, targetURL.String())
		return
	}

	client, err := wsUpgrade(rw, req)
	if err != nil {
		log.Err(err).Str("browserId", browserId).Msg("client ws upgrade failed")
		return
	}
	defer client.Close()

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

func probeVNC(rw http.ResponseWriter, req *http.Request, browserId, target string) {
	log := logctx.FromContext(req.Context())

	backend, err := wsDial(target)
	if err != nil {
		log.Warn().Err(err).Str("browserId", browserId).Msg("vnc probe dial failed")
		http.Error(rw, "vnc is not available", http.StatusServiceUnavailable)
		return
	}
	defer backend.Close()

	greeting := make(chan error, 1)
	go func() {
		_, _, err := backend.ReadMessage()
		greeting <- err
	}()

	select {
	case err := <-greeting:
		if err != nil {
			log.Warn().Err(err).Str("browserId", browserId).Msg("vnc server did not greet")
			http.Error(rw, "vnc is not available", http.StatusServiceUnavailable)
			return
		}
	case <-time.After(vncProbeTimeout):
		log.Warn().Str("browserId", browserId).Msg("vnc probe timed out")
		http.Error(rw, "vnc is not available", http.StatusServiceUnavailable)
		return
	}

	rw.WriteHeader(http.StatusNoContent)
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

func hubError(body []byte, status string) string {
	var payload struct {
		Value struct {
			Message string `json:"message"`
		} `json:"value"`
	}
	if err := json.Unmarshal(body, &payload); err == nil && payload.Value.Message != "" {
		return truncateReason(payload.Value.Message)
	}

	if reason := strings.TrimSpace(string(body)); reason != "" {
		return truncateReason(reason)
	}

	return status
}

func truncateReason(reason string) string {
	if len(reason) <= maxHubErrorReason {
		return reason
	}

	cut := reason[:maxHubErrorReason]
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}

	return cut
}

func writeBrowserError(rw http.ResponseWriter, status int, message, reason string) {
	rw.Header().Set("Content-Type", "application/json")
	rw.WriteHeader(status)

	payload := struct {
		Error  string `json:"error"`
		Reason string `json:"reason,omitempty"`
	}{Error: message, Reason: reason}

	json.NewEncoder(rw).Encode(&payload) //nolint:errcheck
}

func upstreamStatus(status int) int {
	if status >= http.StatusBadRequest && status <= 599 {
		return status
	}
	return http.StatusBadGateway
}

func waitForSessionId(ctx context.Context, sessionId string, store store.Store[*types.Session]) (*types.Session, error) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		if session, ok := findSessionId(sessionId, store); ok {
			return session, nil
		}

		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("timeout waiting for session: %s", sessionId)
		case <-ticker.C:
		}
	}
}

func findSessionId(sessionId string, store store.Store[*types.Session]) (*types.Session, bool) {
	for _, session := range store.List() {
		if session.SessionId == sessionId {
			return session, true
		}
	}
	return nil, false
}
