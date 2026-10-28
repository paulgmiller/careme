package recipes

import (
	"context"
	"net/url"

	ordertypes "careme/internal/orders/types"

	"careme/internal/ai"
)

type OrderViewBuilder interface {
	OrderView(context.Context, ordertypes.OrderViewInput) (*ordertypes.OrderView, error)
}

func (s *server) populateOrderView(ctx context.Context, view *shoppingListPageView) error {
	var items []ai.ShoppingItem
	var variants map[string]string
	for _, group := range view.Purchases {
		for _, item := range group.Items {
			items = append(items, item.ShoppingItem)
			if item.VariantID != "" {
				if variants == nil {
					variants = make(map[string]string)
				}
				variants[item.ProductID] = item.VariantID
			}
		}
	}
	order, err := s.orderView.OrderView(ctx, ordertypes.OrderViewInput{
		UserID:   view.User.ID,
		Location: view.Location, Items: items, ListKey: view.Hash,
		VariantIDs: variants,
		ReturnURL:  "/recipes?h=" + url.QueryEscape(view.Hash) + "#shopping-list-section",
	})
	if err != nil {
		return err
	}
	view.Order = order
	return nil
}
