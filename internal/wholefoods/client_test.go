package wholefoods

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestCategory_BuildsRequestAndDecodesFixture(t *testing.T) {
	t.Parallel()

	fixture := loadFixture(t, "beef.json")

	var capturedReq *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedReq = r
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture)
	}))
	t.Cleanup(server.Close)

	client := NewClientWithBaseURL(server.URL, server.Client())

	resp, err := client.Category(context.Background(), "beef", "10216")
	if err != nil {
		t.Fatalf("Category returned error: %v", err)
	}

	if capturedReq == nil {
		t.Fatal("expected request to be captured")
	}
	if capturedReq.URL.Path != "/api/products/category/beef" {
		t.Fatalf("unexpected path: %s", capturedReq.URL.Path)
	}
	if got := capturedReq.URL.Query().Get("store"); got != "10216" {
		t.Fatalf("unexpected store query value: %q", got)
	}
	if got := capturedReq.URL.Query().Get("limit"); got != "60" {
		t.Fatalf("unexpected limit query value: %q", got)
	}
	if got := capturedReq.URL.Query().Get("offset"); got != "0" {
		t.Fatalf("unexpected offset query value: %q", got)
	}
	if got := capturedReq.Header.Get("Accept"); got != "application/json" {
		t.Fatalf("unexpected Accept header: %q", got)
	}

	if len(resp) != 18 {
		t.Fatalf("unexpected results count: %d", len(resp))
	}
	if got := resp[0].Name; got != "Organic Ground Beef 93% Lean/7% Fat, 16 OZ" {
		t.Fatalf("unexpected first result name: %q", got)
	}
	if got := resp[14].SalePrice; got != 12.44 {
		t.Fatalf("unexpected sale price: %v", got)
	}
}

func TestCategory_RequiresQuerytermAndStore(t *testing.T) {
	t.Parallel()

	client := NewClient(nil)

	_, err := client.Category(context.Background(), "", "10216")
	if err == nil || !strings.Contains(err.Error(), "queryterm is required") {
		t.Fatalf("unexpected queryterm error: %v", err)
	}

	_, err = client.Category(context.Background(), "beef", "")
	if err == nil || !strings.Contains(err.Error(), "store is required") {
		t.Fatalf("unexpected store error: %v", err)
	}
}

func TestCategory_StatusError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadGateway)
	}))
	t.Cleanup(server.Close)

	client := NewClientWithBaseURL(server.URL, server.Client())

	_, err := client.Category(context.Background(), "beef", "10216")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "status 502") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCategory_PaginatesUntilShortPage(t *testing.T) {
	t.Parallel()

	var offsets []int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		offset, err := strconv.Atoi(r.URL.Query().Get("offset"))
		if err != nil {
			t.Fatalf("parse offset: %v", err)
		}
		offsets = append(offsets, offset)

		limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
		if err != nil {
			t.Fatalf("parse limit: %v", err)
		}
		if limit != defaultCategoryLimit {
			t.Fatalf("unexpected limit: %d", limit)
		}

		pageSize := limit
		if offset >= limit*2 {
			pageSize = 5
		}

		results := make([]Product, 0, pageSize)
		for i := 0; i < pageSize; i++ {
			n := offset + i
			results = append(results, Product{
				Name:  fmt.Sprintf("Product %d", n),
				Slug:  fmt.Sprintf("product-%d", n),
				Brand: "Whole Foods Market",
				Store: 10216,
			})
		}

		resp := CategoryResponse{
			Breadcrumb: []Breadcrumb{{Label: "Meat", Slug: "meat"}, {Label: "Fish", Slug: "fish"}},
			Meta: Meta{
				Total: Total{Value: limit*2 + 5, Relation: "eq"},
			},
			Results: results,
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			t.Fatalf("encode response: %v", err)
		}
	}))
	t.Cleanup(server.Close)

	client := NewClientWithBaseURL(server.URL, server.Client())

	resp, err := client.Category(context.Background(), "fish", "10216")
	if err != nil {
		t.Fatalf("Category returned error: %v", err)
	}

	if got, want := offsets, []int{0, defaultCategoryLimit, defaultCategoryLimit * 2}; !slices.Equal(got, want) {
		t.Fatalf("unexpected offsets: got %v want %v", got, want)
	}
	if got, want := len(resp), defaultCategoryLimit*2+5; got != want {
		t.Fatalf("unexpected result count: got %d want %d", got, want)
	}
	if got := resp[0].Slug; got != "product-0" {
		t.Fatalf("unexpected first slug: %q", got)
	}
	if got := resp[len(resp)-1].Slug; got != "product-124" {
		t.Fatalf("unexpected last slug: %q", got)
	}
}

