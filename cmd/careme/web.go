package main

import (
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

	"careme/internal/actowiz"
	"careme/internal/admin"
	"careme/internal/ai"
	"careme/internal/auth"
	"careme/internal/config"
	"careme/internal/ingredients"
	"careme/internal/locations"
	"careme/internal/recipes"
	"careme/internal/recipes/critique"
	"careme/internal/routing"
	"careme/internal/seasons"
	"careme/internal/sitemap"
	"careme/internal/static"
	"careme/internal/templates"
	"careme/internal/users"
	"careme/internal/watchdog"

	cachepkg "careme/internal/cache"

	utypes "careme/internal/users/types"
)

type homeUserLookup interface {
	FromRequest(ctx context.Context, r *http.Request, authClient auth.AuthClient) (*utypes.User, error)
}

type homeLocationLookup interface {
	GetLocationByID(ctx context.Context, locationID string) (*locations.Location, error)
}

func runServer(cfg *config.Config, addr string) error {
	cache, err := cachepkg.MakeCache()
	if err != nil {
		return fmt.Errorf("failed to create cache: %w", err)
	}
	imageCache, err := cachepkg.EnsureCache(recipes.RecipeImagesContainer)
	if err != nil {
		return fmt.Errorf("failed to create recipe image cache: %w", err)
	}

	authClient, err := auth.NewFromConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to create auth client: %w", err)
	}

	rootMux := http.NewServeMux()
	appRoutes := routing.Wrap(rootMux, func(h http.Handler) http.Handler {
		return authClient.WithAuthHTTP(AppMiddleWare(h, newRequestTrackerFromEnv()))
	})
	infraRoutes := routing.Wrap(rootMux, BaseMiddleware)

	authClient.Register(appRoutes)
	static.Register(infraRoutes)

	userStorage := users.NewStorage(cache)
	ro := &readyOnce{}
	watchdogServer := watchdog.Server{}

	var generator recipes.ExtGenerator
	var waitFns []func()

	if cfg.Mocks.Enable {
		generator = recipes.NewMockGenerator()
	} else {
		mc := critique.NewManager(cfg, cache)
		ro.Add(mc)
		aiclient := ai.NewClient(cfg.AI.APIKey, "TODOMODEL")
		ro.Add(aiclient)
		staples, err := recipes.NewCachedStaplesService(cfg, cache)
		if err != nil {
			return fmt.Errorf("failed to create staples service: %w", err)
		}
		watchdogServer.Add("staples", staples, 6.*time.Hour)
		generator, err = recipes.NewGenerator(aiclient, mc, staples)
		if err != nil {
			return fmt.Errorf("failed to create recipe generator: %w", err)
		}
		waitFns = append(waitFns, mc.Wait)
	}
	watchdogServer.Register(infraRoutes)

	centroids := locations.LoadCentroids()

	locationStorage, err := locations.New(cfg, cache, centroids)
	if err != nil {
		return fmt.Errorf("failed to create location server: %w", err)
	}

	userHandler := users.NewHandler(userStorage, locationStorage, authClient, users.NewUnsubscribeTokenFactory(*cfg))
	userHandler.Register(appRoutes)

	locationServer := locations.NewServer(locationStorage, centroids, userStorage)
	ro.Add(locationServer)
	locationServer.Register(appRoutes, authClient)

	sitemapHandler := sitemap.New(cache, cfg.ResolvedPublicOrigin())
	sitemapHandler.Register(infraRoutes)

	recipeHandler := recipes.NewHandler(cfg, userStorage, generator, locationStorage, cache, imageCache, authClient)
	recipeHandler.Register(appRoutes)
	waitFns = append([]func(){recipeHandler.Wait}, waitFns...)

	actowiz.NewServer(locationStorage).Register(infraRoutes)

	adminMux := http.NewServeMux()
	adminMux.Handle("/users", users.AdminUsersPage(userStorage))
	recipeIO := recipes.IO(cache)
	adminMux.Handle("/critiques", critique.AdminCritiquesPage(critique.NewStore(cache), recipeIO))
	ingredientsHandler := ingredients.NewHandler(cache)
	ingredientsHandler.Register(adminMux)
	appRoutes.Handle("/admin/", admin.New(cfg, authClient).Enforce(http.StripPrefix("/admin", adminMux)))

	appRoutes.HandleFunc("/about", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		data := templates.NewAboutPageData(ctx, seasons.GetCurrentStyle())
		if err := templates.About.Execute(w, data); err != nil {
			slog.ErrorContext(ctx, "about template execute error", "error", err)
			http.Error(w, "template error", http.StatusInternalServerError)
		}
	})
	registerHomeRoute(appRoutes, userStorage, locationStorage, authClient)

	// no logging for readyiness too noisy.
	rootMux.Handle("/ready", &recoverer{ro})

	server := &http.Server{
		Addr:    addr,
		Handler: rootMux,
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
		return gracefulShutdown(server, waitFns...)
	}
}

func registerHomeRoute(mux routing.Registrar, userStorage homeUserLookup, locationStorage homeLocationLookup, authClient auth.AuthClient) {
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		currentUser, err := userStorage.FromRequest(ctx, r, authClient)
		if err != nil {
			if !errors.Is(err, auth.ErrNoSession) {
				slog.ErrorContext(ctx, "failed to get user from request", "error", err)
				http.Error(w, "unable to load account", http.StatusInternalServerError)
				return
			}
			// no user is fine we'll just pass nil currentUser to template
			// just have two different templates?
		}

		var favoriteStoreName string
		if currentUser != nil && currentUser.FavoriteStore != "" {
			loc, locErr := locationStorage.GetLocationByID(ctx, currentUser.FavoriteStore)
			if locErr != nil {
				slog.ErrorContext(ctx, "failed to get location name for favorite store", "location_id", currentUser.FavoriteStore, "error", locErr)
			} else {
				favoriteStoreName = loc.Name
			}
		}
		data := struct {
			ClarityScript     template.HTML
			GoogleTagScript   template.HTML
			User              *utypes.User
			FavoriteStoreName string
			Style             seasons.Style
			ServerSignedIn    bool
		}{
			ClarityScript:     templates.ClarityScript(ctx),
			GoogleTagScript:   templates.GoogleTagScript(),
			User:              currentUser,
			FavoriteStoreName: favoriteStoreName,
			Style:             seasons.GetCurrentStyle(),
			ServerSignedIn:    currentUser != nil,
		}
		if err := templates.Home.Execute(w, data); err != nil {
			slog.ErrorContext(ctx, "home template execute error", "error", err)
			http.Error(w, "template error", http.StatusInternalServerError)
		}
	})
}

func gracefulShutdown(svr *http.Server, waitFns ...func()) error {
	// Give outstanding requests 25 seconds to complete (kubernetes has 30 second grace period)
	time.Sleep(5 * time.Second) // buffer to allow ingress ot update. only needed in prod
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
		for _, wait := range waitFns {
			wait()
		}
		close(done)
	}()

	// recipes can take several minutes to complete.
	ctx, cancel = context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
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
