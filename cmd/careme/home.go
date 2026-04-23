package main

import (
	"context"
	"errors"
	"html/template"
	"log/slog"
	"net/http"

	"careme/internal/auth"
	"careme/internal/locations"
	"careme/internal/routing"
	"careme/internal/seasons"
	"careme/internal/templates"
	utypes "careme/internal/users/types"
)

type home struct {
	userStorage interface {
		FromRequest(ctx context.Context, r *http.Request, authClient auth.AuthClient) (*utypes.User, error)
	}
	locationStorage interface {
		GetLocationByID(ctx context.Context, locationID string) (*locations.Location, error)
	}
	authClient auth.AuthClient
}

func (h home) Register(routes routing.Registrar) {
	routes.Handle("/{$}", h)
	routes.Handle("/index.html", h)
	routes.Handle("/index.htm", h)
}

func (h home) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currentUser, err := h.userStorage.FromRequest(ctx, r, h.authClient)
	if err != nil {
		if !errors.Is(err, auth.ErrNoSession) {
			slog.ErrorContext(ctx, "failed to get user from request", "error", err)
			http.Error(w, "unable to load account", http.StatusInternalServerError)
			return
		}
		// no user is fine we'll just pass nil currentUser to template
		// just have two different templates?
	}

	var favoriteStoreName string
	if currentUser != nil && currentUser.FavoriteStore != "" {
		loc, locErr := h.locationStorage.GetLocationByID(ctx, currentUser.FavoriteStore)
		if locErr != nil {
			slog.ErrorContext(ctx, "failed to get location name for favorite store", "location_id", currentUser.FavoriteStore, "error", locErr)
		} else {
			favoriteStoreName = loc.Name
		}
	}
	data := struct {
		ClarityScript     template.HTML
		GoogleTagScript   template.HTML
		User              *utypes.User
		FavoriteStoreName string
		Style             seasons.Style
		ServerSignedIn    bool
	}{
		ClarityScript:     templates.ClarityScript(ctx),
		GoogleTagScript:   templates.GoogleTagScript(),
		User:              currentUser,
		FavoriteStoreName: favoriteStoreName,
		Style:             seasons.GetCurrentStyle(),
		ServerSignedIn:    currentUser != nil,
	}
	if err := templates.Home.Execute(w, data); err != nil {
		slog.ErrorContext(ctx, "home template execute error", "error", err)
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}
