package cachekey

import (
	"testing"
	"time"
)

func TestForStoreUsesStoreDayAndSignature(t *testing.T) {
	day := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	key := ForStore("store-1", day, "catalog-v1")
	if key != ForStore("store-1", day.Add(6*time.Hour), "catalog-v1") {
		t.Fatal("hours within the same day changed the key")
	}
	for _, changed := range []string{
		ForStore("store-2", day, "catalog-v1"),
		ForStore("store-1", day.AddDate(0, 0, 1), "catalog-v1"),
		ForStore("store-1", day, "catalog-v2"),
	} {
		if changed == key {
			t.Fatal("store, day, or signature change did not change the key")
		}
	}
}
