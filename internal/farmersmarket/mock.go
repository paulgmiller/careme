package farmersmarket

import (
	"context"

	"careme/internal/ai"
)

type MockExtractor struct{}

func (MockExtractor) ExtractFarmersMarketIngredients(context.Context, string) ([]ai.InputIngredient, error) {
	tomatoPrice := float32(4.99)
	return []ai.InputIngredient{
		{
			ProductID:    "farmersmarket_mock_tomatoes",
			AisleNumber:  "Mock Farm",
			Description:  "heirloom tomatoes",
			PriceRegular: &tomatoPrice,
		},
		{
			ProductID:   "farmersmarket_mock_basil",
			AisleNumber: "Farmers market",
			Description: "fresh basil",
		},
	}, nil
}
