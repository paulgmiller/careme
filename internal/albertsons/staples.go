package albertsons

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"careme/internal/albertsons/query"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/kroger"
	"careme/internal/parallelism"

	"github.com/samber/lo"
)

var defaultStaplesSignature = lo.Must(json.Marshal(query.StapleCategories()))

type searchClient interface {
	Search(ctx context.Context, storeID, category string, opts query.SearchOptions) (*query.PathwaySearchPayload, error)
}

type searchClientFactory func(baseURL string) (searchClient, error)

type identityProvider struct{}

type StaplesProvider struct {
	identityProvider
	newClient searchClientFactory
}

func NewIdentityProvider() identityProvider {
	return identityProvider{}
}

func NewStaplesProvider(cfg config.AlbertsonsConfig, httpClient *http.Client) (StaplesProvider, error) {
	c, err := cache.EnsureCache(Container)
	if err != nil {
		return StaplesProvider{}, fmt.Errorf("create albertsons cache: %w", err)
	}

	return newStaplesProviderWithFactory(func(baseURL string) (searchClient, error) {
		querycfg := query.SearchClientConfig{
			SubscriptionKey: cfg.SearchSubscriptionKey,
			Reese84Provider: func(ctx context.Context) (string, error) {
				// umm we should cache this and rotate on failure?
				cookie, err := LoadLatestReese84(ctx, c)
				return cookie.Cookie, err
			},
			BaseURL:    baseURL,
			HTTPClient: httpClient,
		}
		return query.NewSearchClient(querycfg)
	}), nil
}

// only used for testing
func newStaplesProviderWithFactory(factory searchClientFactory) StaplesProvider {
	return StaplesProvider{
		newClient: factory,
	}
}

func (p identityProvider) Signature() string {
	return string(defaultStaplesSignature)
}

func (p identityProvider) IsID(locationID string) bool {
	return IsID(locationID)
}

var stapleRows = map[string]uint{
	query.Category_Vegatables: 150, // do we need way more of this?
	query.Category_Fruit:      100,
	query.Category_Meat:       100,
	query.Category_Seafood:    60,
}

func (p StaplesProvider) FetchStaples(ctx context.Context, locationID string) ([]kroger.Ingredient, error) {
	client, storeID, err := p.clientForLocation(locationID)
	if err != nil {
		return nil, err
	}

	return parallelism.Flatten(query.StapleCategories(), func(category string) ([]kroger.Ingredient, error) {
		payload, err := client.Search(ctx, storeID, category, query.SearchOptions{
			// how many rows? different per category? Should we paginate
			Rows: stapleRows[category],
		})
		if err != nil {
			return nil, err
		}

		ingredients := lo.Map(payload.Response.Docs, func(product query.PathwaySearchProduct, _ int) kroger.Ingredient {
			return productToIngredient(product)
		})
		slog.InfoContext(ctx, "found albertsons staples for category", "count", len(ingredients), "category", category, "location", locationID)
		return ingredients, nil
	})
}

// since this is mostly used by wine it isn't actuallyt they helpful.
func (p StaplesProvider) GetIngredients(ctx context.Context, locationID string, searchTerm string, skip int) ([]kroger.Ingredient, error) {
	client, storeID, err := p.clientForLocation(locationID)
	if err != nil {
		return nil, err
	}

	// should we just resturn all instead of search term? how many is this?
	payload, err := client.Search(ctx, storeID, query.Category_Wine, query.SearchOptions{
		Query: searchTerm, Rows: 100,
	})
	if err != nil {
		return nil, err
	}

	ingredients := lo.Map(payload.Response.Docs, func(product query.PathwaySearchProduct, _ int) kroger.Ingredient {
		return productToIngredient(product)
	})
	if skip >= len(ingredients) {
		return []kroger.Ingredient{}, nil
	}
	return ingredients[skip:], nil
}

// clientForLocation takes a prefixed store id and looks up chaing base url and returnes unprefixed id.
func (p StaplesProvider) clientForLocation(locationID string) (searchClient, string, error) {
	baseURL, storeID, ok := searchBaseURLAndStoreID(locationID)
	if !ok {
		return nil, "", fmt.Errorf("invalid albertsons location id %q", locationID)
	}

	client, err := p.newClient(baseURL)
	if err != nil {
		return nil, "", err
	}
	return client, storeID, nil
}

func searchBaseURLAndStoreID(locationID string) (string, string, bool) {
	locationID = strings.TrimSpace(locationID)
	for _, chain := range defaultChains {
		storeID := strings.TrimPrefix(locationID, chain.IDPrefix)
		if storeID == "" || storeID == locationID {
			continue
		}
		// should we append local elsewhere instead of trimming here?
		host := strings.TrimPrefix(chain.Domain, "local.")
		return "https://www." + host, storeID, true
	}
	return "", "", false
}

func productToIngredient(product query.PathwaySearchProduct) kroger.Ingredient {
	productID := stringPtr(strings.TrimSpace(product.ID))
	description := stringPtr(strings.TrimSpace(product.Name))
	size := sizeText(product)
	regularPrice := float32Ptr(product.BasePrice)
	salePrice := float32Ptr(product.Price)
	categories := lo.Compact([]string{product.DepartmentName, product.ShelfName})

	var categoryPtr *[]string
	if len(categories) > 0 {
		categoryPtr = &categories
	}

	return kroger.Ingredient{
		ProductId:    productID,
		Description:  description,
		Size:         size,
		PriceRegular: regularPrice,
		PriceSale:    salePrice,
		Categories:   categoryPtr,
	}
}

// this is a bit squirely shouldn't we take one ratehr than joiing both?
func sizeText(product query.PathwaySearchProduct) *string {
	sizeParts := lo.Compact([]string{product.ItemSizeQty, product.UnitOfMeasure})
	if len(sizeParts) == 0 {
		return nil
	}
	size := strings.Join(sizeParts, " ")
	return &size
}

func stringPtr(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func float32Ptr(value float64) *float32 {
	if value <= 0 {
		return nil
	}
	out := float32(value)
	return &out
}
