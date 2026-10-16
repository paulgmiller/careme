package recipes

import (
	"net/http"
	"net/url"

	"careme/internal/ai"
	"careme/internal/templates"
)

type OrderViewBuilder func(templates.OrderViewInput) (*templates.OrderView, error)

func (s *server) populateOrderView(view *shoppingListPageView, r *http.Request) error {
	var items []ai.ShoppingItem
	for _, group := range view.Purchases {
		for _, item := range group.Items {
			items = append(items, item.ShoppingItem)
		}
	}
	order, err := s.orderView(templates.OrderViewInput{
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
