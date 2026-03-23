package actowiz

import (
	"careme/internal/kroger"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
)

const LocationIDPrefix = "safeway_"

//go:embed safeway_products.json
var safewayProductsJSON []byte

var (
	staples, wines          = mustLoadSafewayProducts()
	defaultStaplesSignature = "everything" // no filtering yet"
)

type identityProvider struct{}

type StaplesProvider struct {
	identityProvider
}

func NewIdentityProvider() identityProvider {
	return identityProvider{}
}

var once sync.Once

func NewStaplesProvider() StaplesProvider {
	once.Do(func() {
		slog.Info("Loaded safeway", "staples_count", len(staples), "wine_count", len(wines),
			"filtered_staples", len(all(staples)), "filtered_wines", len(all(wines)))
	})
	return StaplesProvider{}
}

func (p identityProvider) Signature() string {
	return defaultStaplesSignature
}

func (p identityProvider) IsID(locationID string) bool {
	storeID := strings.TrimPrefix(locationID, LocationIDPrefix)
	return storeID != "" && storeID != locationID
}

func all(products []SafewayProduct) []kroger.Ingredient {
	// do this once instead of every time?
	ingredients := make([]kroger.Ingredient, 0, len(products))
	var unavailableCount int
	var salamiCount int
	var packagedProduceCount int
	for _, product := range products {
		if !product.Availability {
			unavailableCount++
			continue
		}
		if product.ProductName == "N/A" {
			panic("no name")
		}
		// another option but dropped produce score.
		if product.SubCategory == "Packaged Produce" {
			packagedProduceCount++
			continue
		}
		if product.SubCategory == "Salami & Lunch Meats" {
			salamiCount++
			continue
		}

		ingredients = append(ingredients, productToIngredient(product))
	}
	slog.Info("Filtered safeway products", "total_count", len(products),
		"unavailable_count", unavailableCount, "salami_count", salamiCount,
		"packaged_produce_count", packagedProduceCount, "final_count", len(ingredients))
	return ingredients
}

func (p StaplesProvider) FetchStaples(_ context.Context, locationID string) ([]kroger.Ingredient, error) {
	if !p.IsID(locationID) {
		return nil, fmt.Errorf("invalid safeway location id %q", locationID)
	}
	return all(staples), nil
}

func (p StaplesProvider) GetIngredients(_ context.Context, locationID string, searchTerm string, _ int) ([]kroger.Ingredient, error) {
	if !p.IsID(locationID) {
		return nil, fmt.Errorf("invalid safeway location id %q", locationID)
	}

	return all(wines), nil
}

func ingredientMatches(ingredient kroger.Ingredient, searchTerm string) bool {
	for _, value := range []string{
		stringValue(ingredient.Description),
		stringValue(ingredient.Brand),
	} {
		if strings.Contains(strings.ToLower(value), searchTerm) {
			return true
		}
	}
	// categories might help for wine?
	return false
}

func productToIngredient(product SafewayProduct) kroger.Ingredient {
	description, size := splitProductName(product.ProductName) // dubious size is really always
	regularPrice := float32Ptr(product.MRP)
	salePrice := float32Ptr(product.DiscountedPrice)
	if salePrice == nil {
		salePrice = regularPrice
	}

	productID := strconv.FormatInt(product.ID, 10)
	categories := compactStrings([]string{product.Category, product.SubCategory})
	var categoryPtr *[]string
	if len(categories) > 0 {
		categoryPtr = &categories
	}

	return kroger.Ingredient{
		ProductId:    stringPtr(productID),
		Description:  stringPtr(description),
		Size:         stringPtr(size),
		PriceRegular: regularPrice,
		PriceSale:    salePrice,
		Categories:   categoryPtr,
	}
}

func splitProductName(name string) (string, string) {
	name = strings.TrimSpace(name)
	parts := strings.Split(name, " - ")
	if len(parts) < 2 {
		return name, ""
	}
	return strings.TrimSpace(strings.Join(parts[:len(parts)-1], " - ")), strings.TrimSpace(parts[len(parts)-1])
}

func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || strings.EqualFold(value, "N/A") {
			continue
		}
		out = append(out, value)
	}
	return out
}

func mustLoadSafewayProducts() ([]SafewayProduct, []SafewayProduct) {
	var allProducts []SafewayProduct
	if err := json.Unmarshal(safewayProductsJSON, &allProducts); err != nil {
		panic(fmt.Errorf("decode safeway products: %w", err))
	}

	var wines []SafewayProduct
	var staples []SafewayProduct
	for _, product := range allProducts {
		if strings.HasPrefix(product.Category, "Wine") {
			wines = append(wines, product)
		} else {
			// this is a bit hacky but we want to keep the wine products separate for now since they have different filtering needs and we want to be able to easily exclude them from staples.
			staples = append(staples, product)
		}
	}
	slog.Info("Loaded safeway products", "product_count", len(staples), "wine_count", len(wines))

	return staples, wines
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func stringPtr(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}

func float32Ptr(value *float64) *float32 {
	if value == nil {
		return nil
	}
	out := float32(*value)
	return &out
}
