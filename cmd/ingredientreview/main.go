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
	"careme/internal/ingredients/gradereview"

	"github.com/paulgmiller/kage/pkg/kage"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		log.Fatal(err)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("ingredientreview", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:8090", "address for the ingredient grade review app")
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

	server := &http.Server{
		Addr:              *addr,
		Handler:           gradereview.NewHandler(cacheStore),
		ReadHeaderTimeout: 5 * time.Second,
	}
	log.Printf("Ingredient grade review app listening at http://%s", *addr)
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
