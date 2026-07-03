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

	"github.com/samber/lo"
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

type store struct {
	cache cache.ListCache
}

type Market struct {
	ID    string   `json:"id"`
	Names []string `json:"names"`
	geo.Coordinate
	ZipCode    string    `json:"zip_code"`
	PhotoCount int       `json:"photo_count"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type inventoryRecord struct {
	Ingredients []ai.InputIngredient `json:"ingredients"`
}

func NewStore(c cache.ListCache) *store {
	return &store{cache: c}
}

func NewContainerStore() (*store, error) {
	cacheStore, err := cache.EnsureCache(Container)
	if err != nil {
		return nil, fmt.Errorf("create farmers market cache: %w", err)
	}
	return NewStore(cacheStore), nil
}

func (s *store) freshInventory(ctx context.Context, locationID string) ([]ai.InputIngredient, error) {
	if !isID(locationID) {
		return nil, fmt.Errorf("invalid farmers market location id %q", locationID)
	}

	market, err := s.loadMarket(ctx, locationID)
	if err != nil {
		return nil, err
	}
	date := farmersMarketDate(time.Now(), market.ZipCode)
	return s.loadInventoryByDate(ctx, locationID, date)
}

func (m Market) Location() locationtypes.Location {
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
		Lat:      &m.Lat,
		Lon:      &m.Lon,
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

func (s *store) findNearbyMarket(ctx context.Context, lat, lon float64) (*Market, error) {
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
func (s *store) listMarkets(ctx context.Context) ([]Market, error) {
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

func (s *store) loadMarket(ctx context.Context, locationID string) (Market, error) {
	if !isID(locationID) {
		return Market{}, fmt.Errorf("invalid farmers market location id %q", locationID)
	}
	return s.loadMarketByKey(ctx, locationKey(locationID))
}

func (s *store) loadMarketByKey(ctx context.Context, key string) (Market, error) {
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

func (s *store) saveMarket(ctx context.Context, market Market) error {
	raw, err := json.Marshal(market)
	if err != nil {
		return fmt.Errorf("marshal farmers market: %w", err)
	}
	if err := s.cache.Put(ctx, locationKey(market.ID), string(raw), cache.Unconditional()); err != nil {
		return fmt.Errorf("save farmers market: %w", err)
	}
	return nil
}

func (s *store) mergeInventory(ctx context.Context, locationID string, date time.Time, ingredients []ai.InputIngredient) error {
	existing, err := s.loadInventoryByDate(ctx, locationID, date)
	if err != nil && !errors.Is(err, cache.ErrNotFound) {
		return fmt.Errorf("load farmers market inventory: %w", err)
	}

	all := append(existing, ingredients...)
	merged := lo.UniqBy(all, func(i ai.InputIngredient) string {
		return i.ProductID
	})

	raw, err := json.Marshal(inventoryRecord{
		Ingredients: merged,
	})
	if err != nil {
		return fmt.Errorf("marshal farmers market inventory: %w", err)
	}
	if err := s.cache.Put(ctx, inventoryKey(locationID, date), string(raw), cache.Unconditional()); err != nil {
		return fmt.Errorf("save farmers market inventory: %w", err)
	}
	return nil
}

func (s *store) loadInventoryByDate(ctx context.Context, locationID string, date time.Time) ([]ai.InputIngredient, error) {
	reader, err := s.cache.Get(ctx, inventoryKey(locationID, date))
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := reader.Close(); closeErr != nil {
			slog.ErrorContext(ctx, "failed to close farmers market inventory", "key", inventoryKey(locationID, date), "error", closeErr)
		}
	}()

	var record inventoryRecord
	if err := json.NewDecoder(reader).Decode(&record); err == nil && len(record.Ingredients) > 0 {
		return record.Ingredients, nil
	}
	return nil, fmt.Errorf("decode farmers market inventory")
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
