package kroger

import (
	"net/http"
	"net/http/httptest"
	"strings"
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
