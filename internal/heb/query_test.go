package heb

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
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
		PageDelay:  -1,
	})

	page, err := client.CategoryPage(context.Background(), CategoryOptions{
		Reese84:  "test-reese",
		StoreID:  "92",
		ParentID: "490020",
		ChildID:  "490083",
		Page:     2,
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
		PageDelay:  -1,
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

func TestCategoryPageIncludesSCTParameter(t *testing.T) {
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
		PageDelay:  -1,
	})

	_, err := client.CategoryPage(context.Background(), CategoryOptions{
		Reese84:  "test-reese",
		StoreID:  "465",
		ParentID: "490110",
		ChildID:  "490529",
		Page:     3,
		SCT:      "page-token",
	})
	if err != nil {
		t.Fatalf("CategoryPage returned error: %v", err)
	}

	query := capturedReq.URL.Query()
	assertQueryValue(t, query, "sct", "page-token")
	if got, want := capturedReq.Header.Get("Referer"), server.URL+"/category/shop/490110/490529?page=3&sct=page-token"; got != want {
		t.Fatalf("unexpected referer: got %q want %q", got, want)
	}
}

func TestCategoryPageRequiresBuildIDLoaderWhenMissing(t *testing.T) {
	t.Parallel()

	client := NewQueryClient(QueryClientConfig{
		PageDelay: -1,
	})

	_, err := client.CategoryPage(context.Background(), CategoryOptions{
		Reese84:      "test-reese",
		StoreID:      "92",
		ParentID:     "490020",
		ChildID:      "490083",
		CategoryPath: "/category/shop/fruit-vegetables/vegetables/490020/490083",
	})
	if err == nil || err.Error() != "heb build id loader is required" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCategoryPageRefreshesBuildIDWhenMissing(t *testing.T) {
	t.Parallel()

	var (
		buildIDLoads int
		capturedPath string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"props":{"pageProps":{"products":[]}}}`)
	}))
	defer server.Close()

	client := NewQueryClient(QueryClientConfig{
		BaseURL:    server.URL,
		HTTPClient: server.Client(),
		PageDelay:  -1,
		LoadBuildID: func(_ context.Context) (string, error) {
			buildIDLoads++
			return "fresh-build", nil
		},
	})

	_, err := client.CategoryPage(context.Background(), CategoryOptions{
		Reese84:  "test-reese",
		StoreID:  "92",
		ParentID: "490020",
		ChildID:  "490083",
	})
	if err != nil {
		t.Fatalf("CategoryPage returned error: %v", err)
	}
	if buildIDLoads != 1 {
		t.Fatalf("unexpected build id load count: got %d want 1", buildIDLoads)
	}
	if got := client.currentBuildID(); got != "fresh-build" {
		t.Fatalf("unexpected build id: got %q want %q", got, "fresh-build")
	}
	if !strings.Contains(capturedPath, "/_next/data/fresh-build/") {
		t.Fatalf("request did not use refreshed build id: %q", capturedPath)
	}
}

func TestCategoryRefreshesBuildIDAfterFirstPage404(t *testing.T) {
	t.Parallel()

	var buildIDLoads int
	requestPaths := make([]string, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestPaths = append(requestPaths, r.URL.Path)
		if strings.Contains(r.URL.Path, "/_next/data/stale-build/") {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, categoryProductsJSON("p", 1, 1, ""))
	}))
	defer server.Close()

	client := NewQueryClient(QueryClientConfig{
		BaseURL:    server.URL,
		BuildID:    "stale-build",
		HTTPClient: server.Client(),
		PageDelay:  -1,
		LoadBuildID: func(_ context.Context) (string, error) {
			buildIDLoads++
			return "fresh-build", nil
		},
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
	if buildIDLoads != 1 {
		t.Fatalf("unexpected build id load count: got %d want 1", buildIDLoads)
	}
	if got := client.currentBuildID(); got != "fresh-build" {
		t.Fatalf("unexpected build id: got %q want %q", got, "fresh-build")
	}
	if len(products) != 1 {
		t.Fatalf("expected 1 product, got %d", len(products))
	}
	if len(requestPaths) != 2 ||
		!strings.Contains(requestPaths[0], "/_next/data/stale-build/") ||
		!strings.Contains(requestPaths[1], "/_next/data/fresh-build/") {
		t.Fatalf("unexpected request paths: %v", requestPaths)
	}
}

func TestCategoryReturnsBuildIDLoadError(t *testing.T) {
	t.Parallel()

	client := NewQueryClient(QueryClientConfig{
		LoadBuildID: func(context.Context) (string, error) {
			return "", errors.New("homepage blocked")
		},
	})

	_, err := client.CategoryPage(context.Background(), CategoryOptions{
		Reese84:  "test-reese",
		StoreID:  "92",
		ParentID: "490020",
		ChildID:  "490083",
	})
	if err == nil || !strings.Contains(err.Error(), "homepage blocked") {
		t.Fatalf("unexpected error: %v", err)
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

	products, err := decodeCategoryPayload(body)
	if err != nil {
		t.Fatalf("decodeCategoryPayload returned error: %v", err)
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

func TestDecodeCategoryPagePayloadExtractsSCT(t *testing.T) {
	t.Parallel()

	page, err := decodeCategoryPagePayload([]byte(`{
		"props":{
			"pageProps":{
				"categoryData":{
					"products":[{"id":"p1","displayName":"Apples"}],
					"sct":"next-page-token"
				}
			}
		}
	}`))
	if err != nil {
		t.Fatalf("decodeCategoryPagePayload returned error: %v", err)
	}
	if got, want := page.SCT, "next-page-token"; got != want {
		t.Fatalf("unexpected sct: got %q want %q", got, want)
	}
	if len(page.Products) != 1 {
		t.Fatalf("expected 1 product, got %d", len(page.Products))
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

	products, err := decodeCategoryPayload(body)
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

func TestCategoryPaginatesByPage(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("page") {
		case "1":
			_, _ = io.WriteString(w, categoryProductsJSON("p", 1, categoryPageSize, ""))
		case "2":
			_, _ = io.WriteString(w, categoryProductsJSON("p", categoryPageSize+1, 2, ""))
		case "3":
			_, _ = io.WriteString(w, `{"props":{"pageProps":{"products":[]}}}`)
		default:
			t.Fatalf("unexpected page %q", r.URL.Query().Get("page"))
		}
	}))
	defer server.Close()

	client := NewQueryClient(QueryClientConfig{
		BaseURL:    server.URL,
		BuildID:    "test-build",
		HTTPClient: server.Client(),
		PageDelay:  -1,
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
	if len(products) != categoryPageSize+2 {
		t.Fatalf("expected %d products, got %d", categoryPageSize+2, len(products))
	}
	if got, want := products[0].ID, "p-1"; got != want {
		t.Fatalf("unexpected first product: got %q want %q", got, want)
	}
	if got, want := products[categoryPageSize].ID, "p-51"; got != want {
		t.Fatalf("unexpected second product: got %q want %q", got, want)
	}
}

func TestCategoryCarriesSCTBetweenPages(t *testing.T) {
	t.Parallel()

	var (
		pageSCTs []string
		referers []string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pageSCTs = append(pageSCTs, r.URL.Query().Get("sct"))
		referers = append(referers, r.Header.Get("Referer"))
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("page") {
		case "1":
			_, _ = io.WriteString(w, categoryProductsJSON("p", 1, categoryPageSize, "page-2-token"))
		case "2":
			_, _ = io.WriteString(w, categoryProductsJSON("p", categoryPageSize+1, categoryPageSize, "page-3-token"))
		case "3":
			_, _ = io.WriteString(w, `{"props":{"pageProps":{"products":[]}}}`)
		default:
			t.Fatalf("unexpected page %q", r.URL.Query().Get("page"))
		}
	}))
	defer server.Close()

	client := NewQueryClient(QueryClientConfig{
		BaseURL:    server.URL,
		BuildID:    "test-build",
		HTTPClient: server.Client(),
		PageDelay:  -1,
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
	if len(products) != categoryPageSize*2 {
		t.Fatalf("expected %d products, got %d", categoryPageSize*2, len(products))
	}
	if want := []string{"", "page-2-token", "page-3-token"}; !slices.Equal(pageSCTs, want) {
		t.Fatalf("unexpected page scts: got %v want %v", pageSCTs, want)
	}
	if want := []string{
		server.URL + "/category/shop/490020/490083",
		server.URL + "/category/shop/490020/490083",
		server.URL + "/category/shop/490020/490083?page=2&sct=page-2-token",
	}; !slices.Equal(referers, want) {
		t.Fatalf("unexpected referers: got %v want %v", referers, want)
	}
}

func TestCategoryPageThrottlesRequestsAcrossCalls(t *testing.T) {
	t.Parallel()

	requestTimes := make(chan time.Time, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestTimes <- time.Now()
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"props":{"pageProps":{"products":[]}}}`)
	}))
	defer server.Close()

	client := NewQueryClient(QueryClientConfig{
		BaseURL:    server.URL,
		BuildID:    "test-build",
		HTTPClient: server.Client(),
		PageDelay:  20 * time.Millisecond,
	})

	opts := CategoryOptions{
		Reese84:  "test-reese",
		StoreID:  "92",
		ParentID: "490020",
		ChildID:  "490083",
	}
	if _, err := client.CategoryPage(context.Background(), opts); err != nil {
		t.Fatalf("first CategoryPage returned error: %v", err)
	}
	opts.ChildID = "490082"
	if _, err := client.CategoryPage(context.Background(), opts); err != nil {
		t.Fatalf("second CategoryPage returned error: %v", err)
	}

	first := <-requestTimes
	second := <-requestTimes
	if elapsed := second.Sub(first); elapsed < 15*time.Millisecond {
		t.Fatalf("expected throttled requests, elapsed %s", elapsed)
	}
}

