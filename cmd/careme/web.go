package main

import (
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/locations"
	"careme/internal/recipes"
	"context"
	"fmt"
	"log"
	"net/http"
	"time"
)

func runServer(cfg *config.Config, addr string) error {

	cache, err := cache.MakeCache()
	if err != nil {
		return fmt.Errorf("failed to create cache: %w", err)
	}

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

		if recipe, ok := cache.Get(p.Hash()); ok {
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
