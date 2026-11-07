package kroger

import (
	"fmt"
	"net/http"
	"testing"

	"careme/internal/providers/kroger/products"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSearchProductsPagination(t *testing.T) {
	for _, tc := range []struct {
		name    string
		second  string
		status  int
		wantErr string
	}{
		{"success", `{"data":[{"productId":"last"}]}`, http.StatusOK, ""},
		{"later failure", `{"errors":{"reason":"failed"}}`, http.StatusBadRequest, "offset 50"},
		{"missing data", `{}`, http.StatusOK, "missing product data"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			client, err := products.NewClientWithResponses("https://kroger.test", products.WithHTTPClient(&http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				assert.Equal(t, "Kroger|PORK", req.URL.Query().Get("filter.brand"))
				if calls == 1 {
					assert.Equal(t, "0", req.URL.Query().Get("filter.start"))
					body := `{"data":[`
					for i := 0; i < 50; i++ {
						if i > 0 {
							body += ","
						}
						body += fmt.Sprintf(`{"productId":"%d"}`, i)
					}
					return jsonResponse(req, http.StatusOK, body+`]}`), nil
				}
				assert.Equal(t, "50", req.URL.Query().Get("filter.start"))
				return jsonResponse(req, tc.status, tc.second), nil
			})}))
			require.NoError(t, err)
			rows, err := (staplesProvider{client: client}).SearchProducts(t.Context(), "store", "pork", []string{"Kroger", "PORK"})
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				assert.Nil(t, rows)
			} else {
				require.NoError(t, err)
				require.Len(t, rows, 51)
				assert.Equal(t, "last", *rows[50].ProductId)
			}
			assert.Equal(t, 2, calls)
		})
	}
}

func TestSearchStaplesAPIBrandsTrustsUpstreamFilter(t *testing.T) {
	for _, onAPI := range []bool{false, true} {
		t.Run(fmt.Sprint(onAPI), func(t *testing.T) {
			client, err := products.NewClientWithResponses("https://kroger.test", products.WithHTTPClient(&http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
				if onAPI {
					assert.Equal(t, "Kroger|PORK", req.URL.Query().Get("filter.brand"))
				} else {
					assert.NotContains(t, req.URL.Query(), "filter.brand")
				}
				return jsonResponse(req, http.StatusOK, `{"data":[{"productId":"italian","brand":"Different upstream brand label","description":"Italian sausage","categories":["Meat & Seafood"],"items":[{"itemId":"sausage-upc","size":"16 oz","price":{"regular":5},"inventory":{"stockLevel":"HIGH"}}]}]}`), nil
			})}))
			require.NoError(t, err)
			rows, err := (staplesProvider{client: client}).SearchStaples(t.Context(), "store", StaplesSearch{Term: "Meat & Seafood", Brands: []string{"Kroger", "PORK"}, APIBrands: onAPI})
			require.NoError(t, err)
			if onAPI {
				require.Len(t, rows, 1)
				assert.Equal(t, "sausage-upc", rows[0].VariantID)
			} else {
				assert.Empty(t, rows)
			}
		})
	}
}
