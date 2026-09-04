package cocktails

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"sync"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/kroger"
	"careme/internal/locations/geo"
	locationtypes "careme/internal/locations/types"
	"careme/internal/routing"
	"careme/internal/seasons"
	"careme/internal/templates"
)

type (
	Catalog interface {
		FetchCocktailIngredients(context.Context, string, seasons.Season) ([]ai.InputIngredient, error)
	}
	Planner interface {
		GenerateCocktails(context.Context, string, []ai.InputIngredient) (*ai.CocktailMenu, error)
	}
	Locations interface {
		GetLocationByID(context.Context, string) (*locationtypes.Location, error)
		GetLocationsByCoordinates(context.Context, geo.Coordinate) ([]locationtypes.Location, error)
	}
)

type (
	job struct {
		menu *ai.CocktailMenu
		err  error
		done bool
	}
	Server struct {
		catalog   Catalog
		planner   Planner
		locations Locations
		centroids Centroids
		cache     cache.Cache
		mu        sync.Mutex
		jobs      map[string]*job
		wg        sync.WaitGroup
	}
)

func New(catalog Catalog, planner Planner, locations Locations, storage cache.Cache, centroids Centroids) *Server {
	return &Server{catalog: catalog, planner: planner, locations: locations, centroids: centroids, cache: storage, jobs: map[string]*job{}}
}
func (s *Server) Wait() { s.wg.Wait() }
func (s *Server) Register(mux routing.Registrar) {
	mux.HandleFunc("GET /cocktails", s.page)
	mux.HandleFunc("POST /cocktails", s.start)
}

func (s *Server) location(w http.ResponseWriter, r *http.Request) (*locationtypes.Location, bool) {
	id := r.FormValue("location")
	if !kroger.NewIdentityProvider().IsID(id) {
		http.Error(w, "Choose a Kroger store for cocktails.", http.StatusBadRequest)
		return nil, false
	}
	loc, err := s.locations.GetLocationByID(r.Context(), id)
	if err != nil {
		http.Error(w, "Unable to find that Kroger store.", http.StatusBadRequest)
		return nil, false
	}
	return loc, true
}

func menuKey(id string) string {
	return fmt.Sprintf("cocktails/v1/%s/%s/%s.json", id, time.Now().UTC().Format("2006-01-02"), seasons.GetCurrentSeason())
}

func (s *Server) start(w http.ResponseWriter, r *http.Request) {
	loc, ok := s.location(w, r)
	if !ok {
		return
	}
	key := menuKey(loc.ID)
	season := seasons.GetCurrentSeason()
	s.mu.Lock()
	current := s.jobs[key]
	if current == nil || current.done && current.err != nil {
		s.jobs[key] = &job{}
		// Completed menus live in the persistent cache; bound process-local job retention.
		for oldKey, old := range s.jobs {
			if oldKey != key && old.done {
				delete(s.jobs, oldKey)
			}
		}
		s.wg.Go(func() {
			ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 3*time.Minute)
			defer cancel()
			menu, err := s.generate(ctx, key, loc, season)
			if err != nil {
				slog.ErrorContext(ctx, "cocktail generation failed", "location", loc.ID, "error", err)
			}
			s.mu.Lock()
			s.jobs[key] = &job{menu: menu, err: err, done: true}
			s.mu.Unlock()
		})
	}
	s.mu.Unlock()
	http.Redirect(w, r, "/cocktails?location="+url.QueryEscape(loc.ID), http.StatusSeeOther)
}

func (s *Server) generate(ctx context.Context, key string, loc *locationtypes.Location, season seasons.Season) (*ai.CocktailMenu, error) {
	menu, err := s.read(ctx, key)
	if err == nil {
		return menu, nil
	}
	if !errors.Is(err, cache.ErrNotFound) {
		return nil, err
	}
	ingredients, err := s.catalog.FetchCocktailIngredients(ctx, loc.ID, season)
	if err != nil {
		return nil, err
	}
	menu, err = s.planner.GenerateCocktails(ctx, fmt.Sprintf("Date: %s. Season: %s. Store: %s, %s, %s.", time.Now().UTC().Format("2006-01-02"), season, loc.Name, loc.Address, loc.State), ingredients)
	if err != nil {
		return nil, err
	}
	if err := ai.ValidateCocktails(menu, ingredients); err != nil {
		return nil, err
	}
	body, err := json.Marshal(menu)
	if err != nil {
		return nil, err
	}
	if err := s.cache.Put(ctx, key, string(body), cache.Unconditional()); err != nil {
		return nil, fmt.Errorf("save cocktail menu: %w", err)
	}
	return menu, nil
}

func (s *Server) read(ctx context.Context, key string) (*ai.CocktailMenu, error) {
	body, err := s.cache.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	defer func() { _ = body.Close() }()
	var menu ai.CocktailMenu
	if err := json.NewDecoder(body).Decode(&menu); err != nil {
		return nil, err
	}
	return &menu, nil
}

func (s *Server) page(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("location") == "" {
		s.locationsPage(w, r)
		return
	}
	loc, ok := s.location(w, r)
	if !ok {
		return
	}
	key := menuKey(loc.ID)
	data := struct {
		Style    seasons.Style
		Location *locationtypes.Location
		Season   seasons.Season
		Menu     *ai.CocktailMenu
		Pending  bool
		Failed   bool
	}{Style: seasons.GetCurrentStyle(), Location: loc, Season: seasons.GetCurrentSeason()}
	s.mu.Lock()
	current := s.jobs[key]
	if current != nil {
		data.Menu = current.menu
		data.Pending = !current.done
		data.Failed = current.err != nil
	}
	s.mu.Unlock()
	if current == nil {
		menu, err := s.read(r.Context(), key)
		if err == nil {
			data.Menu = menu
		} else if !errors.Is(err, cache.ErrNotFound) {
			data.Failed = true
		}
	}
	var body bytes.Buffer
	if err := templates.Cocktails.Execute(&body, data); err != nil {
		slog.ErrorContext(r.Context(), "render cocktails", "error", err)
		http.Error(w, "Unable to show cocktails.", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if _, err := body.WriteTo(w); err != nil {
		slog.ErrorContext(r.Context(), "write cocktails", "error", err)
	}
}
