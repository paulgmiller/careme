package recipes

import (
	"testing"

	"careme/internal/tcfarm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTCFarmStaplesRouting(t *testing.T) {
	provider := dedupingStaplesProvider{provider: routingStaplesProvider{backends: []backendStaplesProvider{tcfarm.Provider{}}}}
	items, err := provider.FetchStaples(t.Context(), tcfarm.LocationID)
	require.NoError(t, err)
	expected, err := (tcfarm.Provider{}).FetchStaples(t.Context(), tcfarm.LocationID)
	require.NoError(t, err)
	assert.Equal(t, expected, items)
	wines, err := provider.FetchWines(t.Context(), tcfarm.LocationID, []string{"red"})
	require.NoError(t, err)
	require.Len(t, wines, 1)
	assert.Contains(t, wines[0].Description, "dealcoholized")
}
