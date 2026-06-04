package heb

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestCategoryPageBuildsExpectedRequest(t *testing.T) {
	t.Parallel()

	var capturedReq *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedReq = r
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"props":{"pageProps":{"products":[
				{"id":"1895013","storeId":92,"displayName":"H-E-B Mozzarella Cheese Sticks","inventory":{"inventoryState":"IN_STOCK"},"brand":{"name":"H-E-B"},"productImageUrls":[{"url":"https://images.heb.com/001895013.jpg"}]}
			]}}
		}`)
	}))
	defer server.Close()

	client := NewQueryClient(QueryClientConfig{
		BaseURL:    server.URL,
		BuildID:    "test-build",
		HTTPClient: server.Client(),
	})

	page, err := client.CategoryPage(context.Background(), CategoryOptions{
		Reese84:  "test-reese",
		StoreID:  "92",
		ParentID: "490020",
		ChildID:  "490083",
		Page:     2,
		SCT:      "next-token",
	})
	if err != nil {
		t.Fatalf("CategoryPage returned error: %v", err)
	}
	if len(page.Products) != 1 {
		t.Fatalf("expected 1 product, got %d", len(page.Products))
	}
	if capturedReq == nil {
		t.Fatal("expected request to be captured")
	}
	if got, want := capturedReq.URL.Path, "/_next/data/test-build/en/category/shop/490020/490083.json"; got != want {
		t.Fatalf("unexpected path: got %q want %q", got, want)
	}

	query := capturedReq.URL.Query()
	assertQueryValue(t, query, "page", "2")
	assertQueryValue(t, query, "sct", "next-token")
	assertQueryValue(t, query, "parentId", "490020")
	assertQueryValue(t, query, "childId", "490083")

	if got, want := capturedReq.Header.Get("Accept"), "*/*"; got != want {
		t.Fatalf("unexpected accept header: got %q want %q", got, want)
	}
	if got, want := capturedReq.Header.Get("X-Nextjs-Data"), "1"; got != want {
		t.Fatalf("unexpected x-nextjs-data header: got %q want %q", got, want)
	}
	if got, want := capturedReq.Header.Get("Referer"), server.URL+"/category/shop/490020/490083"; got != want {
		t.Fatalf("unexpected referer: got %q want %q", got, want)
	}

	assertCookieValue(t, capturedReq, "reese84", "test-reese")
	assertCookieValue(t, capturedReq, "SHOPPING_STORE_ID", "92")
	assertCookieValue(t, capturedReq, "CURR_SESSION_STORE", "92")
	assertCookieValue(t, capturedReq, "USER_CHOSEN_STORE", "true")
}

func TestCategoryPageIncludesIntParameter(t *testing.T) {
	t.Parallel()

	var capturedReq *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedReq = r
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"props":{"pageProps":{"products":[]}}}`)
	}))
	defer server.Close()

	client := NewQueryClient(QueryClientConfig{
		BaseURL:    server.URL,
		BuildID:    "test-build",
		HTTPClient: server.Client(),
	})

	_, err := client.CategoryPage(context.Background(), CategoryOptions{
		Reese84:      "test-reese",
		StoreID:      "465",
		ParentID:     "490110",
		ChildID:      "490529",
		CategoryPath: "/category/shop/meat-seafood/meat/beef/490110/490529?int=curbside-category-shortcuts.meat.beef",
		Int:          "curbside-category-shortcuts.meat.beef",
		Page:         2,
	})
	if err != nil {
		t.Fatalf("CategoryPage returned error: %v", err)
	}

	query := capturedReq.URL.Query()
	assertQueryValue(t, query, "int", "curbside-category-shortcuts.meat.beef")
	assertQueryValue(t, query, "parentId", "490110")
	assertQueryValue(t, query, "childId", "490529")
	if got, want := capturedReq.Header.Get("Referer"), server.URL+"/category/shop/meat-seafood/meat/beef/490110/490529?int=curbside-category-shortcuts.meat.beef"; got != want {
		t.Fatalf("unexpected referer: got %q want %q", got, want)
	}
}