func TestStoreSummary_BuildsRequestAndDecodesResponse(t *testing.T) {
	t.Parallel()

	fixture := []byte(`{"storeId":10216,"token":"westlake","displayName":"Westlake","status":"Open","phone":"(206) 621-9700","storePrimeEligibility":true,"storeOperationalGuidance":"","bu":10216,"folder":"westlake","openedAt":"2006-11-08T12:00:00Z","links":{"Details":"/stores/westlake","Directions":"https://www.google.com/maps/dir/?api=1&destination=47.618249,-122.337898","Sales":"/sales-flyer?store-id=10216","PrimeNowPickUpAndDelivery":"https://www.wholefoods.com/grocery?ref_=US_TRF_ALL_UFG_WFM_REFER_0417726","MapUrlDesktop":"https://maps.googleapis.com/maps/api/staticmap?zoom=16&size=780x543","MapUrlTablet":"https://maps.googleapis.com/maps/api/staticmap?zoom=16&size=780x458","MapUrlMobile":"https://maps.googleapis.com/maps/api/staticmap?zoom=17&size=780x228"},"primaryLocation":{"address":{"STREET_ADDRESS_LINE1":"2210 Westlake Ave","CITY":"Seattle","STATE":"WA","POSTAL_CODE":"98121","ZIP_CODE":"98121","COUNTRY":"United States of America"},"latitude":47.618249,"longitude":-122.337898},"hours":{"Open":"8 am – 10 pm today","Sat":"8 am – 10 pm","Sun":"8 am – 10 pm","Mon":"8 am – 10 pm","Tue":"8 am – 10 pm","Wed":"8 am – 10 pm","Thu":"8 am – 10 pm"},"holidays":{}}`)

	var capturedReq *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedReq = r
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture)
	}))
	t.Cleanup(server.Close)

	client := NewClientWithBaseURL(server.URL, server.Client())

	resp, err := client.StoreSummary(context.Background(), "10216")
	if err != nil {
		t.Fatalf("StoreSummary returned error: %v", err)
	}

	if capturedReq == nil {
		t.Fatal("expected request to be captured")
	}
	if capturedReq.URL.Path != "/api/stores/10216/summary" {
		t.Fatalf("unexpected path: %s", capturedReq.URL.Path)
	}
	if got := capturedReq.Header.Get("Accept"); got != "application/json" {
		t.Fatalf("unexpected Accept header: %q", got)
	}

	if got := resp.StoreID; got != 10216 {
		t.Fatalf("unexpected store id: %d", got)
	}
	if got := resp.DisplayName; got != "Westlake" {
		t.Fatalf("unexpected display name: %q", got)
	}
	if got := resp.PrimaryLocation.Address.City; got != "Seattle" {
		t.Fatalf("unexpected city: %q", got)
	}
	if got := resp.PrimaryLocation.Latitude; got != 47.618249 {
		t.Fatalf("unexpected latitude: %v", got)
	}
	if got := resp.Hours["Open"]; got != "8 am – 10 pm today" {
		t.Fatalf("unexpected open hours: %q", got)
	}
}

func TestStoreSummary_RequiresStore(t *testing.T) {
	t.Parallel()

	client := NewClient(nil)

	_, err := client.StoreSummary(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "store is required") {
		t.Fatalf("unexpected store error: %v", err)
	}
}

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()

	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}
