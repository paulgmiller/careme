package query

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollectionProductsBuildsExpectedRequest(t *testing.T) {
	t.Parallel()

	var capturedReq *http.Request
	client := NewClient(ClientConfig{
		BaseURL:        "https://example.test",
		ForterToken:    "forter-token",
		PageViewIDFunc: func() string { return "page-view-id" },
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				capturedReq = r
				return jsonResponse(r, http.StatusOK, `{"data":{"collectionProducts":{"items":[]}}}`), nil
			}),
		},
	})

	payload, err := client.CollectionProducts(context.Background(), "29998", "rc-other-fish-18102", SearchOptions{
		PostalCode: "60174",
		ZoneID:     "384",
		First:      12,
	})
	require.NoError(t, err)
	require.NotNil(t, payload)
	require.NotNil(t, capturedReq)

	assert.Equal(t, "/graphql", capturedReq.URL.Path)
	assert.Equal(t, operationName, capturedReq.URL.Query().Get("operationName"))
	assert.Equal(t, "*/*", capturedReq.Header.Get("Accept"))
	assert.Equal(t, "en-US,en;q=0.9", capturedReq.Header.Get("Accept-Language"))
	assert.Equal(t, "application/json", capturedReq.Header.Get("Content-Type"))
	assert.Equal(t, "web", capturedReq.Header.Get("X-Client-Identifier"))
	assert.Equal(t, "page-view-id", capturedReq.Header.Get("X-Page-View-Id"))

	cookie, err := capturedReq.Cookie("forterToken")
	require.NoError(t, err)
	assert.Equal(t, "forter-token", cookie.Value)

	var variables map[string]any
	require.NoError(t, json.Unmarshal([]byte(capturedReq.URL.Query().Get("variables")), &variables))
	assert.Equal(t, "29998", variables["shopId"])
	assert.Equal(t, "60174", variables["postalCode"])
	assert.Equal(t, "384", variables["zoneId"])
	assert.Equal(t, "rc-other-fish-18102", variables["slug"])
	assert.Equal(t, "bestMatch", variables["orderBy"])
	assert.Equal(t, "page-view-id", variables["pageViewId"])
	assert.Equal(t, "collections_nav_child_carousel", variables["itemsDisplayType"])
	assert.Equal(t, float64(12), variables["first"])
	assert.Equal(t, "browse", variables["pageSource"])
	assert.Empty(t, variables["filters"])

	var extensions map[string]map[string]any
	require.NoError(t, json.Unmarshal([]byte(capturedReq.URL.Query().Get("extensions")), &extensions))
	assert.Equal(t, float64(1), extensions["persistedQuery"]["version"])
	assert.Equal(t, persistedQueryHash, extensions["persistedQuery"]["sha256Hash"])
}

func TestCollectionProductsOmitsOptionalPostalCodeAndZone(t *testing.T) {
	t.Parallel()

	var capturedQuery url.Values
	client := NewClient(ClientConfig{
		BaseURL:        "https://example.test",
		PageViewIDFunc: func() string { return "page-view-id" },
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				capturedQuery = r.URL.Query()
				if _, err := r.Cookie("forterToken"); err == nil {
					t.Fatal("unexpected forterToken cookie")
				}
				return jsonResponse(r, http.StatusOK, `{"data":{"collectionProducts":{"items":[]}}}`), nil
			}),
		},
	})

	_, err := client.CollectionProducts(context.Background(), "29998", "rc-other-fish-18102", SearchOptions{})
	require.NoError(t, err)

	var variables map[string]any
	require.NoError(t, json.Unmarshal([]byte(capturedQuery.Get("variables")), &variables))
	assert.NotContains(t, variables, "postalCode")
	assert.NotContains(t, variables, "zoneId")
	assert.Equal(t, float64(defaultFirst), variables["first"])
}

