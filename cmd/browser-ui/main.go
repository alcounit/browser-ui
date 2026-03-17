package main

import (
	"context"
	"encoding/json"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/alcounit/browser-service/pkg/broadcast"
	"github.com/alcounit/browser-service/pkg/client"
	browserclient "github.com/alcounit/browser-service/pkg/client/browser"
	browserconfigclient "github.com/alcounit/browser-service/pkg/client/browserconfig"
	"github.com/alcounit/browser-service/pkg/event"
	"github.com/alcounit/browser-ui/pkg/collector"
	"github.com/alcounit/browser-ui/pkg/types"
	"github.com/alcounit/browser-ui/service"
	"github.com/alcounit/seleniferous/v2/pkg/store"
	"github.com/alcounit/selenosis/v2/pkg/env"
	"github.com/rs/zerolog"

	"github.com/go-chi/chi/middleware"
	"github.com/go-chi/chi/v5"

	logctx "github.com/alcounit/browser-controller/pkg/log"
)

func init() {
	mime.AddExtensionType(".js", "application/javascript")
	mime.AddExtensionType(".mjs", "application/javascript")
	mime.AddExtensionType(".css", "text/css")
}

func main() {
	zerolog.TimeFieldFormat = time.RFC3339
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	log := zerolog.New(os.Stdout).With().Timestamp().Logger()

	addr := env.GetEnvOrDefault("LISTEN_ADDR", ":8080")
	apiURL := env.GetEnvOrDefault("BROWSER_SERVICE_URL", "http://browser-service:8080")
	browserStartTimeout := env.GetEnvDurationOrDefault("BROWSER_STARTUP_TIMEOUT", 3*time.Minute)
	namespace := env.GetEnvOrDefault("BROWSER_NAMESPACE", "default")
	vncPassword := env.GetEnvOrDefault("VNC_PASSWORD", "secret")
	staticPath := env.GetEnvOrDefault("UI_STATIC_PATH", "/app/static")

	sessionStore := store.NewDefaultStore[*types.Session]()
	browserStore := store.NewDefaultStore[types.BrowserVersions]()

	clientConfig := client.ClientConfig{
		BaseURL:    apiURL,
		HTTPClient: http.DefaultClient,
		Logger:     log,
	}

	browserClient, err := browserclient.NewClient(clientConfig)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create Browser client")
	}

	browserConfigClient, err := browserconfigclient.NewClient(clientConfig)
	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create BrowserConfig client")
	}

	broadcaster := broadcast.NewBroadcaster[event.BrowserEvent](10)
	col := collector.NewCollector(browserClient, browserConfigClient, namespace, sessionStore, browserStore, broadcaster)

	collectorCtx := logctx.IntoContext(context.Background(), log)
	go func() {
		for {
			err := col.Run(collectorCtx)
			if err == nil || collectorCtx.Err() != nil {
				return
			}
			log.Error().Err(err).Msgf("event collector failed, retrying in 5s")
			time.Sleep(5 * time.Second)
		}
	}()
	log.Info().Msgf("event collector started, connected to %s", apiURL)

	svc := service.NewService(browserClient, namespace, sessionStore, browserStore, browserStartTimeout)

	router := chi.NewRouter()
	router.Use(middleware.Recoverer)
	router.Use(func(next http.Handler) http.Handler {
		fn := func(rw http.ResponseWriter, req *http.Request) {
			logger := log.With().
				Str("method", req.Method).
				Str("path", req.URL.Path).
				Logger()

			ctx := req.Context()
			ctx = logctx.IntoContext(ctx, logger)

			next.ServeHTTP(rw, req.WithContext(ctx))
		}
		return http.HandlerFunc(fn)
	})

	if _, err := os.Stat(staticPath); err != nil {
		log.Fatal().Err(err).Msg("static directory missing")
	}

	router.Route("/api/v1", func(r chi.Router) {
		r.Route("/status", func(r chi.Router) {
			r.Get("/", svc.GetStatus)
		})
		r.Route("/browsers", func(r chi.Router) {
			r.Post("/", svc.CreateBrowser)
			r.Route("/{browserId}", func(r chi.Router) {
				r.Get("/", svc.GetBrowser)
				r.Delete("/", svc.DeleteBrowser)
				r.HandleFunc("/vnc", svc.RouteVNC)
				r.HandleFunc("/vnc/settings", func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					if err := json.NewEncoder(w).Encode(map[string]string{"password": vncPassword}); err != nil {
						log.Error().Err(err).Msg("failed to encode password response")
						http.Error(w, "failed to encode response", http.StatusInternalServerError)
						return
					}
				})
			})
		})
	})

	router.Get("/ui", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join(staticPath, "index.html"))
	})
	router.Get("/ui/", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, filepath.Join(staticPath, "index.html"))
	})

	fileServer := http.FileServer(http.Dir(staticPath))
	router.Handle("/ui/*", http.StripPrefix("/ui/", fileServer))

	router.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/ui/", http.StatusFound)
	})

	router.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"status":"ok"}`))
	})

	if err := http.ListenAndServe(addr, router); err != nil {
		log.Fatal().Err(err).Msg("Server failed")
	}
}
