package types

import (
	"careme/internal/ai"
	locationtypes "careme/internal/locations/types"
)

// OrderViewInput carries the displayed shopping list to its destination's form builder.
type OrderViewInput struct {
	ListKey  string // shoppinglist hash
	UserID   string // combines with above to look up
	Location locationtypes.Location
	Items    []ai.ShoppingItem

	ReturnURL string
}

// OrderView describes an HTTP handoff using shared form fields and destination-owned copy.
type OrderView struct {
	TransferFailed bool
	ReviewCartURL  string
	// IdempotencyKey identifies one intended addition and is submitted as a hidden form field.
	IdempotencyKey string
	// TransferURL accepts the shopping form POST to create or resume a cart transfer.
	TransferURL         string
	TransferLabel       string
	TransferRequestJSON string
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
	TransferID string         `json:"transfer_id"`
	Status     TransferStatus `json:"status"`
	CartURL    string         `json:"cart_url"`
	Added      int            `json:"added"`
	Unmatched  int            `json:"unmatched"`
}

// TransferStatus is the persisted outcome of a cart handoff.
type TransferStatus string

const (
	TransferPending TransferStatus = "pending"
	TransferFailed  TransferStatus = "failed"
	TransferApplied TransferStatus = "applied"
)
