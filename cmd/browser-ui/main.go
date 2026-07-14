package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"mime"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
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
	"github.com/alcounit/selenosis/v2/pkg/auth"
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

func cookieAuthMiddleware(authStore *auth.AuthStore, log zerolog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
			cookie, err := req.Cookie("browser_ui_auth")
			if err != nil {
				http.Error(rw, "authentication required", http.StatusUnauthorized)
				return
			}
			decoded, err := base64.StdEncoding.DecodeString(cookie.Value)
			if err != nil {
				http.Error(rw, "authentication required", http.StatusUnauthorized)
				return
			}
			parts := strings.SplitN(string(decoded), ":", 2)
			if len(parts) != 2 || !authStore.Authenticate(parts[0], parts[1]) {
				log.Error().Msg("request authentication failed")
				http.Error(rw, "authentication failed", http.StatusUnauthorized)
				return
			}
			req = req.WithContext(auth.WithOwner(req.Context(), auth.Owner{Name: parts[0]}))
			next.ServeHTTP(rw, req)
		})
	}
}

func main() {
	zerolog.TimeFieldFormat = time.RFC3339
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
	log := zerolog.New(os.Stdout).With().Timestamp().Logger()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	addr := env.GetEnvOrDefault("LISTEN_ADDR", ":8080")
	apiURL := env.GetEnvOrDefault("BROWSER_SERVICE_URL", "http://browser-service:8080")
	browserStartTimeout := env.GetEnvDurationOrDefault("BROWSER_STARTUP_TIMEOUT", 3*time.Minute)
	namespace := env.GetEnvOrDefault("BROWSER_NAMESPACE", "default")
	staticPath := env.GetEnvOrDefault("UI_STATIC_PATH", "/app/static")

	var authStore *auth.AuthStore
	if authFilePath := env.GetEnvOrDefault("BASIC_AUTH_FILE", ""); authFilePath != "" {
		var err error
		if authStore, err = auth.LoadFromJSONFile(authFilePath); err != nil {
			log.Fatal().Err(err).Str("path", authFilePath).Msg("BASIC_AUTH_FILE load error")
		}
		go auth.Watch(ctx, authStore)
		log.Info().Str("path", authFilePath).Msg("basic auth enabled")
	}

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

	collectorCtx := logctx.IntoContext(ctx, log)
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
		r.Get("/auth/config", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(map[string]bool{"authEnabled": authStore != nil}); err != nil {
				http.Error(w, "failed to encode response", http.StatusInternalServerError)
			}
		})

		r.Post("/auth/login", func(w http.ResponseWriter, req *http.Request) {
			if req.Body == nil {
				http.Error(w, "request body required", http.StatusBadRequest)
				return
			}
			defer req.Body.Close()
			var creds struct {
				Username string `json:"username"`
				Password string `json:"password"`
			}
			if err := json.NewDecoder(req.Body).Decode(&creds); err != nil {
				http.Error(w, "invalid request", http.StatusBadRequest)
				return
			}
			if authStore != nil && !authStore.Authenticate(creds.Username, creds.Password) {
				log.Error().Str("username", creds.Username).Msg("login failed")
				http.Error(w, "invalid credentials", http.StatusUnauthorized)
				return
			}
			if authStore != nil {
				token := base64.StdEncoding.EncodeToString([]byte(creds.Username + ":" + creds.Password))
				http.SetCookie(w, &http.Cookie{
					Name:     "browser_ui_auth",
					Value:    token,
					Path:     "/",
					HttpOnly: true,
					SameSite: http.SameSiteStrictMode,
				})
			}
			w.WriteHeader(http.StatusOK)
		})

		r.Post("/auth/logout", func(w http.ResponseWriter, _ *http.Request) {
			http.SetCookie(w, &http.Cookie{
				Name:     "browser_ui_auth",
				Value:    "",
				Path:     "/",
				HttpOnly: true,
				MaxAge:   -1,
				SameSite: http.SameSiteStrictMode,
			})
			w.WriteHeader(http.StatusOK)
		})

		r.Group(func(r chi.Router) {
			if authStore != nil {
				r.Use(cookieAuthMiddleware(authStore, log))
			}
			r.Route("/status", func(r chi.Router) {
				r.Get("/", svc.GetStatus)
			})
			r.Route("/browsers", func(r chi.Router) {
				r.Post("/", svc.CreateBrowser)
				r.Route("/{browserId}", func(r chi.Router) {
					r.Get("/", svc.GetBrowser)
					r.Delete("/", svc.DeleteBrowser)
					r.HandleFunc("/vnc", svc.RouteVNC)
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

	srv := &http.Server{
		Addr:    addr,
		Handler: router,
	}

	go func() {
		log.Info().Msgf("HTTP server listening %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal().Err(err).Msg("Server failed")
		}
	}()

	<-ctx.Done()
	stop()
	log.Info().Msg("Shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("Server shutdown error")
	}
}
