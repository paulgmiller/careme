package types

import (
	"careme/internal/ai"
	locationtypes "careme/internal/locations/types"
)

// OrderViewInput carries the displayed shopping list to its destination's form builder.
type OrderViewInput struct {
	Location  locationtypes.Location
	Items     []ai.ShoppingItem
	ListKey   string
	ReturnURL string
}

// OrderView describes an HTTP handoff using shared form fields and destination-owned copy.
type OrderView struct {
	// IdempotencyKey identifies one intended addition and is submitted as a hidden form field.
	IdempotencyKey string
	// TransferURL accepts the shopping form POST to create or resume a cart transfer.
	TransferURL         string
	ActionLabel         string
	TransferRequestJSON string
	Description         string
}

// TransferRequest sends the displayed shopping list to a destination.
// The browser submits the idempotency key separately in the idempotency_key form field.
type TransferRequest struct {
	Items     []ai.ShoppingItem      `json:"items"`
	Location  locationtypes.Location `json:"location"`
	ReturnURL string                 `json:"return_url"`
}

// Transfer describes a cart handoff, never checkout or a placed order.
// CartURL opens the destination cart; the provider handler owns login redirects.
// Added counts packages sent; Unmatched counts shopping items not sent.
type Transfer struct {
	// TransferID is the original idempotency key, scoped to the authenticated user and destination.
	TransferID string `json:"transfer_id"`
	Status     string `json:"status"`
	CartURL    string `json:"cart_url"`
	Added      int    `json:"added"`
	Unmatched  int    `json:"unmatched"`
	Code       string `json:"code,omitempty"`
	Message    string `json:"message"`
}
