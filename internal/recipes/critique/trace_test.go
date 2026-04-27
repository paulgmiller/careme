package critique

import (
	"context"
	"slices"
	"testing"

	"careme/internal/ai"
	"careme/internal/cache"

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

func TestCritiqueSpansRecordBatchAndCacheState(t *testing.T) {
	recorder := recordSpans(t)
	base := &stubCritiquer{
		critique: &ai.RecipeCritique{
			SchemaVersion: "recipe-critique-v1",
			OverallScore:  8,
			Summary:       "Solid.",
		},
	}
	critiquer := newCachingCritiquer(base, NewStore(cache.NewInMemoryCache()))
	mc := &multiCritiquer{critiquer: critiquer}

	results := mc.CritiqueRecipes(t.Context(), []ai.Recipe{{Title: "One", Instructions: []string{"Cook."}}})
	for range results {
	}
	mc.Wait()
	_, err := critiquer.CritiqueRecipe(t.Context(), ai.Recipe{Title: "One", Instructions: []string{"Cook."}})
	require.NoError(t, err)

	names := endedSpanNames(recorder)
	assert.Contains(t, names, "recipes.critique.batch")
	assert.Contains(t, names, "recipes.critique.one")
	assert.Contains(t, names, "recipes.critique.cache")

	var cacheStates []bool
	for _, span := range endedSpansNamed(recorder, "recipes.critique.cache") {
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
}