func TestCategoryPageUsesDefaultBuildID(t *testing.T) {
	t.Parallel()

	var dataRequestPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/_next/data/" + StaplesBuildID + "/en/category/shop/490020/490083.json":
			dataRequestPath = r.URL.Path
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"props":{"pageProps":{"products":[]}}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewQueryClient(QueryClientConfig{
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
	})

	_, err := client.CategoryPage(context.Background(), CategoryOptions{
		Reese84:      "test-reese",
		StoreID:      "92",
		ParentID:     "490020",
		ChildID:      "490083",
		CategoryPath: "/category/shop/fruit-vegetables/vegetables/490020/490083",
	})
	if err != nil {
		t.Fatalf("CategoryPage returned error: %v", err)
	}
	if got, want := dataRequestPath, "/_next/data/"+StaplesBuildID+"/en/category/shop/490020/490083.json"; got != want {
		t.Fatalf("unexpected data path: got %q want %q", got, want)
	}
}

func TestExtractBuildIDFromNextStaticAsset(t *testing.T) {
	t.Parallel()

	buildID, err := extractBuildID([]byte(`<!doctype html><html><head><script src="/_next/static/static-build-id/_buildManifest.js"></script></head></html>`))
	if err != nil {
		t.Fatalf("extractBuildID returned error: %v", err)
	}
	if buildID != "static-build-id" {
		t.Fatalf("unexpected build id: %q", buildID)
	}
}

func TestDecodeCategoryPayloadExtractsProducts(t *testing.T) {
	t.Parallel()

	body := []byte(`{
		"props":{
			"pageProps":{
				"categoryData":{
					"items":[
						{
							"id": "1895013",
							"storeId": 92,
							"shoppingContext": null,
							"displayName": "H-E-B Low Moisture Part-Skim Mozzarella Cheese Sticks, 24 ct",
							"decodedDisplayName": "H-E-B Low Moisture Part-Skim Mozzarella Cheese Sticks, 24 ct",
							"fullDisplayName": "H-E-B Low Moisture Part-Skim Mozzarella Cheese Sticks, 24 ct",
							"fullCategoryHierarchy": "Dairy & eggs/Cheese",
							"minimumOrderQuantity": 1,
							"maximumOrderQuantity": 20,
							"bestAvailable": false,
							"onAd": false,
							"isNew": false,
							"pricedByWeight": false,
							"showCouponFlag": false,
							"inAssortment": true,
							"isEbtSnapProduct": true,
							"productLocation": {"location": "In Meat Market on the Back Wall", "__typename": "ProductLocation"},
							"pastPurchaseInfo": null,
							"purchasePreferenceList": null,
							"inventory": {"inventoryState": "IN_STOCK", "__typename": "Inventory"},
							"brand": {"name": "H-E-B", "isOwnBrand": true, "__typename": "Brand"},
							"productCategory": {"id": "490016", "name": "Dairy & eggs", "__typename": "ProductCategory"},
							"productImageUrls": [{"url": "https://images.heb.com/is/image/HEBGrocery/prd-small/001895013.jpg"}]
						}
					]
				}
			}
		}
	}`)

	products, nextSCT, err := decodeCategoryPayload(body)
	if err != nil {
		t.Fatalf("decodeCategoryPayload returned error: %v", err)
	}
	if nextSCT != "" {
		t.Fatalf("unexpected next sct: %q", nextSCT)
	}
	if len(products) != 1 {
		t.Fatalf("expected 1 product, got %d", len(products))
	}

	product := products[0]
	if product.ID != "1895013" {
		t.Fatalf("unexpected id: %q", product.ID)
	}
	if product.StoreID != 92 {
		t.Fatalf("unexpected store id: %d", product.StoreID)
	}
	if product.DisplayName != "H-E-B Low Moisture Part-Skim Mozzarella Cheese Sticks, 24 ct" {
		t.Fatalf("unexpected display name: %q", product.DisplayName)
	}
	if product.ProductLocation == nil || product.ProductLocation.Location != "In Meat Market on the Back Wall" {
		t.Fatalf("unexpected location: %+v", product.ProductLocation)
	}
	if product.Inventory == nil || product.Inventory.InventoryState != "IN_STOCK" {
		t.Fatalf("unexpected inventory: %+v", product.Inventory)
	}
	if product.Brand == nil || !product.Brand.IsOwnBrand || product.Brand.Name != "H-E-B" {
		t.Fatalf("unexpected brand: %+v", product.Brand)
	}
	if product.ProductCategory == nil || product.ProductCategory.Name != "Dairy & eggs" {
		t.Fatalf("unexpected category: %+v", product.ProductCategory)
	}
	if len(product.ProductImageURLs) != 1 || product.ProductImageURLs[0].URL == "" {
		t.Fatalf("unexpected product images: %+v", product.ProductImageURLs)
	}
	if len(product.Raw) == 0 {
		t.Fatal("expected raw product json")
	}
}

