package publix

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResolveStoreRedirectsToCanonicalURL(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/locations/1083":
			http.Redirect(w, r, "/locations/1083-publix-at-university-town-center", http.StatusMovedPermanently)
		case "/locations/1083-publix-at-university-town-center":
			_, _ = w.Write([]byte("ok"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	client := NewClientWithBaseURL(server.URL, server.Client())
	probe, err := client.ResolveStore(context.Background(), "1083")
	if err != nil {
		t.Fatalf("ResolveStore returned error: %v", err)
	}

	if !probe.Exists {
		t.Fatalf("expected store to exist: %+v", probe)
	}
	if got, want := probe.URL, server.URL+"/locations/1083-publix-at-university-town-center"; got != want {
		t.Fatalf("unexpected canonical url: got %q want %q", got, want)
	}
}

func TestResolveStoreDetectsMissingRedirect(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/locations/2000" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/locations", http.StatusFound)
	}))
	t.Cleanup(server.Close)

	client := NewClientWithBaseURL(server.URL, server.Client())
	probe, err := client.ResolveStore(context.Background(), "2000")
	if err != nil {
		t.Fatalf("ResolveStore returned error: %v", err)
	}

	if probe.Exists {
		t.Fatalf("expected store miss, got %+v", probe)
	}
}

func TestResolveStoreRequiresNumericID(t *testing.T) {
	t.Parallel()

	client := NewClient(nil)
	_, err := client.ResolveStore(context.Background(), "abc")
	if err == nil || !strings.Contains(err.Error(), "must be numeric") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStoreSummaryFetchesAndParsesCanonicalPage(t *testing.T) {
	t.Parallel()

	var requestedPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		_, _ = w.Write([]byte(sampleStoreHTML))
	}))
	t.Cleanup(server.Close)

	client := NewClientWithBaseURL(server.URL, server.Client())
	summary, err := client.StoreSummary(context.Background(), server.URL+"/locations/1083-publix-at-university-town-center")
	if err != nil {
		t.Fatalf("StoreSummary returned error: %v", err)
	}

	if requestedPath != "/locations/1083-publix-at-university-town-center" {
		t.Fatalf("unexpected request path: %q", requestedPath)
	}
	if summary.StoreID != "1083" || summary.ID != "publix_1083" {
		t.Fatalf("unexpected identifiers: %+v", summary)
	}
	if summary.ZipCode != "35401" {
		t.Fatalf("unexpected zip code: %q", summary.ZipCode)
	}
	if summary.Lat == nil || *summary.Lat != 33.212097 || summary.Lon == nil || *summary.Lon != -87.553585 {
		t.Fatalf("unexpected coordinates: %+v", summary)
	}
}

func TestExtractStoreSummaryParsesEmbeddedStorePayload(t *testing.T) {
	t.Parallel()

	summary, err := ExtractStoreSummary("https://www.publix.com/locations/1083-publix-at-university-town-center", []byte(sampleStoreHTML))
	if err != nil {
		t.Fatalf("ExtractStoreSummary returned error: %v", err)
	}

	if summary.Name != "Publix at University Town Center" {
		t.Fatalf("unexpected name: %q", summary.Name)
	}
	if summary.Address != "1190 University Blvd" || summary.State != "AL" || summary.City != "Tuscaloosa" {
		t.Fatalf("unexpected address fields: %+v", summary)
	}
}

