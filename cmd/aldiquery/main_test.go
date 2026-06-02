package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"careme/internal/aldi/query"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunRequiresStoreID(t *testing.T) {
	t.Parallel()

	err := run(context.Background(), []string{"-category", "rc-other-fish-18102"}, ioDiscard{})
	require.ErrorContains(t, err, "store-id is required")
}

func TestRunRequiresCategory(t *testing.T) {
	t.Parallel()

	err := run(context.Background(), []string{"-store-id", "29998"}, ioDiscard{})
	require.ErrorContains(t, err, "slug is required")
}

func TestRunPrintsProducts(t *testing.T) {
	clearCookieEnv(t)

	var capturedReq *http.Request
	originalHTTPClient := newHTTPClient
	t.Cleanup(func() { newHTTPClient = originalHTTPClient })
	newHTTPClient = func(time.Duration) *http.Client {
		return &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/idp/v1/init" {
					resp := &http.Response{
						StatusCode: http.StatusOK,
						Header: http.Header{
							"Content-Type": []string{"application/json"},
							"Set-Cookie":   []string{"__Host-instacart_sid=init-sid; Path=/; Secure; HttpOnly"},
						},
						Body:    io.NopCloser(strings.NewReader(`{}`)),
						Request: r,
					}
					return resp, nil
				}
				capturedReq = r
				return &http.Response{
					StatusCode: http.StatusOK,
					Header: http.Header{
						"Content-Type": []string{"application/json"},
					},
					Body: io.NopCloser(strings.NewReader(`{
						"data": {
							"collectionProducts": {
								"items": [
									{
										"name": "Sea Queen Fresh Atlantic Salmon Portions",
										"size": "per lb",
										"productId": "17771058",
										"availability": {"available": true, "stockLevel": "highlyInStock"},
										"price": {
											"viewSection": {
												"itemCard": {
													"priceString": "$9.49 /pkg (est.)",
													"pricingUnitString": "$9.49 / lb"
												},
												"priceString": "$9.49"
											}
										}
									}
								]
							}
						}
					}`)),
					Request: r,
				}, nil
			}),
		}
	}

	var out bytes.Buffer
	err := run(context.Background(), []string{
		"-base-url", "https://example.test",
		"-store-id", "29998",
		"-slug", "rc-other-fish-18102",
		"-postal-code", "60174",
		"-zone-id", "384",
		"-instacart-sid", "instacart-sid",
		"-first", "12",
	}, &out)
	require.NoError(t, err)
	require.NotNil(t, capturedReq)

	assert.Contains(t, out.String(), "1: Sea Queen Fresh Atlantic Salmon Portions (per lb) - $9.49 /pkg (est.) [$9.49 / lb] highlyInStock product=17771058")
	assert.Contains(t, out.String(), "total products: 1")

	cookie, err := capturedReq.Cookie("__Host-instacart_sid")
	require.NoError(t, err)
	assert.Equal(t, "instacart-sid", cookie.Value)
	assert.Equal(t, "CollectionProductsWithFeaturedProducts", capturedReq.URL.Query().Get("operationName"))
	assert.Contains(t, capturedReq.URL.Query().Get("variables"), `"postalCode":"60174"`)
	assert.Contains(t, capturedReq.URL.Query().Get("variables"), `"zoneId":"384"`)
	assert.Contains(t, capturedReq.URL.Query().Get("variables"), `"itemsDisplayType":"collections_all_items_grid"`)
	assert.Contains(t, capturedReq.URL.Query().Get("variables"), `"first":12`)
}

func TestRunPrintsSearchResultItemIDs(t *testing.T) {
	clearCookieEnv(t)

	var capturedReq *http.Request
	originalHTTPClient := newHTTPClient
	t.Cleanup(func() { newHTTPClient = originalHTTPClient })
	newHTTPClient = func(time.Duration) *http.Client {
		return &http.Client{
			Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/idp/v1/init" {
					return &http.Response{
						StatusCode: http.StatusOK,
						Header: http.Header{
							"Content-Type": []string{"application/json"},
							"Set-Cookie":   []string{"__Host-instacart_sid=init-sid; Path=/; Secure; HttpOnly"},
						},
						Body:    io.NopCloser(strings.NewReader(`{}`)),
						Request: r,
					}, nil
				}
				capturedReq = r
				return &http.Response{
					StatusCode: http.StatusOK,
					Header: http.Header{
						"Content-Type": []string{"application/json"},
					},
					Body: io.NopCloser(strings.NewReader(`{
						"data": {
							"collectionProductsBasedSearchResults": {
								"itemResultList": {
									"featuredProducts": [],
									"itemIds": ["items_516286-17771058", "items_516286-123"]
								}
							}
						}
					}`)),
					Request: r,
				}, nil
			}),
		}
	}

	var out bytes.Buffer
	err := run(context.Background(), []string{
		"-base-url", "https://example.test",
		"-store-id", "516286",
		"-category", "n-beef-67693",
	}, &out)
	require.NoError(t, err)
	require.NotNil(t, capturedReq)

	assert.Contains(t, out.String(), "1: item_id=items_516286-17771058")
	assert.Contains(t, out.String(), "2: item_id=items_516286-123")
	assert.Contains(t, out.String(), "total products: 2")
	cookie, err := capturedReq.Cookie("__Host-instacart_sid")
	require.NoError(t, err)
	assert.Equal(t, "init-sid", cookie.Value)
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) {
	return len(p), nil
}

func clearCookieEnv(t *testing.T) {
	t.Setenv("ALDI_COOKIE", "")
	t.Setenv("ALDI_INSTACART_SID", "")
	t.Setenv("ALDI_INSTACART_SESSION_ID", "")
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestDisplayPriceFallsBackToPlainPrice(t *testing.T) {
	t.Parallel()

	item := query.Item{}
	item.Price.ViewSection.PriceString = "$3.49"
	assert.Equal(t, "$3.49", displayPrice(item))
}
