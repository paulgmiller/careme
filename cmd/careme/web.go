package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"careme/internal/actowiz"
	"careme/internal/admin"
	"careme/internal/ai"
	"careme/internal/appredirect"
	"careme/internal/auth"
	"careme/internal/campaigns"
	"careme/internal/cocktails"
	"careme/internal/config"
	"careme/internal/farmersmarket"
	"careme/internal/ingredients"
	ingredientgrading "careme/internal/ingredients/grading"
	"careme/internal/kroger"
	"careme/internal/locations"
	"careme/internal/recipes"
	"careme/internal/recipes/critique"
	"careme/internal/recipes/prompts"
	"careme/internal/recipes/status"
	"careme/internal/routing"
	"careme/internal/seasons"
	"careme/internal/sitemap"
	"careme/internal/static"
	"careme/internal/templates"
	"careme/internal/users"
	"careme/internal/watchdog"

	cachepkg "careme/internal/cache"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

type waiter interface {
	Wait()
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
		return authClient.WithAuthHTTP(appMiddleware(h))
	})
	infraRoutes := routing.Wrap(rootMux, baseMiddleware)

	authClient.Register(appRoutes)
	campaigns.Register(appRoutes) // could be infra routes?
	static.Register(infraRoutes)
	appredirect.Register(infraRoutes)

	userStorage := users.NewStorage(cache)
	ro := &readyOnce{}
	watchdogServer := watchdog.Server{}
	aiHTTPClient := &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}
	// TODO  make the mock more transparent?
	grader := ingredientgrading.NewManager(cfg, cache, aiHTTPClient)

	centroids := locations.LoadCentroids()
	locationStorage, err := locations.New(cfg, cache, centroids)
	if err != nil {
		return fmt.Errorf("failed to create location server: %w", err)
	}

	var cocktailLocations cocktails.Locations
	var cocktailCatalog cocktails.Catalog
	var cocktailPlanner cocktails.Planner
	var generator recipes.ExtGenerator
	var imageGen recipes.ImageGen
	var marketExtractor farmersmarket.IngredientExtractor
	var waiters []waiter
	if cfg.Mocks.Enable {
		cocktailLocations = locationStorage
		cocktailCatalog = cocktails.Mock{}
		cocktailPlanner = cocktails.Mock{}
		mc := critique.NewMock(cache)
		generator = recipes.NewMockGenerator(recipes.IO(cache), mc)
		imageGen = recipes.NewMockImageGen()
		marketExtractor = farmersmarket.MockExtractor{}

	} else {
		critiquer := critique.NewManager(cfg, cache, aiHTTPClient)
		ro.add(critiquer)

		aiclient := ai.NewClient(cfg.AI.APIKey, "TODOMODEL", aiHTTPClient, prompts.NewCacheRecorder(cache))
		cocktailLocations, err = kroger.NewLocationBackendFromConfig(cfg, aiHTTPClient)
		if err != nil {
			return fmt.Errorf("create cocktail locations: %w", err)
		}
		cocktailPlanner = aiclient
		cocktailCatalog, err = kroger.NewCocktailProvider(cfg, aiHTTPClient)
		if err != nil {
			return fmt.Errorf("create cocktail catalog: %w", err)
		}
		imageGen = aiclient
		marketExtractor = aiclient
		ro.add(aiclient)
		staples, err := recipes.NewCachedStaplesService(cfg, cache, grader)
		if err != nil {
			return fmt.Errorf("failed to create staples service: %w", err)
		}
		watchdogServer.Add("staples", recipes.NewStaplesWatchdog(locationStorage, staples), 6.*time.Hour)
		ss := status.NewStore(cache)
		generator, err = recipes.NewGenerator(aiclient, critiquer, staples, ss, recipes.IO(cache))
		if err != nil {
			return fmt.Errorf("failed to create recipe generator: %w", err)
		}
		waiters = append(waiters, critiquer)
	}
	cocktailHandler := cocktails.New(cocktailCatalog, cocktailPlanner, cocktailLocations, cache, centroids)
	cocktailHandler.Register(appRoutes)
	waiters = append(waiters, cocktailHandler)
	watchdogServer.Register(infraRoutes)

	userHandler := users.NewHandler(userStorage, locationStorage, authClient, users.NewUnsubscribeTokenFactory(*cfg), cfg.ResolvedPublicOrigin())
	userHandler.Register(appRoutes)

	locationServer := locations.NewServer(locationStorage, centroids, userStorage, recipes.NewCachedProduceScorer(recipes.IO(cache)))
	ro.add(locationServer)
	locationServer.Register(appRoutes, authClient)

	farmersMarketCache, err := cachepkg.EnsureCache(farmersmarket.Container)
	if err != nil {
		return fmt.Errorf("failed to create farmers market cache: %w", err)
	}
	farmersMarketStore := farmersmarket.NewStore(farmersMarketCache)
	farmersMarketUploader := farmersmarket.NewUploader(farmersMarketStore)
	farmersMarketHandler := farmersmarket.NewHandler(farmersMarketUploader, farmersMarketCache, authClient, marketExtractor)
	farmersMarketHandler.Register(appRoutes)
	waiters = append(waiters, farmersMarketHandler)

	sitemapHandler := sitemap.New(cache, cfg.ResolvedPublicOrigin(), locationStorage)
	sitemapHandler.Register(infraRoutes)

	recipeHandler := recipes.NewHandler(cfg, userStorage, generator, locationStorage, cache, imageCache, authClient, imageGen)
	recipeHandler.Register(appRoutes)
	waiters = append([]waiter{recipeHandler}, waiters...)
	campaigns.RegisterAdvertisedRecipeGeneration(infraRoutes, locationStorage, recipeHandler)

	actowiz.NewServer(locationStorage).Register(infraRoutes)

	adminMux := http.NewServeMux()
	adminMux.Handle("/{$}", admin.Page())
	adminMux.Handle("/users", users.AdminUsersPage(userStorage))
	recipeIO := recipes.IO(cache)
	adminMux.Handle("/params/{hash}", recipes.AdminParamsJSON(cache))
	adminMux.Handle("/prompt/menu/{hash}", prompts.AdminMenuPromptJSON(cache))
	adminMux.Handle("/prompt/recipe/{hash}", prompts.AdminRecipePromptJSON(cache))
	adminMux.Handle("/mealplan/{hash}", recipes.AdminMealPlanPage(recipeIO))
	ingredientsHandler := ingredients.NewHandler(cache)
	ingredientsHandler.Register(adminMux)
	appRoutes.Handle("/admin/", admin.New(cfg, authClient).Enforce(http.StripPrefix("/admin", adminMux)))
	appRoutes.Handle("/critiques/{hash}", critique.CritiquePage(critique.NewStore(cache), recipeIO))

	appRoutes.HandleFunc("/about", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		data := templates.NewAboutPageData(ctx, seasons.GetCurrentStyle())
		if err := templates.About.Execute(w, data); err != nil {
			slog.ErrorContext(ctx, "about template execute error", "error", err)
			http.Error(w, "template error", http.StatusInternalServerError)
		}
	})
	appRoutes.HandleFunc("/privacy", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		data := templates.NewPrivacyPageData(seasons.GetCurrentStyle())
		if err := templates.Privacy.Execute(w, data); err != nil {
			slog.ErrorContext(ctx, "privacy template execute error", "error", err)
			http.Error(w, "template error", http.StatusInternalServerError)
		}
	})
	appRoutes.HandleFunc("GET /temperature-guide", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		data := templates.NewTemperatureGuidePageData(seasons.GetCurrentStyle())
		if err := templates.TemperatureGuide.Execute(w, data); err != nil {
			slog.ErrorContext(ctx, "temperature guide template execute error", "error", err)
			http.Error(w, "template error", http.StatusInternalServerError)
		}
	})
	home{userStorage, locationStorage, authClient}.Register(appRoutes)

	// no logging for readyiness too noisy.
	rootMux.Handle("/ready", &recoverer{ro})

	return serve(addr, rootMux, waiters)
}

func serve(addr string, handler http.Handler, waiters []waiter) error {
	server := &http.Server{
		Addr:    addr,
		Handler: handler,
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
	defer signal.Stop(shutdown)

	// Block until we receive a signal or server error
	select {
	case err := <-serverErrors:
		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("server error: %w", err)
		}
		return nil
	case sig := <-shutdown:
		slog.Info("Shutdown signal received", "signal", sig)
		return gracefulShutdown(server, waiters...)
	}
}

func gracefulShutdown(svr *http.Server, waiters ...waiter) error {
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
		for _, wait := range waiters {
			wait.Wait()
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
