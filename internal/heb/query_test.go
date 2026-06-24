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
	"strings"
	"testing"
)

func freshBuild(_ context.Context, _ string) (string, error) {
	return "fresh-build", nil
}

func TestCategoryPageBuildsExpectedRequest(t *testing.T) {
	t.Parallel()

	var capturedReq *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedReq = r
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, categoryProductsJSON("p", 1, 1))
	}))
	defer server.Close()

	client := NewQueryClient(QueryClientConfig{
		BaseURL: server.URL,
		LoadBuildID: func(_ context.Context, _ string) (string, error) {
			return "test-build", nil
		},
		HTTPClient: server.Client(),
	})

	page, err := client.categoryPage(context.Background(), CategoryOptions{
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

	assertCookieValue(t, capturedReq, "reese84", "test-reese")
}

func TestCategoryPageBuildsExpectedRequestForBeef(t *testing.T) {
	t.Parallel()

	var capturedReq *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedReq = r
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, emptyCategoryProductsJSON())
	}))
	defer server.Close()

	client := NewQueryClient(QueryClientConfig{
		BaseURL:     server.URL,
		LoadBuildID: freshBuild,
		HTTPClient:  server.Client(),
	})

	_, err := client.categoryPage(context.Background(), CategoryOptions{
		Reese84:  "test-reese",
		StoreID:  "465",
		ParentID: "490110",
		ChildID:  "490529",
		Page:     2,
	})
	if err != nil {
		t.Fatalf("CategoryPage returned error: %v", err)
	}

	query := capturedReq.URL.Query()
	assertQueryValue(t, query, "parentId", "490110")
	assertQueryValue(t, query, "childId", "490529")
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
		_, _ = io.WriteString(w, emptyCategoryProductsJSON())
	}))
	defer server.Close()

	client := NewQueryClient(QueryClientConfig{
		BaseURL:    server.URL,
		HTTPClient: server.Client(),

		LoadBuildID: func(_ context.Context, _ string) (string, error) {
			buildIDLoads++
			return "fresh-build", nil
		},
	})

	_, err := client.categoryPage(context.Background(), CategoryOptions{
		Reese84:  "test-reese",
		StoreID:  "92",
		ParentID: "490020",
		ChildID:  "490083",
		Page:     1,
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
		_, _ = io.WriteString(w, categoryProductsPageJSON("p", 1, 1, 1, ""))
	}))
	defer server.Close()

	client := NewQueryClient(QueryClientConfig{
		BaseURL:    server.URL,
		HTTPClient: server.Client(),

		LoadBuildID: func(_ context.Context, _ string) (string, error) {
			buildIDLoads++
			return "fresh-build", nil
		},
	})
	client.buildID = "stale-build"

	products, err := client.Category(context.Background(), CategoryOptions{
		Reese84:  "test-reese",
		StoreID:  "92",
		ParentID: "490020",
		ChildID:  "490083",
		Limit:    10,
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
		LoadBuildID: func(context.Context, string) (string, error) {
			return "", errors.New("homepage blocked")
		},
	})

	_, err := client.categoryPage(context.Background(), CategoryOptions{
		Reese84:  "test-reese",
		StoreID:  "92",
		ParentID: "490020",
		ChildID:  "490083",
		Page:     1,
	})
	if err == nil || !strings.Contains(err.Error(), "homepage blocked") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDecodeCategoryPagePayloadExtractsProducts(t *testing.T) {
	t.Parallel()

	body := []byte(`{
		"pageProps":{
			"layout":{
				"visualComponents":[{
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
								"productImageUrls": [{"url": "https://images.heb.com/is/image/HEBGrocery/prd-small/001895013.jpg"}],
								"SKUs": [{
									"id": "sku-1",
									"customerFriendlySize": "24 ct",
									"contextPrices": [
										{
											"context": "ONLINE",
											"listPrice": {"unit": "each", "formattedAmount": "$4.99", "amount": 4.99},
											"salePrice": {"unit": "each", "formattedAmount": "$3.99", "amount": 3.99}
										},
										{
											"context": "CURBSIDE",
											"listPrice": {"unit": "each", "formattedAmount": "$5.49", "amount": 5.49},
											"salePrice": {"unit": "each", "formattedAmount": "$4.49", "amount": 4.49}
										}
									]
								}]
							}
						]
					}]
			}
		}
	}`)

	payload, err := decodeCategoryPagePayload(strings.NewReader(string(body)), 1)
	if err != nil {
		t.Fatalf("decodeCategoryPagePayload returned error: %v", err)
	}
	products := payload.Products
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
	if product.ListPrice == nil || *product.ListPrice != float32(5.49) {
		t.Fatalf("unexpected list price: %v", product.ListPrice)
	}
	if product.SalePrice == nil || *product.SalePrice != float32(4.49) {
		t.Fatalf("unexpected sale price: %v", product.SalePrice)
	}
}

