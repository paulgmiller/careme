package main

import (
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/html"
	"careme/internal/locations"
	"careme/internal/recipes"
	"careme/internal/templates"
	"careme/internal/users"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

//go:embed favicon.png
var favicon []byte

const sessionDuration = 365 * 24 * time.Hour

func runServer(cfg *config.Config, addr string) error {

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
	mux := http.NewServeMux()

	locationserver, err := locations.New(context.TODO(), cfg)
	if err != nil {
		return fmt.Errorf("failed to create location server: %w", err)
	}
	locationserver.Register(mux)

	locationAdapter := locations.NewLocationAdapter(locationserver)
	userHandler := users.NewHandler(userStorage, clarityScript, locationAdapter)
	userHandler.Register(mux)

	recipeHandler := recipes.NewHandler(cfg, userStorage, generator, clarityScript, locationserver)
	recipeHandler.Register(mux)

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
		if err := templates.Home.Execute(w, data); err != nil {
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

	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	})

	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png") // <= without this, many UAs ignore it
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.Write(favicon)
	})

	slog.Info("Serving Careme", "address", addr)
	return http.ListenAndServe(addr, WithMiddleware(mux))
}
