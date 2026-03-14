package heb

import (
	"strings"
	"testing"
)

func TestParseStorePageURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		url  string
		want bool
	}{
		{
			name: "store page",
			url:  "https://www.heb.com/heb-store/US/tx/robstown/robstown-h-e-b-22",
			want: true,
		},
		{
			name: "city page",
			url:  "https://www.heb.com/heb-store/US/tx/robstown",
			want: false,
		},
		{
			name: "other path",
			url:  "https://www.heb.com/category/shop/meat",
			want: false,
		},
		{
			name: "missing store number",
			url:  "https://www.heb.com/heb-store/US/tx/robstown/robstown-h-e-b",
			want: false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, got := ParseStorePageURL(tc.url)
			if got != tc.want {
				t.Fatalf("ParseStorePageURL(%q) = %v, want %v", tc.url, got, tc.want)
			}
		})
	}
}

func TestFilterStorePagesDeduplicatesAndSkipsNonStores(t *testing.T) {
	t.Parallel()

	pages := FilterStorePages([]string{
		"https://www.heb.com/heb-store/US/tx/robstown/robstown-h-e-b-22",
		"https://www.heb.com/heb-store/US/tx/robstown/robstown-h-e-b-22",
		"https://www.heb.com/heb-store/US/tx/robstown",
		"https://www.heb.com/category/shop/meat",
	})

	if len(pages) != 1 {
		t.Fatalf("expected 1 store page, got %d", len(pages))
	}
	if pages[0].URLStoreID != "22" {
		t.Fatalf("unexpected store page: %+v", pages[0])
	}
}

func TestExtractStoreSummaryFromJSONLD(t *testing.T) {
	t.Parallel()

	pageURL := "https://www.heb.com/heb-store/US/tx/robstown/robstown-h-e-b-22"
	html := strings.Join([]string{
		`<!doctype html><html><head>`,
		`<title>Robstown H-E-B | 308 E MAIN | HEB.com</title>`,
		`<meta name="geo.position" content="27.7912;-97.6670">`,
		`<script type="application/ld+json">{"@context":"https://schema.org","@type":"GroceryStore","name":"Robstown H-E-B","branchCode":"22","address":{"streetAddress":"308 E Main","addressLocality":"Robstown","addressRegion":"TX","postalCode":"78380"},"geo":{"latitude":27.7912,"longitude":-97.6670}}</script>`,
		`</head><body><h1>Robstown H-E-B</h1><div>Corporate #22</div></body></html>`,
	}, "")

	summary, err := ExtractStoreSummary(pageURL, []byte(html))
	if err != nil {
		t.Fatalf("ExtractStoreSummary returned error: %v", err)
	}

	if summary.ID != "heb_22" || summary.StoreID != "22" {
		t.Fatalf("unexpected ids: %+v", summary)
	}
	if summary.Name != "Robstown H-E-B" {
		t.Fatalf("unexpected name: %q", summary.Name)
	}
	if summary.Address != "308 E Main" || summary.City != "Robstown" || summary.State != "TX" || summary.ZipCode != "78380" {
		t.Fatalf("unexpected address fields: %+v", summary)
	}
	if summary.Lat == nil || summary.Lon == nil {
		t.Fatalf("expected coordinates, got %+v", summary)
	}
}

func TestExtractStoreSummaryFallsBackToBodyAndURL(t *testing.T) {
	t.Parallel()

	pageURL := "https://www.heb.com/heb-store/US/tx/robstown/robstown-h-e-b-22"
	html := strings.Join([]string{
		`<!doctype html><html><head>`,
		`<title>Robstown H-E-B | 308 E MAIN | HEB.com</title>`,
		`</head><body>`,
		`<h1>Robstown H-E-B</h1>`,
		`<address>308 E Main Robstown, TX 78380</address>`,
		`<a href="https://maps.google.com/?destination=27.7912,-97.6670">Directions</a>`,
		`</body></html>`,
	}, "")

	summary, err := ExtractStoreSummary(pageURL, []byte(html))
	if err != nil {
		t.Fatalf("ExtractStoreSummary returned error: %v", err)
	}

	if summary.ID != "heb_22" {
		t.Fatalf("unexpected id: %+v", summary)
	}
	if summary.Address != "308 E Main" || summary.State != "TX" || summary.ZipCode != "78380" {
		t.Fatalf("unexpected address fields: %+v", summary)
	}
	if summary.Lat == nil || summary.Lon == nil {
		t.Fatalf("expected fallback coordinates, got %+v", summary)
	}
}

func TestExtractStoreSummaryFallsBackToGenericEmbeddedJSON(t *testing.T) {
	t.Parallel()

	pageURL := "https://www.heb.com/heb-store/US/tx/stephenville/stephenville-h-e-b-6"
	html := strings.Join([]string{
		`<!doctype html><html><head>`,
		`<title>Stephenville H-E-B | HEB.com</title>`,
		`<meta property="business:contact_data:region" content="TX">`,
		`<script>window.__DATA__={"store":{"storeId":"6","name":"Stephenville H-E-B","address":{"line1":"2150 W Washington St","city":"Stephenville","state":"TX","zipCode":"76401"}}};</script>`,
		`</head><body><h1>Stephenville H-E-B</h1></body></html>`,
	}, "")

	summary, err := ExtractStoreSummary(pageURL, []byte(html))
	if err != nil {
		t.Fatalf("ExtractStoreSummary returned error: %v", err)
	}
	if summary.ID != "heb_6" || summary.StoreID != "6" {
		t.Fatalf("unexpected ids: %+v", summary)
	}
	if summary.Address != "2150 W Washington St" || summary.City != "Stephenville" || summary.State != "TX" || summary.ZipCode != "76401" {
		t.Fatalf("unexpected address fields: %+v", summary)
	}
}

func TestExtractStoreSummaryPrefersURLStoreIDOverGenericJSONID(t *testing.T) {
	t.Parallel()

	pageURL := "https://www.heb.com/heb-store/US/tx/big-spring/big-spring-h-e-b-51"
	html := strings.Join([]string{
		`<!doctype html><html><head>`,
		`<script>window.__DATA__={"store":{"storeId":"92","address":{"line1":"2000 S Gregg St","city":"Big Spring","state":"TX","zipCode":"79720"}}};</script>`,
		`</head><body><h1>Big Spring H-E-B</h1></body></html>`,
	}, "")

	summary, err := ExtractStoreSummary(pageURL, []byte(html))
	if err != nil {
		t.Fatalf("ExtractStoreSummary returned error: %v", err)
	}
	if summary.ID != "heb_51" || summary.StoreID != "51" {
		t.Fatalf("expected URL store id to win, got %+v", summary)
	}
}

func TestExtractStoreSummaryRequiresAddress(t *testing.T) {
	t.Parallel()

	pageURL := "https://www.heb.com/heb-store/US/tx/robstown/robstown-h-e-b-22"
	html := `<!doctype html><html><body><h1>Robstown H-E-B</h1><div>Corporate #22</div></body></html>`

	_, err := ExtractStoreSummary(pageURL, []byte(html))
	if err == nil {
		t.Fatal("expected missing address error")
	}
	if got, want := err.Error(), "store address not found"; got != want {
		t.Fatalf("unexpected error: got %q want %q", got, want)
	}
}
