package recipes

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"careme/internal/locations"

	ordertypes "careme/internal/orders/types"

	"careme/internal/ai"
)

type orderViewBuilder interface {
	OrderView(context.Context, ordertypes.OrderViewInput) (*ordertypes.OrderView, error)
}

func (s *server) populateOrderView(r *http.Request, view *shoppingListPageView) error {
	var items []ai.ShoppingItem
	for _, group := range view.Purchases {
		for _, item := range group.Items {
			if item.Buy == nil || strings.TrimSpace(item.ProductID) == "" {
				continue
			}
			items = append(items, item.ShoppingItem)
		}
	}
	var zipCode string
	if cookie, err := r.Cookie(locations.ZipCodeCookieName); err == nil {
		zipCode = cookie.Value
	}
	order, err := s.orderView.OrderView(r.Context(), ordertypes.OrderViewInput{
		UserID:   view.User.ID,
		ZipCode:  zipCode,
		Location: view.Location, Items: items, ListKey: view.Hash,
		ReturnURL: "/recipes?h=" + url.QueryEscape(view.Hash) + "#shopping-list-section",
	})
	if err != nil {
		return err
	}
	if order != nil && len(items) == 0 {
		order.TransferURL = ""
	}
	view.Order = order
	return nil
}
