package wegmans

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"testing"
)

func TestStoreSummaryBuildsRequestAndNormalizesResponse(t *testing.T) {
	t.Parallel()

	var capturedReq *http.Request
	client := NewClientWithBaseURL("https://example.com", &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			capturedReq = r
			body, err := json.Marshal(StoreResponse{
				ID:                69,
				StoreNumber:       69,
				Name:              "Erie West",
				City:              "Erie",
				StateAbbreviation: "PA",
				Zip:               "16506-1234",
				StreetAddress:     "5028 West Ridge Road",
				Latitude:          42.06996,
				Longitude:         -80.1919,
			})
			if err != nil {
				return nil, err
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       io.NopCloser(bytes.NewReader(body)),
			}, nil
		}),
	})
	summary, err := client.StoreSummary(context.Background(), 69)
	if err != nil {
		t.Fatalf("StoreSummary returned error: %v", err)
	}

	if capturedReq == nil {
		t.Fatal("expected request to be captured")
	}
	if capturedReq.URL.Path != "/api/stores/store-number/69" {
		t.Fatalf("unexpected path: %s", capturedReq.URL.Path)
	}
	if got := capturedReq.Header.Get("Accept"); got != "application/json" {
		t.Fatalf("unexpected Accept header: %q", got)
	}

	if got := summary.ID; got != "wegmans_69" {
		t.Fatalf("unexpected summary id: %q", got)
	}
	if got := summary.Name; got != "Wegmans Erie West" {
		t.Fatalf("unexpected summary name: %q", got)
	}
	if got := summary.ZipCode; got != "16506" {
		t.Fatalf("unexpected zip code: %q", got)
	}
	if summary.Lat == nil || summary.Lon == nil {
		t.Fatalf("expected coordinates to be populated: %+v", summary)
	}
}

func TestStoreSummaryReturnsNotFoundOn404(t *testing.T) {
	t.Parallel()

	client := NewClientWithBaseURL("https://example.com", &http.Client{
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusNotFound,
				Body:       io.NopCloser(bytes.NewReader(nil)),
			}, nil
		}),
	})
	_, err := client.StoreSummary(context.Background(), 0)
	if !errors.Is(err, ErrStoreNotFound) {
		t.Fatalf("expected ErrStoreNotFound, got %v", err)
	}
}

func TestNormalizeStoreRequiresName(t *testing.T) {
	t.Parallel()

	_, err := normalizeStore(StoreResponse{
		ID:                69,
		StoreNumber:       69,
		StateAbbreviation: "PA",
		Zip:               "16506",
		StreetAddress:     "5028 West Ridge Road",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); got != "missing store name" {
		t.Fatalf("unexpected error: %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
