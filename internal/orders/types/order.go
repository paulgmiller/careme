package types

import (
	"net/url"

	"careme/internal/ai"
	locationtypes "careme/internal/locations/types"
)

// OrderViewInput carries the displayed shopping list to its destination's form builder.
type OrderViewInput struct {
	Transfer  *OrderTransfer
	Location  locationtypes.Location
	Items     []ai.ShoppingItem
	ListKey   string
	ReturnURL string
	Query     url.Values
}

// OrderTransfer is persisted transfer state supplied to the view by the controller.
// A nil transfer means this list has not been sent.
type OrderTransfer struct {
	Status      string
	Added       int
	Unmatched   int
	NextAttempt int
}

// OrderView describes an HTTP handoff using shared form fields and destination-owned copy.
type OrderView struct {
	Secondary   bool
	ActionURL   string
	ActionLabel string
	Fields      OrderFields
	Description string
	Notice      string
	ReviewURL   string
	ReviewLabel string
}

// OrderFields is the shared form contract for ordering destinations.
// Items and Location are JSON encoded for hidden form inputs. Location is a source
// store hint; each destination decides how to select its destination store.
// Extend this struct when an integration needs more shopping-list information.
type OrderFields struct {
	Attempt   string
	Items     string
	Location  string
	ListKey   string
	ReturnURL string
}
