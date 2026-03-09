package wholefoods

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log/slog"
	"strings"

	"careme/internal/kroger"
	"careme/internal/parallelism"

	"github.com/samber/lo"
)

var defaultStaplesSignature = lo.Must(json.Marshal(defaultStaples()))

type CategoryClient interface {
	Category(ctx context.Context, queryterm, store string) (*CategoryResponse, error)
}

type identityProvider struct{}

type StaplesProvider struct {
	identityProvider
	client CategoryClient
}

func NewStaplesProvider(client CategoryClient) StaplesProvider {
	return StaplesProvider{client: client}
}

func NewIdentityProvider() identityProvider {
	return identityProvider{}
}

func (p identityProvider) Signature() string {
	return string(defaultStaplesSignature)
}

func (p identityProvider) IsID(locationID string) bool {
	_, ok := parseLocationID(locationID)
	return ok
}

func (p StaplesProvider) FetchStaples(ctx context.Context, locationID string) ([]kroger.Ingredient, error) {
	if p.client == nil {
		return nil, fmt.Errorf("whole foods client is required")
	}

	//should identity provider do this?
	storeID := strings.TrimPrefix(locationID, LocationIDPrefix)
	if storeID == locationID || storeID == "" {
		return nil, fmt.Errorf("invalid whole foods location id %q", locationID)
	}

	return parallelism.Flatten(defaultStaples(), func(category string) ([]kroger.Ingredient, error) {
		resp, err := p.client.Category(ctx, category, storeID)
		if err != nil {
			return nil, err
		}

		ingredients := lo.Map(resp.Results, func(product Product, _ int) kroger.Ingredient {
			return productToIngredient(product)
		})
		slog.InfoContext(ctx, "Found ingredients for category", "count", len(ingredients), "category", category, "location", locationID)

		return ingredients, nil
	})
}

func (p StaplesProvider) GetIngredients(ctx context.Context, locationID string, searchTerm string, skip int) ([]kroger.Ingredient, error) {
	if p.client == nil {
		return nil, fmt.Errorf("whole foods client is required")
	}

	storeID := strings.TrimPrefix(locationID, LocationIDPrefix)
	if storeID == locationID || storeID == "" {
		return nil, fmt.Errorf("invalid whole foods location id %q", locationID)
	}

	resp, err := p.client.Category(ctx, searchTerm, storeID)
	if err != nil {
		return nil, err
	}

	ingredients := lo.Map(resp.Results, func(product Product, _ int) kroger.Ingredient {
		return productToIngredient(product)
	})
	if skip >= len(ingredients) {
		return []kroger.Ingredient{}, nil
	}
	return ingredients[skip:], nil
}

func defaultStaples() []string {
	return []string{
		"fresh-vegetables",
		"fresh-herbs",
		"fresh-fruit",
		"beef",
		"chicken",
		"fish",
		"pork",
		"shellfish",
		"goat-lamb-veal",
		"game-meats",
	}
	//rice-grains?
	//pasta-noodles
	//red-wine, white-wine, sparkling
}

func productToIngredient(product Product) kroger.Ingredient {
	var regularPrice *float32
	if product.RegularPrice > 0 {
		price := float32(product.RegularPrice)
		regularPrice = &price
	}

	var salePrice *float32
	if product.SalePrice > 0 {
		price := float32(product.SalePrice)
		salePrice = &price
	}

	/* unit of measure is more around pricing than total size)
	TODO how should we normalize prices per units here and in kroger.
	var size *string
	sizeText := strings.TrimSpace(strings.Join(compactStrings(product.UOM), " "))
	if sizeText != "" {
		size = &sizeText
	}*/

	//categories := compactStrings(localCategory(product))

	hasher := fnv.New32a()
	_ = lo.Must(hasher.Write([]byte(product.Slug)))
	productId := base64.RawURLEncoding.EncodeToString(hasher.Sum(nil))
	return kroger.Ingredient{
		ProductId:   stringPtr(productId),
		Brand:       stringPtr(strings.TrimSpace(product.Brand)),
		Description: stringPtr(strings.TrimSpace(product.Name)),
		//Size:         size,
		PriceRegular: regularPrice,
		PriceSale:    salePrice,
		// /	Categories:   slicePtr(categories),
	}
}

func stringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
