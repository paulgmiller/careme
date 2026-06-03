package query

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
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
		PageViewIDFunc: func() string { return "page-view-id" },
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/idp/v1/init" {
					return initCookieResponse(r), nil
				}
				capturedReq = r
				return jsonResponse(r, http.StatusOK, `{"data":{"collectionProducts":{"items":[]}}}`), nil
			}),
		},
	})

	payload, err := client.collectionProducts(context.Background(), "29998", "rc-other-fish-18102", SearchOptions{
		PostalCode: "60174",
		ZoneID:     "384",
		First:      12,
	})
	require.NoError(t, err)
	require.NotNil(t, payload)
	require.NotNil(t, capturedReq)

	assert.Equal(t, "/graphql", capturedReq.URL.Path)
	assert.Equal(t, collectionProductsOperationName, capturedReq.URL.Query().Get("operationName"))
	assert.Equal(t, "*/*", capturedReq.Header.Get("Accept"))
	assert.Equal(t, "en-US,en;q=0.9", capturedReq.Header.Get("Accept-Language"))
	assert.Equal(t, "application/json", capturedReq.Header.Get("Content-Type"))
	assert.Equal(t, "web", capturedReq.Header.Get("X-Client-Identifier"))
	assert.Equal(t, "page-view-id", capturedReq.Header.Get("X-Page-View-Id"))
	assert.Equal(t, "https://example.test/store/aldi/collections/rc-other-fish-18102", capturedReq.Header.Get("Referer"))

	cookie, err := capturedReq.Cookie("__Host-instacart_sid")
	require.NoError(t, err)
	assert.Equal(t, "init-sid", cookie.Value)
	cookie, err = capturedReq.Cookie("_instacart_session_id")
	require.NoError(t, err)
	assert.Equal(t, "init-session", cookie.Value)

	var variables map[string]any
	require.NoError(t, json.Unmarshal([]byte(capturedReq.URL.Query().Get("variables")), &variables))
	assert.Equal(t, "29998", variables["shopId"])
	assert.Equal(t, "60174", variables["postalCode"])
	assert.Equal(t, "384", variables["zoneId"])
	assert.Equal(t, "rc-other-fish-18102", variables["slug"])
	assert.Equal(t, defaultOrderBy, variables["orderBy"])
	assert.Equal(t, "page-view-id", variables["pageViewId"])
	assert.Equal(t, float64(12), variables["first"])
	assert.Empty(t, variables["filters"])

	var extensions map[string]map[string]any
	require.NoError(t, json.Unmarshal([]byte(capturedReq.URL.Query().Get("extensions")), &extensions))
	assert.Equal(t, float64(1), extensions["persistedQuery"]["version"])
	assert.Equal(t, collectionProductsPersistedQueryHash, extensions["persistedQuery"]["sha256Hash"])
}

func TestItemsBuildsExpectedRequestAndParsesItems(t *testing.T) {
	t.Parallel()

	var capturedReq *http.Request
	client := NewClient(ClientConfig{
		BaseURL:        "https://example.test",
		PageViewIDFunc: func() string { return "page-view-id" },
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/idp/v1/init" {
					return initCookieResponse(r), nil
				}
				capturedReq = r
				return jsonResponse(r, http.StatusOK, `{
					"data": {
						"items": [
							{
								"id": "items_516286-19115479",
								"name": "Black Angus Beef",
								"size": "1 lb",
								"productId": "19115479",
								"availability": {"available": true, "stockLevel": "highlyInStock"}
							}
						]
					}
				}`), nil
			}),
		},
	})

	payload, err := client.items(context.Background(), "516286", []string{"items_516286-19115479"}, SearchOptions{
		PostalCode: "40222",
		ZoneID:     "289",
	})
	require.NoError(t, err)
	require.NotNil(t, payload)
	require.NotNil(t, capturedReq)

	assert.Equal(t, "/graphql", capturedReq.URL.Path)
	assert.Equal(t, itemsOperationName, capturedReq.URL.Query().Get("operationName"))
	assert.Equal(t, "*/*", capturedReq.Header.Get("Accept"))
	assert.Equal(t, "en-US,en;q=0.9", capturedReq.Header.Get("Accept-Language"))
	assert.Equal(t, "application/json", capturedReq.Header.Get("Content-Type"))
	assert.Equal(t, "web", capturedReq.Header.Get("X-Client-Identifier"))
	assert.Equal(t, "true", capturedReq.Header.Get("X-IC-View-Layer"))
	assert.Equal(t, "page-view-id", capturedReq.Header.Get("X-Page-View-Id"))

	cookie, err := capturedReq.Cookie("__Host-instacart_sid")
	require.NoError(t, err)
	assert.Equal(t, "init-sid", cookie.Value)
	cookie, err = capturedReq.Cookie("_instacart_session_id")
	require.NoError(t, err)
	assert.Equal(t, "init-session", cookie.Value)

	var variables map[string]any
	require.NoError(t, json.Unmarshal([]byte(capturedReq.URL.Query().Get("variables")), &variables))
	assert.Equal(t, []any{"items_516286-19115479"}, variables["ids"])
	assert.Equal(t, "516286", variables["shopId"])
	assert.Equal(t, "40222", variables["postalCode"])
	assert.Equal(t, "289", variables["zoneId"])

	var extensions map[string]map[string]any
	require.NoError(t, json.Unmarshal([]byte(capturedReq.URL.Query().Get("extensions")), &extensions))
	assert.Equal(t, float64(1), extensions["persistedQuery"]["version"])
	assert.Equal(t, itemsPersistedQueryHash, extensions["persistedQuery"]["sha256Hash"])

	require.Len(t, payload.Data.Items, 1)
	assert.Equal(t, "Black Angus Beef", payload.Data.Items[0].Name)
}

