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
	"careme/internal/ingredients/grading"

	"github.com/paulgmiller/kage/pkg/kage"
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
	if err := kage.Load(); err != nil {
		return fmt.Errorf("load environment: %w", err)
	}

	cacheStore, err := cache.MakeCache()
	if err != nil {
		return fmt.Errorf("create cache: %w", err)
	}

	// TODO: When review becomes store-specific, use cached store ingredients and
	// their embedded grades instead of depending on the grading manager's cache version.
	// Select the cached grader even when generation is currently disabled.
	manager := grading.NewManager(&config.Config{
		AI: config.AIConfig{APIKey: os.Getenv("AI_API_KEY")},
		IngredientGrading: config.IngredientGradingConfig{
			Enable: true,
			Model:  os.Getenv("INGREDIENT_GRADING_MODEL"),
		},
	}, cacheStore, http.DefaultClient)

	server := &http.Server{
		Addr:              *addr,
		Handler:           gradereview.NewHandler(cacheStore, manager.CacheVersion()),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("Ingredient grade review app listening at http://%s", *addr)
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
