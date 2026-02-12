package actowiz

import (
	"encoding/json"
	"testing"
)

func TestAlbertsonsProductUnmarshal(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"Sr No": 3,
		"Product URL": "https:\/\/www.jewelosco.com\/shop\/product-details.970029269.html",
		"Product Name": "Deli Grilled Chicken Breast Hot - Each (Available After 10 AM)\n",
		"Breadcrumbs": "Categories | Deli | Deli Sides & Meals | Roasted Whole Chicken",
		"Category": "Fresh",
		"Sub Category": "Deli",
		"Price": 1.99,
		"Discounted Price": "N\/A",
		"Image URL": "https:\/\/images.albertsons-media.com\/is\/image\/ABS\/970029269?$ng-ecom-pdp-desktop$&defaultImage=Not_Available",
		"Store Location": "1224 S Wabash Ave Chicago, IL 60605",
		"Availability": "IN Stock"
	}`)

	var got AlbertsonsProduct
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("json.Unmarshal(): %v", err)
	}

	if got.SerialNumber != 3 {
		t.Fatalf("SerialNumber = %d, want 3", got.SerialNumber)
	}
	if got.Price != 1.99 {
		t.Fatalf("Price = %f, want 1.99", got.Price)
	}
	if got.Availability != "IN Stock" {
		t.Fatalf("Availability = %q, want %q", got.Availability, "IN Stock")
	}
}

func TestWholeFoodsProductUnmarshal(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"url": "https:\/\/www.wholefoodsmarket.com\/product\/1luv-spice-mulled-hibiscus-drink-16-fz-b0842r5q37",
		"product_id": 86027300163,
		"product_name": "SPICE MULLED HIBISCUS DRINK",
		"product_json_name": "Spice Mulled Hibiscus Drink, 16 FZ",
		"brand": "1Luv",
		"images": "[\"https:\/\/m.media-amazon.com\/images\/S\/assets.wholefoodsmarket.com\/PIE\/product\/41T0ChIo-bL.jpg\", \"https:\/\/m.media-amazon.com\/images\/S\/assets.wholefoodsmarket.com\/PIE\/product\/21YfsvqElML.jpg\"]",
		"ingredients_string": "FILTERED WATER, HIBISCUS PETALS ORANGE ZEST, CINNAMON, ORGANIC CANE SUGAR, CLOVES",
		"nutritional_data": "{\"Calories\": {\"value\": \"70\", \"unit\": \"N\/A\", \"per_serving\": \"70\", \"full_daily_value\": 0}, \"Total Carbohydrate\": {\"value\": \"22\", \"unit\": \"g\", \"per_serving\": \"22\", \"full_daily_value\": 8}}",
		"store_id": 10112,
		"regular_price": 6.99,
		"sale_price": "N\/A",
		"incremental_sale_price": "N\/A",
		"allergens": "N\/A",
		"net_weight": "N\/A",
		"location": "929 South St, Philadelphia, PA 19147",
		"pack_size": "N\/A",
		"description": "N\/A",
		"calories": 70
	}`)

	var got WholeFoodsProduct
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("json.Unmarshal(): %v", err)
	}

	if got.ProductID != 86027300163 {
		t.Fatalf("ProductID = %d, want 86027300163", got.ProductID)
	}
	if len(got.Images) != 2 {
		t.Fatalf("len(Images) = %d, want 2", len(got.Images))
	}
	if got.NutritionalData["Calories"].Value != "70" {
		t.Fatalf("NutritionalData[Calories].Value = %q, want %q", got.NutritionalData["Calories"].Value, "70")
	}
	if got.RegularPrice != 6.99 {
		t.Fatalf("RegularPrice = %f, want 6.99", got.RegularPrice)
	}
}
