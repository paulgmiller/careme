package farmersmarket

import (
	"context"

	"careme/internal/ai"
)

type MockExtractor struct{}

func (MockExtractor) ExtractFarmersMarketIngredients(context.Context, []ai.FarmersMarketPhoto) ([]ai.InputIngredient, error) {
	return []ai.InputIngredient{
		{
			ProductID:   "farmersmarket_mock_tomatoes",
			Brand:       "Mock Farm",
			Description: "heirloom tomatoes",
			Size:        "per lb",
			Categories:  []string{"produce", "vegetables"},
		},
		{
			ProductID:   "farmersmarket_mock_basil",
			Brand:       "Farmers market",
			Description: "fresh basil",
			Size:        "1 bunch",
			Categories:  []string{"produce", "herbs"},
		},
	}, nil
}