func TestCollectionProductsUsesDefaultFirst(t *testing.T) {
	t.Parallel()

	var capturedQuery url.Values
	var capturedInitReq *http.Request
	client := NewClient(ClientConfig{
		BaseURL:        "https://example.test",
		PageViewIDFunc: func() string { return "page-view-id" },
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/idp/v1/init" {
					capturedInitReq = r
					resp := jsonResponse(r, http.StatusOK, `{}`)
					resp.Header.Set("Set-Cookie", "__Host-instacart_sid=init-sid; Path=/; Secure; HttpOnly")
					return resp, nil
				}
				capturedQuery = r.URL.Query()
				cookie, err := r.Cookie("__Host-instacart_sid")
				require.NoError(t, err)
				assert.Equal(t, "init-sid", cookie.Value)
				return jsonResponse(r, http.StatusOK, `{"data":{"collectionProducts":{"items":[]}}}`), nil
			}),
		},
	})

	_, err := client.collectionProducts(context.Background(), "29998", "rc-other-fish-18102", SearchOptions{})
	require.NoError(t, err)
	require.NotNil(t, capturedInitReq)
	assert.Equal(t, http.MethodPost, capturedInitReq.Method)
	assert.Equal(t, "https://example.test/store/aldi/storefront", capturedInitReq.Header.Get("Referer"))

	var variables map[string]any
	require.NoError(t, json.Unmarshal([]byte(capturedQuery.Get("variables")), &variables))
	assert.NotContains(t, variables, "postalCode")
	zoneID, ok := variables["zoneId"].(string)
	require.True(t, ok)
	zoneNum, err := strconv.Atoi(zoneID)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, zoneNum, 100)
	assert.LessOrEqual(t, zoneNum, 300)
	assert.Equal(t, float64(defaultFirst), variables["first"])
}

func TestCollectionProductsParsesItems(t *testing.T) {
	t.Parallel()

	client := NewClient(ClientConfig{
		BaseURL:        "https://example.test",
		PageViewIDFunc: func() string { return "page-view-id" },
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/idp/v1/init" {
					return initCookieResponse(r), nil
				}
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

	payload, err := client.collectionProducts(context.Background(), "29998", "rc-other-fish-18102", SearchOptions{})
	require.NoError(t, err)
	require.Len(t, payload.Data.Items(), 1)

	item := payload.Data.Items()[0]
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

func TestCollectionProductsParsesSearchResultItemIDs(t *testing.T) {
	t.Parallel()

	client := NewClient(ClientConfig{
		BaseURL:        "https://example.test",
		PageViewIDFunc: func() string { return "page-view-id" },
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/idp/v1/init" {
					return initCookieResponse(r), nil
				}
				return jsonResponse(r, http.StatusOK, `{
					"data": {
						"collectionProductsBasedSearchResults": {
							"itemResultList": {
								"featuredProducts": [],
								"itemIds": ["items_29998-17771058", "items_29998-123"]
							},
							"searchId": "search-id",
							"viewSection": {"headerString": "Beef"}
						}
					}
				}`), nil
			}),
		},
	})

	payload, err := client.collectionProducts(context.Background(), "29998", "n-beef-67693", SearchOptions{})
	require.NoError(t, err)

	assert.Empty(t, payload.Data.Items())
	assert.Equal(t, []string{"items_29998-17771058", "items_29998-123"}, payload.Data.ItemIDs())
	assert.Equal(t, "Beef", payload.Data.CollectionProductsBasedSearchResults.ViewSection.HeaderString)
}

func TestCollectionProductsParsesCollectionProductItemIDs(t *testing.T) {
	t.Parallel()

	client := NewClient(ClientConfig{
		BaseURL:        "https://example.test",
		PageViewIDFunc: func() string { return "page-view-id" },
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/idp/v1/init" {
					return initCookieResponse(r), nil
				}
				return jsonResponse(r, http.StatusOK, `{
					"data": {
						"collectionProducts": {
							"itemIds": ["items_516286-19115479", "items_516286-20112308"],
							"items": []
						}
					}
				}`), nil
			}),
		},
	})

	payload, err := client.collectionProducts(context.Background(), "516286", "n-beef-67693", SearchOptions{})
	require.NoError(t, err)

	assert.Equal(t, []string{"items_516286-19115479", "items_516286-20112308"}, payload.Data.ItemIDs())
}

