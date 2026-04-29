package critique

import (
	"context"
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
	previousTracer := tracer
	otel.SetTracerProvider(provider)
	tracer = provider.Tracer("careme/internal/recipes/critique")
	t.Cleanup(func() {
		tracer = previousTracer
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

func endedSpanCount(recorder *tracetest.SpanRecorder, name string) int {
	count := 0
	for _, span := range recorder.Ended() {
		if span.Name() == name {
			count++
		}
	}
	return count
}

func TestCritiqueEmitsHighLevelLatencySpans(t *testing.T) {
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
	assert.Equal(t, 2, endedSpanCount(recorder, "recipes.critique.cache"))
}
