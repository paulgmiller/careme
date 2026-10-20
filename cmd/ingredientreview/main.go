package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/ingredients/gradereview"
	"careme/internal/locations"
	"careme/internal/providerregistry"
	"careme/internal/recipes"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("ingredientreview", flag.ContinueOnError)
	addr := fs.String("addr", ":8090", "address for the ingredient grade review app")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := config.LoadOnlyProviders()
	if err != nil {
		return fmt.Errorf("load provider configuration: %w", err)
	}

	cacheStore, err := cache.MakeCache()
	if err != nil {
		return fmt.Errorf("create cache: %w", err)
	}

	centroids := locations.LoadCentroids()
	backends, err := providerregistry.NewFactory(cfg).NewLocationBackends(centroids)
	if err != nil {
		return fmt.Errorf("create location backends: %w", err)
	}
	locationStore, err := locations.New(cacheStore, centroids, backends)
	if err != nil {
		return fmt.Errorf("create location storage: %w", err)
	}
	store := gradereview.NewStore(cacheStore, locationStore, recipes.IO(cacheStore))
	server := &http.Server{
		Addr:              *addr,
		Handler:           gradereview.NewHandler(store),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("Ingredient grade review app listening at http://%s", *addr)
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
