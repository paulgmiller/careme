package kroger

import (
	"fmt"
	"net/http"
	"testing"

	"careme/internal/providers/kroger/products"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInspectStaplesPreservesVariantsAndExplainsFiltering(t *testing.T) {
	const raw = `{"data":[
 {"productId":"sausage","brand":"Kroger","description":"Mild Italian sausage","categories":["Meat"],"items":[
 {"itemId":"low","size":"1 lb","price":{"regular":4},"inventory":{"stockLevel":"LOW"}},
 {"itemId":"high","size":"2 lb","price":{"regular":7},"inventory":{"stockLevel":"HIGH"}},
 {"itemId":"out","size":"3 lb","inventory":{"stockLevel":"TEMPORARILY_OUT_OF_STOCK"}}]},
 {"productId":"ground","brand":"Other","description":"Ground pork","categories":["Meat"],"items":[{"itemId":"ground-upc","price":{"regular":5}}]},
 {"productId":"country","brand":"Kroger","description":"Country pork sausage","categories":["Frozen"],"items":[{"itemId":"country-upc","price":{"regular":5},"inventory":{"stockLevel":"TEMPORARILY_OUT_OF_STOCK"}}]}
 ]}`
	client, err := products.NewClientWithResponses("https://kroger.test", products.WithHTTPClient(&http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		assert.Equal(t, "store", req.URL.Query().Get("filter.locationId"))
		assert.Equal(t, "ais", req.URL.Query().Get("filter.fulfillment"))
		if req.URL.Query().Get("filter.term") == "Dairy & Eggs" && req.URL.Query().Has("filter.brand") {
			assert.Equal(t, "Simple Truth Organic|Kroger|Simple Truth", req.URL.Query().Get("filter.brand"))
		} else {
			assert.NotContains(t, req.URL.Query(), "filter.brand")
		}
		switch req.URL.Query().Get("filter.term") {
		case "sausage", "Meat & Seafood":
			return jsonResponse(req, http.StatusOK, raw), nil
		case "Seafood":
			return jsonResponse(req, http.StatusOK, raw), nil // Repeated product rows survive inspection.
		default:
			return jsonResponse(req, http.StatusOK, `{"data":[]}`), nil
		}
	})}))
	require.NoError(t, err)
	report, err := (staplesProvider{client: client}).InspectStaples(t.Context(), "store", []string{"sausage"}, nil)
	require.NoError(t, err)
	require.Len(t, report.Searches, 1)
	require.Len(t, report.Searches[0].Products, 3)
	assert.Len(t, *report.Searches[0].Products[0].Items, 3)
	require.Len(t, report.Staples, 4)
	var sausageCount, groundCount int
	for _, row := range report.Staples {
		switch row.ProductID {
		case "sausage":
			sausageCount++
			assert.Equal(t, "high", row.VariantID)
		case "ground":
			groundCount++
			assert.Equal(t, "ground-upc", row.VariantID)
		default:
			t.Errorf("unexpected product %s", row.ProductID)
		}
	}
	assert.Equal(t, 2, sausageCount)
	assert.Equal(t, 2, groundCount)
	assert.Contains(t, report.Filters, staplesFilter{Term: "Meat & Seafood", Brands: []string{"*"}, Frozen: true})
}

func TestInspectionSearchPagination(t *testing.T) {
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
			rows, err := (staplesProvider{client: client}).searchProducts(t.Context(), "store", "pork", []string{"Kroger", "PORK"})
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