func TestCollectionProductsParsesItems(t *testing.T) {
	t.Parallel()

	client := NewClient(ClientConfig{
		BaseURL:        "https://example.test",
		PageViewIDFunc: func() string { return "page-view-id" },
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				return jsonResponse(r, http.StatusOK, `{
					"data": {
						"collectionProducts": {
							"items": [
								{
									"id": "items_23593-17771058",
									"name": "Sea Queen Fresh Atlantic Salmon Portions",
									"size": "per lb",
									"productId": "17771058",
									"legacyId": "601604564",
									"brandName": "sea queen",
									"brandId": "50470",
									"evergreenUrl": "17771058-fresh-salmon-1-per-lb",
									"availability": {
										"available": true,
										"stockLevel": "highlyInStock"
									},
									"viewSection": {
										"itemImage": {
											"url": "https://d2lnr5mha7bycj.cloudfront.net/product-image/file/salmon.jpg",
											"templateUrl": "https://www.instacart.com/image-server/{width=}x{height=}/salmon.jpg"
										},
										"trackingProperties": {
											"product_id": "17771058",
											"item_id": "601604564",
											"stock_level": "highly_in_stock",
											"available_ind": true,
											"product_category_name": "Salmon Fillets",
											"item_name": "Sea Queen Fresh Atlantic Salmon Portions"
										}
									},
									"price": {
										"viewSection": {
											"itemCard": {
												"priceString": "$9.49 /pkg (est.)",
												"pricingUnitString": "$9.49 / lb"
											},
											"priceString": "$9.49",
											"priceValueString": "9.49",
											"currencySymbolString": "$"
										},
										"parWeightTotalEstimate": {
											"viewSection": {
												"parWeightString": "About 1.0 lb each"
											}
										}
									}
								}
							]
						}
					}
				}`), nil
			}),
		},
	})

	payload, err := client.CollectionProducts(context.Background(), "29998", "rc-other-fish-18102", SearchOptions{})
	require.NoError(t, err)
	require.Len(t, payload.Data.CollectionProducts.Items, 1)

	item := payload.Data.CollectionProducts.Items[0]
	assert.Equal(t, "Sea Queen Fresh Atlantic Salmon Portions", item.Name)
	assert.Equal(t, "17771058", item.ProductID)
	assert.Equal(t, "sea queen", item.BrandName)
	assert.True(t, item.Availability.Available)
	assert.Equal(t, "highlyInStock", item.Availability.StockLevel)
	assert.Equal(t, "https://d2lnr5mha7bycj.cloudfront.net/product-image/file/salmon.jpg", item.ViewSection.ItemImage.URL)
	assert.Equal(t, "Salmon Fillets", item.ViewSection.TrackingProperties.ProductCategoryName)
	assert.Equal(t, "$9.49", item.Price.ViewSection.PriceString)
	assert.Equal(t, "9.49", item.Price.ViewSection.PriceValueString)
	assert.Equal(t, "$9.49 / lb", item.Price.ViewSection.ItemCard.PricingUnitString)
	assert.Equal(t, "About 1.0 lb each", item.Price.ParWeightTotalEstimate.ViewSection.ParWeightString)
}

func TestCollectionProductsReturnsGraphQLErrors(t *testing.T) {
	t.Parallel()

	client := NewClient(ClientConfig{
		BaseURL:        "https://example.test",
		PageViewIDFunc: func() string { return "page-view-id" },
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				return jsonResponse(r, http.StatusOK, `{"errors":[{"message":"Not Authenticated","path":["collectionProducts"]}],"data":null}`), nil
			}),
		},
	})

	_, err := client.CollectionProducts(context.Background(), "29998", "rc-other-fish-18102", SearchOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Not Authenticated")
}

func TestCollectionProductsValidatesRequiredFields(t *testing.T) {
	t.Parallel()

	client := NewClient(ClientConfig{
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				t.Fatal("unexpected HTTP call")
				return nil, nil
			}),
		},
	})

	_, err := client.CollectionProducts(context.Background(), "", "rc-other-fish-18102", SearchOptions{})
	require.ErrorContains(t, err, "store id is required")

	_, err = client.CollectionProducts(context.Background(), "29998", "", SearchOptions{})
	require.ErrorContains(t, err, "category slug is required")

	_, err = client.CollectionProducts(context.Background(), "29998", "rc-other-fish-18102", SearchOptions{First: -1})
	require.ErrorContains(t, err, "first must be greater than or equal to 0")
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func jsonResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header: http.Header{
			"Content-Type": []string{"application/json"},
		},
		Body:    io.NopCloser(strings.NewReader(body)),
		Request: req,
	}
}
