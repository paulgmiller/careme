package kroger

import (
	"context"
	"net/http"
	"testing"

	retryablehttp "github.com/hashicorp/go-retryablehttp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithRetries_UsesDefaultStatusPolicy(t *testing.T) {
	client := withRetries(&http.Client{})
	transport, ok := client.Transport.(*retryablehttp.RoundTripper)
	require.True(t, ok)

	tests := []struct {
		name       string
		statusCode int
		wantRetry  bool
	}{
		{name: "not found", statusCode: http.StatusNotFound, wantRetry: false},
		{name: "bad request", statusCode: http.StatusBadRequest, wantRetry: false},
		{name: "too many requests", statusCode: http.StatusTooManyRequests, wantRetry: true},
		{name: "service unavailable", statusCode: http.StatusServiceUnavailable, wantRetry: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := transport.Client.CheckRetry(t.Context(), &http.Response{StatusCode: tt.statusCode}, nil)
			require.NoError(t, err)
			assert.Equal(t, tt.wantRetry, got)
		})
	}
}

func TestWithRetries_DoesNotRetryCanceledRequest(t *testing.T) {
	client := withRetries(&http.Client{})
	transport, ok := client.Transport.(*retryablehttp.RoundTripper)
	require.True(t, ok)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	got, err := transport.Client.CheckRetry(ctx, &http.Response{StatusCode: http.StatusNotFound}, nil)

	assert.False(t, got)
	assert.ErrorIs(t, err, context.Canceled)
}
