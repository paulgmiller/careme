package recipes

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	locationtypes "careme/internal/locations/types"
	ordertypes "careme/internal/orders/types"

	"careme/internal/ai"
	"careme/internal/cache"
)

type OrderViewBuilder interface {
	LoadTransfer(context.Context, cache.Cache, locationtypes.Location, string, string) (*ordertypes.OrderTransfer, error)
	OrderView(ordertypes.OrderViewInput) (*ordertypes.OrderView, error)
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
	transfer, err := s.orderView.LoadTransfer(r.Context(), s.Cache, view.Location, userID, view.Hash)
	if err != nil {
		return fmt.Errorf("load order transfer: %w", err)
	}
	order, err := s.orderView.OrderView(ordertypes.OrderViewInput{
		Transfer: transfer,
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
