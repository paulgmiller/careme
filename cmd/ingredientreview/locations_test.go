package main

import (
	"context"
	"encoding/json"
	"io"
	"testing"

	"careme/internal/cache"
	"careme/internal/locations"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCachedLocations(t *testing.T) {
	for _, tt := range []struct {
		name, id, key string
	}{
		{"Kroger current version", "70100023", "location/kroger-v2/70100023"},
		{"other provider", "aldi-123", "location/aldi-123"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c := cache.NewInMemoryCache()
			lat, lon := 44.98, -93.26
			want := locations.Location{ID: tt.id, Name: "Test store", Lat: &lat, Lon: &lon}
			body, err := json.Marshal(want)
			require.NoError(t, err)
			require.NoError(t, c.Put(t.Context(), tt.key, string(body), cache.Unconditional()))
			got, err := (cachedLocations{c}).GetLocationByID(t.Context(), tt.id)
			require.NoError(t, err)
			assert.Equal(t, &want, got)
		})
	}
}

func TestCachedLocationsDoNotUseLegacyKrogerCache(t *testing.T) {
	c := cache.NewInMemoryCache()
	require.NoError(t, c.Put(t.Context(), "location/70100023", `{"id":"70100023","name":"Outdated store"}`, cache.Unconditional()))
	_, err := (cachedLocations{c}).GetLocationByID(t.Context(), "70100023")
	require.ErrorIs(t, err, cache.ErrNotFound)
}

func TestCachedLocationsFailures(t *testing.T) {
	c := cache.NewInMemoryCache()
	require.NoError(t, c.Put(t.Context(), "location/aldi-corrupt", "not JSON", cache.Unconditional()))
	for _, id := range []string{"missing", "", ".", "..", "../secret", "kroger-v2/70100023", `..\secret`} {
		t.Run(id, func(t *testing.T) {
			_, err := (cachedLocations{c}).GetLocationByID(t.Context(), id)
			require.ErrorIs(t, err, cache.ErrNotFound)
		})
	}
	_, err := (cachedLocations{c}).GetLocationByID(t.Context(), "aldi-corrupt")
	require.ErrorContains(t, err, "decode cached location")
	_, err = (cachedLocations{unavailableLocationCache{c}}).GetLocationByID(t.Context(), "70100023")
	require.ErrorIs(t, err, assert.AnError)
}

type unavailableLocationCache struct{ cache.Cache }

func (unavailableLocationCache) Get(context.Context, string) (io.ReadCloser, error) {
	return nil, assert.AnError
}
