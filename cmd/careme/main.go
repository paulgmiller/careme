package main

import (
	"context"
	_ "embed"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/locations"
	"careme/internal/recipes"
)

//go:embed html/spinner.html
var spinnerHTML []byte

func main() {
	var location string
	var zipcode string
	var serve bool
	var addr string
	var help bool

	flag.StringVar(&location, "location", "", "Location for recipe sourcing (e.g., 70100023)")
	flag.StringVar(&location, "l", "", "Location for recipe sourcing (short form)")
	flag.StringVar(&zipcode, "zipcode", "", "return location ids for a zip code.")
	flag.StringVar(&zipcode, "z", "", "return location ids for a zip code (short form)")
	flag.BoolVar(&serve, "serve", false, "Run HTTP server mode")
	flag.StringVar(&addr, "addr", ":8080", "Address to bind in server mode")
	flag.BoolVar(&help, "help", false, "Show help message")
	flag.BoolVar(&help, "h", false, "Show help message")
	flag.Parse()

	if help {
		showHelp()
		return
	}

	if err := os.MkdirAll("recipes", 0755); err != nil {
		log.Fatalf("failed to create recipes directory: %v", err)
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("failed to load configuration: %v", err)
	}

	if serve {
		if err := runServer(cfg, addr); err != nil {
			log.Fatalf("server error: %v", err)
		}
		return
	}

	if zipcode != "" {

		locs, err := locations.GetLocationsByZip(context.TODO(), cfg, zipcode)
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
		showHelp()
		os.Exit(1)
	}

	if err := run(cfg, location); err != nil {
		log.Fatalf("Error: %v", err)
	}
}

func runServer(cfg *config.Config, addr string) error {

	cache := cache.NewFileCache("cache")

	mux := http.NewServeMux()
	generator, err := recipes.NewGenerator(cfg, cache)
	if err != nil {
		return fmt.Errorf("failed to create recipe generator: %w", err)
	}

	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	})
	mux.HandleFunc("/locations", func(w http.ResponseWriter, r *http.Request) {
		zip := r.URL.Query().Get("zip")
		if zip == "" {
			log.Printf("no zip code provided to /locations")
			http.Error(w, "provide a zip code with ?zip=12345", http.StatusBadRequest)
			return
		}
		locs, err := locations.GetLocationsByZip(context.TODO(), cfg, zip)
		if err != nil {
			log.Printf("failed to get locations for zip %s: %v", zip, err)
			http.Error(w, "could not get locations", http.StatusInternalServerError)
			return
		}
		// Render locations
		w.Write([]byte(locations.Html(locs, zip)))
	})

	mux.HandleFunc("/recipes", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		loc := r.URL.Query().Get("location")
		if loc == "" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("specify a location id to generate recipes"))
			return
		}
		var dateStr string
		if dateStr = r.URL.Query().Get("date"); dateStr == "" {
			http.Redirect(w, r, "/recipes?location="+loc+"&date="+time.Now().Format("2006-01-02"), http.StatusSeeOther)
			return
		}
		var date time.Time
		if date, err = time.Parse("2006-01-02", dateStr); err != nil {
			http.Error(w, "invalid date format, use YYYY-MM-DD", http.StatusBadRequest)
			return
		}
		l, err := locations.GetLocationByID(ctx, cfg, loc) // get details but ignore error
		if err != nil {
			http.Error(w, "could not get location details", http.StatusBadRequest)
			return
		}

		p := recipes.DefaultParams(l)

		p.Date = date
		if i := r.URL.Query().Get("instructions"); i != "" {
			log.Println("got instructions " + i)
			p.Instructions = i
		}

		if recipe, ok := cache.Get(p.Hash() + ".recipe"); ok {
			log.Printf("serving cached recipes for %s on %s", loc, date.Format("2006-01-02"))
			_, _ = w.Write([]byte(recipes.FormatChatHTML(*l, date, string(recipe))))
			return
		}
		go func() {

			_, err := generator.GenerateRecipes(p)
			if err != nil {
				log.Printf("generate error: %v", err)
				return
			}

		}()

		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
		_, _ = w.Write(spinnerHTML)
	})

	log.Printf("Serving Careme on %s", addr)
	return http.ListenAndServe(addr, mux)
}

func run(cfg *config.Config, location string) error {

	generator, err := recipes.NewGenerator(cfg, cache.NewFileCache("cache"))
	if err != nil {
		return fmt.Errorf("failed to create recipe generator: %w", err)
	}

	l, err := locations.GetLocationByID(context.TODO(), cfg, location) // get details but ignore error
	if err != nil {
		return fmt.Errorf("could not get location details: %w", err)
	}

	p := recipes.DefaultParams(l)
	generatedRecipes, err := generator.GenerateRecipes(p)
	if err != nil {
		return fmt.Errorf("failed to generate recipes: %w", err)
	}

	//output := formatter.FormatRecipes(generatedRecipes)
	fmt.Println(generatedRecipes)

	return nil
}

func showHelp() {
	fmt.Println("Careme - Weekly Recipe Generator")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  careme -location <location>")
	fmt.Println("  careme -l <location>")
	fmt.Println()
	fmt.Println("Options:")
	fmt.Println("  -location, -l   Location for recipe sourcing (required)")
	fmt.Println("  -help, -h       Show this help message")
}
