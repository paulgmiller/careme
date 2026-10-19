package recipes

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	ordertypes "careme/internal/orders/types"

	"careme/internal/ai"
)

type OrderViewBuilder interface {
	OrderView(context.Context, ordertypes.OrderViewInput) (*ordertypes.OrderView, error)
}

func (s *server) populateOrderView(view *shoppingListPageView, r *http.Request) error {
	var items []ai.ShoppingItem
	for _, group := range view.Purchases {
		for _, item := range group.Items {
			items = append(items, item.ShoppingItem)
		}
	}
	userID, err := s.clerk.GetUserIDFromRequest(r)
	if err != nil {
		return fmt.Errorf("load ordering user: %w", err)
	}
	order, err := s.orderView.OrderView(r.Context(), ordertypes.OrderViewInput{
		UserID:   userID,
		Location: view.Location, Items: items, ListKey: view.Hash,
		ReturnURL: "/recipes?h=" + url.QueryEscape(view.Hash) + "#shopping-list-section",
		Query:     r.URL.Query(),
	})
	if err != nil {
		return err
	}
	view.Order = order
	return nil
}
