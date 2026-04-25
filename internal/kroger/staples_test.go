package kroger

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIdentityProviderSignature_UsesJSONStaples(t *testing.T) {
	got := NewIdentityProvider().Signature()
	want := mustJSONSignature(defaultStaples())

	if got != want {
		t.Fatalf("unexpected signature: got %q want %q", got, want)
	}
}

func TestParseProductSearchResponse_DecodesStructuredJSON400(t *testing.T) {
	rsp := httptest.NewRecorder()
	rsp.Header().Set("Content-Type", "application/json")
	rsp.WriteHeader(http.StatusBadRequest)
	_, _ = rsp.WriteString(`{"errors":{"timestamp":1776969026460,"code":"PRODUCT-2011","reason":"Field 'locationId' must have a length of 8 alphanumeric characters"}}`)

	parsed, err := ParseProductSearchResponse(rsp.Result())
	if err != nil {
		t.Fatalf("ParseProductSearchResponse returned error: %v", err)
	}
	if parsed.JSON400 == nil || parsed.JSON400.Errors == nil {
		t.Fatalf("expected JSON400 error payload, got %+v", parsed.JSON400)
	}
	if got, want := toStr(parsed.JSON400.Errors.Code), "PRODUCT-2011"; got != want {
		t.Fatalf("unexpected error code: got %q want %q", got, want)
	}
	if got := toStr(parsed.JSON400.Errors.Reason); !strings.Contains(got, "length of 8") {
		t.Fatalf("unexpected error reason: %q", got)
	}
}

func TestProductSearchErrorPayloadPrefersDecodedJSON400(t *testing.T) {
	code := "PRODUCT-2011"
	reason := "Field 'locationId' must have a length of 8 alphanumeric characters"
	resp := &ProductSearchResponse{
		JSON400: &struct {
			Errors *struct {
				Code      *string  `json:"code,omitempty"`
				Reason    *string  `json:"reason,omitempty"`
				Timestamp *float32 `json:"timestamp,omitempty"`
			} `json:"errors,omitempty"`
		}{
			Errors: &struct {
				Code      *string  `json:"code,omitempty"`
				Reason    *string  `json:"reason,omitempty"`
				Timestamp *float32 `json:"timestamp,omitempty"`
			}{
				Code:   &code,
				Reason: &reason,
			},
		},
	}

	payload := productSearchErrorPayload(resp)
	if payload == nil {
		t.Fatal("expected non-nil payload")
	}
	if !strings.Contains(krogerError(http.StatusBadRequest, payload).Error(), "PRODUCT-2011") {
		t.Fatalf("expected krogerError to include decoded payload, got %v", krogerError(http.StatusBadRequest, payload))
	}
}
