package main

import (
	"careme/internal/cache"
	"careme/internal/clerk"
	"careme/internal/config"
	"careme/internal/locations"
	"careme/internal/logs"
	"careme/internal/logsink"
	"careme/internal/recipes"
	"careme/internal/seasons"
	"careme/internal/templates"
	"careme/internal/users"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

//go:embed favicon.png
var favicon []byte

//go:embed static/tailwind.css
var tailwindCSS []byte

const sessionDuration = 365 * 24 * time.Hour

func runServer(cfg *config.Config, logsinkCfg logsink.Config, addr string) error {
	cache, err := cache.MakeCache()
	if err != nil {
		return fmt.Errorf("failed to create cache: %w", err)
	}

	userStorage := users.NewStorage(cache)

	// Initialize Clerk client
	clerkClient, err := clerk.NewClient(cfg.Clerk.SecretKey)
	if err != nil {
		return fmt.Errorf("failed to create clerk client: %w", err)
	}

	generator, err := recipes.NewGenerator(cfg, cache)
	if err != nil {
		return fmt.Errorf("failed to create recipe generator: %w", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/static/tailwind.css", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.Write(tailwindCSS)
	})

	locationserver, err := locations.New(context.TODO(), cfg)
	if err != nil {
		return fmt.Errorf("failed to create location server: %w", err)
	}
	locations.Register(locationserver, mux)

	userHandler := users.NewHandler(userStorage, locationserver)
	userHandler.Register(mux)

	recipeHandler := recipes.NewHandler(cfg, userStorage, generator, locationserver, cache)
	recipeHandler.Register(mux)

	if logsinkCfg.Enabled() {
		logsHandler, err := logs.NewHandler(logsinkCfg)
		if err != nil {
			return fmt.Errorf("failed to create logs handler: %w", err)
		}
		logsHandler.Register(mux)
	}

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		
		// Try to get user from Clerk session first
		var currentUser *users.User
		clerkUserID, err := clerk.GetUserIDFromRequest(r)
		if err == nil && clerkUserID != "" {
			// Get or create local user from Clerk user ID
			currentUser, err = getOrCreateUserFromClerk(ctx, clerkClient, userStorage, clerkUserID)
			if err != nil {
				slog.ErrorContext(ctx, "failed to sync clerk user", "error", err, "clerk_user_id", clerkUserID)
				// Continue without user rather than failing
			}
		}
		
		// Fall back to cookie-based auth for existing users
		if currentUser == nil {
			currentUser, err = users.FromRequest(r, userStorage)
			if err != nil {
				if errors.Is(err, users.ErrNotFound) {
					users.ClearCookie(w)
				} else {
					slog.ErrorContext(ctx, "failed to load user from cookie", "error", err)
					http.Error(w, "unable to load account", http.StatusInternalServerError)
					return
				}
			}
		}
		
		data := struct {
			ClarityScript     template.HTML
			User              *users.User
			Style             seasons.Style
			ClerkPublishableKey string
		}{
			ClarityScript:     templates.ClarityScript(),
			User:              currentUser,
			Style:             seasons.GetCurrentStyle(),
			ClerkPublishableKey: cfg.Clerk.PublishableKey,
		}
		if err := templates.Home.Execute(w, data); err != nil {
			slog.ErrorContext(ctx, "home template execute error", "error", err)
			http.Error(w, "template error", http.StatusInternalServerError)
		}
	})

	// Redirect to Clerk hosted sign-in page
	mux.HandleFunc("/sign-in", func(w http.ResponseWriter, r *http.Request) {
		// Clerk hosted sign-in URL
		http.Redirect(w, r, "https://bold-salmon-53.accounts.dev/sign-in", http.StatusSeeOther)
	})

	// Redirect to Clerk hosted sign-up page
	mux.HandleFunc("/sign-up", func(w http.ResponseWriter, r *http.Request) {
		// Clerk hosted sign-up URL
		http.Redirect(w, r, "https://bold-salmon-53.accounts.dev/sign-up", http.StatusSeeOther)
	})

	// Callback handler after Clerk authentication
	mux.HandleFunc("/auth/callback", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		
		// Get Clerk user ID from session
		clerkUserID, err := clerk.GetUserIDFromRequest(r)
		if err != nil {
			slog.ErrorContext(ctx, "no clerk session in callback", "error", err)
			http.Redirect(w, r, "/sign-in", http.StatusSeeOther)
			return
		}

		// Sync Clerk user with local storage
		user, err := getOrCreateUserFromClerk(ctx, clerkClient, userStorage, clerkUserID)
		if err != nil {
			slog.ErrorContext(ctx, "failed to sync user in callback", "error", err, "clerk_user_id", clerkUserID)
			http.Error(w, "unable to complete sign in", http.StatusInternalServerError)
			return
		}

		// Set local cookie for backwards compatibility
		users.SetCookie(w, user.ID, sessionDuration)
		
		// Redirect to home
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})

	// Keep old login endpoint for backwards compatibility
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

	server := &http.Server{
		Addr:    addr,
		Handler: WithMiddleware(clerkClient.WithClerkHTTP(mux)),
	}

	// Channel to listen for errors coming from the server
	serverErrors := make(chan error, 1)

	// Start the server in a goroutine
	go func() {
		slog.Info("Serving Careme", "address", addr)
		serverErrors <- server.ListenAndServe()
	}()

	// Channel to listen for interrupt or terminate signals
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, os.Interrupt, syscall.SIGTERM)

	// Block until we receive a signal or server error
	select {
	case err := <-serverErrors:
		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("server error: %w", err)
		}
		return nil
	case sig := <-shutdown:
		slog.Info("Shutdown signal received", "signal", sig)
		return gracefulShutdown(server, recipeHandler.Wait)
	}
}

