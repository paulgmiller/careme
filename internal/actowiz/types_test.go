package actowiz

import (
	"encoding/json"
	"os"
	"testing"
)

func TestSafewayProductUnmarshal(t *testing.T) {
	t.Parallel()

	raw := []byte(`{
		"Store Name": "140th Ave NE",
		"Zip-Code": "98005",
		"Product Name": "Fresh Farmed Atlantic Salmon Fillet Color Added - 1.5 lb",
		"ID": 186190041,
		"URL": "https://www.safeway.com/shop/product-details.186190041.html",
		"Product Description": "N/A",
		"MRP": 19.49,
		"Discount": "N/A",
		"Discounted Price": 14.99,
		"Category": "Meat & Seafood",
		"Sub-Category": "Fish & Shellfish",
		"Availability": true
	}`)

	var got SafewayProduct
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("json.Unmarshal(): %v", err)
	}

	if got.ID != 186190041 {
		t.Fatalf("ID = %d, want 186190041", got.ID)
	}
	if got.MRP == nil || *got.MRP != 19.49 {
		t.Fatalf("MRP = %+v, want 19.49", got.MRP)
	}
	if got.DiscountedPrice == nil || *got.DiscountedPrice != 14.99 {
		t.Fatalf("DiscountedPrice = %+v, want 14.99", got.DiscountedPrice)
	}
	if !got.Availability {
		t.Fatal("Availability = false, want true")
	}
}

func TestSafewayProductsJSONUnmarshal(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("safeway_products.json")
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}

	var got []SafewayProduct
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("json.Unmarshal(): %v", err)
	}
	if len(got) != 750 {
		t.Fatalf("len(got) = %d, want 750", len(got))
	}
	if got[0].ProductName == "" {
		t.Fatal("first product name is empty")
	}
}
