package campaigns

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"careme/internal/locations"
	"careme/internal/recipes"
	"careme/internal/routing"
)

type recipeGenerationKickstarter interface {
	KickGenerationIfNotPresent(ctx context.Context, p *recipes.GeneratorParams) error
}

type advertisedGenerationServer struct {
	generator recipeGenerationKickstarter
}

func RegisterAdvertisedRecipeGeneration(
	mux routing.Registrar,
	generator recipeGenerationKickstarter,
) {
	h := advertisedGenerationServer{
		generator: generator,
	}
	mux.HandleFunc("POST /campaigns/advertised-recipes/generate", h.handleGenerate)
}

func (s advertisedGenerationServer) handleGenerate(w http.ResponseWriter, r *http.Request) {
	err := s.Generate(r.Context())
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to trigger advertised recipe generation", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (s advertisedGenerationServer) Generate(ctx context.Context) error {
	var err error
	for _, advertised := range AdvertisedRecipeLocations() {
		err = errors.Join(err, s.generateLocation(ctx, &advertised))
	}
	return err
}

func (s advertisedGenerationServer) generateLocation(ctx context.Context, loc *locations.Location) error {
	date, err := recipes.StoreToDate(ctx, time.Now(), loc)
	if err != nil {
		fmt.Errorf("resolve store date: %w", err)
	}

	err = s.generator.KickGenerationIfNotPresent(ctx, recipes.DefaultParams(loc, date))
	if err != nil {
		return fmt.Errorf("kick generation: %w", err)
	}

	return nil
}
