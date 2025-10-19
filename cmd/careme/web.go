package main

import (
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/html"
	"careme/internal/locations"
	"careme/internal/recipes"
	"careme/internal/users"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

const sessionDuration = 365 * 24 * time.Hour

func runServer(cfg *config.Config, addr string) error {
	// Parse templates and spinner on startup (no init function)
	homeTmpl, spinnerTmpl, userTmpl := loadTemplates()

	cache, err := cache.MakeCache()
	if err != nil {
		return fmt.Errorf("failed to create cache: %w", err)
	}

	clarityScript := html.ClarityScript(cfg)
	userStorage := users.NewStorage(cache)

	generator, err := recipes.NewGenerator(cfg, cache)
	if err != nil {
		return fmt.Errorf("failed to create recipe generator: %w", err)
	}

	userHandler := users.NewHandler(userStorage, clarityScript, userTmpl)
	recipeHandler := recipes.NewHandler(cfg, userStorage, generator, clarityScript, spinnerTmpl)

	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		currentUser, err := users.FromRequest(r, userStorage)
		if err != nil {
			if errors.Is(err, users.ErrNotFound) {
				users.ClearCookie(w)
			} else {
				slog.ErrorContext(ctx, "failed to load user from cookie", "error", err)
				http.Error(w, "unable to load account", http.StatusInternalServerError)
				return
			}
		}
		data := struct {
			ClarityScript template.HTML
			User          *users.User
		}{
			ClarityScript: clarityScript,
			User:          currentUser,
		}
		if err := homeTmpl.Execute(w, data); err != nil {
			slog.ErrorContext(ctx, "home template execute error", "error", err)
			http.Error(w, "template error", http.StatusInternalServerError)
		}
	})

	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form submission", http.StatusBadRequest)
			return
		}
		email := strings.TrimSpace(r.FormValue("email"))
		if email == "" {
			http.Error(w, "email is required", http.StatusBadRequest)
			return
		}
		user, err := userStorage.FindOrCreateByEmail(email)
		if err != nil {
			slog.ErrorContext(r.Context(), "failed to find or create user", "error", err)
			http.Error(w, fmt.Sprintf("unable to sign in: %v", err), http.StatusInternalServerError)
			return
		}
		users.SetCookie(w, user.ID, sessionDuration)
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	mux.HandleFunc("/logout", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		users.ClearCookie(w)
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	mux.Handle("/user", userHandler)
	mux.Handle("/user/", userHandler)
	mux.Handle("/recipes", recipeHandler)
	mux.Handle("/recipes/", recipeHandler)

	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	})

	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})

	mux.HandleFunc("/locations", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		_, err := users.FromRequest(r, userStorage)
		if err != nil {
			if errors.Is(err, users.ErrNotFound) {
				users.ClearCookie(w)
				http.Redirect(w, r, "/", http.StatusSeeOther)
				return
			}
			slog.ErrorContext(ctx, "failed to load user for locations", "error", err)
			http.Error(w, "unable to load account", http.StatusInternalServerError)
			return
		}
		zip := r.URL.Query().Get("zip")
		if zip == "" {
			slog.InfoContext(ctx, "no zip code provided to /locations")
			http.Error(w, "provide a zip code with ?zip=12345", http.StatusBadRequest)
			return
		}
		locs, err := locations.GetLocationsByZip(ctx, cfg, zip)
		if err != nil {
			slog.ErrorContext(ctx, "failed to get locations for zip", "zip", zip, "error", err)
			http.Error(w, "could not get locations", http.StatusInternalServerError)
			return
		}
		w.Write([]byte(locations.Html(cfg, locs, zip)))
	})

	slog.Info("Serving Careme", "address", addr)
	return http.ListenAndServe(addr, WithMiddleware(mux))
}
