// Package imperfectfoods serves a fixed, product-only demo catalog.
package imperfectfoods

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"careme/internal/ai"
)

const baseURL = "https://www.imperfectfoods.com"

var sourceAisles = [...]struct{ name, path string }{
	{"Vegetables", "/shop/aisle/vegetables/all"},
	{"Meat & Seafood", "/shop/aisle/meat+%26+seafood/all"},
	{"Fruit", "/shop/aisle/fruit/all"},
	{"Wine Shop", "/shop/aisle/wine+shop/all"},
	{"Rescued", "/shop/aisle/rescued"},
	{"Pantry", "/shop/aisle/pantry/all"},
	{"Dairy & Eggs", "/shop/aisle/dairy+%26+eggs/all"},
}

type snapshot struct {
	CapturedAt time.Time       `json:"captured_at"`
	Aisles     []snapshotAisle `json:"aisles"`
}

type snapshotAisle struct {
	Name     string            `json:"name"`
	URL      string            `json:"url"`
	Products []snapshotProduct `json:"products"`
}

type snapshotProduct struct {
	ai.InputIngredient
	URL  string `json:"url"`
	Kind string `json:"kind"` // food or wine, including products in Rescued.
}

type catalog struct {
	staples []ai.InputIngredient
	wines   []ai.InputIngredient
}

func parseSnapshot(raw []byte) (catalog, error) {
	var snapshot snapshot
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return catalog{}, fmt.Errorf("decode snapshot: %w", err)
	}
	if snapshot.CapturedAt.IsZero() {
		return catalog{}, fmt.Errorf("catalog has not been captured")
	}
	if err := validateAisles(snapshot.Aisles); err != nil {
		return catalog{}, err
	}
	var result catalog
	products := make(map[string]snapshotProduct)
	var ids []string
	for _, aisle := range snapshot.Aisles {
		for _, product := range aisle.Products {
			product.InputIngredient = ai.NormalizeInputIngredient(product.InputIngredient)
			if product.ProductID == "" {
				product.ProductID = strings.TrimSpace(product.URL)
			}
			if product.ProductID == "" || product.Description == "" {
				return catalog{}, fmt.Errorf("%s product needs an ID or URL and description", aisle.Name)
			}
			if product.Kind != "food" && product.Kind != "wine" {
				return catalog{}, fmt.Errorf("product %q has invalid kind %q", product.ProductID, product.Kind)
			}
			product.Categories = appendCategory(product.Categories, aisle.Name)
			if previous, ok := products[product.ProductID]; ok {
				if previous.Kind != product.Kind {
					return catalog{}, fmt.Errorf("product %q has conflicting food/wine classifications", product.ProductID)
				}
				for _, category := range product.Categories {
					previous.Categories = appendCategory(previous.Categories, category)
				}
				products[product.ProductID] = previous
				continue
			}
			ids = append(ids, product.ProductID)
			products[product.ProductID] = product
		}
	}
	for _, id := range ids {
		product := products[id]
		if product.Kind == "wine" {
			result.wines = append(result.wines, product.InputIngredient)
		} else {
			result.staples = append(result.staples, product.InputIngredient)
		}
	}
	if len(result.staples) == 0 || len(result.wines) == 0 {
		return catalog{}, fmt.Errorf("snapshot must include both food and wine products")
	}
	return result, nil
}

func validateAisles(aisles []snapshotAisle) error {
	if len(aisles) != len(sourceAisles) {
		return fmt.Errorf("snapshot needs all %d source aisles; got %d", len(sourceAisles), len(aisles))
	}
	seen := make(map[string]bool)
	for _, aisle := range aisles {
		matched := false
		for _, source := range sourceAisles {
			if aisle.Name == source.name && aisle.URL == baseURL+source.path {
				matched = true
				break
			}
		}
		if !matched || seen[aisle.Name] {
			return fmt.Errorf("unexpected or duplicate source aisle %q (%s)", aisle.Name, aisle.URL)
		}
		if len(aisle.Products) == 0 {
			return fmt.Errorf("source aisle %q has no captured products", aisle.Name)
		}
		seen[aisle.Name] = true
	}
	return nil
}

func appendCategory(categories []string, category string) []string {
	for _, existing := range categories {
		if existing == category {
			return categories
		}
	}
	return append(categories, category)
}
