package heb

import (
	"context"
	"encoding/json"
	"fmt"
	"hash"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/ingredients/cachekey"
	"careme/internal/parallelism"

	"github.com/samber/lo"
)

const (
	categoryFruitParent      = "490020"
	categoryFruitChild       = "490082"
	categoryVegetablesParent = "490020"
	categoryVegetablesChild  = "490083"
	categoryBeefParent       = "490110"
	categoryBeefChild        = "490529"
	categoryPorkParent       = "490110"
	categoryPorkChild        = "490536"
	categoryChickenParent    = "490110"
	categoryChickenChild     = "490531"
	categorySausageParent    = "490110"
	categorySausageChild     = "490537"
	categoryFishParent       = "490111"
	categoryFishChild        = "490540"
	categoryShrimpParent     = "490111"
	categoryShrimpChild      = "490541"

	defaultStapleLimit = 48
	bigStapleLimit     = 100
	produceStapleLimit = 300
	seafoodStapleLimit = 60
)

var defaultHEBStaplesSignature = lo.Must(json.Marshal(stapleCategories()))

type stapleCategory struct {
	Name     string
	ParentID string
	ChildID  string
	Limit    int
}

type hebQueryClient interface {
	Category(ctx context.Context, opts categoryOptions) ([]product, error)
}

type loadReese84 func(context.Context) (string, error)

type identityProvider struct{}

func (p identityProvider) HashForStore(locationID string, date time.Time) hash.Hash {
	date = cachekey.StoreDay(date)
	return cachekey.NewHash(locationID, date, string(defaultHEBStaplesSignature))
}

type staplesProvider struct {
	identityProvider
	client      hebQueryClient
	loadReese84 loadReese84
}

func NewIdentityProvider() identityProvider {
	return identityProvider{}
}

func NewStaplesProvider(httpClient *http.Client) (staplesProvider, error) {
	hebCache, err := cache.EnsureCache(Container)
	if err != nil {
		return staplesProvider{}, fmt.Errorf("create heb cache: %w", err)
	}
	loadBuildID, err := newBrightDataBuildIDLoaderFromEnv()
	if err != nil {
		return staplesProvider{}, err
	}

	loadReese84 := func(ctx context.Context) (string, error) {
		record, err := LoadLatestReese84(ctx, hebCache)
		if err != nil {
			return "", fmt.Errorf("load cached heb reese84 token: %w", err)
		}
		slog.InfoContext(ctx, "reese84", "token", record.Cookie)
		return record.Cookie, nil
	}

	return newStaplesProviderWithDeps(newQueryClient(queryClientConfig{
		HTTPClient:  httpClient,
		LoadBuildID: loadBuildID,
	}), loadReese84), nil
}

func newStaplesProviderWithClient(client hebQueryClient, loadReese84 loadReese84) staplesProvider {
	return newStaplesProviderWithDeps(client, loadReese84)
}

func newStaplesProviderWithDeps(client hebQueryClient, loadReese84 loadReese84) staplesProvider {
	return staplesProvider{
		client:      client,
		loadReese84: loadReese84,
	}
}

func (p identityProvider) IsID(locationID string) bool {
	return isID(locationID)
}

func (p staplesProvider) FetchStaples(ctx context.Context, locationID string) ([]ai.InputIngredient, error) {
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

	return parallelism.Flatten(stapleCategories(), func(category stapleCategory) ([]ai.InputIngredient, error) {
		products, err := p.client.Category(ctx, categoryOptions{
			Reese84:  reese84,
			StoreID:  storeID,
			ParentID: category.ParentID,
			ChildID:  category.ChildID,
			Limit:    category.Limit,
		})
		if err != nil {
			slog.WarnContext(ctx, "failed to fetch heb category", "category", category.Name, "location", locationID, "error", err)
			return nil, err
		}

		ingredients := lo.Map(products, func(product product, _ int) ai.InputIngredient {
			return productToIngredient(product, category)
		})
		slog.InfoContext(ctx, "found heb staples for category", "count", len(ingredients), "category", category.Name, "location", locationID)
		return ingredients, nil
	})
}

func (p staplesProvider) FetchWines(_ context.Context, locationID string, _ []string) ([]ai.InputIngredient, error) {
	return nil, fmt.Errorf("wine lookup is not supported for location %q", locationID)
}

func stapleCategories() []stapleCategory {
	return []stapleCategory{
		{Name: "beef", ParentID: categoryBeefParent, ChildID: categoryBeefChild, Limit: bigStapleLimit},
		{Name: "pork", ParentID: categoryPorkParent, ChildID: categoryPorkChild, Limit: bigStapleLimit},
		{Name: "chicken", ParentID: categoryChickenParent, ChildID: categoryChickenChild, Limit: bigStapleLimit},
		{Name: "sausage", ParentID: categorySausageParent, ChildID: categorySausageChild, Limit: defaultStapleLimit},
		{Name: "fish", ParentID: categoryFishParent, ChildID: categoryFishChild, Limit: seafoodStapleLimit},
		{Name: "shrimp", ParentID: categoryShrimpParent, ChildID: categoryShrimpChild, Limit: seafoodStapleLimit},
		{Name: "vegetables", ParentID: categoryVegetablesParent, ChildID: categoryVegetablesChild, Limit: produceStapleLimit},
		{Name: "fruit", ParentID: categoryFruitParent, ChildID: categoryFruitChild, Limit: produceStapleLimit},
	}
}

func storeIDFromLocation(locationID string) (string, error) {
	locationID = strings.TrimSpace(locationID)
	if !isID(locationID) {
		return "", fmt.Errorf("invalid heb location id %q", locationID)
	}
	return strings.TrimPrefix(locationID, locationIDPrefix), nil
}

func productToIngredient(product product, category stapleCategory) ai.InputIngredient {
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
		ProductID:    product.ID,
		Description:  product.DisplayName,
		Size:         product.Size,
		Brand:        brand,
		Categories:   categories,
		AisleNumber:  location,
		PriceRegular: product.ListPrice,
		PriceSale:    product.SalePrice,
	})
}

func categoryNames(product product, category stapleCategory) []string {
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
