package albertsons

import (
	"strings"
	"testing"
)

func TestParseStorePageURL(t *testing.T) {
	t.Parallel()

	chains := DefaultChains()
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{
			name: "safeway store page",
			url:  "https://local.safeway.com/safeway/wa/bellevue/15100-se-38th-st.html",
			want: true,
		},
		{
			name: "albertsons store page",
			url:  "https://local.albertsons.com/ar/texarkana/3710-state-line-ave.html",
			want: true,
		},
		{
			name: "category page",
			url:  "https://local.acmemarkets.com/ct/new-canaan/288-elm-st/produce.html",
			want: false,
		},
		{
			name: "city page",
			url:  "https://local.haggen.com/wa/bellingham.html",
			want: false,
		},
		{
			name: "other brand under safeway host",
			url:  "https://local.safeway.com/pak-n-save/ca/emeryville/3889-san-pablo-ave.html",
			want: false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, got := ParseStorePageURL(tc.url, chains)
			if got != tc.want {
				t.Fatalf("ParseStorePageURL(%q) = %v, want %v", tc.url, got, tc.want)
			}
		})
	}
}

func TestFilterStorePagesDeduplicatesAndSkipsNonStores(t *testing.T) {
	t.Parallel()

	pages := FilterStorePages([]string{
		"https://local.safeway.com/safeway/wa/bellevue/15100-se-38th-st.html",
		"https://local.safeway.com/safeway/wa/bellevue/15100-se-38th-st.html",
		"https://local.safeway.com/safeway/wa/bellevue/15100-se-38th-st/produce.html",
		"https://local.starmarket.com/search.html",
	}, DefaultChains())

	if len(pages) != 1 {
		t.Fatalf("expected 1 store page, got %d", len(pages))
	}
	if pages[0].Chain.Brand != "safeway" {
		t.Fatalf("unexpected brand: %+v", pages[0])
	}
}

func TestExtractStoreSummary(t *testing.T) {
	t.Parallel()

	pageURL := "https://local.safeway.com/safeway/wa/bellevue/15100-se-38th-st.html"
	html := strings.Join([]string{
		`<!doctype html><html><head>`,
		`<meta name="geo.position" content="47.5765527;-122.1381125">`,
		`<script type="text/javascript">window.Yext = (function(Yext){Yext.Profile = {"id":"1444","name":"Safeway","mainPhone":"(425) 641-0902","address":{"city":"Bellevue","line1":"15100 SE 38th St","postalCode":"98006","region":"WA"},"appleActionLinks":[{"quickLinkUrl":"https://www.safeway.com/?storeId=1444\u0026zipcode=98006"}]}; return Yext;})(window.Yext || {});</script>`,
		`</head><body></body></html>`,
	}, "")

	summary, err := ExtractStoreSummary(pageURL, []byte(html), DefaultChains())
	if err != nil {
		t.Fatalf("ExtractStoreSummary returned error: %v", err)
	}

	if summary.ID != "safeway_1444" {
		t.Fatalf("unexpected id: %+v", summary)
	}
	if summary.StoreID != "1444" || summary.Brand != "safeway" || summary.Domain != "local.safeway.com" {
		t.Fatalf("unexpected summary identifiers: %+v", summary)
	}
	if summary.Name != "Safeway 15100 SE 38th St" {
		t.Fatalf("unexpected name: %q", summary.Name)
	}
	if summary.Address != "15100 SE 38th St" || summary.State != "WA" || summary.ZipCode != "98006" {
		t.Fatalf("unexpected address fields: %+v", summary)
	}
	if summary.Lat == nil || summary.Lon == nil {
		t.Fatalf("expected coordinates, got %+v", summary)
	}
}

func TestExtractStoreSummaryFallsBackToActionLinkStoreID(t *testing.T) {
	t.Parallel()

	pageURL := "https://local.albertsons.com/ar/texarkana/3710-state-line-ave.html"
	html := `<!doctype html><html><head><script>window.Yext = (function(Yext){Yext.Profile = {"name":"Albertsons","address":{"city":"Texarkana","line1":"3710 State Line Ave","postalCode":"71854","region":"AR"},"appleActionLinks":[{"quickLinkUrl":"https://www.albertsons.com/?storeId=611\u0026zipcode=71854"}]}; return Yext;})(window.Yext || {});</script></head><body></body></html>`

	summary, err := ExtractStoreSummary(pageURL, []byte(html), DefaultChains())
	if err != nil {
		t.Fatalf("ExtractStoreSummary returned error: %v", err)
	}

	if summary.ID != "albertsons_611" || summary.StoreID != "611" {
		t.Fatalf("unexpected summary: %+v", summary)
	}
}
