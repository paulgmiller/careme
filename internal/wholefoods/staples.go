package wholefoods

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log/slog"
	"strings"

	"careme/internal/ai"
	"careme/internal/parallelism"

	"github.com/samber/lo"
)

var defaultStaplesSignature = lo.Must(json.Marshal(defaultStaples()))

type CategoryClient interface {
	Category(ctx context.Context, queryterm, store string) ([]product, error)
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

func (p StaplesProvider) FetchStaples(ctx context.Context, locationID string) ([]ai.InputIngredient, error) {
	if p.client == nil {
		return nil, fmt.Errorf("whole foods client is required")
	}

	// should identity provider do this?
	storeID := strings.TrimPrefix(locationID, LocationIDPrefix)
	if storeID == locationID || storeID == "" {
		return nil, fmt.Errorf("invalid whole foods location id %q", locationID)
	}

	return parallelism.Flatten(defaultStaples(), func(category string) ([]ai.InputIngredient, error) {
		resp, err := p.client.Category(ctx, category, storeID)
		if err != nil {
			slog.WarnContext(ctx, "Failed to fetch category", "category", category, "location", locationID, "error", err)
			return nil, err
		}

		ingredients := lo.Map(resp, func(p product, _ int) ai.InputIngredient {
			return productToIngredient(p, category)
		})
		slog.InfoContext(ctx, "Found ingredients for category", "count", len(ingredients), "category", category, "location", locationID)

		return ingredients, nil
	})
}

func (p StaplesProvider) FetchWines(ctx context.Context, locationID string, _ []string) ([]ai.InputIngredient, error) {
	if p.client == nil {
		return nil, fmt.Errorf("whole foods client is required")
	}

	storeID := strings.TrimPrefix(locationID, LocationIDPrefix)
	if storeID == locationID || storeID == "" {
		return nil, fmt.Errorf("invalid whole foods location id %q", locationID)
	}

	return parallelism.Flatten(defaultWineCategories(), func(category string) ([]ai.InputIngredient, error) {
		resp, err := p.client.Category(ctx, category, storeID)
		if err != nil {
			return nil, err
		}
		return lo.Map(resp, func(p product, _ int) ai.InputIngredient {
			return productToIngredient(p, category)
		}), nil
	})
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
		"rice-grains",
		"pasta-noodles",
	}
}

func defaultWineCategories() []string {
	return []string{"red-wine", "white-wine", "sparkling"}
}

func productToIngredient(product product, category string) ai.InputIngredient {
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

	hasher := fnv.New32a()
	// dupes for different units of measure?
	_ = lo.Must(hasher.Write([]byte(product.Slug)))
	productId := base64.RawURLEncoding.EncodeToString(hasher.Sum(nil))
	return ai.NormalizeInputIngredient(ai.InputIngredient{
		ProductID:   productId,
		Brand:       strings.TrimSpace(product.Brand),
		Description: strings.TrimSpace(product.Name),
		// Size:         size,
		PriceRegular: regularPrice,
		PriceSale:    salePrice,
		AisleNumber:  category, // not as good as an actual aisle but still lets us sort
	})
}