func TestProductsHydratesItemIDsUpToLimit(t *testing.T) {
	t.Parallel()

	var capturedItemsReq *http.Request
	client := NewClient(ClientConfig{
		BaseURL:        "https://example.test",
		PageViewIDFunc: func() string { return "page-view-id" },
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/idp/v1/init" {
					return initCookieResponse(r), nil
				}
				switch r.URL.Query().Get("operationName") {
				case collectionProductsOperationName:
					return jsonResponse(r, http.StatusOK, `{
						"data": {
							"collectionProductsBasedSearchResults": {
								"itemResultList": {
									"itemIds": ["items_516286-19115479", "items_516286-20112308", "items_516286-333"]
								}
							}
						}
					}`), nil
				case itemsOperationName:
					capturedItemsReq = r
					return jsonResponse(r, http.StatusOK, `{
						"data": {
							"items": [
								{"id": "items_516286-19115479", "name": "Black Angus Beef", "productId": "19115479"},
								{"id": "items_516286-20112308", "name": "Ground Beef", "productId": "20112308"}
							]
						}
					}`), nil
				default:
					t.Fatalf("unexpected operation: %s", r.URL.Query().Get("operationName"))
					return nil, nil
				}
			}),
		},
	})

	items, err := client.Products(context.Background(), "516286", "n-beef-67693", SearchOptions{
		PostalCode: "40222",
		ZoneID:     "289",
		First:      2,
	})
	require.NoError(t, err)

	require.Len(t, items, 2)
	assert.Equal(t, "Black Angus Beef", items[0].Name)
	assert.Equal(t, "Ground Beef", items[1].Name)
	require.NotNil(t, capturedItemsReq)
	assert.Equal(t, itemsOperationName, capturedItemsReq.URL.Query().Get("operationName"))
	assert.Contains(t, capturedItemsReq.URL.Query().Get("variables"), `"ids":["items_516286-19115479","items_516286-20112308"]`)
	assert.NotContains(t, capturedItemsReq.URL.Query().Get("variables"), "items_516286-333")
}

func TestProductsReusesGeneratedZoneIDForStore(t *testing.T) {
	t.Parallel()

	var collectionZoneID string
	var itemsZoneID string
	client := NewClient(ClientConfig{
		BaseURL:        "https://example.test",
		PageViewIDFunc: func() string { return "page-view-id" },
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/idp/v1/init" {
					return initCookieResponse(r), nil
				}
				var variables map[string]any
				require.NoError(t, json.Unmarshal([]byte(r.URL.Query().Get("variables")), &variables))
				zoneID, _ := variables["zoneId"].(string)

				switch r.URL.Query().Get("operationName") {
				case collectionProductsOperationName:
					collectionZoneID = zoneID
					return jsonResponse(r, http.StatusOK, `{
						"data": {
							"collectionProductsBasedSearchResults": {
								"itemResultList": {
									"itemIds": ["items_516286-19115479"]
								}
							}
						}
					}`), nil
				case itemsOperationName:
					itemsZoneID = zoneID
					return jsonResponse(r, http.StatusOK, `{
						"data": {
							"items": [
								{"id": "items_516286-19115479", "name": "Black Angus Beef", "productId": "19115479"}
							]
						}
					}`), nil
				default:
					t.Fatalf("unexpected operation: %s", r.URL.Query().Get("operationName"))
					return nil, nil
				}
			}),
		},
	})

	_, err := client.Products(context.Background(), "516286", "n-beef-67693", SearchOptions{})
	require.NoError(t, err)

	assert.NotEmpty(t, collectionZoneID)
	assert.Equal(t, collectionZoneID, itemsZoneID)
}

func TestCollectionProductsReturnsGraphQLErrors(t *testing.T) {
	t.Parallel()

	client := NewClient(ClientConfig{
		BaseURL:        "https://example.test",
		PageViewIDFunc: func() string { return "page-view-id" },
		HTTPClient: &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/idp/v1/init" {
					return initCookieResponse(r), nil
				}
				return jsonResponse(r, http.StatusOK, `{"errors":[{"message":"Not Authenticated","path":["collectionProducts"]}],"data":null}`), nil
			}),
		},
	})

	_, err := client.collectionProducts(context.Background(), "29998", "rc-other-fish-18102", SearchOptions{})
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

	_, err := client.Products(context.Background(), "", "rc-other-fish-18102", SearchOptions{})
	require.ErrorContains(t, err, "store id is required")

	_, err = client.Products(context.Background(), "29998", "", SearchOptions{})
	require.ErrorContains(t, err, "category slug is required")

	_, err = client.Products(context.Background(), "29998", "rc-other-fish-18102", SearchOptions{First: -1})
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

func initCookieResponse(req *http.Request) *http.Response {
	resp := jsonResponse(req, http.StatusOK, `{}`)
	resp.Header.Set("Set-Cookie", "__Host-instacart_sid=init-sid; Path=/; Secure; HttpOnly")
	resp.Header.Add("Set-Cookie", "_instacart_session_id=init-session; Path=/; Secure; HttpOnly")
	return resp
}
