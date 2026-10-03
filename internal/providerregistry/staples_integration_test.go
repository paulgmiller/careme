package providerregistry

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"testing"

	"careme/internal/ai"
	"careme/internal/locations"
	"careme/internal/providers/smithbrothersfarms"
	"careme/internal/recipes"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type smithBrothersTransport func(*http.Request) (*http.Response, error)

func (f smithBrothersTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSmithBrothersFarmsStaplesRouting(t *testing.T) {
	pages := map[string]string{
		"/produce": "produce", "/meat-poultry": "meat",
		"/smith-brothers-organic-harvest-box": "organic-box", "/harvest-produce-box": "standard-box",
	}
	client := &http.Client{Transport: smithBrothersTransport(func(req *http.Request) (*http.Response, error) {
		file, ok := pages[req.URL.Path]
		require.True(t, ok, req.URL.String())
		data, err := os.ReadFile("../providers/smithbrothersfarms/testdata/" + file + ".html")
		require.NoError(t, err)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(data))}, nil
	})}
	backend := smithbrothersfarms.NewStaplesProvider(smithbrothersfarms.NewClient(client))
	provider := recipes.NewStaplesProvider([]locations.StaplesBackend{backend})
	got, err := provider.FetchStaples(t.Context(), "smithbrothersfarms_delivery")
	require.NoError(t, err)
	require.Len(t, got, 25)
	var rendered bytes.Buffer
	require.NoError(t, ai.InputIngredientsToTSV(got, &rendered))
	assert.Contains(t, rendered.String(), "Organic Produce Box — Local Organic Sugar Bee Apples")
	assert.Contains(t, rendered.String(), "Produce Box — Avocados")
	assert.Contains(t, rendered.String(), "Organic Bartlett Pears - 2 lbs")
	for _, item := range got {
		assert.NotEqual(t, "4975", item.ProductID)
		assert.NotEqual(t, "2997", item.ProductID)
	}
}
