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
	"careme/internal/sitemap"
	"careme/internal/static"
	"careme/internal/templates"
	"careme/internal/users"
	utypes "careme/internal/users/types"
	"context"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func runServer(cfg *config.Config, logsinkCfg logsink.Config, addr string) error {
	cache, err := cache.MakeCache()
	if err != nil {
		return fmt.Errorf("failed to create cache: %w", err)
	}

	authClient, err := auth.NewFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to create auth client: %w", err)
	}

	mux := http.NewServeMux()
	authClient.Register(mux)
	static.Register(mux)

	userStorage := users.NewStorage(cache)

	generator, err := recipes.NewGenerator(cfg, cache)
	if err != nil {
		return fmt.Errorf("failed to create recipe generator: %w", err)
	}

	locationStorage, err := locations.New(cfg)
	if err != nil {
		return fmt.Errorf("failed to create location server: %w", err)
	}

	userHandler := users.NewHandler(userStorage, locationStorage, authClient)
	userHandler.Register(mux)

	locationServer := locations.NewServer(locationStorage, userStorage)
	locationServer.Register(mux, authClient)

	sitemapHandler := sitemap.New(cache)
	sitemapHandler.Register(mux)

	recipeHandler := recipes.NewHandler(cfg, userStorage, generator, locationStorage, cache, authClient)
	recipeHandler.Register(mux)

	if logsinkCfg.Enabled() {
		logsHandler, err := logs.NewHandler(logsinkCfg)
		if err != nil {
			return fmt.Errorf("failed to create logs handler: %w", err)
		}
		logsHandler.Register(mux)
	}

	mux.HandleFunc("/about", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		data := struct {
			ClarityScript template.HTML
			Style         seasons.Style
		}{
			ClarityScript: templates.ClarityScript(),
			Style:         seasons.GetCurrentStyle(),
		}
		if err := templates.About.Execute(w, data); err != nil {
			slog.ErrorContext(ctx, "about template execute error", "error", err)
			http.Error(w, "template error", http.StatusInternalServerError)
		}
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		var currentUser *utypes.User
		currentUser, err := userStorage.FromRequest(ctx, r, authClient)
		if err != nil {
			if !errors.Is(err, auth.ErrNoSession) {
				slog.ErrorContext(ctx, "failed to get user from request", "error", err)
				http.Error(w, "unable to load account", http.StatusInternalServerError)
				return
			}
			//no user is fine we'll just pass nil currentUser to template
			// just have two different templates?

		}
		data := struct {
			ClarityScript  template.HTML
			User           *utypes.User
			Style          seasons.Style
			ServerSignedIn bool
		}{
			ClarityScript:  templates.ClarityScript(),
			User:           currentUser,
			Style:          seasons.GetCurrentStyle(),
			ServerSignedIn: currentUser != nil,
		}
		if err := templates.Home.Execute(w, data); err != nil {
			slog.ErrorContext(ctx, "home template execute error", "error", err)
			http.Error(w, "template error", http.StatusInternalServerError)
		}
	})

	ro := &readyOnce{}
	ro.Add(generator, locationServer)

	mux.Handle("/ready", ro)

	server := &http.Server{
		Addr:    addr,
		Handler: authClient.WithAuthHTTP(WithMiddleware(mux)),
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

	slog.Info("serving metrics at: %s", ":9090")
	go http.ListenAndServe(":9090", promhttp.Handler())

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
