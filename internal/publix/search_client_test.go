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
						{"itemCode":101,"title":"Asparagus","priceLine":"2 for $5.00","originalPriceLine":"$3.49","sizeDescription":"1 lb"}
					],
					"totalCount": 2
				}
			}
		}`))
	}))
	t.Cleanup(server.Close)

	client := NewSearchClientWithBaseURL(server.URL, server.Client())
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
		requestBody.Variables.Keyword != "" ||
		requestBody.Variables.CategoryID != CategoryBeef ||
		requestBody.Variables.SortOrder != storeProductsSavingsSort ||
		requestBody.Variables.Source != storeProductsSavingsSource {
		t.Fatalf("unexpected graphql variables: %+v", requestBody.Variables)
	}
	if !strings.Contains(requestBody.Query, "storeProductsSavingsSearchResult(") ||
		!strings.Contains(requestBody.Query, "sortOrder: $sortOrder") ||
		!strings.Contains(requestBody.Query, "source: $source") {
		t.Fatalf("unexpected graphql query: %q", requestBody.Query)
	}
	if strings.Contains(requestBody.Query, "searchVariation") || strings.Contains(requestBody.Query, "facetOverrideStr") {
		t.Fatalf("graphql query was not simplified: %q", requestBody.Query)
	}
	if !strings.Contains(requestBody.Query, "productAlerts") {
		t.Fatalf("missing broad storeProducts selection in graphql query: %q", requestBody.Query)
	}
	if !strings.Contains(requestBody.Query, "originalPriceLine") {
		t.Fatalf("missing originalPriceLine in graphql query: %q", requestBody.Query)
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
	if got.StoreProducts[1].OriginalPriceLine == nil || *got.StoreProducts[1].OriginalPriceLine != "$3.49" {
		t.Fatalf("unexpected original price line: %+v", got.StoreProducts[1].OriginalPriceLine)
	}
}
