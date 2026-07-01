package kroger

import (
	"testing"

	locationtypes "careme/internal/locations/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientWithResponsesIsID(t *testing.T) {
	t.Parallel()

	client := &LocationBackend{}
	tests := []struct {
		id   string
		want bool
	}{
		{id: "70500874", want: true},
		{id: "0001", want: true},
		{id: "", want: false},
		{id: "7050A874", want: false},
		{id: "walmart_123", want: false},
	}

	for _, tc := range tests {
		assert.Equal(t, tc.want, client.IsID(tc.id), "IsID(%q)", tc.id)
	}
}

func TestFloat32PtrToFloat64Ptr(t *testing.T) {
	t.Parallel()

	assert.Nil(t, float32PtrToFloat64Ptr(nil))

	v := float32(47.5)
	got := float32PtrToFloat64Ptr(&v)
	require.NotNil(t, got)
	assert.Equal(t, 47.5, *got)
}

func TestChainNameIsCanonicalized(t *testing.T) {
	t.Parallel()

	loc := locationtypes.Location{
		ID:      "70500874",
		Name:    "QFC Bellevue",
		Chain:   chainName,
		Address: "10116 NE 8th St",
	}
	assert.Equal(t, "kroger", loc.Chain)
}
