package recipes

import (
	"careme/internal/cache"
	"careme/internal/kroger"
	"careme/internal/locations"
	"context"
	"slices"
	"testing"
	"time"
)

type stubStaplesProvider struct {
	ids         map[string]bool
	ingredients []kroger.Ingredient
	err         error
	calls       int
}

func (s *stubStaplesProvider) IsID(locationID string) bool {
	return s.ids[locationID]
}

func (s *stubStaplesProvider) Signature() string {
	return "stub-staples-v1"
}

func (s *stubStaplesProvider) FetchStaples(_ context.Context, _ string) ([]kroger.Ingredient, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return slices.Clone(s.ingredients), nil
}

type stubRoutingStaplesProvider struct {
	ingredients []kroger.Ingredient
	err         error
	calls       int
}

func (s *stubRoutingStaplesProvider) FetchStaples(_ context.Context, _ *locations.Location) ([]kroger.Ingredient, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return slices.Clone(s.ingredients), nil
}

func TestRoutingStaplesProvider_SelectsProviderByLocationID(t *testing.T) {
	krogerProvider := &stubStaplesProvider{ids: map[string]bool{"70100023": true}}
	wholeFoodsProvider := &stubStaplesProvider{ids: map[string]bool{"wholefoods_10216": true}}
	provider := routingStaplesProvider{
		backends: []backendStaplesProvider{krogerProvider, wholeFoodsProvider},
	}

	if _, err := provider.FetchStaples(t.Context(), &locations.Location{ID: "70100023"}); err != nil {
		t.Fatalf("FetchStaples kroger returned error: %v", err)
	}
	if krogerProvider.calls != 1 || wholeFoodsProvider.calls != 0 {
		t.Fatalf("expected kroger provider to be selected, got kroger=%d wholefoods=%d", krogerProvider.calls, wholeFoodsProvider.calls)
	}

	if _, err := provider.FetchStaples(t.Context(), &locations.Location{ID: "wholefoods_10216"}); err != nil {
		t.Fatalf("FetchStaples whole foods returned error: %v", err)
	}
	if krogerProvider.calls != 1 || wholeFoodsProvider.calls != 1 {
		t.Fatalf("expected whole foods provider to be selected once, got kroger=%d wholefoods=%d", krogerProvider.calls, wholeFoodsProvider.calls)
	}
}

func TestRoutingStaplesProvider_RejectsUnsupportedLocationBackend(t *testing.T) {
	provider := routingStaplesProvider{
		backends: []backendStaplesProvider{
			&stubStaplesProvider{ids: map[string]bool{"70100023": true}},
			&stubStaplesProvider{ids: map[string]bool{"wholefoods_10216": true}},
		},
	}

	_, err := provider.FetchStaples(t.Context(), &locations.Location{ID: "walmart_3098"})
	if err == nil {
		t.Fatal("expected unsupported backend error")
	}
	if got, want := err.Error(), `staples provider does not support location "walmart_3098"`; got != want {
		t.Fatalf("unexpected error: got %q want %q", got, want)
	}
}

func TestGetStaples_UsesProviderAndCachesWholeFoodsResults(t *testing.T) {
	cacheStore := cache.NewFileCache(t.TempDir())
	provider := &stubRoutingStaplesProvider{
		ingredients: []kroger.Ingredient{
			{Description: loPtr("Honeycrisp Apple")},
			{Description: loPtr("Honeycrisp Apple")},
			{Description: loPtr("Baby Spinach")},
		},
	}
	g := &Generator{
		io:              IO(cacheStore),
		staplesProvider: provider,
	}
	params := &generatorParams{
		Location: &locations.Location{ID: "wholefoods_10216", Name: "Westlake"},
		Date:     time.Date(2026, 3, 8, 0, 0, 0, 0, time.UTC),
	}

	got, err := g.GetStaples(t.Context(), params)
	if err != nil {
		t.Fatalf("GetStaples returned error: %v", err)
	}
	if provider.calls != 1 {
		t.Fatalf("expected provider to be called once, got %d", provider.calls)
	}
	if len(got) != 2 {
		t.Fatalf("expected deduped results, got %d", len(got))
	}

	cached, err := IO(cacheStore).IngredientsFromCache(t.Context(), params.LocationHash())
	if err != nil {
		t.Fatalf("IngredientsFromCache returned error: %v", err)
	}
	if len(cached) != 2 {
		t.Fatalf("expected cached deduped results, got %d", len(cached))
	}

	gotAgain, err := g.GetStaples(t.Context(), params)
	if err != nil {
		t.Fatalf("GetStaples returned error on cached call: %v", err)
	}
	if provider.calls != 1 {
		t.Fatalf("expected cached call to skip provider, got %d calls", provider.calls)
	}
	if len(gotAgain) != 2 {
		t.Fatalf("expected cached results, got %d", len(gotAgain))
	}
}
