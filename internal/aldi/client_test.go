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

func TestInStoreShopIDInitializesSessionAndMatchesStoreAddress(t *testing.T) {
	t.Parallel()

	var initCalled bool
	var shopsCookie string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/idp/v1/init":
			if r.Method != http.MethodPost {
				t.Fatalf("unexpected init method: %s", r.Method)
			}
			initCalled = true
			http.SetCookie(w, &http.Cookie{Name: "_instacart_session_id", Value: "session"})
			w.WriteHeader(http.StatusOK)
		case "/idp/v1/shops":
			shopsCookie = r.Header.Get("Cookie")
			if got := r.URL.Query().Get("postal_code"); got != "40222" {
				t.Fatalf("unexpected postal_code: %q", got)
			}
			if err := json.NewEncoder(w).Encode(map[string]any{
				"shops": []Shop{
					{
						ID:                "38764",
						RetailerKey:       "aldi",
						FulfillmentOption: "delivery",
						Address: ShopAddress{
							StreetAddress: "825 S. Hurstbourne Pkwy",
							City:          "Louisville",
							State:         "KY",
							PostalCode:    "40222",
						},
						LocationCode: "444-042",
					},
					{
						ID:                "516286",
						RetailerKey:       "aldi",
						FulfillmentOption: "instore",
						Address: ShopAddress{
							StreetAddress: "825 S Hurstbourne Pkwy",
							City:          "Louisville",
							State:         "KY",
							PostalCode:    "40222",
						},
						LocationCode: "444-042",
					},
				},
			}); err != nil {
				t.Fatalf("encode shops: %v", err)
			}
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	client := NewClientWithBaseURL(server.URL, "test-key", server.Client())
	client.ShopURL = server.URL

	shopID, err := client.InStoreShopID(context.Background(), &StoreSummary{
		ID:      "aldi_F219",
		Address: "825 S. Hurstbourne Pkwy",
		City:    "Louisville",
		State:   "KY",
		ZipCode: "40222",
	})
	if err != nil {
		t.Fatalf("InStoreShopID returned error: %v", err)
	}
	if shopID != "516286" {
		t.Fatalf("unexpected shop id: %q", shopID)
	}
	if !initCalled {
		t.Fatal("expected init endpoint to be called")
	}
	if !strings.Contains(shopsCookie, "_instacart_session_id=session") {
		t.Fatalf("expected shops request to include session cookie, got %q", shopsCookie)
	}
}

func TestInStoreShopIDFallsBackWhenNoInstoreMatch(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/idp/v1/init":
			w.WriteHeader(http.StatusOK)
		case "/idp/v1/shops":
			if err := json.NewEncoder(w).Encode(map[string]any{
				"shops": []Shop{
					{
						ID:                "38764",
						RetailerKey:       "aldi",
						FulfillmentOption: "delivery",
						Address: ShopAddress{
							StreetAddress: "825 S. Hurstbourne Pkwy",
							City:          "Louisville",
							State:         "KY",
							PostalCode:    "40222",
						},
					},
				},
			}); err != nil {
				t.Fatalf("encode shops: %v", err)
			}
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	client := NewClientWithBaseURL(server.URL, "test-key", server.Client())
	client.ShopURL = server.URL

	shopID, err := client.InStoreShopID(context.Background(), &StoreSummary{
		ID:      "aldi_F219",
		Address: "825 S. Hurstbourne Pkwy",
		City:    "Louisville",
		State:   "KY",
		ZipCode: "40222",
	})
	if err != nil {
		t.Fatalf("InStoreShopID returned error: %v", err)
	}
	if shopID != "38764" {
		t.Fatalf("unexpected shop id: %q", shopID)
	}
}

