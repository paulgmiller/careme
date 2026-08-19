package kroger

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	krogerlocations "careme/internal/kroger/locations"
	"careme/internal/locations/geo"
	locationtypes "careme/internal/locations/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

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

func TestGetLocationsByCoordinatesUsesKrogerCoordinateFilter(t *testing.T) {
	t.Parallel()

	httpClient := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		query := req.URL.Query()
		assert.Equal(t, "47.6097,-122.3331", query.Get("filter.latLong.near"))
		assert.Equal(t, "20", query.Get("filter.radiusInMiles"))
		assert.Empty(t, query.Get("filter.zipCode.near"))
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"data":[]}`)),
			Request:    req,
		}, nil
	})}
	client, err := krogerlocations.NewClientWithResponses("https://example.test", krogerlocations.WithHTTPClient(httpClient))
	require.NoError(t, err)

	backend := &LocationBackend{client: client}
	got, err := backend.GetLocationsByCoordinates(context.Background(), geo.Coordinate{Lat: 47.6097, Lon: -122.3331})

	require.NoError(t, err)
	assert.Empty(t, got)
}
