package hydrator

import (
	"context"
	"errors"
	"testing"

	locationtypes "careme/internal/locations/types"
)

type fakeLoader struct {
	load func(context.Context, string) (locationtypes.Location, error)
}

func (f fakeLoader) Load(ctx context.Context, locationID string) (locationtypes.Location, error) {
	return f.load(ctx, locationID)
}

func TestLazyHydratorCachesLoadedLocations(t *testing.T) {
	t.Parallel()

	loads := 0
	hydrator := NewLazyHydrator(fakeLoader{
		load: func(_ context.Context, locationID string) (locationtypes.Location, error) {
			loads++
			return locationtypes.Location{ID: locationID, Name: "Loaded"}, nil
		},
	})

	first, err := hydrator.Hydrate(context.Background(), "store-1")
	if err != nil {
		t.Fatalf("first hydrate: %v", err)
	}
	second, err := hydrator.Hydrate(context.Background(), "store-1")
	if err != nil {
		t.Fatalf("second hydrate: %v", err)
	}

	if loads != 1 {
		t.Fatalf("expected one load, got %d", loads)
	}
	if first != second {
		t.Fatalf("expected cached location on second hydrate")
	}
}

func TestLazyHydratorReturnsLoaderError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("boom")
	hydrator := NewLazyHydrator(fakeLoader{
		load: func(_ context.Context, _ string) (locationtypes.Location, error) {
			return locationtypes.Location{}, wantErr
		},
	})

	_, err := hydrator.Hydrate(context.Background(), "store-1")
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected %v, got %v", wantErr, err)
	}
}
