package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"careme/internal/cache"
	"careme/internal/ingredients/gradereview"

	"github.com/paulgmiller/kage/pkg/kage"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		log.Fatal(err)
	}
}

func parseOptions(args []string) (gradereview.EvalOptions, error) {
	fs := flag.NewFlagSet("ingredientrevieweval", flag.ContinueOnError)
	location := fs.String("location", "", "only export reviews recorded at this store")
	version := fs.String("cache-version", "", "only export reviews for this ingredient grade cache version")
	if err := fs.Parse(args); err != nil {
		return gradereview.EvalOptions{}, err
	}
	if fs.NArg() != 0 {
		return gradereview.EvalOptions{}, fmt.Errorf("unexpected positional arguments")
	}
	return gradereview.EvalOptions{LocationID: strings.TrimSpace(*location), CacheVersion: strings.TrimSpace(*version)}, nil
}

func run(args []string, out io.Writer) error {
	options, err := parseOptions(args)
	if err != nil {
		return err
	}
	if err := kage.Load(); err != nil {
		return fmt.Errorf("load environment: %w", err)
	}
	c, err := cache.MakeCache()
	if err != nil {
		return fmt.Errorf("create cache: %w", err)
	}
	return gradereview.WriteEvalCases(context.Background(), out, c, options)
}
