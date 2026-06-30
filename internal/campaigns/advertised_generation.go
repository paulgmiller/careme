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

// AdvertisedRecipeLocations returns the stores we intentionally pre-generate and promote.
// should probably vagule align with StaplesWatchdogLocations() as why wouldn't we monitor
// the most importnant stores
func AdvertisedRecipeLocations() map[string]locations.Location {
	return map[string]locations.Location{
		//{ID: "wholefoods_10153", ZipCode: "97209"},
		//{ID: "safeway_490", ZipCode: "86403"},
		"bellevue": {ID: "70500874", ZipCode: "98101"}, // bellevue
		"issaquah": {ID: "70100658", ZipCode: "98029"},
	}
}

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
	ctx := r.Context()
	var err error
	for _, advertised := range AdvertisedRecipeLocations() {
		err = errors.Join(err, s.generateLocation(ctx, &advertised))
	}
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to trigger advertised recipe generation", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (s advertisedGenerationServer) generateLocation(ctx context.Context, loc *locations.Location) error {
	date, err := recipes.StoreToDate(ctx, time.Now(), loc)
	if err != nil {
		return fmt.Errorf("resolve store date: %w", err)
	}

	err = s.generator.KickGenerationIfNotPresent(ctx, recipes.DefaultParams(loc, date))
	if err != nil {
		return fmt.Errorf("kick generation: %w", err)
	}

	return nil
}
