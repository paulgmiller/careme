package main

import (
	"careme/internal/auth"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/locations"
	"careme/internal/logs"
	"careme/internal/logsink"
	"careme/internal/recipes"
	"careme/internal/seasons"
	"careme/internal/templates"
	"careme/internal/users"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/clerk/clerk-sdk-go/v2/session"
)

//go:embed favicon.png
var favicon []byte

//go:embed static/tailwind.css
var tailwindCSS []byte

const sessionDuration = 365 * 24 * time.Hour

func runServer(cfg *config.Config, logsinkCfg logsink.Config, addr string) error {
	cache, err := cache.MakeCache()
	if err != nil {
		return fmt.Errorf("failed to create cache: %w", err)
	}

	userStorage := users.NewStorage(cache)
	authHandler := auth.NewClerkAuth(cfg, userStorage)

	generator, err := recipes.NewGenerator(cfg, cache)
	if err != nil {
		return fmt.Errorf("failed to create recipe generator: %w", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/static/tailwind.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		if _, err := w.Write(tailwindCSS); err != nil {
			slog.ErrorContext(r.Context(), "failed to write tailwind css", "error", err)
		}
	})

	locationserver, err := locations.New(context.TODO(), cfg)
	if err != nil {
		return fmt.Errorf("failed to create location server: %w", err)
	}
	locations.Register(locationserver, mux)

	userHandler := users.NewHandler(userStorage, locationserver)
	userHandler.Register(mux)

	recipeHandler := recipes.NewHandler(cfg, userStorage, generator, locationserver, cache)
	recipeHandler.Register(mux)

	if logsinkCfg.Enabled() {
		logsHandler, err := logs.NewHandler(logsinkCfg)
		if err != nil {
			return fmt.Errorf("failed to create logs handler: %w", err)
		}
		logsHandler.Register(mux)
	}

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		currentUser, err := users.FromRequest(r, userStorage)
		if err != nil {
			if errors.Is(err, users.ErrNotFound) {
				users.ClearCookie(w)
			} else {
				slog.ErrorContext(ctx, "failed to load user from cookie", "error", err)
				http.Error(w, "unable to load account", http.StatusInternalServerError)
				return
			}
		}
		data := struct {
			ClarityScript template.HTML
			User          *users.User
			Style         seasons.Style
			UseClerk      bool
			SignInURL     string
			SignUpURL     string
			ClerkFrontend string
			ClerkPublish  string
		}{
			ClarityScript: templates.ClarityScript(),
			User:          currentUser,
			Style:         seasons.GetCurrentStyle(),
			UseClerk:      authHandler.Enabled(),
			SignInURL:     addRedirectParam(authHandler.SignInURL(), currentRedirectURL(r)),
			SignUpURL:     addRedirectParam(authHandler.SignUpURL(), currentRedirectURL(r)),
			ClerkFrontend: cfg.Clerk.FrontendAPI,
			ClerkPublish:  cfg.Clerk.PublishableKey,
		}
		if err := templates.Home.Execute(w, data); err != nil {
			slog.ErrorContext(ctx, "home template execute error", "error", err)
			http.Error(w, "template error", http.StatusInternalServerError)
		}
	})

	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if authHandler.Enabled() {
			http.Redirect(w, r, authHandler.SignInURL(), http.StatusSeeOther)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form submission", http.StatusBadRequest)
			return
		}
		email := strings.TrimSpace(r.FormValue("email"))
		if email == "" {
			http.Error(w, "email is required", http.StatusBadRequest)
			return
		}
		user, err := userStorage.FindOrCreateByEmail(email)
		if err != nil {
			slog.ErrorContext(r.Context(), "failed to find or create user", "error", err)
			http.Error(w, fmt.Sprintf("unable to sign in: %v", err), http.StatusInternalServerError)
			return
		}
		users.SetCookie(w, user.ID, sessionDuration)
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	mux.HandleFunc("/logout", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if authHandler.Enabled() {
			if claims, ok := clerk.SessionClaimsFromContext(r.Context()); ok && claims.SessionID != "" {
				if _, err := session.Revoke(r.Context(), &session.RevokeParams{ID: claims.SessionID}); err != nil {
					slog.WarnContext(r.Context(), "failed to revoke clerk session", "error", err)
				}
			}
		}
		users.ClearCookie(w)
		if authHandler.Enabled() && authHandler.SignOutURL() != "" {
			http.Redirect(w, r, authHandler.SignOutURL(), http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write([]byte("OK")); err != nil {
			slog.ErrorContext(r.Context(), "failed to write readiness response", "error", err)
		}
	})

	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png") // <= without this, many UAs ignore it
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		if _, err := w.Write(favicon); err != nil {
			slog.ErrorContext(r.Context(), "failed to write favicon", "error", err)
		}
	})

	server := &http.Server{
		Addr:    addr,
		Handler: WithMiddleware(authHandler.Middleware(mux)),
	}

	// Channel to listen for errors coming from the server
	serverErrors := make(chan error, 1)

	// Start the server in a goroutine
	go func() {
		slog.Info("Serving Careme", "address", addr)
		serverErrors <- server.ListenAndServe()
	}()

	// Channel to listen for interrupt or terminate signals
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	// Block until we receive a signal or server error
	select {
	case err := <-serverErrors:
		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("server error: %w", err)
		}
		return nil
	case sig := <-shutdown:
		slog.Info("Shutdown signal received", "signal", sig)
		return gracefulShutdown(server, recipeHandler.Wait)
	}
}

func gracefulShutdown(svr *http.Server, recipesWait func()) error {
	// Give outstanding requests 25 seconds to complete (kubernetes has 30 second grace period)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	// Gracefully shutdown the HTTP server
	if err := svr.Shutdown(ctx); err != nil {
		slog.Error("Server shutdown error", "error", err)
		// Force close after timeout
		if closeErr := svr.Close(); closeErr != nil {
			slog.Error("Server close error", "error", closeErr)
		}
		return err
	}

	done := make(chan struct{})
	go func() {
		recipesWait()
		close(done)
	}()

	// Wait for all recipe generation goroutines to complete
	slog.Info("Waiting for recipe generation goroutines to complete")

	select {
	case <-done:
		slog.Info("All recipe generation goroutines completed")
	case <-ctx.Done():
		slog.Warn("Timeout waiting for recipe generation goroutines")
		return ctx.Err()
	}
	return nil
}

func currentRedirectURL(r *http.Request) string {
	if r == nil {
		return ""
	}
	scheme := requestScheme(r)
	host := forwardedHost(r)
	if host == "" {
		host = r.Host
	}
	if scheme == "" || host == "" {
		return ""
	}
	u := url.URL{
		Scheme: scheme,
		Host:   host,
		Path:   r.URL.Path,
	}
	u.RawQuery = r.URL.RawQuery
	return u.String()
}

func requestScheme(r *http.Request) string {
	if r == nil {
		return ""
	}
	if proto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); proto != "" {
		return proto
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

func forwardedHost(r *http.Request) string {
	if r == nil {
		return ""
	}
	if host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); host != "" {
		return host
	}
	return ""
}

func addRedirectParam(baseURL, redirectURL string) string {
	if baseURL == "" || redirectURL == "" {
		return baseURL
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return baseURL
	}
	q := parsed.Query()
	if q.Get("redirect_url") == "" {
		q.Set("redirect_url", redirectURL)
	}
	parsed.RawQuery = q.Encode()
	return parsed.String()
}
