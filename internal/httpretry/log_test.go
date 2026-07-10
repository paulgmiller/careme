package httpretry

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogRetry(t *testing.T) {
	var output bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	hook := LogRetry("kroger")
	req, err := http.NewRequest(http.MethodGet, "https://example.com/products/category/fresh%20vegetables?store=123&offset=20", nil)
	require.NoError(t, err)

	hook(nil, nil, 1)
	hook(nil, req, 0)
	assert.Empty(t, output.String())

	hook(nil, req, 1)

	var entry map[string]any
	require.NoError(t, json.Unmarshal(output.Bytes(), &entry))
	assert.Equal(t, "Retrying HTTP request", entry["msg"])
	assert.Equal(t, "kroger", entry["source"])
	assert.Equal(t, "https://example.com/products/category/fresh%20vegetables?store=123&offset=20", entry["url"])
	assert.Equal(t, float64(2), entry["attempt"])
}