func TestFindInStoreShopPrefersAddressWordMatchBeforeFulfillment(t *testing.T) {
	t.Parallel()

	shop, ok := findInStoreShop(&StoreSummary{
		ID:      "aldi_F219",
		Address: "825 S. Hurstbourne Pkwy",
		City:    "Louisville",
		State:   "KY",
		ZipCode: "40222",
	}, []Shop{
		{
			ID:                "wrong",
			RetailerKey:       "aldi",
			FulfillmentOption: "instore",
			Address: ShopAddress{
				StreetAddress: "4301 Bardstown Rd",
				City:          "Louisville",
				State:         "KY",
				PostalCode:    "40218",
			},
		},
		{
			ID:                "right",
			RetailerKey:       "aldi",
			FulfillmentOption: "instore",
			Address: ShopAddress{
				StreetAddress: "825 South Hurstbourne Parkway",
				City:          "Louisville",
				State:         "KY",
				PostalCode:    "40222",
			},
		},
		{
			ID:                "delivery",
			RetailerKey:       "aldi",
			FulfillmentOption: "delivery",
			Address: ShopAddress{
				StreetAddress: "825 S. Hurstbourne Pkwy",
				City:          "Louisville",
				State:         "KY",
				PostalCode:    "40222",
			},
		},
	})
	if !ok {
		t.Fatal("expected shop match")
	}
	if shop.ID != "delivery" {
		t.Fatalf("unexpected shop id: %q", shop.ID)
	}
}

func TestFindInStoreShopReturnsFalseWhenBestMatchTiesSameFulfillment(t *testing.T) {
	t.Parallel()

	_, ok := findInStoreShop(&StoreSummary{
		ID:      "aldi_F219",
		Address: "825 S. Hurstbourne Pkwy",
		City:    "Louisville",
		State:   "KY",
		ZipCode: "40222",
	}, []Shop{
		{
			ID:                "first",
			RetailerKey:       "aldi",
			FulfillmentOption: "instore",
			Address: ShopAddress{
				StreetAddress: "825 S Hurstbourne Pkwy",
				City:          "Louisville",
				State:         "KY",
				PostalCode:    "40222",
			},
		},
		{
			ID:                "second",
			RetailerKey:       "aldi",
			FulfillmentOption: "instore",
			Address: ShopAddress{
				StreetAddress: "825 S Hurstbourne Pkwy",
				City:          "Louisville",
				State:         "KY",
				PostalCode:    "40222",
			},
		},
		{
			ID:                "delivery",
			RetailerKey:       "aldi",
			FulfillmentOption: "delivery",
			Address: ShopAddress{
				StreetAddress: "825 S. Hurstbourne Pkwy",
				City:          "Louisville",
				State:         "KY",
				PostalCode:    "40222",
			},
		},
	})
	if ok {
		t.Fatal("expected tied same-fulfillment match to fail")
	}
}

func TestFindInStoreShopFulfillmentBreaksAddressScoreTie(t *testing.T) {
	t.Parallel()

	shop, ok := findInStoreShop(&StoreSummary{
		ID:      "aldi_F219",
		Address: "825 S. Hurstbourne Pkwy",
		City:    "Louisville",
		State:   "KY",
		ZipCode: "40222",
	}, []Shop{
		{
			ID:                "delivery-one",
			RetailerKey:       "aldi",
			FulfillmentOption: "delivery",
			Address: ShopAddress{
				StreetAddress: "825 S Hurstbourne Pkwy",
				City:          "Louisville",
				State:         "KY",
				PostalCode:    "40222",
			},
		},
		{
			ID:                "delivery-two",
			RetailerKey:       "aldi",
			FulfillmentOption: "delivery",
			Address: ShopAddress{
				StreetAddress: "825 S. Hurstbourne Pkwy",
				City:          "Louisville",
				State:         "KY",
				PostalCode:    "40222",
			},
		},
		{
			ID:                "instore",
			RetailerKey:       "aldi",
			FulfillmentOption: "instore",
			Address: ShopAddress{
				StreetAddress: "825 S Hurstbourne Pkwy",
				City:          "Louisville",
				State:         "KY",
				PostalCode:    "40222",
			},
		},
	})
	if !ok {
		t.Fatal("expected shop match")
	}
	if shop.ID != "instore" {
		t.Fatalf("unexpected shop id: %q", shop.ID)
	}
}
