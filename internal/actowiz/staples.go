package actowiz

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"

	"careme/internal/kroger"
)

const LocationIDPrefix = "safeway_"

//go:embed safeway_products.json
var safewayProductsJSON []byte

var (
	embeddedSafewayProducts = mustLoadSafewayProducts()
	defaultStaplesSignature = "everything" //no filtering yet"
)

type identityProvider struct{}

type StaplesProvider struct {
	identityProvider
}

func NewIdentityProvider() identityProvider {
	return identityProvider{}
}

func NewStaplesProvider() StaplesProvider {
	slog.Info("Loaded safeway", "safeway_count", len(embeddedSafewayProducts), "filtered_count", len(all()))
	return StaplesProvider{}
}

func (p identityProvider) Signature() string {
	return defaultStaplesSignature
}

func (p identityProvider) IsID(locationID string) bool {
	storeID := strings.TrimPrefix(locationID, LocationIDPrefix)
	return storeID != "" && storeID != locationID
}

func all() []kroger.Ingredient {
	//do this once instead of every time?
	ingredients := make([]kroger.Ingredient, 0, len(embeddedSafewayProducts))
	for _, product := range embeddedSafewayProducts {
		if !product.Availability {
			continue
		}
		if product.ProductName == "N/A" {
			continue
		}
		//another option but dropped produce score.
		//||	product.SubCategory == "Packaged Produce"
		if product.SubCategory == "Salami & Lunch Meats" {
			continue
		}

		ingredients = append(ingredients, productToIngredient(product))
	}
	return ingredients
}

func (p StaplesProvider) FetchStaples(_ context.Context, locationID string) ([]kroger.Ingredient, error) {
	if !p.IsID(locationID) {
		return nil, fmt.Errorf("invalid safeway location id %q", locationID)
	}
	return all(), nil
}

func (p StaplesProvider) GetIngredients(_ context.Context, locationID string, searchTerm string, _ int) ([]kroger.Ingredient, error) {
	if !p.IsID(locationID) {
		return nil, fmt.Errorf("invalid safeway location id %q", locationID)
	}

	return filterIngredients(all(), searchTerm), nil
}

func filterIngredients(ingredients []kroger.Ingredient, searchTerm string) []kroger.Ingredient {
	searchTerm = strings.TrimSpace(strings.ToLower(searchTerm))
	if searchTerm == "" {
		return slices.Clone(ingredients)
	}

	filtered := make([]kroger.Ingredient, 0, len(ingredients))
	for _, ingredient := range ingredients {
		if ingredientMatches(ingredient, searchTerm) {
			filtered = append(filtered, ingredient)
		}
	}
	return filtered
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
	//categories might help for wine?
	return false
}

func productToIngredient(product SafewayProduct) kroger.Ingredient {
	description, size := splitProductName(product.ProductName) //dubious size is really always
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

func mustLoadSafewayProducts() []SafewayProduct {
	var products []SafewayProduct
	if err := json.Unmarshal(safewayProductsJSON, &products); err != nil {
		panic(fmt.Errorf("decode safeway products: %w", err))
	}
	return products
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
