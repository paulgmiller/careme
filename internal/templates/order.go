package templates

import (
	"net/url"

	"careme/internal/ai"
	locationtypes "careme/internal/locations/types"
)

// OrderViewInput carries the displayed shopping list to its destination's form builder.
type OrderViewInput struct {
	Location  locationtypes.Location
	Items     []ai.ShoppingItem
	ListKey   string
	ReturnURL string
	Query     url.Values
}

// OrderView describes an HTTP handoff. The destination owns field names and copy.
type OrderView struct {
	ActionURL   string
	ActionLabel string
	Fields      []OrderField
	Description string
	Notice      string
	ReviewURL   string
	ReviewLabel string
}

type OrderField struct {
	Name  string
	Value string
}
