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
	"github.com/alcounit/browser-service/pkg/event"
	"github.com/alcounit/browser-ui/internal/service"
	"github.com/alcounit/browser-ui/pkg/collector"
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
	namespace := env.GetEnvOrDefault("BROWSER_NAMESPACE", "default")
	vncPassword := env.GetEnvOrDefault("VNC_PASSWORD", "secret")
	staticPath := env.GetEnvOrDefault("UI_STATIC_PATH", "/app/static")

	store := store.NewDefaultStore()
	browserClient, err := client.NewClient(client.ClientConfig{
		BaseURL:    apiURL,
		HTTPClient: http.DefaultClient,
		Logger:     log,
	})

	if err != nil {
		log.Fatal().Err(err).Msg("Failed to create Browser client")
	}

	broadcaster := broadcast.NewBroadcaster[event.BrowserEvent](10)
	client := collector.NewCollector(browserClient, namespace, store, broadcaster)

	collectorCtx := logctx.IntoContext(context.Background(), log)
	go client.Run(collectorCtx)
	log.Info().Msgf("event collector started, connected to %s", apiURL)

	svc := service.NewService(store)

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
		r.Route("/browsers", func(r chi.Router) {
			r.Get("/", svc.ListBrowsers)
			r.Route("/{browserId}", func(r chi.Router) {
				r.Get("/", svc.GetBrowser)
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
