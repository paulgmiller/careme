package recipes

import (
	"context"
	"slices"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	ingredientgrading "careme/internal/ingredients/grading"
	"careme/internal/locations"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func recordSpans(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	t.Cleanup(func() {
		otel.SetTracerProvider(previous)
		_ = provider.Shutdown(context.Background())
	})
	return recorder
}

func endedSpanNames(recorder *tracetest.SpanRecorder) []string {
	spans := recorder.Ended()
	names := make([]string, 0, len(spans))
	for _, span := range spans {
		names = append(names, span.Name())
	}
	return names
}

func endedSpansNamed(recorder *tracetest.SpanRecorder, name string) []sdktrace.ReadOnlySpan {
	spans := recorder.Ended()
	matches := make([]sdktrace.ReadOnlySpan, 0)
	for _, span := range spans {
		if span.Name() == name {
			matches = append(matches, span)
		}
	}
	return matches
}

func spanBoolAttr(span sdktrace.ReadOnlySpan, key string) (bool, bool) {
	for _, attr := range span.Attributes() {
		if string(attr.Key) == key {
			return attr.Value.AsBool(), true
		}
	}
	return false, false
}

func TestGenerateRecipesEmitsHighLevelLatencySpans(t *testing.T) {
	recorder := recordSpans(t)
	cacheStore := cache.NewFileCache(t.TempDir())
	io := IO(cacheStore)
	params := DefaultParams(&locations.Location{ID: "70004001", Name: "Store", State: "WA"}, time.Now())
	require.NoError(t, io.SaveIngredients(t.Context(), params.LocationHash(), []ai.InputIngredient{{ProductID: "chicken-1", Description: "Chicken"}}))

	g := &generatorService{
		staples: &cachedStaplesService{
			cache:  io,
			grader: ingredientgrading.NewManager(nil, nil),
		},
		aiClient: &sequenceAIClient{generateResponses: []*ai.ShoppingList{{
			ResponseID: "resp-stable",
			Recipes:    []ai.Recipe{{Title: "Steady Dinner", Description: "Good enough"}},
		}}},
		critiquer:    &captureCritiqueService{},
		statusWriter: noopstatuswriter{},
	}

	_, err := g.GenerateRecipes(t.Context(), params)
	require.NoError(t, err)

	names := endedSpanNames(recorder)
	assert.Contains(t, names, "recipes.generate")
	assert.Contains(t, names, "recipes.staples.fetch")
	assert.Contains(t, names, "ingredients.grade")
	assert.Contains(t, names, "recipes.ai.generate")
	assert.Contains(t, names, "recipes.critique_and_retry")
}

func TestFetchStaplesSpansRecordCacheHitAndMiss(t *testing.T) {
	recorder := recordSpans(t)
	cacheStore := cache.NewInMemoryCache()
	params := &generatorParams{
		Location: &locations.Location{ID: "70100023", Name: "Test Store"},
		Date:     time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC),
	}
	s := &cachedStaplesService{
		cache:  IO(cacheStore),
		grader: &stubIngredientGrader{},
		provider: &stubRoutingStaplesProvider{
			ingredients: []ai.InputIngredient{{ProductID: "apple-1", Description: "Apple"}},
		},
	}

	_, err := s.FetchStaples(t.Context(), params)
	require.NoError(t, err)
	_, err = s.FetchStaples(t.Context(), params)
	require.NoError(t, err)

	var cacheStates []bool
	for _, span := range endedSpansNamed(recorder, "recipes.staples.fetch") {
		cacheHit, ok := spanBoolAttr(span, "cache.hit")
		require.True(t, ok)
		cacheStates = append(cacheStates, cacheHit)
	}
	slices.SortFunc(cacheStates, func(a, b bool) int {
		if a == b {
			return 0
		}
		if !a {
			return -1
		}
		return 1
	})
	assert.Equal(t, []bool{false, true}, cacheStates)
	assert.Contains(t, endedSpanNames(recorder), "recipes.staples.provider_fetch")
}
