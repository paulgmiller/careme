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

// Campaign is a promoted store plus campaign-specific page context.
type campaign struct {
	Location    locations.Location
	HelpMessage string
}

func genericLocationHelp(location string) string {
	return fmt.Sprintf(`Here are 3 recipes made with ingredients in stock today at %s.
Want something different? Add a note below and choose Try again, chef. 
Add the recipes you like, hide the ones you don't, and we'll build your shopping list.`, location)
}

// AdvertisedRecipeLocations returns the campaigns we intentionally pre-generate and promote.
// should probably vagule align with StaplesWatchdogLocations() as why wouldn't we monitor
// the most importnant stores
func AdvertisedRecipeLocations() map[string]campaign {
	return map[string]campaign{
		// https://chatgpt.com/share/6a4d4793-987c-83e8-9f0e-e12c72772df7
		// west lake wholefoods_10216 98121
		// bella bottega qfc 70500860 98052
		"university_village_qfc": {
			Location:    locations.Location{ID: "70500807", ZipCode: "98105"},
			HelpMessage: genericLocationHelp("University Village QFC"),
		},
		"redmond_wf": {
			Location:    locations.Location{ID: "wholefoods_10260", ZipCode: "98052"},
			HelpMessage: genericLocationHelp("Redmond Whole Foods"),
		},
		"bellevue_wf": {
			Location:    locations.Location{ID: "wholefoods_10153", ZipCode: "98004"},
			HelpMessage: genericLocationHelp("Bellevue Whole Foods"),
		},
		"bellevue": {
			Location:    locations.Location{ID: "70100023", ZipCode: "98007"},
			HelpMessage: genericLocationHelp("Bellevue Fred Meyer"),
		},
		"issaquah": {
			Location:    locations.Location{ID: "70100658", ZipCode: "98029"},
			HelpMessage: genericLocationHelp("Issaquah Fred Meyer"),
		},
	}
}

type recipeGenerationKickstarter interface {
	KickGenerationIfNotPresent(ctx context.Context, p *recipes.GeneratorParams) error
}

type advertisedLocationStore interface {
	GetLocationByID(ctx context.Context, locationID string) (*locations.Location, error)
}

type advertisedGenerationServer struct {
	generator recipeGenerationKickstarter
	locations advertisedLocationStore
}

func RegisterAdvertisedRecipeGeneration(
	mux routing.Registrar,
	locations advertisedLocationStore,
	generator recipeGenerationKickstarter,
) {
	h := advertisedGenerationServer{
		generator: generator,
		locations: locations,
	}
	mux.HandleFunc("POST /campaigns/advertised-recipes/generate", h.handleGenerate)
}

func (s advertisedGenerationServer) handleGenerate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var err error
	for _, advertised := range AdvertisedRecipeLocations() {
		err = errors.Join(err, s.generateLocation(ctx, advertised.Location.ID))
	}
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to trigger advertised recipe generation", "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func (s advertisedGenerationServer) generateLocation(ctx context.Context, locationID string) error {
	loc, err := s.locations.GetLocationByID(ctx, locationID)
	if err != nil {
		return fmt.Errorf("hydrate location %s: %w", locationID, err)
	}

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