// getOrCreateUserFromClerk syncs a Clerk user with local storage
func getOrCreateUserFromClerk(ctx context.Context, clerkClient *clerk.Client, userStorage *users.Storage, clerkUserID string) (*users.User, error) {
	// Fetch user details from Clerk
	clerkUser, err := clerkClient.GetUser(ctx, clerkUserID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch clerk user: %w", err)
	}

	// Get primary email from Clerk user
	var primaryEmail string
	for _, emailAddr := range clerkUser.EmailAddresses {
		if clerkUser.PrimaryEmailAddressID != nil && emailAddr.ID == *clerkUser.PrimaryEmailAddressID {
			primaryEmail = emailAddr.EmailAddress
			break
		}
	}
	if primaryEmail == "" && len(clerkUser.EmailAddresses) > 0 {
		// Fallback to first email if no primary is set
		primaryEmail = clerkUser.EmailAddresses[0].EmailAddress
	}
	if primaryEmail == "" {
		return nil, fmt.Errorf("clerk user has no email address")
	}

	// Find or create user in local storage
	user, err := userStorage.FindOrCreateByEmail(primaryEmail)
	if err != nil {
		return nil, fmt.Errorf("failed to find or create local user: %w", err)
	}

	return user, nil
}

func gracefulShutdown(svr *http.Server, recipesWait func()) error {
	// Give outstanding requests 25 seconds to complete (kubernetes has 30 second grace period)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	// Gracefully shutdown the HTTP server
	if err := svr.Shutdown(ctx); err != nil {
		slog.Error("Server shutdown error", "error", err)
		// Force close after timeout
		svr.Close()
		return err
	}

	done := make(chan struct{})
	go func() {
		recipesWait()
		close(done)
	}()

	// Wait for all recipe generation goroutines to complete
	slog.Info("Waiting for recipe generation goroutines to complete")

	select {
	case <-done:
		slog.Info("All recipe generation goroutines completed")
	case <-ctx.Done():
		slog.Warn("Timeout waiting for recipe generation goroutines")
		return ctx.Err()
	}
	return nil
}