func TestDecodeCategoryPagePayloadExtractsLayoutProducts(t *testing.T) {
	t.Parallel()

	body := strings.NewReader(`{
		"pageProps": {
			"layout": {
				"visualComponents": [
					{
						"__typename": "SearchGridV2",
						"searchContextToken": "next-page-token",
						"total": 68,
						"items": [
							{
								"__typename": "Product",
								"id": "15928526",
								"storeId": 754,
								"displayName": "H-E-B Fish Market Fresh Whole Scored Texas Tilapia",
								"decodedDisplayName": "H-E-B Fish Market Fresh Whole Scored Texas Tilapia, Avg. 2.0 lbs",
								"fullDisplayName": "H-E-B Fish Market Fresh Whole Scored Texas Tilapia, Avg. 2.0 lbs",
								"fullCategoryHierarchy": "Meat & seafood/Seafood/Fish",
								"minimumOrderQuantity": 0.25,
								"maximumOrderQuantity": 25,
								"brand": {"name": "H-E-B", "isOwnBrand": true, "__typename": "Brand"},
								"productCategory": {"id": "490023", "name": "Meat & seafood", "__typename": "ProductCategory"},
								"productLocation": {"location": "In Seafood on the Left Wall, A13", "__typename": "ProductLocation"},
								"inventory": {"inventoryState": "IN_STOCK", "__typename": "Inventory"},
								"productImageUrls": [
									{"url": "https://images.heb.com/is/image/HEBGrocery/prd-small/015928526.jpg", "__typename": "Image"}
								],
								"SKUs": [
									{
										"id": "23720900000",
										"customerFriendlySize": "Avg. 2.0 lbs",
										"twelveDigitUPC": "237209000009",
										"contextPrices": [
											{
												"context": "ONLINE",
												"listPrice": {"unit": "each", "formattedAmount": "$9.94", "amount": 9.94},
												"salePrice": {"unit": "each", "formattedAmount": "$9.94", "amount": 9.94}
											},
											{
												"context": "CURBSIDE",
												"listPrice": {"unit": "each", "formattedAmount": "$10.44", "amount": 10.44},
												"salePrice": {"unit": "each", "formattedAmount": "$10.44", "amount": 10.44}
											}
										],
										"__typename": "SKU"
									}
								]
							}
						]
					}
				]
			}
		}
	}`)

	payload, err := decodeCategoryPagePayload(body, 1)
	if err != nil {
		t.Fatalf("decodeCategoryPagePayload returned error: %v", err)
	}
	products := payload.Products
	if len(products) != 1 {
		t.Fatalf("expected 1 product, got %d", len(products))
	}
	if payload.SearchContextToken != "next-page-token" {
		t.Fatalf("unexpected search context token: %q", payload.SearchContextToken)
	}
	if payload.Total != 68 {
		t.Fatalf("unexpected total: %d", payload.Total)
	}
	product := products[0]
	if product.ID != "15928526" {
		t.Fatalf("unexpected id: %q", product.ID)
	}
	if product.Brand == nil || product.Brand.Name != "H-E-B" {
		t.Fatalf("unexpected brand: %+v", product.Brand)
	}
	if product.ProductLocation == nil || product.ProductLocation.Location != "In Seafood on the Left Wall, A13" {
		t.Fatalf("unexpected location: %+v", product.ProductLocation)
	}
	if product.MinimumOrderQuantity != float32(0.25) {
		t.Fatalf("unexpected minimum order quantity: %v", product.MinimumOrderQuantity)
	}
	if product.ListPrice == nil || *product.ListPrice != float32(10.44) {
		t.Fatalf("unexpected list price: %v", product.ListPrice)
	}
	if product.SalePrice == nil || *product.SalePrice != float32(10.44) {
		t.Fatalf("unexpected sale price: %v", product.SalePrice)
	}
}

