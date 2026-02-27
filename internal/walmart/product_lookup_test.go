package walmart

import (
	"careme/internal/config"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestProductLookup_SetsQueryAndParsesResponse(t *testing.T) {
	t.Parallel()

	_, encodedKey := newBase64RSAPrivateKey(t)

	var capturedReq *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedReq = r
		_, _ = w.Write([]byte(`{
			"items": [
				{
					"itemId": 1234,
					"name": "Honeycrisp Apple",
					"salePrice": 1.99,
					"stock": "Available"
				}
			],
			"totalResults": 1,
			"numItems": 1
		}`))
	}))
	t.Cleanup(server.Close)

	client, err := NewClient(config.WalmartConfig{
		ConsumerID: "consumer-id-123",
		KeyVersion: "1",
		PrivateKey: encodedKey,
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	results, err := client.ProductLookup(context.Background(), []string{"1234", " 5678 ", ""}, "6065")
	if err != nil {
		t.Fatalf("product lookup: %v", err)
	}

	if capturedReq == nil {
		t.Fatal("expected request to be captured")
	}
	if capturedReq.URL.Path != "/items" {
		t.Fatalf("unexpected path: %s", capturedReq.URL.Path)
	}
	if got := capturedReq.URL.Query().Get("ids"); got != "1234,5678" {
		t.Fatalf("unexpected ids query value: %q", got)
	}
	if got := capturedReq.URL.Query().Get("storeId"); got != "6065" {
		t.Fatalf("unexpected storeId query value: %q", got)
	}

	if results == nil || len(results.Items) != 1 {
		t.Fatalf("unexpected results: %+v", results)
	}
	if results.Items[0].Name != "Honeycrisp Apple" {
		t.Fatalf("unexpected item name: %q", results.Items[0].Name)
	}
	if results.Items[0].SalePrice != 1.99 {
		t.Fatalf("unexpected sale price: %f", results.Items[0].SalePrice)
	}
}

func TestProductLookup_Validation(t *testing.T) {
	t.Parallel()

	_, encodedKey := newBase64RSAPrivateKey(t)

	client, err := NewClient(config.WalmartConfig{
		ConsumerID: "consumer-id-123",
		KeyVersion: "1",
		PrivateKey: encodedKey,
		BaseURL:    "https://example.com",
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	_, err = client.ProductLookup(context.Background(), nil, "6065")
	if err == nil || !strings.Contains(err.Error(), "produce ID") {
		t.Fatalf("expected produce ID validation error, got: %v", err)
	}

	_, err = client.ProductLookup(context.Background(), []string{"1234"}, "   ")
	if err == nil || !strings.Contains(err.Error(), "store ID or zip code is required") {
		t.Fatalf("expected store/zip validation error, got: %v", err)
	}
}

func TestProductLookupByZIP_SetsZipQuery(t *testing.T) {
	t.Parallel()

	_, encodedKey := newBase64RSAPrivateKey(t)

	var capturedReq *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedReq = r
		_, _ = w.Write([]byte(`{"items":[{"itemId":1234,"name":"Zip Product"}],"totalResults":1,"numItems":1}`))
	}))
	t.Cleanup(server.Close)

	client, err := NewClient(config.WalmartConfig{
		ConsumerID: "consumer-id-123",
		KeyVersion: "1",
		PrivateKey: encodedKey,
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	results, err := client.ProductLookupByZIP(context.Background(), []string{"1234"}, "98007")
	if err != nil {
		t.Fatalf("product lookup by zip: %v", err)
	}
	if capturedReq == nil {
		t.Fatal("expected request to be captured")
	}
	if got := capturedReq.URL.Query().Get("zip"); got != "98007" {
		t.Fatalf("unexpected zip query value: %q", got)
	}
	if got := capturedReq.URL.Query().Get("storeId"); got != "" {
		t.Fatalf("did not expect storeId query value, got: %q", got)
	}
	if len(results.Items) != 1 || results.Items[0].Name != "Zip Product" {
		t.Fatalf("unexpected results: %+v", results)
	}
}

func TestProductLookup_StatusError(t *testing.T) {
	t.Parallel()

	_, encodedKey := newBase64RSAPrivateKey(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	t.Cleanup(server.Close)

	client, err := NewClient(config.WalmartConfig{
		ConsumerID: "consumer-id-123",
		KeyVersion: "1",
		PrivateKey: encodedKey,
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	_, err = client.ProductLookup(context.Background(), []string{"1234"}, "6065")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "status 401") {
		t.Fatalf("unexpected error: %v", err)
	}

	var statusErr *StatusError
	if !errors.As(err, &statusErr) {
		t.Fatalf("expected StatusError, got: %T", err)
	}
	if statusErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unexpected status code: %d", statusErr.StatusCode)
	}
	if !strings.Contains(statusErr.Body, "nope") {
		t.Fatalf("expected status body to include response body, got: %q", statusErr.Body)
	}
}

func TestProductLookup_NotFoundReturnsEmptySet(t *testing.T) {
	t.Parallel()

	_, encodedKey := newBase64RSAPrivateKey(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	client, err := NewClient(config.WalmartConfig{
		ConsumerID: "consumer-id-123",
		KeyVersion: "1",
		PrivateKey: encodedKey,
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	results, err := client.ProductLookup(context.Background(), []string{"1234"}, "6065")
	if err != nil {
		t.Fatalf("product lookup should not error on 404: %v", err)
	}
	if results == nil {
		t.Fatal("expected empty results, got nil")
	}
	if len(results.Items) != 0 || results.TotalResults != 0 || results.NumItems != 0 {
		t.Fatalf("expected empty results, got: %+v", results)
	}
}

func TestProductLookup_ResultsNotFound6001ReturnsEmptySet(t *testing.T) {
	t.Parallel()

	_, encodedKey := newBase64RSAPrivateKey(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errors":[{"code":6001,"message":"Results not found"}]}`))
	}))
	t.Cleanup(server.Close)

	client, err := NewClient(config.WalmartConfig{
		ConsumerID: "consumer-id-123",
		KeyVersion: "1",
		PrivateKey: encodedKey,
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	results, err := client.ProductLookup(context.Background(), []string{"1234"}, "6065")
	if err != nil {
		t.Fatalf("product lookup should not error on code 6001: %v", err)
	}
	if results == nil {
		t.Fatal("expected empty results, got nil")
	}
	if len(results.Items) != 0 || results.TotalResults != 0 || results.NumItems != 0 {
		t.Fatalf("expected empty results, got: %+v", results)
	}
}

func TestParseProductLookupResults_HandlesArrayAndSingleItem(t *testing.T) {
	t.Parallel()

	arrayPayload := []byte(`[{"itemId":1,"name":"Item One"},{"itemId":2,"name":"Item Two"}]`)
	results, err := ParseProductLookupResults(arrayPayload)
	if err != nil {
		t.Fatalf("parse array payload: %v", err)
	}
	if len(results.Items) != 2 || results.TotalResults != 2 || results.NumItems != 2 {
		t.Fatalf("unexpected array results: %+v", results)
	}

	singlePayload := []byte(`{"itemId":99,"name":"Single Item"}`)
	results, err = ParseProductLookupResults(singlePayload)
	if err != nil {
		t.Fatalf("parse single payload: %v", err)
	}
	if len(results.Items) != 1 || results.Items[0].ItemID != 99 {
		t.Fatalf("unexpected single item results: %+v", results)
	}
}

func TestProductLookupCatalogItem_FallbackToParentItemIDOn400(t *testing.T) {
	t.Parallel()

	_, encodedKey := newBase64RSAPrivateKey(t)

	var requestedIDs []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ids := r.URL.Query().Get("ids")
		requestedIDs = append(requestedIDs, ids)

		switch ids {
		case "111":
			http.Error(w, `{"message":"invalid item id"}`, http.StatusBadRequest)
		case "222":
			_, _ = w.Write([]byte(`{"items":[{"itemId":222,"name":"Resolved Parent"}],"totalResults":1,"numItems":1}`))
		default:
			t.Fatalf("unexpected ids query: %s", ids)
		}
	}))
	t.Cleanup(server.Close)

	client, err := NewClient(config.WalmartConfig{
		ConsumerID: "consumer-id-123",
		KeyVersion: "1",
		PrivateKey: encodedKey,
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	results, err := client.ProductLookupCatalogItem(context.Background(), CatalogProduct{
		ItemID:       111,
		ParentItemID: 222,
	}, "6065")
	if err != nil {
		t.Fatalf("lookup catalog item with fallback: %v", err)
	}
	if len(results.Items) != 1 || results.Items[0].ItemID != 222 {
		t.Fatalf("unexpected lookup result: %+v", results)
	}

	if !reflect.DeepEqual(requestedIDs, []string{"111", "222"}) {
		t.Fatalf("unexpected retry sequence: %+v", requestedIDs)
	}
}

func TestProductLookupCatalogItem_All400ReturnsEmpty(t *testing.T) {
	t.Parallel()

	_, encodedKey := newBase64RSAPrivateKey(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"message":"invalid id"}`, http.StatusBadRequest)
	}))
	t.Cleanup(server.Close)

	client, err := NewClient(config.WalmartConfig{
		ConsumerID: "consumer-id-123",
		KeyVersion: "1",
		PrivateKey: encodedKey,
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	results, err := client.ProductLookupCatalogItem(context.Background(), CatalogProduct{
		ItemID:       111,
		ParentItemID: 222,
		UPC:          "333",
	}, "6065")
	if err != nil {
		t.Fatalf("expected no error when all candidates are rejected with 400, got: %v", err)
	}
	if len(results.Items) != 0 || results.TotalResults != 0 || results.NumItems != 0 {
		t.Fatalf("expected empty results, got: %+v", results)
	}
}

func TestProductLookupCatalogItemByZIP_FallbackToParentItemIDOn400(t *testing.T) {
	t.Parallel()

	_, encodedKey := newBase64RSAPrivateKey(t)

	var requestedIDs []string
	var zips []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ids := r.URL.Query().Get("ids")
		requestedIDs = append(requestedIDs, ids)
		zips = append(zips, r.URL.Query().Get("zip"))

		switch ids {
		case "111":
			http.Error(w, `{"message":"invalid item id"}`, http.StatusBadRequest)
		case "222":
			_, _ = w.Write([]byte(`{"items":[{"itemId":222,"name":"Resolved Parent"}],"totalResults":1,"numItems":1}`))
		default:
			t.Fatalf("unexpected ids query: %s", ids)
		}
	}))
	t.Cleanup(server.Close)

	client, err := NewClient(config.WalmartConfig{
		ConsumerID: "consumer-id-123",
		KeyVersion: "1",
		PrivateKey: encodedKey,
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	})
	if err != nil {
		t.Fatalf("new client: %v", err)
	}

	results, err := client.ProductLookupCatalogItemByZIP(context.Background(), CatalogProduct{
		ItemID:       111,
		ParentItemID: 222,
	}, "98007")
	if err != nil {
		t.Fatalf("lookup catalog item by zip with fallback: %v", err)
	}
	if len(results.Items) != 1 || results.Items[0].ItemID != 222 {
		t.Fatalf("unexpected lookup result: %+v", results)
	}

	if !reflect.DeepEqual(requestedIDs, []string{"111", "222"}) {
		t.Fatalf("unexpected retry sequence: %+v", requestedIDs)
	}
	if !reflect.DeepEqual(zips, []string{"98007", "98007"}) {
		t.Fatalf("unexpected zip usage sequence: %+v", zips)
	}
}
