package campaigns

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"careme/internal/cache"
	"careme/internal/locations"
)

const AdvertisedRecipesManifestCacheKey = "advertising/advertised_recipes.json"

type AdvertisedRecipeManifest struct {
	GeneratedAt time.Time                 `json:"generated_at"`
	Entries     []AdvertisedRecipeEntry   `json:"entries"`
	Failures    []AdvertisedRecipeFailure `json:"failures,omitempty"`
}

type AdvertisedRecipeEntry struct {
	Location         locations.Location `json:"location"`
	Date             time.Time          `json:"date"`
	ShoppingListHash string             `json:"shopping_list_hash"`
	RecipeHashes     []string           `json:"recipe_hashes"`
	GeneratedAt      time.Time          `json:"generated_at"`
}

type AdvertisedRecipeFailure struct {
	LocationID string `json:"location_id"`
	Error      string `json:"error"`
}

// AdvertisedRecipeLocations returns the stores we intentionally pre-generate and promote.
func AdvertisedRecipeLocations() []locations.Location {
	return []locations.Location{
		{ID: "wholefoods_10153", ZipCode: "97209"},
		{ID: "safeway_490", ZipCode: "86403"},
		{ID: "70500874", ZipCode: "98101"},
		{ID: "starmarket_3566", ZipCode: "02108"},
		{ID: "acmemarkets_806", ZipCode: "19711"},
		{ID: "publix_1847", ZipCode: "35401"},
		{ID: "aldi_F219", ZipCode: "40222"},
		{ID: "heb_540", ZipCode: "77023"},
	}
}

func LoadAdvertisedRecipeManifest(ctx context.Context, c cache.Cache) (*AdvertisedRecipeManifest, error) {
	reader, err := c.Get(ctx, AdvertisedRecipesManifestCacheKey)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := reader.Close(); err != nil {
			slog.ErrorContext(ctx, "failed to close advertised recipe manifest", "error", err)
		}
	}()

	var manifest AdvertisedRecipeManifest
	if err := json.NewDecoder(reader).Decode(&manifest); err != nil {
		return nil, fmt.Errorf("decode advertised recipe manifest: %w", err)
	}
	return &manifest, nil
}

func SaveAdvertisedRecipeManifest(ctx context.Context, c cache.Cache, manifest AdvertisedRecipeManifest) error {
	body, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("marshal advertised recipe manifest: %w", err)
	}
	return c.Put(ctx, AdvertisedRecipesManifestCacheKey, string(body), cache.Unconditional())
}
