// Package tcfarm provides the frozen September 21–26 partner demo catalog.
package tcfarm

import (
	"context"
	"fmt"
	"strings"

	"careme/internal/ai"
	"careme/internal/locations/geo"
	locationtypes "careme/internal/locations/types"
)

const LocationID = "tcfarm_september21"

// Provider serves the same inventory regardless of the requested recipe date.
type Provider struct{}

func (Provider) IsID(id string) bool           { return id == LocationID }
func (Provider) Signature() string             { return "tcfarm-september21-26-v1" }
func (p Provider) HasInventory(id string) bool { return p.IsID(id) }

func (p Provider) GetLocationByID(_ context.Context, id string) (*locationtypes.Location, error) {
	if !p.IsID(id) {
		return nil, fmt.Errorf("unknown TC Farm demo location %q", id)
	}
	return &locationtypes.Location{
		ID: LocationID, Name: "TC Farm · September 21–26", Chain: "TC Farm",
		State: "MN", ZipCode: "55401",
		// Downtown Minneapolis ZIP centroid represents the Twin Cities metro.
		// This is a demo location, not a retail storefront or pickup address.
		Lat: new(44.985367), Lon: new(-93.270208),
	}, nil
}

// This one-time demo is accessed by its direct link, not nearby store search.
func (Provider) GetLocationsByCoordinates(context.Context, geo.Coordinate) ([]locationtypes.Location, error) {
	return nil, nil
}

func (p Provider) FetchStaples(_ context.Context, id string) ([]ai.InputIngredient, error) {
	if !p.IsID(id) {
		return nil, fmt.Errorf("unknown TC Farm demo location %q", id)
	}
	return catalog(), nil
}

func (p Provider) FetchWines(_ context.Context, id string, _ []string) ([]ai.InputIngredient, error) {
	if !p.IsID(id) {
		return nil, fmt.Errorf("unknown TC Farm demo location %q", id)
	}
	return []ai.InputIngredient{recommendedWine()}, nil
}

// Produce is deduplicated across shares; categories retain share membership.
// No prices, quantities, or unlisted pantry items are inferred from the photos.
func catalog() []ai.InputIngredient {
	type share struct {
		name  string
		items []string
	}
	shares := []share{
		{"Seasonal", []string{"Butternut Squash", "Easter Egg Radish", "Green Bell Peppers", "Russet Potatoes", "Red Cipollini Onions", "Yellow Grape Tomatoes", "Cucumbers"}},
		{"Small Seasonal", []string{"Delicata Squash", "Easter Egg Radish", "Green Bell Peppers", "Russet Potatoes", "Cucumbers"}},
		{"Low Carb", []string{"Cherry Tomatoes", "Yellow Bell Peppers", "Dino Kale", "Eggplant", "Cucumbers", "Green Top Radish"}},
		{"Staple", []string{"Purple Bell Peppers", "Leeks", "Red Potatoes", "Red Cabbage", "Celery", "Collards"}},
		{"Fruit", []string{"Kiwi Berry", "Mango", "Red Bartlett Pears", "Cantaloupe", "Pie Apples (2nds)"}},
		{"Small Fruit", []string{"Mango", "Red Bartlett Pears", "Cantaloupe", "Pie Apples (2nds)"}},
		{"Fruit substitution", []string{"Blueberries"}},
	}
	var ingredients []ai.InputIngredient
	indexes := map[string]int{}
	for _, share := range shares {
		for _, name := range share.items {
			if i, ok := indexes[name]; ok {
				ingredients[i].Categories = append(ingredients[i].Categories, share.name)
				continue
			}
			indexes[name] = len(ingredients)
			ingredients = append(ingredients, ingredient(name, "Produce", share.name))
		}
	}
	for _, name := range []string{"TC Farm Pork Tenderloin", "TC Farm Bratwurst", "TC Farm Italian Sausage", "TC Farm Chicken Thighs – Ranger"} {
		ingredients = append(ingredients, ingredient(name, "Meat", "Recommended add-on"))
	}
	ingredients = append(ingredients, ingredient("Sour Cream", "Dairy", "Recommended add-on"), recommendedWine())
	return ingredients
}

func recommendedWine() ai.InputIngredient {
	return ingredient("Oddbird GSM NA Red Wine (dealcoholized)", "Nonalcoholic wine", "Recommended add-on")
}

func ingredient(name string, categories ...string) ai.InputIngredient {
	return ai.InputIngredient{
		ProductID:   LocationID + "_" + strings.ToLower(strings.ReplaceAll(name, " ", "-")),
		Description: name, Categories: categories,
	}
}
