package heb

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"careme/internal/ai"
	"careme/internal/albertsons"
	"careme/internal/cache"
	"careme/internal/parallelism"

	"github.com/samber/lo"
)

const (
	CategoryFruitParent       = "490020"
	CategoryFruitChild        = "490082"
	CategoryVegetablesParent  = "490020"
	CategoryVegetablesChild   = "490083"
	CategoryMeatSeafoodParent = "2863"
	CategoryMeatSeafoodChild  = "490023"
	CategoryDairyEggsParent   = "2863"
	CategoryDairyEggsChild    = "490016"
	CategoryPastaRiceParent   = "490024"
	CategoryPastaRiceChild    = "490121"
)

var defaultHEBStaplesSignature = lo.Must(json.Marshal(StapleCategories()))

type StapleCategory struct {
	Name     string
	ParentID string
	ChildID  string
}

type hebQueryClient interface {
	Category(ctx context.Context, opts CategoryOptions) ([]Product, error)
}

type loadReese84 func(context.Context) (string, error)

type identityProvider struct{}

type StaplesProvider struct {
	identityProvider
	client      hebQueryClient
	loadReese84 loadReese84
}

func NewIdentityProvider() identityProvider {
	return identityProvider{}
}

func NewStaplesProvider(httpClient *http.Client) (StaplesProvider, error) {
	albertsonsCache, err := cache.EnsureCache(albertsons.Container)
	if err != nil {
		return StaplesProvider{}, fmt.Errorf("create albertsons cache for HEB reese84 token: %w", err)
	}

	return newStaplesProviderWithClient(NewQueryClient(QueryClientConfig{
		HTTPClient: httpClient,
	}), func(ctx context.Context) (string, error) {
		record, err := albertsons.LoadLatestReese84(ctx, albertsonsCache)
		if err != nil {
			return "", fmt.Errorf("load cached albertsons reese84 token for HEB: %w", err)
		}
		return record.Cookie, nil
	}), nil
}

func newStaplesProviderWithClient(client hebQueryClient, loadReese84 loadReese84) StaplesProvider {
	return StaplesProvider{
		client:      client,
		loadReese84: loadReese84,
	}
}

func (p identityProvider) Signature() string {
	return string(defaultHEBStaplesSignature)
}

func (p identityProvider) IsID(locationID string) bool {
	return IsID(locationID)
}

func (p StaplesProvider) FetchStaples(ctx context.Context, locationID string) ([]ai.InputIngredient, error) {
	if p.client == nil {
		return nil, fmt.Errorf("heb client is required")
	}
	if p.loadReese84 == nil {
		return nil, fmt.Errorf("heb reese84 loader is required")
	}

	storeID, err := storeIDFromLocation(locationID)
	if err != nil {
		return nil, err
	}

	reese84, err := p.loadReese84(ctx)
	if err != nil {
		return nil, err
	}

	return parallelism.Flatten(StapleCategories(), func(category StapleCategory) ([]ai.InputIngredient, error) {
		products, err := p.client.Category(ctx, CategoryOptions{
			Reese84:  reese84,
			StoreID:  storeID,
			ParentID: category.ParentID,
			ChildID:  category.ChildID,
		})
		if err != nil {
			slog.WarnContext(ctx, "failed to fetch heb category", "category", category.Name, "location", locationID, "error", err)
			return nil, err
		}

		ingredients := lo.Map(products, func(product Product, _ int) ai.InputIngredient {
			return productToIngredient(product, category)
		})
		slog.InfoContext(ctx, "found heb staples for category", "count", len(ingredients), "category", category.Name, "location", locationID)
		return ingredients, nil
	})
}

func (p StaplesProvider) FetchWines(_ context.Context, locationID string, _ []string) ([]ai.InputIngredient, error) {
	return nil, fmt.Errorf("wine lookup is not supported for location %q", locationID)
}

func StapleCategories() []StapleCategory {
	return []StapleCategory{
		{Name: "fruit", ParentID: CategoryFruitParent, ChildID: CategoryFruitChild},
		{Name: "vegetables", ParentID: CategoryVegetablesParent, ChildID: CategoryVegetablesChild},
		{Name: "meat & seafood", ParentID: CategoryMeatSeafoodParent, ChildID: CategoryMeatSeafoodChild},
		{Name: "dairy & eggs", ParentID: CategoryDairyEggsParent, ChildID: CategoryDairyEggsChild},
		{Name: "pasta & rice", ParentID: CategoryPastaRiceParent, ChildID: CategoryPastaRiceChild},
	}
}

func storeIDFromLocation(locationID string) (string, error) {
	locationID = strings.TrimSpace(locationID)
	if !IsID(locationID) {
		return "", fmt.Errorf("invalid heb location id %q", locationID)
	}
	return strings.TrimPrefix(locationID, LocationIDPrefix), nil
}

func productToIngredient(product Product, category StapleCategory) ai.InputIngredient {
	categories := categoryNames(product, category)
	location := ""
	if product.ProductLocation != nil {
		location = product.ProductLocation.Location
	}
	brand := ""
	if product.Brand != nil {
		brand = product.Brand.Name
	}

	return ai.NormalizeInputIngredient(ai.InputIngredient{
		ProductID:   product.ID,
		Description: product.DisplayName,
		Brand:       brand,
		Categories:  categories,
		AisleNumber: location,
	})
}

func categoryNames(product Product, category StapleCategory) []string {
	parts := strings.Split(product.FullCategoryHierarchy, "/")
	names := make([]string, 0, len(parts)+1)
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			names = append(names, part)
		}
	}
	if len(names) == 0 {
		names = append(names, category.Name)
	}
	return names
}
