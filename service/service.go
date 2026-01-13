package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"

	logctx "github.com/alcounit/browser-controller/pkg/log"
	"github.com/alcounit/browser-ui/pkg/types"
	"github.com/alcounit/seleniferous/v2/pkg/store"
	"github.com/gorilla/websocket"

	"github.com/go-chi/chi/v5"
)

type Service struct {
	store store.Store
}

func NewService(store store.Store) *Service {
	return &Service{
		store: store,
	}
}

func (s Service) GetBrowser(rw http.ResponseWriter, req *http.Request) {
	log := logctx.FromContext(req.Context())

	browserId := chi.URLParam(req, "browserId")
	session, ok := s.store.Get(browserId)
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

func (s *Service) ListBrowsers(rw http.ResponseWriter, req *http.Request) {
	log := logctx.FromContext(req.Context())

	sessions := s.store.List()
	rw.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(rw).Encode(sessions); err != nil {
		log.Error().Err(err).Msg("failed to encode sessions response")
		http.Error(rw, "failed to encode response", http.StatusInternalServerError)
		return
	}
	log.Info().Msg("session list retrived")
}

func (s *Service) RouteVNC(rw http.ResponseWriter, req *http.Request) {
	log := logctx.FromContext(req.Context())

	browserId := chi.URLParam(req, "browserId")

	val, ok := s.store.Get(browserId)
	if !ok {
		log.Error().Str("browserId", browserId).Msgf("unknown browserId")
		http.Error(rw, "invalid session", http.StatusBadRequest)
		return
	}

	session := val.(*types.Session)
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}

	client, err := upgrader.Upgrade(rw, req, nil)
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

	dialer := websocket.Dialer{}
	backend, _, err := dialer.Dial(targetURL.String(), nil)
	if err != nil {
		log.Err(err).Str("browserId", browserId).Str("url", targetURL.String()).Msg("backend ws dial failed")
		return
	}
	defer backend.Close()

	log.Info().Str("browserId", browserId).Msg("ws connection established")

	errCh := make(chan error, 1)

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