func TestDecodeCategoryPayloadExtractsNormalizedProductObjects(t *testing.T) {
	t.Parallel()

	body := []byte(`{
		"props": {
			"pageProps": {
				"apolloState": {
					"Product:beef-1": {
						"__typename": "Product",
						"id": "beef-1",
						"storeId": 465,
						"displayName": "H-E-B Ground Beef",
						"fullCategoryHierarchy": "Meat & seafood/Meat/Beef",
						"brand": {"name": "H-E-B"},
						"productLocation": {"location": "Meat Market"}
					},
					"Product:beef-1-duplicate": {
						"__typename": "Product",
						"id": "beef-1",
						"storeId": 465,
						"displayName": "H-E-B Ground Beef"
					},
					"Product:beef-2": {
						"__typename": "Product",
						"id": "beef-2",
						"storeId": 465,
						"displayName": "Beef Chuck Roast"
					}
				}
			}
		}
	}`)

	products, _, err := decodeCategoryPayload(body)
	if err != nil {
		t.Fatalf("decodeCategoryPayload returned error: %v", err)
	}
	if len(products) != 2 {
		t.Fatalf("expected 2 products, got %d: %+v", len(products), products)
	}
	productIDs := map[string]bool{}
	for _, product := range products {
		productIDs[product.ID] = true
	}
	if !productIDs["beef-1"] || !productIDs["beef-2"] {
		t.Fatalf("missing normalized products: %+v", products)
	}
}

func TestCategoryAutoPaginatesWithSCT(t *testing.T) {
	t.Parallel()

	var pageTwoSCT string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("page") {
		case "1":
			_, _ = io.WriteString(w, `{"props":{"pageProps":{"products":[{"id":"p1","displayName":"Apples"}],"pagination":{"nextSct":"page-two-token"}}}}`)
		case "2":
			pageTwoSCT = r.URL.Query().Get("sct")
			_, _ = io.WriteString(w, `{"props":{"pageProps":{"products":[{"id":"p2","displayName":"Broccoli"}],"pagination":{}}}}`)
		default:
			t.Fatalf("unexpected page %q", r.URL.Query().Get("page"))
		}
	}))
	defer server.Close()

	client := NewQueryClient(QueryClientConfig{
		BaseURL:    server.URL,
		BuildID:    "test-build",
		HTTPClient: server.Client(),
	})

	products, err := client.Category(context.Background(), CategoryOptions{
		Reese84:  "test-reese",
		StoreID:  "92",
		ParentID: "490020",
		ChildID:  "490083",
	})
	if err != nil {
		t.Fatalf("Category returned error: %v", err)
	}
	if len(products) != 2 {
		t.Fatalf("expected 2 products, got %d", len(products))
	}
	if got, want := products[0].ID, "p1"; got != want {
		t.Fatalf("unexpected first product: got %q want %q", got, want)
	}
	if got, want := products[1].ID, "p2"; got != want {
		t.Fatalf("unexpected second product: got %q want %q", got, want)
	}
	if got, want := pageTwoSCT, "page-two-token"; got != want {
		t.Fatalf("unexpected page two sct: got %q want %q", got, want)
	}
}

