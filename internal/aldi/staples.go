package aldi

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"careme/internal/ai"
	"careme/internal/aldi/query"
	"careme/internal/cache"
	"careme/internal/parallelism"

	"github.com/samber/lo"
)

const (
	defaultStapleTake = 48
	bigStapleTake     = 100
	produceStapleTake = 250
)

var defaultStaplesSignature = lo.Must(json.Marshal(StapleCategories()))

type StapleCategory struct {
	Name  string `json:"name"`
	Slug  string `json:"slug"`
	Limit int    `json:"limit"`
}

type productClient interface {
	Products(ctx context.Context, storeID, postalCode, categorySlug string, opts query.SearchOptions) ([]query.Item, error)
}

type identityProvider struct{}

type StaplesProvider struct {
	identityProvider
	client productClient
	cache  cache.Cache
}

func NewIdentityProvider() identityProvider {
	return identityProvider{}
}

func NewStaplesProvider(httpClient *http.Client) (StaplesProvider, error) {
	cacheStore, err := cache.EnsureCache(Container)
	if err != nil {
		return StaplesProvider{}, fmt.Errorf("create ALDI staples cache: %w", err)
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 20 * time.Second}
	}
	return newStaplesProviderWithCache(query.NewClient(query.ClientConfig{
		HTTPClient: httpClient,
	}), cacheStore), nil
}

func newStaplesProviderWithCache(client productClient, cacheStore cache.Cache) StaplesProvider {
	return StaplesProvider{
		client: client,
		cache:  cacheStore,
	}
}

func (p identityProvider) Signature() string {
	return string(defaultStaplesSignature)
}

func (p identityProvider) IsID(locationID string) bool {
	return IsID(locationID)
}

func (p StaplesProvider) FetchStaples(ctx context.Context, locationID string) ([]ai.InputIngredient, error) {
	summary, err := p.storeSummary(ctx, locationID)
	if err != nil {
		return nil, err
	}
	if p.client == nil {
		return nil, fmt.Errorf("ALDI client is required")
	}

	storeID := strings.TrimSpace(summary.InstoreShopID)
	postalCode := strings.TrimSpace(summary.ZipCode)
	return parallelism.Flatten(StapleCategories(), func(category StapleCategory) ([]ai.InputIngredient, error) {
		items, err := p.client.Products(ctx, storeID, postalCode, category.Slug, query.SearchOptions{
			First: category.Limit,
		})
		if err != nil {
			slog.WarnContext(ctx, "failed to fetch ALDI category", "category", category.Name, "location", locationID, "error", err)
			return nil, err
		}

		ingredients := lo.Map(items, func(item query.Item, _ int) ai.InputIngredient {
			return itemToIngredient(item, category)
		})
		slog.InfoContext(ctx, "found ALDI staples for category", "count", len(ingredients), "category", category.Name, "location", locationID)
		return ingredients, nil
	})
}

func (p StaplesProvider) FetchWines(_ context.Context, locationID string, _ []string) ([]ai.InputIngredient, error) {
	return nil, fmt.Errorf("wine lookup is not supported for location %q", locationID)
}

func (p StaplesProvider) storeSummary(ctx context.Context, locationID string) (StoreSummary, error) {
	locationID = strings.TrimSpace(locationID)
	if !IsID(locationID) {
		return StoreSummary{}, fmt.Errorf("ALDI location id %q is invalid", locationID)
	}
	if p.cache == nil {
		return StoreSummary{}, fmt.Errorf("ALDI cache is required")
	}

	reader, err := p.cache.Get(ctx, StoreCachePrefix+locationID)
	if err != nil {
		return StoreSummary{}, fmt.Errorf("load ALDI store summary for %q: %w", locationID, err)
	}
	defer func() {
		_ = reader.Close()
	}()

	var summary StoreSummary
	if err := json.NewDecoder(reader).Decode(&summary); err != nil {
		return StoreSummary{}, fmt.Errorf("decode ALDI store summary for %q: %w", locationID, err)
	}
	if strings.TrimSpace(summary.InstoreShopID) == "" {
		return StoreSummary{}, fmt.Errorf("ALDI location %q has no instore shop id", locationID)
	}
	return summary, nil
}

func StapleCategories() []StapleCategory {
	return []StapleCategory{
		{Name: "beef", Slug: "n-beef-67693", Limit: bigStapleTake},
		{Name: "chicken", Slug: "n-chicken-81381", Limit: bigStapleTake},
		{Name: "fruit", Slug: "n-fresh-fruits-35754", Limit: produceStapleTake},
		{Name: "vegetables", Slug: "n-fresh-vegetables-9190", Limit: produceStapleTake},
		{Name: "pork", Slug: "n-pork-99214", Limit: bigStapleTake},
		{Name: "fish", Slug: "n-fish-33891", Limit: bigStapleTake},
		{Name: "shellfish", Slug: "n-shellfish-45452", Limit: defaultStapleTake},
		{Name: "lamb", Slug: "n-lamb-91217", Limit: defaultStapleTake},
		{Name: "pasta and dry goods", Slug: "n-dry-goods-pasta-19255", Limit: bigStapleTake},
	}
}

func itemToIngredient(item query.Item, category StapleCategory) ai.InputIngredient {
	productID := strings.TrimSpace(item.ProductID)
	if productID == "" {
		productID = strings.TrimSpace(item.ID)
	}

	return ai.NormalizeInputIngredient(ai.InputIngredient{
		ProductID:    productID,
		AisleNumber:  category.Name,
		Brand:        item.BrandName,
		Description:  item.Name,
		Size:         itemSize(item),
		PriceRegular: itemPrice(item),
		PriceSale:    itemPrice(item),
		Categories:   []string{category.Name},
	})
}

func itemSize(item query.Item) string {
	if size := strings.TrimSpace(item.Size); size != "" {
		return size
	}
	return strings.TrimSpace(item.Price.ParWeightTotalEstimate.ViewSection.ParWeightString)
}

func itemPrice(item query.Item) *float32 {
	for _, value := range []string{
		item.Price.ViewSection.PriceValueString,
		item.Price.ViewSection.ItemCard.PriceString,
		item.Price.ViewSection.PriceString,
		item.Price.ViewSection.ItemCard.FullPriceString,
	} {
		if price := priceFromString(value); price != nil {
			return price
		}
	}
	return nil
}

var pricePattern = regexp.MustCompile(`(?i)(?:(\d+)\s*(?:for|/)\s*)?\$?([0-9]+(?:\.[0-9]{1,2})?)`)

func priceFromString(value string) *float32 {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}

	matches := pricePattern.FindStringSubmatch(value)
	if len(matches) == 0 {
		return nil
	}

	price, err := strconv.ParseFloat(matches[2], 32)
	if err != nil || price <= 0 {
		return nil
	}
	if matches[1] != "" {
		count, err := strconv.ParseFloat(matches[1], 32)
		if err != nil || count <= 0 {
			return nil
		}
		price = price / count
	}

	out := float32(price)
	return &out
}
