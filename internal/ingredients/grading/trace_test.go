package grading

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

func endedSpanNamed(recorder *tracetest.SpanRecorder, name string) sdktrace.ReadOnlySpan {
	for _, span := range recorder.Ended() {
		if span.Name() == name {
			return span
		}
	}
	return nil
}

func spanIntAttr(span sdktrace.ReadOnlySpan, key string) (int64, bool) {
	for _, attr := range span.Attributes() {
		if string(attr.Key) == key {
			return attr.Value.AsInt64(), true
		}
	}
	return 0, false
}

func TestIngredientGradingSpansRecordCacheMisses(t *testing.T) {
	recorder := recordSpans(t)
	backend := &stubGradeBackend{}
	manager := &multiGrader{
		grader: newCachingGrader(backend, NewStore(cache.NewInMemoryCache())),
	}

	_, err := manager.GradeIngredients(t.Context(), []ai.InputIngredient{
		{ProductID: "apple-1", Description: "Apple"},
		{ProductID: "beef-1", Description: "Beef"},
	})
	require.NoError(t, err)

	names := endedSpanNames(recorder)
	assert.Contains(t, names, "ingredients.grade")
	assert.Contains(t, names, "ingredients.grade.cache")
	assert.Contains(t, names, "ingredients.grade.external")

	cacheSpan := endedSpanNamed(recorder, "ingredients.grade.cache")
	require.NotNil(t, cacheSpan)
	misses, ok := spanIntAttr(cacheSpan, "ingredient.cache_miss_count")
	require.True(t, ok)
	assert.Equal(t, int64(2), misses)
}
