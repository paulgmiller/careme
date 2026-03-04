package recipes

import (
	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/kroger"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"sync"

	"github.com/samber/lo"
	"github.com/samber/lo/mutable"
)

type krogerIngredientProvider struct {
	krogerClient kroger.ClientWithResponsesInterface
	cache        cache.Cache
}

func NewKrogerIngredientProvider(cfg *config.Config, c cache.Cache) (ingredientSource, error) {
	client, err := kroger.FromConfig(cfg)
	if err != nil {
		return nil, err
	}
	return &krogerIngredientProvider{
		krogerClient: client,
		cache:        c,
	}, nil
}

// calls get ingredients for a number of "staples" basically fresh produce and vegatbles.
// tries to filter to no brand or certain brands to avoid shelved products
func (k *krogerIngredientProvider) GetStaples(ctx context.Context, p *generatorParams) ([]ai.InputIngredient, error) {
	lochash := p.LocationHash()
	var ingredients []ai.InputIngredient
	rio := IO(k.cache)

	if cachedIngredients, err := rio.IngredientsFromCache(ctx, lochash); err == nil {
		slog.Info("serving cached ingredients", "location", p.String(), "hash", lochash, "count", len(cachedIngredients))
		return cachedIngredients, nil
	} else if !errors.Is(err, cache.ErrNotFound) {
		slog.ErrorContext(ctx, "failed to read cached ingredients", "location", p.String(), "error", err)
	}

	var wg sync.WaitGroup
	var lock sync.Mutex
	wg.Add(len(p.Staples))
	for _, category := range p.Staples {
		go func(category filter) {
			defer wg.Done()
			categoryIngredients, err := k.GetIngredients(ctx, p.Location.ID, category, 0)
			if err != nil {
				slog.ErrorContext(ctx, "failed to get ingredients", "category", category.Term, "location", p.Location.ID, "error", err)
				return
			}
			lock.Lock()
			defer lock.Unlock()
			ingredients = append(ingredients, categoryIngredients...)
			ingredients = lo.UniqBy(ingredients, func(i ai.InputIngredient) string { return toStr(i.Description) })
			slog.InfoContext(ctx, "Found ingredients for category", "count", len(categoryIngredients), "category", category.Term, "location", p.Location.ID, "runningtotal", len(ingredients))
		}(category)
	}

	wg.Wait()

	mutable.Shuffle(ingredients)

	if err := rio.SaveIngredients(ctx, lochash, ingredients); err != nil {
		slog.ErrorContext(ctx, "failed to cache ingredients", "location", p.String(), "error", err)
		return nil, err
	}
	return ingredients, nil
}

func (k *krogerIngredientProvider) GetIngredients(ctx context.Context, location string, f filter, skip int) ([]ai.InputIngredient, error) {
	limit := 50
	limitStr := strconv.Itoa(limit)
	startStr := strconv.Itoa(skip)
	products, err := k.krogerClient.ProductSearchWithResponse(ctx, &kroger.ProductSearchParams{
		FilterLocationId: &location,
		FilterTerm:       &f.Term,
		FilterLimit:      &limitStr,
		FilterStart:      &startStr,
	})
	if err != nil {
		return nil, fmt.Errorf("kroger product search request failed: %w", err)
	}
	if products.StatusCode() != http.StatusOK {
		output, _ := json.Marshal(products.JSON500)
		return nil, fmt.Errorf("got %d code from kroger : %s", products.StatusCode(), string(output))
	}

	var ingredients []ai.InputIngredient

	for _, product := range *products.JSON200.Data {
		wildcard := len(f.Brands) > 0 && f.Brands[0] == "*"

		if product.Brand != nil && !slices.Contains(f.Brands, toStr(product.Brand)) && !wildcard {
			continue
		}
		if slices.Contains(*product.Categories, "Frozen") && !f.Frozen {
			continue
		}
		for _, item := range *product.Items {
			if item.Price == nil {
				continue
			}

			var aisle *string
			if product.AisleLocations != nil && len(*product.AisleLocations) > 0 {
				aisle = (*product.AisleLocations)[0].Number
			}

			ingredient := ai.InputIngredient{
				ProductID:    product.ProductId,
				Brand:        product.Brand,
				Description:  product.Description,
				Size:         item.Size,
				PriceRegular: item.Price.Regular,
				PriceSale:    item.Price.Promo,
				AisleNumber:  aisle,
			}

			ingredients = append(ingredients, ingredient)
		}
	}

	if len(*products.JSON200.Data) == limit && skip < 250 {
		page, err := k.GetIngredients(ctx, location, f, skip+limit)
		if err != nil {
			return ingredients, nil
		}
		ingredients = append(ingredients, page...)
	}

	return ingredients, nil
}

// toStr returns the string value if non-nil, or "empty" otherwise.
func toStr(s *string) string {
	if s == nil {
		return "empty"
	}
	return *s
}