func TestCategoryStopsAtLimit(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("page") {
		case "1":
			_, _ = io.WriteString(w, categoryProductsJSON("p", 1, categoryPageSize, ""))
		case "2":
			_, _ = io.WriteString(w, categoryProductsJSON("p", categoryPageSize+1, 4, ""))
		default:
			t.Fatalf("unexpected page %q", r.URL.Query().Get("page"))
		}
	}))
	defer server.Close()

	client := NewQueryClient(QueryClientConfig{
		BaseURL:    server.URL,
		BuildID:    "test-build",
		HTTPClient: server.Client(),
		PageDelay:  -1,
	})

	products, err := client.Category(context.Background(), CategoryOptions{
		Reese84:  "test-reese",
		StoreID:  "92",
		ParentID: "490020",
		ChildID:  "490083",
		Limit:    categoryPageSize + 3,
	})
	if err != nil {
		t.Fatalf("Category returned error: %v", err)
	}
	if len(products) != categoryPageSize+3 {
		t.Fatalf("expected %d products, got %d", categoryPageSize+3, len(products))
	}
	if got, want := products[categoryPageSize+2].ID, "p-53"; got != want {
		t.Fatalf("unexpected limited product: got %q want %q", got, want)
	}
}

func TestCategoryStopsPagePaginationOnLaterHTTPError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("page") {
		case "1":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, categoryProductsJSON("p", 1, categoryPageSize, ""))
		case "2":
			http.NotFound(w, r)
		default:
			t.Fatalf("unexpected page %q", r.URL.Query().Get("page"))
		}
	}))
	defer server.Close()

	client := NewQueryClient(QueryClientConfig{
		BaseURL:    server.URL,
		BuildID:    "test-build",
		HTTPClient: server.Client(),
		PageDelay:  -1,
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
	if len(products) != categoryPageSize {
		t.Fatalf("expected %d products, got %d", categoryPageSize, len(products))
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
				PageDelay:  -1,
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
		page, err := strconv.Atoi(r.URL.Query().Get("page"))
		if err != nil {
			t.Fatalf("unexpected page %q", r.URL.Query().Get("page"))
		}
		_, _ = io.WriteString(w, categoryProductsJSON("p", ((page-1)*categoryPageSize)+1, categoryPageSize, ""))
	}))
	defer server.Close()

	client := NewQueryClient(QueryClientConfig{
		BaseURL:    server.URL,
		BuildID:    "test-build",
		HTTPClient: server.Client(),
		MaxPages:   2,
		PageDelay:  -1,
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

func categoryProductsJSON(prefix string, start, count int, sct string) string {
	var b strings.Builder
	b.WriteString(`{"props":{"pageProps":{"products":[`)
	for i := 0; i < count; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		id := start + i
		_, _ = fmt.Fprintf(&b, `{"id":"%s-%d","displayName":"Product %d"}`, prefix, id, id)
	}
	b.WriteByte(']')
	if sct != "" {
		_, _ = fmt.Fprintf(&b, `,"sct":%q`, sct)
	}
	b.WriteString(`}}}`)
	return b.String()
}
