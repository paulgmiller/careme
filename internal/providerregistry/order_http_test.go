package providerregistry

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

func TestOrderHTTPClientInstrumentsWithoutRetrying(t *testing.T) {
	previous := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() { otel.SetTextMapPropagator(previous) })
	span := trace.NewSpanContext(trace.SpanContextConfig{TraceID: trace.TraceID{1}, SpanID: trace.SpanID{2}, TraceFlags: trace.FlagsSampled})
	ctx := trace.ContextWithSpanContext(t.Context(), span)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		assert.Contains(t, r.Header.Get("Traceparent"), span.TraceID().String())
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()
	client := newOrderHTTPClient()
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		req, err := http.NewRequestWithContext(ctx, method, server.URL, strings.NewReader(`{}`))
		require.NoError(t, err)
		response, err := client.Do(req)
		require.NoError(t, err)
		assert.Equal(t, http.StatusServiceUnavailable, response.StatusCode)
		require.NoError(t, response.Body.Close())
	}
	assert.Equal(t, int32(2), calls.Load(), "each ordering request gets one attempt")
}

func TestOrderHTTPClientDoesNotFollowAPIRedirects(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Redirect(w, r, "/other", http.StatusFound)
	}))
	defer server.Close()
	response, err := newOrderHTTPClient().Get(server.URL)
	require.NoError(t, err)
	assert.Equal(t, http.StatusFound, response.StatusCode)
	require.NoError(t, response.Body.Close())
	assert.Equal(t, int32(1), calls.Load())
}