func TestCategoryDoesNotPaginateOnGenericSCT(t *testing.T) {
	t.Parallel()

	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"props":{"pageProps":{"products":[{"id":"p1","displayName":"Apples"}],"sct":"current-page-token"}}}`)
	}))
	defer server.Close()

	client := NewQueryClient(QueryClientConfig{
		BaseURL:    server.URL,
		BuildID:    "test-build",
		HTTPClient: server.Client(),
	})

	products, err := client.Category(context.Background(), CategoryOptions{
		Reese84:  "test-reese",
		StoreID:  "92",
		ParentID: "490020",
		ChildID:  "490083",
	})
	if err != nil {
		t.Fatalf("Category returned error: %v", err)
	}
	if len(products) != 1 {
		t.Fatalf("expected 1 product, got %d", len(products))
	}
	if calls != 1 {
		t.Fatalf("expected 1 request, got %d", calls)
	}
}

func TestCategoryValidation(t *testing.T) {
	t.Parallel()

	client := NewQueryClient(QueryClientConfig{BuildID: "test-build"})
	valid := CategoryOptions{
		Reese84:  "test-reese",
		StoreID:  "92",
		ParentID: "490020",
		ChildID:  "490083",
	}

	tests := []struct {
		name string
		opts CategoryOptions
		want string
	}{
		{
			name: "missing reese84",
			opts: withOption(valid, func(o *CategoryOptions) {
				o.Reese84 = ""
			}),
			want: "reese84 token is required",
		},
		{
			name: "missing store",
			opts: withOption(valid, func(o *CategoryOptions) {
				o.StoreID = ""
			}),
			want: "store id is required",
		},
		{
			name: "missing parent",
			opts: withOption(valid, func(o *CategoryOptions) {
				o.ParentID = ""
			}),
			want: "parent category id is required",
		},
		{
			name: "missing child",
			opts: withOption(valid, func(o *CategoryOptions) {
				o.ChildID = ""
			}),
			want: "child category id is required",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := client.CategoryPage(context.Background(), tc.opts)
			if err == nil || err.Error() != tc.want {
				t.Fatalf("unexpected error: got %v want %q", err, tc.want)
			}
		})
	}
}

func TestCategoryPageReturnsHTTPAndJSONErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		code int
		body string
		want string
	}{
		{
			name: "http error",
			code: http.StatusForbidden,
			body: "forbidden",
			want: "category request failed: status 403: forbidden",
		},
		{
			name: "json error",
			code: http.StatusOK,
			body: "{",
			want: "decode category json response",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.code)
				_, _ = io.WriteString(w, tc.body)
			}))
			defer server.Close()

			client := NewQueryClient(QueryClientConfig{
				BaseURL:    server.URL,
				BuildID:    "test-build",
				HTTPClient: server.Client(),
			})

			_, err := client.CategoryPage(context.Background(), CategoryOptions{
				Reese84:  "test-reese",
				StoreID:  "92",
				ParentID: "490020",
				ChildID:  "490083",
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("unexpected error: got %v want contains %q", err, tc.want)
			}
		})
	}
}

func TestCategoryReturnsMaxPagesError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"props":{"pageProps":{"products":[{"id":"p%s","displayName":"Product"}],"pagination":{"nextSct":"token-%s"}}}}`, r.URL.Query().Get("page"), r.URL.Query().Get("page"))
	}))
	defer server.Close()

	client := NewQueryClient(QueryClientConfig{
		BaseURL:    server.URL,
		BuildID:    "test-build",
		HTTPClient: server.Client(),
		MaxPages:   2,
	})

	_, err := client.Category(context.Background(), CategoryOptions{
		Reese84:  "test-reese",
		StoreID:  "92",
		ParentID: "490020",
		ChildID:  "490083",
	})
	if err == nil || !strings.Contains(err.Error(), "category pagination exceeded max pages 2") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExtractBuildIDErrorsWhenMissing(t *testing.T) {
	t.Parallel()

	_, err := extractBuildID([]byte(`<!doctype html><html><body></body></html>`))
	if err == nil || !errors.Is(err, errors.New("next data build id not found")) && !strings.Contains(err.Error(), "next data build id not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func withOption(opts CategoryOptions, mutate func(*CategoryOptions)) CategoryOptions {
	mutate(&opts)
	return opts
}

func assertQueryValue(t *testing.T, values url.Values, key, want string) {
	t.Helper()
	if got := values.Get(key); got != want {
		t.Fatalf("unexpected %s: got %q want %q", key, got, want)
	}
}

func assertCookieValue(t *testing.T, req *http.Request, name, want string) {
	t.Helper()
	cookie, err := req.Cookie(name)
	if err != nil {
		t.Fatalf("expected cookie %s: %v", name, err)
	}
	if cookie.Value != want {
		t.Fatalf("unexpected cookie %s: got %q want %q", name, cookie.Value, want)
	}
}