func TestExtractStoreSummaryErrorsWhenPayloadMissing(t *testing.T) {
	t.Parallel()

	_, err := ExtractStoreSummary("https://www.publix.com/locations/1083", []byte("<html><body>no store here</body></html>"))
	if err == nil || !strings.Contains(err.Error(), "payload not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStoreProductsSavingsBuildsRequestAndDecodesProducts(t *testing.T) {
	t.Parallel()

	var requestedPath string
	var requestedQuery string
	var requestBody storeProductsSavingsGraphQLRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		requestedQuery = r.URL.RawQuery
		if got, want := r.Method, http.MethodPost; got != want {
			t.Fatalf("unexpected method: got %q want %q", got, want)
		}
		if got, want := r.Header.Get("Content-Type"), "application/json"; got != want {
			t.Fatalf("unexpected content type: got %q want %q", got, want)
		}
		if got, want := r.Header.Get("Accept"), "*/*"; got != want {
			t.Fatalf("unexpected accept: got %q want %q", got, want)
		}
		if got, want := r.Header.Get("Accept-Language"), "en-US,en;q=0.9"; got != want {
			t.Fatalf("unexpected accept language: got %q want %q", got, want)
		}
		if got, want := r.Header.Get("Origin"), DefaultBaseURL; got != want {
			t.Fatalf("unexpected origin: got %q want %q", got, want)
		}
		if got, want := r.Header.Get("Referer"), DefaultBaseURL+"/"; got != want {
			t.Fatalf("unexpected referer: got %q want %q", got, want)
		}
		if got, want := r.Header.Get("PublixStore"), "1847"; got != want {
			t.Fatalf("unexpected publix store: got %q want %q", got, want)
		}
		if got, want := r.Header.Get("User-Agent"), storeProductsSavingsUserAgent; got != want {
			t.Fatalf("unexpected user agent: got %q want %q", got, want)
		}
		if got, want := r.Header.Get("X-Src"), storeProductsSavingsXSrc; got != want {
			t.Fatalf("unexpected x-src: got %q want %q", got, want)
		}
		if got, want := r.Header.Get("Cookie"), "_abck=token-value; bm_sv=bm-token"; got != want {
			t.Fatalf("unexpected cookie: got %q want %q", got, want)
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		if err := json.Unmarshal(raw, &requestBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": {
				"storeProductsSavingsSearchResult": {
					"storeProducts": [
						{"itemCode":96320,"title":"Publix Veal Cubed Steaks, USDA Choice, Group Raised","priceLine":null,"sizeDescription":null},
						{"itemCode":101,"title":"Asparagus","priceLine":"2 for $5.00","sizeDescription":"1 lb"}
					],
					"totalCount": 2
				}
			}
		}`))
	}))
	t.Cleanup(server.Close)

	client := NewClientWithBaseURLs(server.URL, server.URL, server.Client())
	got, err := client.StoreProductsSavings(context.Background(), StoreProductsSavingsOptions{
		StoreNumber: "1847",
		CategoryID:  CategoryBeef,
		Abck:        "token-value; bm_sv=bm-token",
		Take:        48,
		Skip:        7,
	})
	if err != nil {
		t.Fatalf("StoreProductsSavings returned error: %v", err)
	}

	if got, want := requestedPath, "/search/api/search/storeproductssavings/"; got != want {
		t.Fatalf("unexpected request path: got %q want %q", got, want)
	}
	if !strings.Contains(requestedQuery, "keyword=") ||
		!strings.Contains(requestedQuery, "storeNumber=1847") ||
		!strings.Contains(requestedQuery, "cat="+CategoryBeef) ||
		!strings.Contains(requestedQuery, "source="+storeProductsSavingsSource) {
		t.Fatalf("unexpected request query: %q", requestedQuery)
	}
	if requestBody.Variables.Take != 48 ||
		requestBody.Variables.Skip != 7 ||
		requestBody.Variables.CategoryID != CategoryBeef {
		t.Fatalf("unexpected graphql variables: %+v", requestBody.Variables)
	}
	if !strings.Contains(requestBody.Query, "storeProductsSavingsSearchResult(skip: $skip, take: $take, categoryID: $categoryID)") {
		t.Fatalf("unexpected graphql query: %q", requestBody.Query)
	}
	if got.TotalCount != 2 || len(got.StoreProducts) != 2 {
		t.Fatalf("unexpected response: %+v", got)
	}
	if got.StoreProducts[0].ItemCode != 96320 || got.StoreProducts[0].PriceLine != nil || got.StoreProducts[0].SizeDescription != nil {
		t.Fatalf("unexpected nullable product fields: %+v", got.StoreProducts[0])
	}
	if got.StoreProducts[1].PriceLine == nil || *got.StoreProducts[1].PriceLine != "2 for $5.00" {
		t.Fatalf("unexpected price line: %+v", got.StoreProducts[1].PriceLine)
	}
}

const sampleStoreHTML = `<!doctype html>
<html>
<body>
<store-details
	:store="{&quot;storeNumber&quot;:1083,&quot;type&quot;:&quot;R&quot;,&quot;name&quot;:&quot;Publix at University Town Center&quot;,&quot;address&quot;:{&quot;streetAddress&quot;:&quot;1190 University Blvd&quot;,&quot;city&quot;:&quot;Tuscaloosa&quot;,&quot;state&quot;:&quot;AL&quot;,&quot;zip&quot;:&quot;35401-1601&quot;},&quot;latitude&quot;:33.212097,&quot;longitude&quot;:-87.553585}">
</store-details>
</body>
</html>`
