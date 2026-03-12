package kroger

import (
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
)

func TestEncodeSaleIngredientsToTS(t *testing.T) {
	brand := "Foster Farms"
	description := "Fresh chicken thighs"

	var sb strings.Builder
	err := ToTSV([]Ingredient{
		{
			ProductId:    to.Ptr("444"),
			Brand:        &brand,
			Description:  &description,
			AisleNumber:  to.Ptr("7"),
			PriceRegular: to.Ptr(float32(3.50)), // should be omitted due to omitempty
			PriceSale:    nil,                   // should be omitted due to omitempty
		},
	}, &sb)
	encoded := sb.String()
	if err != nil {
		t.Fatalf("unexpected encode error: %v", err)
	}

	if strings.Contains(encoded, "omitempty") {
		t.Fatalf("encoded payload should not contain omitempty tags: %s", encoded)
	}
	if strings.Contains(encoded, "null") {
		t.Fatalf("encoded payload should not include null values: %s", encoded)
	}
	if !strings.Contains(encoded, "Brand") || !strings.Contains(encoded, "Description") {
		t.Fatalf("encoded payload should include expected keys:\n %s", encoded)
	}
	if strings.Contains(encoded, "favorite") {
		t.Fatalf("encoded payload should omit nil fields with omitempty: %s", encoded)
	}
}
