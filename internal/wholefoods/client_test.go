package wholefoods

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
	if got := capturedReq.Header.Get("Accept"); got != "application/json" {
		t.Fatalf("unexpected Accept header: %q", got)
	}

	if len(resp.Facets) != 3 {
		t.Fatalf("unexpected facets count: %d", len(resp.Facets))
	}
	if len(resp.Breadcrumb) != 2 {
		t.Fatalf("unexpected breadcrumb count: %d", len(resp.Breadcrumb))
	}
	if got := resp.Breadcrumb[1].Slug; got != "beef" {
		t.Fatalf("unexpected breadcrumb slug: %q", got)
	}
	if got := resp.Meta.Total.Value; got != 18 {
		t.Fatalf("unexpected total value: %d", got)
	}
	if len(resp.Results) != 18 {
		t.Fatalf("unexpected results count: %d", len(resp.Results))
	}
	if got := resp.Results[0].Name; got != "Organic Ground Beef 93% Lean/7% Fat, 16 OZ" {
		t.Fatalf("unexpected first result name: %q", got)
	}
	if got := resp.Results[14].SalePrice; got != 12.44 {
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

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()

	path := filepath.Join("..", "..", "wf-output", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return data
}
