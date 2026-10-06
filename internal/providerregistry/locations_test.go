package providerregistry

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"careme/internal/cache"
	"careme/internal/config"
	locationtypes "careme/internal/locations/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"careme/internal/locations"
	"careme/internal/locations/geo"
)

type namedBackend struct {
	id string
}

func (b namedBackend) GetLocationByID(context.Context, string) (*locations.Location, error) {
	return nil, fmt.Errorf("not implemented")
}

func (b namedBackend) GetLocationsByCoordinates(context.Context, geo.Coordinate) ([]locations.Location, error) {
	return nil, nil
}

func (b namedBackend) IsID(string) bool {
	return false
}

func (b namedBackend) HasInventory(string) bool {
	return false
}

func TestInitializeLocationBackendsRunsFactoriesInParallelAndCollectsBackends(t *testing.T) {
	started := make(chan string, 2)
	release := make(chan struct{})

	factories := []locationBackendFactory{
		func(context.Context) (locations.LocationBackend, error) {
			started <- "first"
			<-release
			return namedBackend{id: "first"}, nil
		},
		func(context.Context) (locations.LocationBackend, error) {
			started <- "second"
			<-release
			return namedBackend{id: "second"}, nil
		},
	}

	type result struct {
		backends []locations.LocationBackend
		err      error
	}
	done := make(chan result, 1)
	go func() {
		backends, err := initializeLocationBackends(factories, locationInitializationTimeout)
		done <- result{backends: backends, err: err}
	}()

	for range factories {
		select {
		case <-started:
		case <-time.After(200 * time.Millisecond):
			t.Fatal("expected all backend factories to start before any finished")
		}
	}

	close(release)

	select {
	case result := <-done:
		if result.err != nil {
			t.Fatalf("initializeLocationBackends returned error: %v", result.err)
		}
		if len(result.backends) != 2 {
			t.Fatalf("expected 2 backends, got %d", len(result.backends))
		}

		require.Equal(t, []locations.LocationBackend{namedBackend{id: "first"}, namedBackend{id: "second"}}, result.backends)

		gotIDs := make(map[string]bool, len(result.backends))
		for _, backend := range result.backends {
			named, ok := backend.(namedBackend)
			if !ok {
				t.Fatalf("expected backend type namedBackend, got %T", backend)
			}
			gotIDs[named.id] = true
		}
		if !gotIDs["first"] || !gotIDs["second"] {
			t.Fatalf("expected both backends to be returned, got %v", gotIDs)
		}
	case <-time.After(time.Second):
		t.Fatal("initializeLocationBackends did not finish")
	}
}

func TestInitializeLocationBackendsSkipsDisabledAndFailsOnInitializationError(t *testing.T) {
	failure := errors.New("provider unavailable")
	for _, tt := range []struct {
		name       string
		backendErr error
		wantErr    bool
	}{
		{name: "disabled", backendErr: locationtypes.DisabledBackendError("disabled")},
		{name: "failed", backendErr: failure, wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			first := namedBackend{id: "first"}
			last := namedBackend{id: "last"}
			backends, err := initializeLocationBackends([]locationBackendFactory{
				func(context.Context) (locations.LocationBackend, error) { return first, nil },
				func(context.Context) (locations.LocationBackend, error) { return nil, tt.backendErr },
				func(context.Context) (locations.LocationBackend, error) { return last, nil },
			}, locationInitializationTimeout)
			if tt.wantErr {
				require.ErrorIs(t, err, failure)
				require.ErrorContains(t, err, "failed to initialize location backend")
				assert.Nil(t, backends)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, []locations.LocationBackend{first, last}, backends)
		})
	}
}

func TestNewLocationBackendsWithMocks(t *testing.T) {
	backends, err := NewFactory(&config.Config{Mocks: config.MockConfig{Enable: true}}).NewLocationBackends(locations.LoadCentroids())
	require.NoError(t, err)
	require.Len(t, backends, 1)
	backend := backends[0]
	assert.True(t, backend.IsID("70500010"))
	assert.False(t, backend.IsID("unknown"))
	loc, err := backend.GetLocationByID(t.Context(), "70500010")
	require.NoError(t, err)
	assert.Equal(t, "Big Willys", loc.Name)
}

func TestMockLocationBackendsThroughStorage(t *testing.T) {
	centroids := locations.LoadCentroids()
	backends, err := NewFactory(&config.Config{Mocks: config.MockConfig{Enable: true}}).NewLocationBackends(centroids)
	require.NoError(t, err)
	store, err := locations.New(cache.NewInMemoryCache(), centroids, backends)
	require.NoError(t, err)
	coordinates := geo.Coordinate{Lat: 34.05, Lon: -118.3}
	got, err := store.GetLocationsByCoordinates(t.Context(), coordinates)
	require.NoError(t, err)
	require.Len(t, got, 2)
	for _, loc := range got {
		assert.True(t, store.HasInventory(loc.ID))
		assert.Equal(t, coordinates, loc.Coordinate())
		detail, err := store.GetLocationByID(t.Context(), loc.ID)
		require.NoError(t, err)
		assert.Equal(t, loc.ID, detail.ID)
	}
}

func TestInitializeLocationBackendsTimesOut(t *testing.T) {
	timeout := 10 * time.Millisecond
	var hasDeadline bool
	backends, err := initializeLocationBackends([]locationBackendFactory{
		func(ctx context.Context) (locations.LocationBackend, error) {
			_, hasDeadline = ctx.Deadline()
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}, timeout)
	assert.True(t, hasDeadline)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.ErrorContains(t, err, "failed to initialize location backend")
	assert.Nil(t, backends)
}
