package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"careme/internal/config"
	"careme/internal/recipes"
)

func main() {
	var location string
	var serve bool
	var addr string
	var help bool

	flag.StringVar(&location, "location", "", "Location for recipe sourcing (e.g., 70100023)")
	flag.StringVar(&location, "l", "", "Location for recipe sourcing (short form)")
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

	if serve {
		if err := runServer(addr); err != nil {
			log.Fatalf("server error: %v", err)
		}
		return
	}

	if location == "" {
		fmt.Println("Error: Location is required (or use -serve for web mode)")
		showHelp()
		os.Exit(1)
	}

	if err := run(location); err != nil {
		log.Fatalf("Error: %v", err)
	}
}

func runServer(addr string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}
	generator, err := recipes.NewGenerator(cfg)
	if err != nil {
		return fmt.Errorf("failed to create recipe generator: %w", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		loc := r.URL.Query().Get("location")
		if loc == "" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("specify a location id to generate recipes"))
			return
		}
		var date = time.Now()

		if dateStr := r.URL.Query().Get("date"); dateStr != "" {
			var err error
			if date, err = time.Parse("2006-01-02", dateStr); err == nil {
				http.Error(w, "invalid date format, use YYYY-MM-DD", http.StatusBadRequest)
				return
			}

		}
		if recipe, err := os.ReadFile("recipes/" + recipes.Hash(loc, date) + ".txt"); err == nil {
			_, _ = w.Write([]byte(recipes.FormatChatHTML(loc, string(recipe))))
			return
		}
		go func() {
			start := time.Now()
			_, err := generator.GenerateRecipes(loc, date)
			if err != nil {
				log.Printf("generate error: %v", err)
				return
			}
			log.Printf("generated chat for %s in %s, stored in recipes/%s.txt", loc, time.Since(start), recipes.Hash(loc, date))
		}()

		_, _ = w.Write([]byte("recipe generation in progress, please refresh in a minute or two..."))
		w.WriteHeader(http.StatusAccepted)
	})

	log.Printf("Serving Careme on %s", addr)
	return http.ListenAndServe(addr, mux)
}

func run(location string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	generator, err := recipes.NewGenerator(cfg)
	if err != nil {
		return fmt.Errorf("failed to create recipe generator: %w", err)
	}

	//fmt.Printf("🍽️  Generating 4 weekly recipes for location: %s\n", location)
	//fmt.Println("🏷️  Checking current sales at local QFC/Fred Meyer...")
	//fmt.Println("📚 Avoiding recipes from the past 2 weeks...")
	//fmt.Println()

	generatedRecipes, err := generator.GenerateRecipes(location, time.Now())
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
