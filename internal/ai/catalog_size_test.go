package ai

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCatalogSizeFromDescription(t *testing.T) {
	for _, tc := range []struct{ description, want string }{
		{"Italian Ground Sausage - avg 1.03lb", "avg 1.03lb"},
		{"Ground Turkey - Ferndale - avg 1lb", "avg 1lb"},
		{"Beef & Pork - 1lb", "1lb"},
		{"Avocados - qty 3", "3 count"},
		{"Ground Beef 80% Lean/20% Fat Value Pack, 48 OZ", "48 OZ"},
		{"365 By Whole Foods Market, Ground Beef, 16 Ounce", "16 Ounce"},
		{"Milk, 32 fl oz ", "32 fl oz"},
		{"12 ct", "12 ct"},
		{"Small Seasonal Box", ""},
		{"Turkey Breast - Select Size", ""},
		{"Turkey Breast - 1-2 lb", ""},
		{"Turkey Breast - 1 - 2 lb", ""},
		{"Ground Beef, 80/20", ""},
		{"Vitamin B12", ""},
		{"", ""},
	} {
		t.Run(tc.description, func(t *testing.T) {
			assert.Equal(t, tc.want, CatalogSizeFromDescription(tc.description))
		})
	}
}
