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
	"io"
	"log"
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
		currentUser, err := userFromCookie(r, userStorage)
		if err != nil {
			if errors.Is(err, users.ErrNotFound) {
				clearUserCookie(w)
			} else {
				log.Printf("failed to load user from cookie: %v", err)
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
			log.Printf("home template execute error: %v", err)
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
			log.Printf("failed to find or create user: %v", err)
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

	mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		currentUser, err := userFromCookie(r, userStorage)
		if err != nil {
			if errors.Is(err, users.ErrNotFound) {
				clearUserCookie(w)
				http.Redirect(w, r, "/", http.StatusSeeOther)
				return
			}
			log.Printf("failed to load user for user page: %v", err)
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
				log.Printf("failed to update user: %v", err)
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
			log.Printf("user template execute error: %v", err)
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

	mux.HandleFunc("/locations", func(w http.ResponseWriter, r *http.Request) {
		_, err := userFromCookie(r, userStorage)
		if err != nil {
			if errors.Is(err, users.ErrNotFound) {
				clearUserCookie(w)
				http.Redirect(w, r, "/", http.StatusSeeOther)
				return
			}
			log.Printf("failed to load user for locations: %v", err)
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
		_, err := userFromCookie(r, userStorage)
		if err != nil {
			if errors.Is(err, users.ErrNotFound) {
				clearUserCookie(w)
				http.Redirect(w, r, "/", http.StatusSeeOther)
				return
			}
			log.Printf("failed to load user for recipes: %v", err)
			http.Error(w, "unable to load account", http.StatusInternalServerError)
			return
		}
		/* Not forcing login yet
		if currentUser == nil {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}*/
		ctx := r.Context()

		// Check if using hash-based sharing
		if hashParam := r.URL.Query().Get("h"); hashParam != "" {
			// Load the recipe content from cache
			recipe, err := cache.Get(hashParam)
			if err != nil {
				http.Error(w, "recipe not found or expired", http.StatusNotFound)
				return
			}
			defer recipe.Close()

			recipebytes, err := io.ReadAll(recipe)
			if err != nil {
				log.Printf("failed to read cached recipe for hash %s: %v", hashParam, err)
				http.Error(w, "failed to read cached recipe", http.StatusInternalServerError)
				return
			}

			// Load the params to properly format the HTML
			params, err := generator.LoadParamsFromHash(hashParam)
			if err != nil {
				log.Printf("failed to load params for hash %s: %v", hashParam, err)
				http.Error(w, "recipe metadata not found", http.StatusNotFound)
				return
			}

			log.Printf("serving shared recipe by hash: %s", hashParam)
			if err := generator.FormatChatHTML(params, recipebytes, w); err != nil {
				log.Printf("failed to format shared recipe for hash %s: %v", hashParam, err)
				http.Error(w, "failed to format recipe", http.StatusInternalServerError)
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

		hash := p.Hash()
		if recipe, err := cache.Get(hash); err == nil {
			log.Printf("serving cached recipes for %s %s", p.String(), hash)
			defer recipe.Close()
			recipebytes, err := io.ReadAll(recipe) // read to EOF to avoid leaks
			if err != nil {
				log.Printf("failed to read cached recipe for %s: %v", p, err)
				http.Error(w, "failed to read cached recipe", http.StatusInternalServerError)
				return
			}
			if err := generator.FormatChatHTML(p, recipebytes, w); err != nil {
				log.Printf("failed to format cached recipe for %s: %v", p, err)
				http.Error(w, "failed to format cached recipe", http.StatusInternalServerError)
			}
			return
		}
		go func() {
			log.Printf("generating cached recipes for %s %s", p.String(), hash)
			_, err := generator.GenerateRecipes(p)
			if err != nil {
				log.Printf("generate error: %v", err)
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
			log.Printf("home template execute error: %v", err)
			http.Error(w, "template error", http.StatusInternalServerError)
		}
	})

	log.Printf("Serving Careme on %s", addr)
	return http.ListenAndServe(addr, mux)
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