func TestDecodeCategoryPagePayloadSkipsBlankProductIDs(t *testing.T) {
	t.Parallel()

	body := strings.NewReader(`{
		"pageProps": {
			"layout": {
				"visualComponents": [
					{
						"items": [
							{"id": "", "displayName": "Ignore me"},
							{"id": "valid-1", "displayName": "Valid product"}
						]
					}
				]
			}
		}
	}`)

	payload, err := decodeCategoryPagePayload(body, 1)
	if err != nil {
		t.Fatalf("decodeCategoryPagePayload returned error: %v", err)
	}
	if len(payload.Products) != 1 {
		t.Fatalf("expected 1 product, got %d", len(payload.Products))
	}
	if got, want := payload.Products[0].ID, "valid-1"; got != want {
		t.Fatalf("unexpected product id: got %q want %q", got, want)
	}
}

func TestCategoryPaginatesByPage(t *testing.T) {
	t.Parallel()

	const firstPageCount = 3

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("page") {
		case "1":
			_, _ = io.WriteString(w, categoryProductsJSON("p", 1, firstPageCount))
		case "2":
			_, _ = io.WriteString(w, categoryProductsJSON("p", firstPageCount+1, 2))
		case "3":
			_, _ = io.WriteString(w, emptyCategoryProductsJSON())
		default:
			t.Fatalf("unexpected page %q", r.URL.Query().Get("page"))
		}
	}))
	defer server.Close()

	client := NewQueryClient(QueryClientConfig{
		BaseURL:     server.URL,
		LoadBuildID: freshBuild,
		HTTPClient:  server.Client(),
	})

	products, err := client.Category(context.Background(), CategoryOptions{
		Reese84:  "test-reese",
		StoreID:  "92",
		ParentID: "490020",
		ChildID:  "490083",
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("Category returned error: %v", err)
	}
	if len(products) != firstPageCount+2 {
		t.Fatalf("expected %d products, got %d", firstPageCount+2, len(products))
	}
	if got, want := products[0].ID, "p-1"; got != want {
		t.Fatalf("unexpected first product: got %q want %q", got, want)
	}
	if got, want := products[firstPageCount].ID, "p-4"; got != want {
		t.Fatalf("unexpected second product: got %q want %q", got, want)
	}
}

func TestCategoryCarriesSearchContextTokenBetweenPages(t *testing.T) {
	t.Parallel()

	const firstPageCount = 2

	var pageSCTs []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pageSCTs = append(pageSCTs, r.URL.Query().Get("sct"))
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("page") {
		case "1":
			_, _ = io.WriteString(w, categoryProductsWithSearchContextToken("p", 1, firstPageCount, "page-2-token"))
		case "2":
			_, _ = io.WriteString(w, categoryProductsWithSearchContextToken("p", firstPageCount+1, 3, "page-3-token"))
		case "3":
			_, _ = io.WriteString(w, emptyCategoryProductsJSON())
		default:
			t.Fatalf("unexpected page %q", r.URL.Query().Get("page"))
		}
	}))
	defer server.Close()

	client := NewQueryClient(QueryClientConfig{
		BaseURL:     server.URL,
		LoadBuildID: freshBuild,
		HTTPClient:  server.Client(),
	})

	products, err := client.Category(context.Background(), CategoryOptions{
		Reese84:  "test-reese",
		StoreID:  "92",
		ParentID: "490020",
		ChildID:  "490083",
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("Category returned error: %v", err)
	}
	if len(products) != firstPageCount+3 {
		t.Fatalf("expected %d products, got %d", firstPageCount+3, len(products))
	}
	if want := []string{"", "page-2-token", "page-3-token"}; !slices.Equal(pageSCTs, want) {
		t.Fatalf("unexpected page scts: got %v want %v", pageSCTs, want)
	}
}

func TestCategoryStopsAtTotal(t *testing.T) {
	t.Parallel()

	const (
		firstPageCount  = 3
		secondPageCount = 2
		total           = firstPageCount + secondPageCount
	)

	var pages []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := r.URL.Query().Get("page")
		pages = append(pages, page)
		w.Header().Set("Content-Type", "application/json")
		switch page {
		case "1":
			_, _ = io.WriteString(w, categoryProductsPageJSON("p", 1, firstPageCount, total, "page-2-token"))
		case "2":
			_, _ = io.WriteString(w, categoryProductsPageJSON("p", firstPageCount+1, secondPageCount, total, ""))
		default:
			t.Fatalf("unexpected page %q", page)
		}
	}))
	defer server.Close()

	client := NewQueryClient(QueryClientConfig{
		BaseURL:     server.URL,
		LoadBuildID: freshBuild,
		HTTPClient:  server.Client(),
	})

	products, err := client.Category(context.Background(), CategoryOptions{
		Reese84:  "test-reese",
		StoreID:  "92",
		ParentID: "490020",
		ChildID:  "490083",
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("Category returned error: %v", err)
	}
	if len(products) != total {
		t.Fatalf("expected %d products, got %d", total, len(products))
	}
	if want := []string{"1", "2"}; !slices.Equal(pages, want) {
		t.Fatalf("unexpected pages: got %v want %v", pages, want)
	}
}

