package ai

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEstimateResponseCostUSD(t *testing.T) {
	for _, tc := range []struct {
		name, model                  string
		input, cached, write, output int64
		want                         float64
		wantErr                      string
	}{
		{name: "astra mixed cache", model: "gpt-6-astra", input: 1000, cached: 200, write: 300, output: 100, want: 0.01395},
		{name: "sol mixed cache", model: "gpt-5.6-sol", input: 1000, cached: 200, write: 300, output: 100, want: 0.007475},
		{name: "luna mixed cache", model: "gpt-5.6-luna", input: 1000, cached: 200, write: 300, output: 100, want: 0.000299},
		{name: "unknown", model: "unknown", input: 1000, output: 100, wantErr: "price_not_configured"},
		{name: "large context", model: "gpt-6-astra", input: 272001, output: 100, wantErr: "up to 272000"},
		{name: "invalid cache", model: "gpt-6-astra", input: 100, cached: 80, write: 30, output: 100, wantErr: "invalid token usage"},
		{name: "missing tokens", model: "gpt-6-astra", wantErr: "invalid token usage"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := EstimateResponseCostUSD(tc.model, tc.input, tc.cached, tc.write, tc.output)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.InDelta(t, tc.want, got, 1e-10)
		})
	}
}
