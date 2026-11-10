package smithbrothersfarms

import (
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name + ".html")
	require.NoError(t, err)
	return string(data)
}

func TestParseCatalog(t *testing.T) {
	produce, err := Parse(strings.NewReader(fixture(t, "produce")))
	require.NoError(t, err)
	require.Len(t, produce, 3) // Both whole-box cards were removed.
	assert.Equal(t, "4197", produce[0].ProductID)
	assert.Equal(t, "Organic Bartlett Pears - 2 lbs", produce[0].Description)
	assert.Equal(t, "2 lbs", produce[0].Size)
	assert.Equal(t, "Produce", produce[0].AisleNumber)
	assert.Equal(t, []string{"Produce", "Fruit"}, produce[0].Categories)
	require.NotNil(t, produce[0].PriceRegular)
	require.NotNil(t, produce[0].PriceSale)
	assert.Equal(t, float32(5.99), *produce[0].PriceRegular)
	assert.Equal(t, float32(4.99), *produce[0].PriceSale)
	meat, err := Parse(strings.NewReader(fixture(t, "meat")))
	require.NoError(t, err)
	require.Len(t, meat, 3)
	assert.Equal(t, "5023", meat[0].ProductID)
	assert.Equal(t, "Porter & York", meat[0].Brand)
	assert.Equal(t, "4 pack", meat[0].Size)
	assert.Equal(t, []string{"Meat", "Beef"}, meat[0].Categories)
	require.NotNil(t, meat[0].PriceRegular)
	assert.Equal(t, float32(16.99), *meat[0].PriceRegular)
	assert.Nil(t, meat[0].PriceSale)
	assert.Equal(t, []string{"Meat", "Poultry"}, meat[2].Categories)
}

func TestParseCatalogFailures(t *testing.T) {
	valid := `<div class="catalog-scroll"><h1>Produce</h1><div class="item-grid"><div class="product-item" data-productid="1"><div class="product-title">Apple</div><div class="prices"><span class="price">$2</span></div></div></div></div>`
	for _, test := range []struct{ name, html, want string }{
		{"missing grid", "login", "no product grid"},
		{"missing heading", strings.Replace(valid, "<h1>Produce</h1>", "", 1), "no heading"},
		{"empty grid", `<div class="catalog-scroll"><h1>Produce</h1><div class="item-grid"></div></div>`, "no product cards"},
		{"ID", strings.Replace(valid, `data-productid="1"`, "", 1), "missing product ID"},
		{"title", strings.Replace(valid, ">Apple<", "><", 1), "missing product ID or title"},
		{"price", strings.Replace(valid, "$2", "invalid", 1), "invalid price"},
		{"prices", strings.Replace(valid, `class="prices"`, `class="other"`, 1), "no prices"},
		{"regular price", strings.Replace(valid, `<span class="price">`, `<span class="old-price">bad</span><span class="price">`, 1), "regular price"},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := Parse(strings.NewReader(test.html))
			require.ErrorContains(t, err, test.want)
			assert.Nil(t, got)
		})
	}
	for _, price := range []string{"", "$-1", "NaN", "+Inf", "1e100", "two"} {
		_, err := parsePrice(price)
		require.Error(t, err, price)
	}
	price, err := parsePrice("$1,234.50")
	require.NoError(t, err)
	assert.Equal(t, float32(1234.5), price)
}

func TestParseHarvestBoxes(t *testing.T) {
	expected := [][]string{
		{"Local Organic Sugar Bee Apples", "Organic Strawberries", "Organic Limes", "Local Organic Star Krimson Pears", "Organic Celery", "Organic Cucumbers", "Organic Bell Peppers", "Organic Slicing Tomatoes", "Local Organic Delicata Squash", "Organic Cauliflower", "Local Organic Romaine Lettuce", "Organic Brussels Sprouts"},
		{"Avocados", "Tsugaru Apples", "Flemish Beauty Pears", "Tangerines", "Kiwis", "NW Sweet Onions", "Organic Kabocha Squash"},
	}
	for i, file := range []string{"organic-box", "standard-box"} {
		box := harvestBoxes[i]
		got, err := ParseHarvestBox(strings.NewReader(fixture(t, file)), baseURL+"/"+box.slug, box.name)
		require.NoError(t, err)
		require.Len(t, got, len(expected[i]))
		for j, item := range got {
			assert.Equal(t, box.name+" — "+expected[i][j], item.Description)
			assert.Equal(t, []string{"Produce", box.name}, item.Categories)
			assert.Equal(t, 10, item.Grade.Score)
			assert.Nil(t, item.PriceRegular)
			assert.Nil(t, item.PriceSale)
			assert.Empty(t, item.Size)
		}
	}
	html := `<div class="full-description"><p><ul><li>  Apples &amp; <b>Pears</b> </li></ul></p></div>`
	first, err := ParseHarvestBox(strings.NewReader(html), baseURL+"/"+harvestBoxes[0].slug, "Organic Produce Box")
	require.NoError(t, err)
	other, err := ParseHarvestBox(strings.NewReader(html), baseURL+"/"+harvestBoxes[1].slug, "Produce Box")
	require.NoError(t, err)
	assert.NotEqual(t, first[0].ProductID, other[0].ProductID)
	reordered, err := ParseHarvestBox(strings.NewReader(strings.Replace(html, "<ul>", "<ul><li>Banana</li>", 1)), baseURL+"/"+harvestBoxes[0].slug, "Organic Produce Box")
	require.NoError(t, err)
	assert.Equal(t, first[0].ProductID, reordered[1].ProductID)
	assert.Equal(t, "Organic Produce Box — Apples & Pears", first[0].Description)
}

func TestHarvestBoxFailures(t *testing.T) {
	for _, html := range []string{"", `<div class="full-description">no list</div>`, `<div class="full-description"><ul></ul></div>`, `<div class="full-description"><ul><li>Apple</li><li> </li></ul></div>`} {
		got, err := ParseHarvestBox(strings.NewReader(html), baseURL+"/harvest-produce-box", "Produce Box")
		require.Error(t, err)
		assert.Nil(t, got)
	}
}

type errorReader struct{ err error }

func (r errorReader) Read([]byte) (int, error) { return 0, r.err }

func TestParseReadFailure(t *testing.T) {
	want := errors.New("read failed")
	for _, parse := range []func(io.Reader) error{
		func(r io.Reader) error { _, err := Parse(r); return err },
		func(r io.Reader) error {
			_, err := ParseHarvestBox(r, baseURL+"/harvest-produce-box", "Produce Box")
			return err
		},
	} {
		require.ErrorIs(t, parse(errorReader{want}), want)
	}
}
