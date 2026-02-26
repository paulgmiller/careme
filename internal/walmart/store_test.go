package walmart

import "testing"

func TestParseStore_SampleJSON(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"no":3098,"name":"Bellevue Neighborhood Market","country":"US","coordinates":[-122.139487,47.609036],"streetAddress":"15063 MAIN ST","city":"Bellevue","stateProvCode":"WA","zip":"98007","phoneNumber":"425-643-9054","sundayOpen":true,"timezone":"PST"}`)

	store, err := ParseStore(payload)
	if err != nil {
		t.Fatalf("parse store: %v", err)
	}

	if store.No != 3098 {
		t.Fatalf("unexpected store number: %d", store.No)
	}
	if store.Name != "Bellevue Neighborhood Market" {
		t.Fatalf("unexpected store name: %q", store.Name)
	}
	if store.Coordinates.Longitude != -122.139487 {
		t.Fatalf("unexpected longitude: %f", store.Coordinates.Longitude)
	}
	if store.Coordinates.Latitude != 47.609036 {
		t.Fatalf("unexpected latitude: %f", store.Coordinates.Latitude)
	}
	if store.Zip != "98007" {
		t.Fatalf("unexpected zip: %q", store.Zip)
	}
	if !store.SundayOpen {
		t.Fatal("expected sundayOpen=true")
	}
}

func TestParseStores_WrappedResults(t *testing.T) {
	t.Parallel()

	payload := []byte(`{"results":[{"no":3098,"name":"Bellevue Neighborhood Market","country":"US","coordinates":[-122.139487,47.609036],"streetAddress":"15063 MAIN ST","city":"Bellevue","stateProvCode":"WA","zip":"98007","phoneNumber":"425-643-9054","sundayOpen":true,"timezone":"PST"}]}`)

	stores, err := ParseStores(payload)
	if err != nil {
		t.Fatalf("parse stores: %v", err)
	}
	if len(stores) != 1 {
		t.Fatalf("unexpected store count: %d", len(stores))
	}
	if stores[0].No != 3098 {
		t.Fatalf("unexpected store number: %d", stores[0].No)
	}
}

func TestCoordinates_UnmarshalJSON_RequiresLonLatPair(t *testing.T) {
	t.Parallel()

	_, err := ParseStore([]byte(`{"coordinates":[-122.139487]}`))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestStoreToLocation_UsesWalmartPrefixWithUnderscore(t *testing.T) {
	t.Parallel()

	location := storeToLocation(Store{
		No:            3098,
		Name:          "Test",
		StreetAddress: "123 Main",
		StateProvCode: "WA",
		Zip:           "98007",
		Coordinates: Coordinates{
			Longitude: -122.139487,
			Latitude:  47.609036,
		},
	})
	if location.ID != "walmart_3098" {
		t.Fatalf("unexpected location ID: %q", location.ID)
	}
	if location.Lat == nil || *location.Lat != 47.609036 {
		t.Fatalf("unexpected location latitude: %v", location.Lat)
	}
	if location.Lon == nil || *location.Lon != -122.139487 {
		t.Fatalf("unexpected location longitude: %v", location.Lon)
	}
}
