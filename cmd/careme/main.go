package main

import (
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/locations"
	"careme/internal/logsink"
	"careme/internal/recipes"
	"context"
	_ "embed"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"os"
	"time"

	"github.com/alpkeskin/gotoon"
	multi "github.com/samber/slog-multi"
)

func main() {
	var location string
	var zipcode string
	var ingredient string
	var serve, mail bool
	var addr string

	flag.StringVar(&location, "location", "", "Location for recipe sourcing (e.g., 70100023)")
	flag.StringVar(&location, "l", "", "Location for recipe sourcing (short form)")
	flag.StringVar(&zipcode, "zipcode", "", "return location ids for a zip code.")
	flag.StringVar(&zipcode, "z", "", "return location ids for a zip code (short form)")
	flag.StringVar(&ingredient, "ingredient", "", "just list ingredients")
	flag.StringVar(&ingredient, "i", "", "just list ingredients (short form)")
	flag.BoolVar(&serve, "serve", false, "Run HTTP server mode")
	flag.BoolVar(&mail, "mail", false, "Run mail sender loop")
	flag.StringVar(&addr, "addr", ":8080", "Address to bind in server mode")
	flag.Parse()

	if err := os.MkdirAll("recipes", 0755); err != nil {
		log.Fatalf("failed to create recipes directory: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load configuration: %v", err)
	}

	logcfg := logsink.ConfigFromEnv("logs")
	if logcfg.Enabled() {
		handler, closer, err := logsink.NewJson(ctx, logcfg)
		if err != nil {
			log.Fatalf("failed to create logsink: %v", err)
		}
		defer closer.Close()
		slog.SetDefault(slog.New(multi.Fanout(handler, slog.NewTextHandler(os.Stdout, nil))))
		// log.SetOutput(os.Stdout) // https://github.com/golang/go/issues/61892

	}

	if mail {
		mailer, err := NewMailer(cfg)
		if err != nil {
			log.Fatalf("failed to create mailer: %v", err)
		}
		slog.InfoContext(ctx, "mail sender engaged")
		go mailer.Iterate(ctx, 1*time.Hour)
	}

	if serve {
		if err := runServer(cfg, logcfg, addr); err != nil {
			log.Fatalf("server error: %v", err)
		}
		return
	}

	if zipcode != "" {
		ls, err := locations.New(ctx, cfg)
		if err != nil {
			log.Fatalf("failed to create location server: %v", err)
		}
		locs, err := ls.GetLocationsByZip(ctx, zipcode)
		if err != nil {
			log.Fatalf("failed to get locations for zip %s: %v", zipcode, err)
		}
		fmt.Printf("Locations for zip code %s:\n", zipcode)
		for _, loc := range locs {
			fmt.Printf("- %s, %s: %s\n", loc.Name, loc.Address, loc.ID)
		}
		return
	}

	if location == "" {
		fmt.Println("Error: Location is required (or use -serve for web mode)")
		os.Exit(1)
	}

	if err := run(cfg, location, ingredient); err != nil {
		log.Fatalf("Error: %v", err)
	}
}

func run(cfg *config.Config, location string, ingredient string) error {
	ctx := context.Background()
	cache, err := cache.MakeCache()
	if err != nil {
		return fmt.Errorf("failed to create cache: %w", err)
	}

	generator, err := recipes.NewGenerator(cfg, cache)
	if err != nil {
		return fmt.Errorf("failed to create recipe generator: %w", err)
	}

	// just use the kroger client directly or punt all this and go pure web
	g := generator.(*recipes.Generator)

	if ingredient != "" {
		f := recipes.Filter(ingredient, []string{"*"}, false /*frozen*/)
		ings, err := g.GetIngredients(ctx, location, f, 0)
		if err != nil {
			return fmt.Errorf("failed to get ingredients: %w", err)
		}
		encoded, err := gotoon.Encode(ings)
		if err != nil {
			return fmt.Errorf("failed to encode ingredients to TOON: %w", err)
		}
		fmt.Println(encoded)
		return nil
	}

	ls, err := locations.New(ctx, cfg)
	if err != nil {
		return fmt.Errorf("failed to create location server: %w", err)
	}

	l, err := ls.GetLocationByID(ctx, location) // get details but ignore error
	if err != nil {
		return fmt.Errorf("could not get location details: %w", err)
	}

	p := recipes.DefaultParams(l, time.Now())
	ingredients, err := g.GetStaples(ctx, p)
	if err != nil {
		return fmt.Errorf("failed to get staple ingredients: %w", err)
	}
	log.Println("Staple Ingredients:")
	for _, ing := range ingredients {
		fmt.Printf("- %s\n", *ing.Description)
	}

	return nil
}
