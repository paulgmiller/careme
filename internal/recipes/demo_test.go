package recipes

import (
	"testing"

	"careme/internal/demo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDemoStaplesRouting(t *testing.T) {
	provider := dedupingStaplesProvider{provider: routingStaplesProvider{backends: []backendStaplesProvider{demo.Provider{}}}}
	items, err := provider.FetchStaples(t.Context(), demo.LocationID)
	require.NoError(t, err)
	expected, err := (demo.Provider{}).FetchStaples(t.Context(), demo.LocationID)
	require.NoError(t, err)
	assert.Equal(t, expected, items)
	wines, err := provider.FetchWines(t.Context(), demo.LocationID, []string{"red"})
	require.NoError(t, err)
	require.Len(t, wines, 1)
	assert.Contains(t, wines[0].Description, "dealcoholized")
}
