package kroger

import (
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"careme/internal/kroger/products"
)

func TestIdentityProviderSignature_UsesJSONStaples(t *testing.T) {
	got := NewIdentityProvider().Signature()
	want := mustJSONSignature(defaultStaples())

	if got != want {
		t.Fatalf("unexpected signature: got %q want %q", got, want)
	}
}

func TestParseProductGetResponse_KeepsJSON400Body(t *testing.T) {
	rsp := httptest.NewRecorder()
	rsp.Header().Set("Content-Type", "application/json")
	rsp.WriteHeader(http.StatusBadRequest)
	_, _ = rsp.WriteString(`{"errors":{"timestamp":1776969026460,"code":"PRODUCT-2011","reason":"Field 'locationId' must have a length of 8 alphanumeric characters"}}`)

	parsed, err := products.ParseProductGetResponse(rsp.Result())
	if err != nil {
		t.Fatalf("ParseProductGetResponse returned error: %v", err)
	}
	if parsed.JSON400 == nil {
		t.Fatalf("expected JSON400 marker, got %+v", parsed.JSON400)
	}
	if got := string(parsed.Body); !strings.Contains(got, "PRODUCT-2011") || !strings.Contains(got, "length of 8") {
		t.Fatalf("expected raw body to include kroger error details, got %q", got)
	}
}

func TestParseProductGetResponse_IgnoresUnusedPriceDateTimes(t *testing.T) {
	rsp := httptest.NewRecorder()
	rsp.Header().Set("Content-Type", "application/json")
	rsp.WriteHeader(http.StatusOK)
	_, _ = rsp.WriteString(`{
		"data": [{
			"items": [{
				"price": {
					"expirationDate": {"value": "9999-12-31T00:00:00Z", "timezone": "UTC"},
					"effectiveDate": {"value": "2026-04-29T03:59:59.999Z", "timezone": "UTC"}
				}
			}]
		}]
	}`)

	parsed, err := products.ParseProductGetResponse(rsp.Result())
	if err != nil {
		t.Fatalf("ParseProductGetResponse returned error: %v", err)
	}
	if parsed.JSON200 == nil || parsed.JSON200.Data == nil {
		t.Fatalf("expected JSON200 payload, got %+v", parsed.JSON200)
	}
}

func TestParseProductGetResponse_IgnoresUnusedNutritionInformationArray(t *testing.T) {
	rsp := httptest.NewRecorder()
	rsp.Header().Set("Content-Type", "application/json")
	rsp.WriteHeader(http.StatusOK)
	_, _ = rsp.WriteString(`{
		"data": [{
			"nutritionInformation": [{
				"ingredientStatement": "beef"
			}]
		}]
	}`)

	parsed, err := products.ParseProductGetResponse(rsp.Result())
	if err != nil {
		t.Fatalf("ParseProductGetResponse returned error: %v", err)
	}
	if parsed.JSON200 == nil || parsed.JSON200.Data == nil {
		t.Fatalf("expected JSON200 payload, got %+v", parsed.JSON200)
	}
}

func TestSearchIngredients_RetriesTransientProductFailures(t *testing.T) {
	var calls atomic.Int32
	baseClient := &http.Client{Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		call := calls.Add(1)
		if call < 3 {
			return jsonResponse(req, http.StatusServiceUnavailable, `{"errors":{"code":"PRODUCT-4109-500","reason":"Service Unavailable"}}`), nil
		}
		return jsonResponse(req, http.StatusOK, `{"data":[]}`), nil
	})}

	client, err := products.NewClientWithResponses(
		"https://kroger.test",
		products.WithHTTPClient(withRetries(baseClient)),
	)
	if err != nil {
		t.Fatalf("NewClientWithResponses returned error: %v", err)
	}

	got, err := searchIngredients(t.Context(), client, "70500874", "pork", []string{"*"}, false, 0)
	if err != nil {
		t.Fatalf("searchIngredients returned error after transient 503s: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty ingredient list from test payload, got %+v", got)
	}
	if gotCalls := calls.Load(); gotCalls != 3 {
		t.Fatalf("expected 2 retries before success, got %d calls", gotCalls)
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (fn roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func jsonResponse(req *http.Request, statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Status:     http.StatusText(statusCode),
		Header:     http.Header{"Content-Type": []string{"application/json"}, "Retry-After": []string{"0"}},
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}
}

func TestProductSearchErrorPayloadPrefersRawBody(t *testing.T) {
	resp := &products.ProductGetResponse{
		Body: []byte(`{"errors":{"code":"PRODUCT-2011","reason":"Field 'locationId' must have a length of 8 alphanumeric characters"}}`),
	}

	payload := productSearchErrorPayload(resp)
	if payload == nil {
		t.Fatal("expected non-nil payload")
	}
	if !strings.Contains(krogerError(http.StatusBadRequest, payload).Error(), "PRODUCT-2011") {
		t.Fatalf("expected krogerError to include decoded payload, got %v", krogerError(http.StatusBadRequest, payload))
	}
}

func TestInputIngredientFromKrogerIngredientMapsFields(t *testing.T) {
	regular := float32(4.99)
	sale := float32(3.49)
	categories := []string{"Produce", "Fresh Fruit"}
	ingredient := inputIngredientFromKrogerIngredient(Ingredient{
		ProductId:    stringPtr(" apple-1 "),
		AisleNumber:  stringPtr(" 12 "),
		Brand:        stringPtr(" Orchard Co "),
		Description:  stringPtr(" Honeycrisp Apple "),
		Size:         stringPtr(" 3 lb "),
		PriceRegular: &regular,
		PriceSale:    &sale,
		Categories:   &categories,
	}, 0)

	if ingredient.ProductID != "apple-1" {
		t.Fatalf("unexpected product id: %+v", ingredient)
	}
	if ingredient.AisleNumber != "12" || ingredient.Brand != "Orchard Co" || ingredient.Description != "Honeycrisp Apple" || ingredient.Size != "3 lb" {
		t.Fatalf("unexpected normalized ingredient: %+v", ingredient)
	}
	if ingredient.PriceRegular == nil || *ingredient.PriceRegular != regular {
		t.Fatalf("unexpected regular price: %+v", ingredient.PriceRegular)
	}
	if ingredient.PriceSale == nil || *ingredient.PriceSale != sale {
		t.Fatalf("unexpected sale price: %+v", ingredient.PriceSale)
	}
	if !slices.Equal(ingredient.Categories, categories) {
		t.Fatalf("unexpected categories: got %v want %v", ingredient.Categories, categories)
	}
}

func stringPtr(value string) *string {
	return &value
}