func TestCategoryStopsAtLimit(t *testing.T) {
	t.Parallel()

	const (
		firstPageCount = 3
		limit          = 5
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("page") {
		case "1":
			_, _ = io.WriteString(w, categoryProductsJSON("p", 1, firstPageCount))
		case "2":
			_, _ = io.WriteString(w, categoryProductsJSON("p", firstPageCount+1, 4))
		default:
			t.Fatalf("unexpected page %q", r.URL.Query().Get("page"))
		}
	}))
	defer server.Close()

	client := NewQueryClient(QueryClientConfig{
		BaseURL:     server.URL,
		LoadBuildID: freshBuild,
		HTTPClient:  server.Client(),
	})

	products, err := client.Category(context.Background(), CategoryOptions{
		Reese84:  "test-reese",
		StoreID:  "92",
		ParentID: "490020",
		ChildID:  "490083",
		Limit:    limit,
	})
	if err != nil {
		t.Fatalf("Category returned error: %v", err)
	}
	if len(products) < limit {
		t.Fatalf("expected at least %d products, got %d", limit, len(products))
	}
	if got, want := products[limit-1].ID, "p-5"; got != want {
		t.Fatalf("unexpected limited product: got %q want %q", got, want)
	}
}

func TestCategoryStopsPagePaginationOnLaterHTTPError(t *testing.T) {
	t.Parallel()

	const firstPageCount = 3

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("page") {
		case "1":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, categoryProductsJSON("p", 1, firstPageCount))
		case "2":
			http.NotFound(w, r)
		default:
			t.Fatalf("unexpected page %q", r.URL.Query().Get("page"))
		}
	}))
	defer server.Close()

	client := NewQueryClient(QueryClientConfig{
		BaseURL:     server.URL,
		LoadBuildID: freshBuild,
		HTTPClient:  server.Client(),
	})

	products, err := client.Category(context.Background(), CategoryOptions{
		Reese84:  "test-reese",
		StoreID:  "92",
		ParentID: "490020",
		ChildID:  "490083",
		Limit:    10,
	})
	if err != nil {
		t.Fatalf("Category returned error: %v", err)
	}
	if len(products) != firstPageCount {
		t.Fatalf("expected %d products, got %d", firstPageCount, len(products))
	}
}

func TestCategoryValidation(t *testing.T) {
	t.Parallel()

	valid := CategoryOptions{
		Reese84:  "test-reese",
		StoreID:  "92",
		ParentID: "490020",
		ChildID:  "490083",
		Limit:    1,
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

			err := tc.opts.validate()
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
				BaseURL: server.URL,
				LoadBuildID: func(_ context.Context, _ string) (string, error) {
					return "fresh-build", nil
				},
				HTTPClient: server.Client(),
			})

			_, err := client.categoryPage(context.Background(), CategoryOptions{
				Reese84:  "test-reese",
				StoreID:  "92",
				ParentID: "490020",
				ChildID:  "490083",
				Page:     1,
			})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("unexpected error: got %v want contains %q", err, tc.want)
			}
		})
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

func categoryProductsJSON(prefix string, start, count int) string {
	return categoryProductsPageJSON(prefix, start, count, 0, "")
}

func categoryProductsWithSearchContextToken(prefix string, start, count int, searchContextToken string) string {
	return categoryProductsPageJSON(prefix, start, count, 0, searchContextToken)
}

func categoryProductsPageJSON(prefix string, start, count, total int, searchContextToken string) string {
	var b strings.Builder
	b.WriteString(`{"pageProps":{"layout":{"visualComponents":[{`)
	if total > 0 {
		_, _ = fmt.Fprintf(&b, `"total":%d,`, total)
	}
	if searchContextToken != "" {
		_, _ = fmt.Fprintf(&b, `"searchContextToken":%q,`, searchContextToken)
	}
	b.WriteString(`"items":[`)
	for i := 0; i < count; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		id := start + i
		_, _ = fmt.Fprintf(&b, `{"id":"%s-%d","displayName":"Product %d"}`, prefix, id, id)
	}
	b.WriteByte(']')
	b.WriteString(`}]}}}`)
	return b.String()
}

func emptyCategoryProductsJSON() string {
	return categoryProductsJSON("p", 1, 0)
}
