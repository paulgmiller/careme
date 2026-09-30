package mnfoodclub

import (
	"errors"
	"os"
	"strings"
	"testing"

	"careme/internal/ai"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseListings(t *testing.T) {
	for _, test := range []struct {
		name string
		want []ai.InputIngredient
	}{
		{"produce", []ai.InputIngredient{
			{ProductID: "951", Description: "Small Seasonal Box", Brand: "TC Farm", PriceRegular: pricePointer(26.99), AisleNumber: "Produce", Categories: []string{"Produce"}},
			{ProductID: "194", Description: "Avocados - qty 3", Brand: "Organic Produce", PriceRegular: pricePointer(6.99), AisleNumber: "Produce", Categories: []string{"Produce"}},
		}},
		{"meat", []ai.InputIngredient{
			{ProductID: "2249", Description: "Ground Turkey - Ferndale - avg 1lb", Brand: "Ferndale Market", PriceRegular: pricePointer(5.39), AisleNumber: "Meat", Categories: []string{"Meat"}},
			{ProductID: "543", Description: "Whole Ground Beef - avg 1.03lb", Brand: "TC Farm", PriceRegular: pricePointer(12.49), AisleNumber: "Meat", Categories: []string{"Meat"}},
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			file, err := os.Open("testdata/" + test.name + ".html")
			require.NoError(t, err)
			t.Cleanup(func() { assert.NoError(t, file.Close()) })
			got, err := Parse(file)
			require.NoError(t, err)
			assert.Equal(t, test.want, got)
		})
	}
}

func TestParsePricesAndText(t *testing.T) {
	for _, test := range []struct {
		name    string
		regular string
		want    ai.InputIngredient
	}{
		{"sale", "$1,200.00", ai.InputIngredient{ProductID: "42", Description: "Beef & Pork - 1lb", Brand: "Farm & Co", PriceRegular: pricePointer(1200), PriceSale: pricePointer(999.99)}},
		{"blank hidden price", " \n ", ai.InputIngredient{ProductID: "42", Description: "Beef & Pork - 1lb", Brand: "Farm & Co", PriceRegular: pricePointer(999.99)}},
		{"equal prices", "$999.99", ai.InputIngredient{ProductID: "42", Description: "Beef & Pork - 1lb", Brand: "Farm & Co", PriceRegular: pricePointer(999.99)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			card := `<article class="card extra"><a data-product-id="42"></a><h4 class="card-title"><a> Beef &amp; <b>Pork</b> - 1lb </a></h4><p data-test-info-type="brandName">Farm &amp; Co</p><span data-product-price-without-tax>$999.99</span><span data-product-non-sale-price-without-tax>` + test.regular + `</span><span data-product-rrp-without-tax>$9999</span></article>`
			got, err := Parse(strings.NewReader(card + `<ul class="extra productGrid">` + card + `</ul>`))
			require.NoError(t, err)
			assert.Equal(t, []ai.InputIngredient{test.want}, got)
		})
	}
}

func TestParseVariantPriceRange(t *testing.T) {
	file, err := os.Open("testdata/meat-price-range.html")
	require.NoError(t, err)
	t.Cleanup(func() { assert.NoError(t, file.Close()) })
	got, err := Parse(file)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "1728", got[0].ProductID)
	assert.Equal(t, "Boneless Turkey Breast - Ferndale - Select Size", got[0].Description)
	assert.Equal(t, pricePointer(16.99), got[0].PriceRegular)
	assert.Nil(t, got[0].PriceSale)
}

func TestParseFailures(t *testing.T) {
	valid := `<article class="card"><a data-product-id="42"></a><h4 class="card-title">Broccoli</h4><span data-product-price-without-tax>$3.00</span></article>`
	for _, test := range []struct {
		name string
		html string
		want string
	}{
		{"no grid", `<html>Sign in</html>`, "no product grid"},
		{"missing ID", strings.ReplaceAll(valid, `data-product-id="42"`, ""), "missing product ID or title"},
		{"missing title", strings.ReplaceAll(valid, "Broccoli", ""), "missing product ID or title"},
		{"missing price", strings.ReplaceAll(valid, "$3.00", ""), "invalid price"},
		{"bad price", strings.ReplaceAll(valid, "$3.00", "Call for price"), "invalid price"},
		{"negative price", strings.ReplaceAll(valid, "$3.00", "$-3.00"), "invalid price"},
		{"nonfinite price", strings.ReplaceAll(valid, "$3.00", "NaN"), "invalid price"},
		{"bad range minimum", strings.ReplaceAll(valid, "$3.00", "invalid - $5.00"), "invalid price"},
		{"bad range maximum", strings.ReplaceAll(valid, "$3.00", "$3.00 - invalid"), "invalid price range"},
		{"reversed range", strings.ReplaceAll(valid, "$3.00", "$5.00 - $3.00"), "invalid price range"},
		{"bad regular price", strings.ReplaceAll(valid, "</article>", `<span data-product-non-sale-price-without-tax>invalid</span></article>`), "non-sale price"},
	} {
		t.Run(test.name, func(t *testing.T) {
			document := test.html
			if test.name != "no grid" {
				document = `<ul class="productGrid">` + valid + document + `</ul>`
			}
			got, err := Parse(strings.NewReader(document))
			require.ErrorContains(t, err, test.want)
			assert.Nil(t, got)
		})
	}
}

func TestParseEmptyGrid(t *testing.T) {
	got, err := Parse(strings.NewReader(`<ul class="productGrid"></ul>`))
	require.NoError(t, err)
	assert.NotNil(t, got)
	assert.Empty(t, got)
}

func TestParseReaderFailure(t *testing.T) {
	want := errors.New("read failed")
	got, err := Parse(errorReader{want})
	require.ErrorIs(t, err, want)
	assert.Nil(t, got)
}

type errorReader struct{ err error }

func (r errorReader) Read([]byte) (int, error) { return 0, r.err }

func pricePointer(price float32) *float32 { return &price }
