package main

import (
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/html"
	"careme/internal/locations"
	"careme/internal/recipes"
	"context"
	"fmt"
	"log"
	"net/http"
	"time"
)

func generateSpinnerHTML(cfg *config.Config) string {
	clarityScript := html.ClarityScript(cfg)
	return `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <title>Generating…</title>
  <meta http-equiv="refresh" content="60" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <!-- discourage caching so each reload re-requests -->
  <meta http-equiv="Cache-Control" content="no-store, no-cache, must-revalidate" />
  <meta http-equiv="Pragma" content="no-cache" />
  <style>
    :root { --size: 48px; --thickness: 6px; }
    html, body { height: 100%; margin: 0; }
    body { display: grid; place-items: center; font: 16px system-ui, -apple-system, Segoe UI, Roboto, sans-serif; }
    .card { text-align: center; padding: 2rem; }
    .spinner {
      width: var(--size); height: var(--size);
      border-radius: 50%;
      border: var(--thickness) solid #ddd;
      border-top-color: #555;
      animation: spin 1s linear infinite;
      margin: 0 auto 1rem;
    }
    @keyframes spin { to { transform: rotate(360deg); } }
    @media (prefers-reduced-motion: reduce) { .spinner { animation: none; } }
  </style>
  ` + string(clarityScript) + `
</head>
<body>
  <main class="card" role="status" aria-live="polite">
    <div class="spinner" aria-hidden="true"></div>
    <h1>Please wait…</h1>
    <p>We're generating your result. This page refreshes every 60 seconds.</p>
    <p><a href="">Refresh now</a></p>
  </main>
</body>
</html>`
}

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
		w.Write([]byte(locations.Html(cfg, locs, zip)))
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

		p := recipes.DefaultParams(l, date)

		if i := r.URL.Query().Get("instructions"); i != "" {
			p.Instructions = i
		}

		if recipe, ok := cache.Get(p.Hash()); ok {
			log.Printf("serving cached recipes for %s", p.String())
			_, _ = w.Write([]byte(recipes.FormatChatHTML(cfg, *l, date, string(recipe))))
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
		_, _ = w.Write([]byte(generateSpinnerHTML(cfg)))
	})

	log.Printf("Serving Careme on %s", addr)
	return http.ListenAndServe(addr, mux)
}
