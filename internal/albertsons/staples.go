package albertsons

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"careme/internal/albertsons/query"
	"careme/internal/kroger"
	"careme/internal/parallelism"

	"github.com/samber/lo"
)

var defaultStaplesSignature = mustJSONSignature(query.StapleCategories())

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

func NewStaplesProvider(cfg query.SearchClientConfig) StaplesProvider {
	return newStaplesProviderWithFactory(func(baseURL string) (searchClient, error) {
		cfg.BaseURL = baseURL
		return query.NewSearchClient(cfg)
	})
}

func newStaplesProviderWithFactory(factory searchClientFactory) StaplesProvider {
	return StaplesProvider{
		newClient: factory,
	}
}

func (p identityProvider) Signature() string {
	return defaultStaplesSignature
}

func (p identityProvider) IsID(locationID string) bool {
	return IsID(locationID)
}

func (p StaplesProvider) FetchStaples(ctx context.Context, locationID string) ([]kroger.Ingredient, error) {
	client, storeID, err := p.clientForLocation(locationID)
	if err != nil {
		return nil, err
	}

	return parallelism.Flatten(query.StapleCategories(), func(category string) ([]kroger.Ingredient, error) {
		payload, err := client.Search(ctx, storeID, category, query.SearchOptions{})
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

func (p StaplesProvider) GetIngredients(ctx context.Context, locationID string, searchTerm string, skip int) ([]kroger.Ingredient, error) {
	client, storeID, err := p.clientForLocation(locationID)
	if err != nil {
		return nil, err
	}

	ingredients, err := parallelism.Flatten(query.StapleCategories(), func(category string) ([]kroger.Ingredient, error) {
		payload, err := client.Search(ctx, storeID, category, query.SearchOptions{
			Query: searchTerm,
		})
		if err != nil {
			return nil, err
		}

		return lo.Map(payload.Response.Docs, func(product query.PathwaySearchProduct, _ int) kroger.Ingredient {
			return productToIngredient(product)
		}), nil
	})
	if err != nil {
		return nil, err
	}
	if skip >= len(ingredients) {
		return []kroger.Ingredient{}, nil
	}
	return ingredients[skip:], nil
}

func (p StaplesProvider) clientForLocation(locationID string) (searchClient, string, error) {
	if p.newClient == nil {
		return nil, "", fmt.Errorf("albertsons query client is required")
	}

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
	categories := compactStrings([]string{product.DepartmentName, product.ShelfName})

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

func sizeText(product query.PathwaySearchProduct) *string {
	sizeParts := compactStrings([]string{product.ItemSizeQty, product.UnitOfMeasure})
	if len(sizeParts) == 0 {
		return nil
	}
	size := strings.Join(sizeParts, " ")
	return &size
}

func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		out = append(out, value)
	}
	return out
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

func mustJSONSignature(value any) string {
	signature, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Errorf("marshal staples signature: %w", err))
	}
	return string(signature)
}
