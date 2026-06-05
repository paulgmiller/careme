package heb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/parallelism"

	"github.com/samber/lo"
)

const (
	CategoryFruitParent      = "490020"
	CategoryFruitChild       = "490082"
	CategoryVegetablesParent = "490020"
	CategoryVegetablesChild  = "490083"
	CategoryBeefParent       = "490110"
	CategoryBeefChild        = "490529"
	CategoryPorkParent       = "490110"
	CategoryPorkChild        = "490536"
	CategoryChickenParent    = "490110"
	CategoryChickenChild     = "490531"
	CategorySausageParent    = "490110"
	CategorySausageChild     = "490537"
	CategoryFishParent       = "490111"
	CategoryFishChild        = "490540"
	CategoryShrimpParent     = "490111"
	CategoryShrimpChild      = "490541"

	defaultStapleLimit = 48
	bigStapleLimit     = 100
	produceStapleLimit = 300
	seafoodStapleLimit = 60
)

var defaultHEBStaplesSignature = lo.Must(json.Marshal(StapleCategories()))

type StapleCategory struct {
	Name         string
	ParentID     string
	ChildID      string
	CategoryPath string
	Int          string
	Limit        int
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
	hebCache, err := cache.EnsureCache(Container)
	if err != nil {
		return StaplesProvider{}, fmt.Errorf("create heb cache: %w", err)
	}
	buildID, err := LoadLatestBuildID(context.Background(), hebCache)
	if err != nil && !errors.Is(err, cache.ErrNotFound) {
		return StaplesProvider{}, fmt.Errorf("load cached heb build id: %w", err)
	}
	buildIDLoader := func(ctx context.Context, opts buildIDOptions) (string, error) {
		loader, err := newBrightDataBuildIDLoaderFromEnv()
		if err != nil {
			return "", err
		}
		buildID, err := loader(ctx, opts)
		if err != nil {
			return "", err
		}
		if err := SaveLatestBuildID(ctx, hebCache, buildID); err != nil {
			return "", err
		}
		return buildID, nil
	}

	return newStaplesProviderWithDeps(NewQueryClient(QueryClientConfig{
		HTTPClient:  httpClient,
		BuildID:     buildID,
		LoadBuildID: buildIDLoader,
	}), func(ctx context.Context) (string, error) {
		record, err := LoadLatestReese84(ctx, hebCache)
		if err != nil {
			return "", fmt.Errorf("load cached heb reese84 token: %w", err)
		}
		slog.InfoContext(ctx, "reese84", "token", record.Cookie)
		return record.Cookie, nil
	}), nil
}

func newStaplesProviderWithClient(client hebQueryClient, loadReese84 loadReese84) StaplesProvider {
	return newStaplesProviderWithDeps(client, loadReese84)
}

func newStaplesProviderWithDeps(client hebQueryClient, loadReese84 loadReese84) StaplesProvider {
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
			Reese84:      reese84,
			StoreID:      storeID,
			ParentID:     category.ParentID,
			ChildID:      category.ChildID,
			CategoryPath: category.CategoryPath,
			Int:          category.Int,
			Limit:        category.Limit,
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

func isCategoryNotFound(err error) bool {
	var httpErr *CategoryHTTPError
	return errors.As(err, &httpErr) && httpErr.StatusCode == http.StatusNotFound
}

func (p StaplesProvider) FetchWines(_ context.Context, locationID string, _ []string) ([]ai.InputIngredient, error) {
	return nil, fmt.Errorf("wine lookup is not supported for location %q", locationID)
}

func StapleCategories() []StapleCategory {
	return []StapleCategory{
		{Name: "beef", ParentID: CategoryBeefParent, ChildID: CategoryBeefChild, CategoryPath: "/category/shop/meat-seafood/meat/beef/490110/490529?int=curbside-category-shortcuts.meat.beef", Int: "curbside-category-shortcuts.meat.beef", Limit: bigStapleLimit},
		{Name: "pork", ParentID: CategoryPorkParent, ChildID: CategoryPorkChild, CategoryPath: "/category/shop/meat-seafood/meat/pork/490110/490536?int=curbside-category-shortcuts.meat.pork", Int: "curbside-category-shortcuts.meat.pork", Limit: bigStapleLimit},
		{Name: "chicken", ParentID: CategoryChickenParent, ChildID: CategoryChickenChild, CategoryPath: "/category/shop/meat-seafood/meat/chicken/490110/490531?int=curbside-category-shortcuts.meat.chicken", Int: "curbside-category-shortcuts.meat.chicken", Limit: bigStapleLimit},
		{Name: "sausage", ParentID: CategorySausageParent, ChildID: CategorySausageChild, CategoryPath: "/category/shop/meat-seafood/meat/sausage/490110/490537?int=curbside-category-shortcuts.meat.sausage", Int: "curbside-category-shortcuts.meat.sausage", Limit: defaultStapleLimit},
		{Name: "fish", ParentID: CategoryFishParent, ChildID: CategoryFishChild, CategoryPath: "/category/shop/meat-seafood/seafood/fish/490111/490540?int=curbside-category-shortcuts.seafood.fish", Int: "curbside-category-shortcuts.seafood.fish", Limit: seafoodStapleLimit},
		{Name: "shrimp", ParentID: CategoryShrimpParent, ChildID: CategoryShrimpChild, CategoryPath: "/category/shop/meat-seafood/seafood/shrimp-shellfish/490111/490541?int=curbside-category-shortcuts.seafood.shrimp", Int: "curbside-category-shortcuts.seafood.shrimp", Limit: seafoodStapleLimit},
		{Name: "vegetables", ParentID: CategoryVegetablesParent, ChildID: CategoryVegetablesChild, CategoryPath: "/category/shop/fruit-vegetables/vegetables/490020/490083", Limit: produceStapleLimit},
		{Name: "fruit", ParentID: CategoryFruitParent, ChildID: CategoryFruitChild, CategoryPath: "/category/shop/fruit-vegetables/fruit/490020/490082?int=curbside-category-shortcuts.fruit-vegetables.fruits", Int: "curbside-category-shortcuts.fruit-vegetables.fruits", Limit: produceStapleLimit},
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
