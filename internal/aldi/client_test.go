package aldi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStoreSummariesBuildsRequestAndNormalizesResponse(t *testing.T) {
	t.Parallel()

	var capturedReq *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedReq = r
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"status": "SUCCESS",
			"response": map[string]any{
				"locations": []SourceLocation{
					{
						ID:              5757831,
						Identifier:      "F100",
						Name:            "ALDI",
						StreetAndNumber: "201 W Division St",
						City:            "Chicago",
						Province:        "Illinois",
						Zip:             "60610",
						Lat:             41.894989,
						Lng:             -87.629197,
					},
				},
			},
		}); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	client := NewClientWithBaseURL(server.URL, "test-key", server.Client())

	summaries, err := client.StoreSummaries(context.Background())
	if err != nil {
		t.Fatalf("StoreSummaries returned error: %v", err)
	}

	if capturedReq == nil {
		t.Fatal("expected request to be captured")
	}
	if capturedReq.URL.Path != "/api/locators/test-key/locations/all" {
		t.Fatalf("unexpected path: %s", capturedReq.URL.Path)
	}
	if got := capturedReq.URL.Query().Get("language"); got != DefaultLanguage {
		t.Fatalf("unexpected language query value: %q", got)
	}
	if got := capturedReq.URL.Query().Get("country"); got != DefaultCountry {
		t.Fatalf("unexpected country query value: %q", got)
	}
	if got := capturedReq.URL.Query().Get("fieldMask"); got != "" {
		t.Fatalf("expected no fieldMask query value, got %q", got)
	}
	if got := capturedReq.Header.Get("Accept"); got != "application/json" {
		t.Fatalf("unexpected Accept header: %q", got)
	}

	if len(summaries) != 1 {
		t.Fatalf("expected 1 summary, got %d", len(summaries))
	}
	if got := summaries[0].ID; got != "aldi_F100" {
		t.Fatalf("unexpected summary id: %q", got)
	}
	if got := summaries[0].State; got != "IL" {
		t.Fatalf("unexpected state: %q", got)
	}
	if got := summaries[0].Name; got != "ALDI 201 W Division St" {
		t.Fatalf("unexpected name: %q", got)
	}
}

func TestStoreSummariesIncludesAddressExtra(t *testing.T) {
	t.Parallel()

	extra := "Ste 300"
	summary, err := normalizeLocation(SourceLocation{
		ID:              5757831,
		Identifier:      "F216",
		Name:            "ALDI",
		StreetAndNumber: "1951 S. Mccall Rd #300",
		AddressExtra:    &extra,
		City:            "Englewood",
		Province:        "FL",
		Zip:             "34224",
	})
	if err != nil {
		t.Fatalf("normalizeLocation returned error: %v", err)
	}

	if got := summary.Address; got != "1951 S. Mccall Rd #300, Ste 300" {
		t.Fatalf("unexpected address: %q", got)
	}
}

func TestStoreSummariesRequiresIdentifier(t *testing.T) {
	t.Parallel()

	_, err := normalizeLocation(SourceLocation{
		Name:            "ALDI",
		StreetAndNumber: "201 W Division St",
		Province:        "IL",
		Zip:             "60610",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "missing identifier") {
		t.Fatalf("unexpected error: %v", err)
	}
}
