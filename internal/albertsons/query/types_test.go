package query

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPathwaySearchPayloadUnmarshalFixture(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("acmeresp.json")
	require.NoError(t, err)

	var payload PathwaySearchPayload
	require.NoError(t, json.Unmarshal(raw, &payload))

	assert.Equal(t, 194, payload.Response.NumFound)
	require.NotEmpty(t, payload.Response.Docs)
	assert.Equal(t, "806", payload.Response.Docs[0].StoreID)
	assert.True(t, payload.Response.Docs[0].ChannelEligibility.Delivery)
}
