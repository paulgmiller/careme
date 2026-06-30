package campaigns

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/locations"
	"careme/internal/recipes"
	"careme/internal/routing"
)

type advertisedLocationStore interface {
	GetLocationByID(ctx context.Context, locationID string) (*locations.Location, error)
}

type recipeGenerationKickstarter interface {
	KickGenerationIfNotPresent(ctx context.Context, p *recipes.GeneratorParams) (recipes.GenerationKickResult, error)
}

type advertisedGenerationServer struct {
	locations advertisedLocationStore
	generator recipeGenerationKickstarter
	rio       advertisedRecipeIO
	cache     cache.Cache
}

type advertisedRecipeIO interface {
	FromCache(ctx context.Context, hash string) (*ai.ShoppingList, error)
}

type AdvertisedRecipeGenerationResponse struct {
	GeneratedAt time.Time                 `json:"generated_at"`
	Entries     []AdvertisedRecipeEntry   `json:"entries"`
	Failures    []AdvertisedRecipeFailure `json:"failures,omitempty"`
	Kicked      []string                  `json:"kicked,omitempty"`
}

func RegisterAdvertisedRecipeGeneration(
	mux routing.Registrar,
	locations advertisedLocationStore,
	generator recipeGenerationKickstarter,
	c cache.Cache,
) {
	advertisedGenerationServer{
		locations: locations,
		generator: generator,
		rio:       recipes.IO(c),
		cache:     c,
	}.Register(mux)
}

func (s advertisedGenerationServer) Register(mux routing.Registrar) {
	mux.HandleFunc("POST /campaigns/advertised-recipes/generate", s.handleGenerate)
}

func (s advertisedGenerationServer) handleGenerate(w http.ResponseWriter, r *http.Request) {
	response, err := s.Generate(r.Context())
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to trigger advertised recipe generation", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if len(response.Kicked) > 0 {
		w.WriteHeader(http.StatusAccepted)
	}
	if err := json.NewEncoder(w).Encode(response); err != nil {
		slog.ErrorContext(r.Context(), "failed to write advertised recipe generation response", "error", err)
	}
}

func (s advertisedGenerationServer) Generate(ctx context.Context) (*AdvertisedRecipeGenerationResponse, error) {
	response := AdvertisedRecipeGenerationResponse{
		GeneratedAt: time.Now(),
	}
	for _, advertised := range AdvertisedRecipeLocations() {
		entry, kicked, err := s.generateLocation(ctx, advertised.ID)
		if err != nil {
			slog.ErrorContext(ctx, "failed to trigger advertised recipes", "location", advertised.ID, "error", err)
			response.Failures = append(response.Failures, AdvertisedRecipeFailure{
				LocationID: advertised.ID,
				Error:      err.Error(),
			})
			continue
		}
		if kicked {
			response.Kicked = append(response.Kicked, advertised.ID)
		}
		if entry.ShoppingListHash != "" {
			response.Entries = append(response.Entries, entry)
		}
	}

	if len(response.Entries) > 0 {
		manifest := AdvertisedRecipeManifest{
			GeneratedAt: response.GeneratedAt,
			Entries:     response.Entries,
			Failures:    response.Failures,
		}
		if err := SaveAdvertisedRecipeManifest(ctx, s.cache, manifest); err != nil {
			return nil, fmt.Errorf("save advertised recipe manifest: %w", err)
		}
	}

	slog.InfoContext(ctx, "triggered advertised recipe generation", "entries", len(response.Entries), "kicked", len(response.Kicked), "failures", len(response.Failures))
	return &response, nil
}

func (s advertisedGenerationServer) generateLocation(ctx context.Context, locationID string) (AdvertisedRecipeEntry, bool, error) {
	loc, err := s.locations.GetLocationByID(ctx, locationID)
	if err != nil {
		return AdvertisedRecipeEntry{}, false, fmt.Errorf("hydrate location: %w", err)
	}

	date, err := recipes.StoreToDate(ctx, time.Now(), loc)
	if err != nil {
		return AdvertisedRecipeEntry{}, false, fmt.Errorf("resolve store date: %w", err)
	}

	params := recipes.DefaultParams(loc, date)
	result, err := s.generator.KickGenerationIfNotPresent(ctx, params)
	if err != nil {
		return AdvertisedRecipeEntry{}, false, fmt.Errorf("kick generation: %w", err)
	}

	_, err = s.rio.FromCache(ctx, result.Hash)
	if err != nil {
		if !result.Kicked {
			return AdvertisedRecipeEntry{}, result.Kicked, fmt.Errorf("load generated shopping list: %w", err)
		}
		return AdvertisedRecipeEntry{}, result.Kicked, nil
	}

	return AdvertisedRecipeEntry{
		Location:         *loc,
		Date:             params.Date,
		ShoppingListHash: result.Hash,
		GeneratedAt:      time.Now(),
	}, result.Kicked, nil
}
