// Package smithbrothersfarms serves Smith Brothers Farms' public delivery catalog.
package smithbrothersfarms

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"

	"careme/internal/ai"
)

const (
	LocationIDPrefix = "smithbrothersfarms_"
	baseURL          = "https://www.smithbrothersfarms.com"
	parserVersion    = "v1"
)

var stapleURLs = []string{baseURL + "/produce", baseURL + "/meat-poultry"}

var harvestBoxes = []struct {
	slug string
	name string
}{
	{"smith-brothers-organic-harvest-box", "Organic Produce Box"},
	{"harvest-produce-box", "Produce Box"},
}

type identityProvider struct{}

func NewIdentityProvider() identityProvider { return identityProvider{} }

func (identityProvider) IsID(id string) bool { return strings.HasPrefix(id, LocationIDPrefix) }

func (identityProvider) Signature() string {
	parts := append([]string{}, stapleURLs...)
	for _, box := range harvestBoxes {
		parts = append(parts, baseURL+"/"+box.slug)
	}
	parts = append(parts, parserVersion)
	return fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(parts, "\n"))))
}

type ingredientClient interface {
	Fetch(context.Context, string) ([]ai.InputIngredient, error)
	FetchHarvestBox(context.Context, string, string) ([]ai.InputIngredient, error)
}

type StaplesProvider struct {
	identityProvider
	client ingredientClient
}

func NewStaplesProvider(client ingredientClient) StaplesProvider {
	return StaplesProvider{client: client}
}

func (p StaplesProvider) FetchStaples(ctx context.Context, id string) ([]ai.InputIngredient, error) {
	if !p.IsID(id) {
		return nil, fmt.Errorf("invalid Smith Brothers Farms location ID %q", id)
	}
	var ingredients []ai.InputIngredient
	for _, url := range stapleURLs {
		items, err := p.client.Fetch(ctx, url)
		if err != nil {
			return nil, fmt.Errorf("fetch Smith Brothers Farms staples from %s: %w", url, err)
		}
		ingredients = append(ingredients, items...)
	}
	for _, box := range harvestBoxes {
		url := baseURL + "/" + box.slug
		items, err := p.client.FetchHarvestBox(ctx, url, box.name)
		if err != nil {
			return nil, fmt.Errorf("fetch Smith Brothers Farms harvest box from %s: %w", url, err)
		}
		ingredients = append(ingredients, items...)
	}
	return ingredients, nil
}

func (p StaplesProvider) FetchWines(_ context.Context, id string, _ []string) ([]ai.InputIngredient, error) {
	if !p.IsID(id) {
		return nil, fmt.Errorf("invalid Smith Brothers Farms location ID %q", id)
	}
	return nil, fmt.Errorf("wine lookup is not supported for location %q", id)
}
