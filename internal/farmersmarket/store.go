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
	"careme/internal/locations/nearby"
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

type ZipFinder interface {
	NearestZIPToCoordinates(lat, lon float64) (string, bool)
}

type ZipCentroidLookup interface {
	ZipCentroidByZIP(zip string) (locationtypes.ZipCentroid, bool)
}

type identityProvider struct{}

type Store struct {
	identityProvider
	cache cache.ListCache
}

type LocationBackend struct {
	identityProvider
	store     *Store
	zipLookup ZipCentroidLookup
}

type Uploader struct {
	store     *Store
	zipFinder ZipFinder
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

func NewLocationBackend(store *Store, zipLookup ZipCentroidLookup) *LocationBackend {
	return &LocationBackend{store: store, zipLookup: zipLookup}
}

func NewContainerLocationBackend(zipLookup ZipCentroidLookup) (*LocationBackend, error) {
	store, err := NewContainerStore()
	if err != nil {
		return nil, err
	}
	return NewLocationBackend(store, zipLookup), nil
}

func NewUploader(store *Store, zipFinder ZipFinder) *Uploader {
	return &Uploader{store: store, zipFinder: zipFinder}
}

func NewContainerUploader(zipFinder ZipFinder) (*Uploader, error) {
	store, err := NewContainerStore()
	if err != nil {
		return nil, err
	}
	return NewUploader(store, zipFinder), nil
}

func NewStaplesProvider() (*Store, error) {
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
	return strings.HasPrefix(locationID, LocationIDPrefix) && strings.TrimPrefix(locationID, LocationIDPrefix) != ""
}

func (s identityProvider) Signature() string {
	return signature
}

func (s *Store) HasInventory(locationID string) bool {
	if s == nil || s.cache == nil {
		return false
	}
	_, err := s.freshInventory(context.Background(), locationID)
	return err == nil
}

func (b *LocationBackend) HasInventory(locationID string) bool {
	return b != nil && b.store != nil && b.store.HasInventory(locationID)
}

func (b *LocationBackend) GetLocationByID(ctx context.Context, locationID string) (*locationtypes.Location, error) {
	market, err := b.store.loadMarket(ctx, locationID)
	if err != nil {
		return nil, err
	}
	loc := market.Location()
	return &loc, nil
}

func (b *LocationBackend) GetLocationsByZip(ctx context.Context, zipcode string) ([]locationtypes.Location, error) {
	if b == nil || b.store == nil {
		return nil, cache.ErrNotFound
	}
	if b.zipLookup == nil {
		return nil, fmt.Errorf("zip lookup is required")
	}

	markets, err := b.store.listMarkets(ctx)
	if err != nil {
		return nil, err
	}

	locations := make([]locationtypes.Location, 0, len(markets))
	for _, market := range markets {
		locations = append(locations, market.Location())
	}
	return nearby.FilterAndSortByZip(ctx, b.zipLookup, zipcode, locations, nearby.MaxLocationDistanceMiles), nil
}

func (s *Store) FetchStaples(ctx context.Context, locationID string) ([]ai.InputIngredient, error) {
	record, err := s.freshInventory(ctx, locationID)
	if err != nil {
		return nil, err
	}
	return record.Ingredients, nil
}

func (s *Store) freshInventory(ctx context.Context, locationID string) (*inventoryRecord, error) {
	if !s.IsID(locationID) {
		return nil, fmt.Errorf("invalid farmers market location id %q", locationID)
	}
	if s.cache == nil {
		return nil, cache.ErrNotFound
	}

	// this will get expensive. Need to just take first 1/2 (assuming they are odered ) or try gets on last couple of days.
	keys, err := s.cache.List(ctx, inventoryPrefix+locationID+"/", "")
	if err != nil {
		return nil, fmt.Errorf("list farmers market inventory: %w", err)
	}

	cutoff := time.Now().Add(-24 * time.Hour)
	var newest *inventoryRecord
	for _, key := range keys {
		record, err := s.loadInventoryByKey(ctx, inventoryPrefix+locationID+"/"+key)
		if err != nil {
			slog.WarnContext(ctx, "failed to load farmers market inventory", "location", locationID, "key", key, "error", err)
			continue
		}
		if record.CachedAt.Before(cutoff) {
			continue
		}
		if newest == nil || record.CachedAt.After(newest.CachedAt) {
			recordCopy := record
			newest = &recordCopy
		}
	}

	if newest == nil {
		return nil, cache.ErrNotFound
	}
	return newest, nil
}

func (s *Store) FetchWines(context.Context, string, []string) ([]ai.InputIngredient, error) {
	return nil, nil
}

func (u *Uploader) SaveUpload(ctx context.Context, name string, lat, lon float64, photoCount int, date time.Time, ingredients []ai.InputIngredient) (*Market, []ai.InputIngredient, error) {
	if u == nil || u.store == nil || u.store.cache == nil {
		return nil, nil, fmt.Errorf("cache is required")
	}
	if u.zipFinder == nil {
		return nil, nil, fmt.Errorf("zip finder is required")
	}
	if photoCount <= 0 {
		return nil, nil, fmt.Errorf("at least one geotagged photo is required")
	}
	name = normalizeName(name)
	if name == "" {
		return nil, nil, fmt.Errorf("market name is required")
	}
	if !validCoordinate(lat, lon) {
		return nil, nil, fmt.Errorf("invalid market coordinates")
	}

	market, err := u.store.findNearbyMarket(ctx, lat, lon)
	if err != nil {
		return nil, nil, err
	}

	now := time.Now().UTC()
	if market == nil {
		zip, _ := u.zipFinder.NearestZIPToCoordinates(lat, lon)
		market = &Market{
			ID:         marketID(name, lat, lon),
			Names:      []string{name},
			Lat:        lat,
			Lon:        lon,
			ZipCode:    zip,
			PhotoCount: photoCount,
			CreatedAt:  now,
			UpdatedAt:  now,
		}
	} else {
		market.merge(name, lat, lon, photoCount, now)
		if market.ZipCode == "" {
			market.ZipCode, _ = u.zipFinder.NearestZIPToCoordinates(market.Lat, market.Lon)
		}
	}

	if err := u.store.saveMarket(ctx, *market); err != nil {
		return nil, nil, err
	}

	merged, err := u.store.mergeInventory(ctx, market.ID, date, ingredients)
	if err != nil {
		return nil, nil, err
	}
	return market, merged, nil
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
	if !s.IsID(locationID) {
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
