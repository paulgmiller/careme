package farmersmarket

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"log/slog"
	"math"
	"slices"
	"strings"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/locations/geo"
	locationtypes "careme/internal/locations/types"
)

const (
	Container        = "farmersmarket"
	LocationIDPrefix = "farmersmarket_"
	ChainName        = "Farmers Market"

	locationPrefix  = "locations/"
	inventoryPrefix = "inventory/"
	mergeRadiusMI   = 0.5
	signature       = "farmersmarket-staples-v1"
)

type identityProvider struct{}

type Store struct {
	cache cache.ListCache
}

type Market struct {
	ID         string    `json:"id"`
	Names      []string  `json:"names"`
	Lat        float64   `json:"lat"`
	Lon        float64   `json:"lon"`
	ZipCode    string    `json:"zip_code"`
	PhotoCount int       `json:"photo_count"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type inventoryRecord struct {
	CachedAt    time.Time            `json:"cached_at"`
	Ingredients []ai.InputIngredient `json:"ingredients"`
}

func NewStore(c cache.ListCache) *Store {
	return &Store{cache: c}
}

func NewContainerStore() (*Store, error) {
	cacheStore, err := cache.EnsureCache(Container)
	if err != nil {
		return nil, fmt.Errorf("create farmers market cache: %w", err)
	}
	return NewStore(cacheStore), nil
}

func NewIdentityProvider() identityProvider {
	return identityProvider{}
}

func (s identityProvider) IsID(locationID string) bool {
	return isID(locationID)
}

func (s identityProvider) Signature() string {
	return signature
}

func (s *Store) freshInventory(ctx context.Context, locationID string) (*inventoryRecord, error) {
	if !isID(locationID) {
		return nil, fmt.Errorf("invalid farmers market location id %q", locationID)
	}
	if s.cache == nil {
		return nil, cache.ErrNotFound
	}

	market, err := s.loadMarket(ctx, locationID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	date := farmersMarketDate(now, market.ZipCode)
	record, err := s.loadInventoryByKey(ctx, inventoryKey(locationID, date))
	if err != nil {
		return nil, err
	}
	if record.CachedAt.Before(now.Add(-24 * time.Hour)) {
		return nil, cache.ErrNotFound
	}
	return &record, nil
}

func (m Market) Location() locationtypes.Location {
	lat := m.Lat
	lon := m.Lon
	name := "Farmers Market"
	if len(m.Names) > 0 {
		name = m.Names[0]
	}
	address := "Farmers market"
	if m.ZipCode != "" {
		address = "Farmers market near ZIP " + m.ZipCode
	}
	return locationtypes.Location{
		ID:       m.ID,
		Name:     name,
		Address:  address,
		ZipCode:  m.ZipCode,
		Lat:      &lat,
		Lon:      &lon,
		CachedAt: m.UpdatedAt,
		Chain:    ChainName,
	}
}

func (m *Market) merge(name string, lat, lon float64, photoCount int, now time.Time) {
	total := m.PhotoCount + photoCount
	if total > 0 {
		m.Lat = ((m.Lat * float64(m.PhotoCount)) + (lat * float64(photoCount))) / float64(total)
		m.Lon = ((m.Lon * float64(m.PhotoCount)) + (lon * float64(photoCount))) / float64(total)
		m.PhotoCount = total
	}
	if !slices.ContainsFunc(m.Names, func(existing string) bool {
		return strings.EqualFold(strings.TrimSpace(existing), name)
	}) {
		m.Names = append(m.Names, name)
	}
	m.UpdatedAt = now
}

func (s *Store) findNearbyMarket(ctx context.Context, lat, lon float64) (*Market, error) {
	markets, err := s.listMarkets(ctx)
	if err != nil {
		return nil, err
	}
	var nearest *Market
	nearestDistance := math.MaxFloat64
	for i := range markets {
		market := markets[i]
		distance := geo.HaversineMiles(lat, lon, market.Lat, market.Lon)
		if distance > mergeRadiusMI || distance >= nearestDistance {
			continue
		}
		nearest = &market
		nearestDistance = distance
	}
	return nearest, nil
}

// this won't scale long term but maybe doesn't matter if we only ever have a dozen markets?
func (s *Store) listMarkets(ctx context.Context) ([]Market, error) {
	keys, err := s.cache.List(ctx, locationPrefix, "")
	if err != nil {
		return nil, fmt.Errorf("list farmers markets: %w", err)
	}
	markets := make([]Market, 0, len(keys))
	for _, key := range keys {
		market, err := s.loadMarketByKey(ctx, locationPrefix+key)
		if err != nil {
			slog.WarnContext(ctx, "failed to load farmers market", "key", key, "error", err)
			continue
		}
		markets = append(markets, market)
	}
	return markets, nil
}

func (s *Store) loadMarket(ctx context.Context, locationID string) (Market, error) {
	if !isID(locationID) {
		return Market{}, fmt.Errorf("invalid farmers market location id %q", locationID)
	}
	return s.loadMarketByKey(ctx, locationKey(locationID))
}

func (s *Store) loadMarketByKey(ctx context.Context, key string) (Market, error) {
	reader, err := s.cache.Get(ctx, key)
	if err != nil {
		return Market{}, err
	}
	defer func() {
		_ = reader.Close()
	}()
	var market Market
	if err := json.NewDecoder(reader).Decode(&market); err != nil {
		return Market{}, fmt.Errorf("decode farmers market: %w", err)
	}
	return market, nil
}

func (s *Store) saveMarket(ctx context.Context, market Market) error {
	raw, err := json.Marshal(market)
	if err != nil {
		return fmt.Errorf("marshal farmers market: %w", err)
	}
	if err := s.cache.Put(ctx, locationKey(market.ID), string(raw), cache.Unconditional()); err != nil {
		return fmt.Errorf("save farmers market: %w", err)
	}
	return nil
}

func (s *Store) mergeInventory(ctx context.Context, locationID string, date time.Time, ingredients []ai.InputIngredient) ([]ai.InputIngredient, error) {
	existing, err := s.loadInventoryByDate(ctx, locationID, date)
	if err != nil && !errors.Is(err, cache.ErrNotFound) {
		return nil, fmt.Errorf("load farmers market inventory: %w", err)
	}

	merged := dedupeIngredients(append(existing, ingredients...))
	raw, err := json.Marshal(inventoryRecord{
		CachedAt:    time.Now().UTC(),
		Ingredients: merged,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal farmers market inventory: %w", err)
	}
	if err := s.cache.Put(ctx, inventoryKey(locationID, date), string(raw), cache.Unconditional()); err != nil {
		return nil, fmt.Errorf("save farmers market inventory: %w", err)
	}
	return merged, nil
}

func (s *Store) loadInventoryByDate(ctx context.Context, locationID string, date time.Time) ([]ai.InputIngredient, error) {
	record, err := s.loadInventoryByKey(ctx, inventoryKey(locationID, date))
	if err != nil {
		return nil, err
	}
	return record.Ingredients, nil
}

func (s *Store) loadInventoryByKey(ctx context.Context, key string) (inventoryRecord, error) {
	reader, err := s.cache.Get(ctx, key)
	if err != nil {
		return inventoryRecord{}, err
	}
	defer func() {
		if closeErr := reader.Close(); closeErr != nil {
			slog.ErrorContext(ctx, "failed to close farmers market inventory", "key", key, "error", closeErr)
		}
	}()

	var record inventoryRecord
	if err := json.NewDecoder(reader).Decode(&record); err == nil && len(record.Ingredients) > 0 {
		return record, nil
	}
	return inventoryRecord{}, fmt.Errorf("decode farmers market inventory")
}

func dedupeIngredients(ingredients []ai.InputIngredient) []ai.InputIngredient {
	seen := make(map[string]struct{}, len(ingredients))
	deduped := make([]ai.InputIngredient, 0, len(ingredients))
	for _, ingredient := range ingredients {
		ingredient = ai.NormalizeInputIngredient(ingredient)
		if ingredient.Description == "" {
			continue
		}
		if ingredient.ProductID == "" {
			ingredient.ProductID = "farmersmarket_item_" + ingredient.Hash()
		}
		key := strings.ToLower(ingredient.Description) + "\x00" + strings.ToLower(ingredient.Size)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		deduped = append(deduped, ingredient)
	}
	return deduped
}

func locationKey(locationID string) string {
	return locationPrefix + locationID + ".json"
}

func inventoryKey(locationID string, date time.Time) string {
	return inventoryPrefix + locationID + "/" + date.Format("2006-01-02") + ".json"
}

func marketID(name string, lat, lon float64) string {
	h := fnv.New64a()
	_, _ = io.WriteString(h, strings.ToLower(strings.TrimSpace(name)))
	_, _ = io.WriteString(h, fmt.Sprintf("|%.4f|%.4f", lat, lon))
	return LocationIDPrefix + base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

func normalizeName(name string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(name)), " ")
}

func validCoordinate(lat, lon float64) bool {
	return lat >= -90 && lat <= 90 && lon >= -180 && lon <= 180 && !(lat == 0 && lon == 0)
}

func isID(locationID string) bool {
	return strings.HasPrefix(locationID, LocationIDPrefix) && strings.TrimPrefix(locationID, LocationIDPrefix) != ""
}
