package main

import (
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/html"
	"careme/internal/locations"
	"careme/internal/recipes"
	"careme/internal/users"
	"context"
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

	mux := http.NewServeMux()

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		currentUser, err := userFromCookie(r, userStorage)
		if err != nil {
			if errors.Is(err, users.ErrNotFound) {
				clearUserCookie(w)
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
		setUserCookie(w, user.ID)
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	mux.HandleFunc("/logout", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		clearUserCookie(w)
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	mux.HandleFunc("/user/recipes", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		ctx := r.Context()
		currentUser, err := userFromCookie(r, userStorage)
		if err != nil {
			slog.ErrorContext(ctx, "failed to load user for user page", "error", err)
			http.Error(w, "unable to load account", http.StatusInternalServerError)
			return
		}
		if currentUser == nil {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		recipeTitle := strings.TrimSpace(r.FormValue("recipe"))
		if recipeTitle == "" {
			slog.ErrorContext(ctx, "no recipe title provided")
			http.Error(w, "no recipe title provided", http.StatusBadRequest)
			return
		}

		hash := strings.TrimSpace(r.FormValue("hash"))

		// Check for duplicates
		for _, existing := range currentUser.LastRecipes {
			if strings.EqualFold(existing.Title, recipeTitle) {
				slog.InfoContext(ctx, "duplicate previous recipe", "title", recipeTitle)
				http.Redirect(w, r, "/user", http.StatusSeeOther)
				return
			}
		}
		newRecipe := users.Recipe{
			Title:     recipeTitle,
			Hash:      hash,
			CreatedAt: time.Now(),
		}
		currentUser.LastRecipes = append(currentUser.LastRecipes, newRecipe)
		if err := userStorage.Update(currentUser); err != nil {
			slog.ErrorContext(ctx, "failed to update user", "error", err)
			http.Error(w, "unable to save preferences", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, "/user", http.StatusSeeOther)
	})

	mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		currentUser, err := userFromCookie(r, userStorage)
		if err != nil {
			if errors.Is(err, users.ErrNotFound) {
				clearUserCookie(w)
				http.Redirect(w, r, "/", http.StatusSeeOther)
				return
			}
			slog.ErrorContext(ctx, "failed to load user for user page", "error", err)
			http.Error(w, "unable to load account", http.StatusInternalServerError)
			return
		}
		if currentUser == nil {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}

		success := false
		if r.Method == http.MethodPost {
			if err := r.ParseForm(); err != nil {
				http.Error(w, "invalid form submission", http.StatusBadRequest)
				return
			}
			currentUser.FavoriteStore = strings.TrimSpace(r.FormValue("favorite_store"))
			currentUser.ShoppingDay = strings.TrimSpace(r.FormValue("shopping_day"))

			if err := userStorage.Update(currentUser); err != nil {
				slog.ErrorContext(ctx, "failed to update user", "error", err)
				http.Error(w, "unable to save preferences", http.StatusInternalServerError)
				return
			}
			success = true
		}

		data := struct {
			ClarityScript template.HTML
			User          *users.User
			Success       bool
		}{
			ClarityScript: clarityScript,
			User:          currentUser,
			Success:       success,
		}
		if err := userTmpl.Execute(w, data); err != nil {
			slog.ErrorContext(ctx, "user template execute error", "error", err)
			http.Error(w, "template error", http.StatusInternalServerError)
		}
	})

	generator, err := recipes.NewGenerator(cfg, cache)
	if err != nil {
		return fmt.Errorf("failed to create recipe generator: %w", err)
	}

	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	})

	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {})

	mux.HandleFunc("/locations", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		_, err := userFromCookie(r, userStorage)
		if err != nil {
			if errors.Is(err, users.ErrNotFound) {
				clearUserCookie(w)
				http.Redirect(w, r, "/", http.StatusSeeOther)
				return
			}
			slog.ErrorContext(ctx, "failed to load user for locations", "error", err)
			http.Error(w, "unable to load account", http.StatusInternalServerError)
			return
		}
		/* Not forcing login yet
		if currentUser == nil {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}*/
		zip := r.URL.Query().Get("zip")
		if zip == "" {
			slog.InfoContext(ctx, "no zip code provided to /locations")
			http.Error(w, "provide a zip code with ?zip=12345", http.StatusBadRequest)
			return
		}
		locs, err := locations.GetLocationsByZip(context.TODO(), cfg, zip)
		if err != nil {
			slog.ErrorContext(ctx, "failed to get locations for zip", "zip", zip, "error", err)
			http.Error(w, "could not get locations", http.StatusInternalServerError)
			return
		}
		// Render locations
		w.Write([]byte(locations.Html(cfg, locs, zip)))
	})

	mux.HandleFunc("/recipes", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		currentUser, err := userFromCookie(r, userStorage)
		if err != nil {
			if errors.Is(err, users.ErrNotFound) {
				clearUserCookie(w)
				http.Redirect(w, r, "/", http.StatusSeeOther)
				return
			}
			slog.ErrorContext(ctx, "failed to load user for recipes", "error", err)
			http.Error(w, "unable to load account", http.StatusInternalServerError)
			return
		}
		// Not forcing login yet
		if currentUser == nil {
			currentUser = &users.User{
				LastRecipes: []users.Recipe{},
			}
		}

		// Check if using hash-based sharing
		if hashParam := r.URL.Query().Get("h"); hashParam != "" {
			if err := generator.FromCache(ctx, hashParam, nil, w); err != nil {
				slog.ErrorContext(ctx, "failed to load shared recipe for hash", "hash", hashParam, "error", err)
				http.Error(w, "recipe not found or expired", http.StatusNotFound)
			}
			return

		}

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
		if date, err = time.ParseInLocation("2006-01-02", dateStr, time.UTC); err != nil {
			http.Error(w, "invalid date format, use YYYY-MM-DD", http.StatusBadRequest)
			return
		}
		l, err := locations.GetLocationByID(ctx, cfg, loc) // get details but ignore error
		if err != nil {
			http.Error(w, "could not get location details", http.StatusBadRequest)
			return
		}

		p := recipes.DefaultParams(l, date)
		for _, r := range currentUser.LastRecipes {
			if r.CreatedAt.Before(time.Now().AddDate(0, 0, -14)) { // older than 2 weeks
				continue
			}
			p.LastRecipes = append(p.LastRecipes, r.Title) //need to think about how this messes with hash
		}
		// Override instructions if provided

		if i := r.URL.Query().Get("instructions"); i != "" {
			p.Instructions = i
		}
		// Set user ID if available
		if currentUser != nil && currentUser.ID != "" {
			p.UserID = currentUser.ID
		}
		hash := p.Hash()
		if err := generator.FromCache(ctx, hash, p, w); err == nil {
			return
		}

		go func() {
			slog.InfoContext(ctx, "generating cached recipes", "params", p.String(), "hash", hash)
			err := generator.GenerateRecipes(ctx, p)
			if err != nil {
				slog.ErrorContext(ctx, "generate error", "error", err)
				return
			}
		}()

		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
		spinnerData := struct {
			ClarityScript template.HTML
		}{
			ClarityScript: clarityScript,
		}
		if err := spinnerTmpl.Execute(w, spinnerData); err != nil {
			slog.ErrorContext(ctx, "home template execute error", "error", err)
			http.Error(w, "template error", http.StatusInternalServerError)
		}
	})

	slog.Info("Serving Careme", "address", addr)
	return http.ListenAndServe(addr, WithMiddleware(mux))
}

func setUserCookie(w http.ResponseWriter, userID string) {
	http.SetCookie(w, &http.Cookie{
		Name:     users.CookieName,
		Value:    userID,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(sessionDuration),
		MaxAge:   int(sessionDuration / time.Second),
	})
}

func clearUserCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     users.CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
}

func userFromCookie(r *http.Request, store *users.Storage) (*users.User, error) {
	cookie, err := r.Cookie(users.CookieName)
	if err != nil {
		if errors.Is(err, http.ErrNoCookie) {
			return nil, nil
		}
		return nil, err
	}
	if cookie.Value == "" {
		return nil, nil
	}
	user, err := store.GetByID(cookie.Value)
	if err != nil {
		return nil, err
	}
	return user, nil
}
