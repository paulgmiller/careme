package types

import (
	"crypto/sha256"
	"encoding/hex"

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
	ActionURL   string
	StatusURL   string
	Transfer    *Transfer
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
	Items     string
	Location  string
	ListKey   string
	ReturnURL string
}

// TransferRequest sends the displayed shopping list to a destination. ListKey is
// the idempotency key for this user's intended addition, not an order number.
type TransferRequest struct {
	Items     []ai.ShoppingItem      `json:"items"`
	Location  locationtypes.Location `json:"location"`
	ListKey   string                 `json:"list_key"`
	ReturnURL string                 `json:"return_url"`
}

// Transfer describes a cart handoff, never checkout or a placed order.
// CartURL opens login while pending_login, or the destination cart otherwise.
// Added counts packages sent; Unmatched counts shopping items not sent.
type Transfer struct {
	TransferID string `json:"transfer_id"`
	Status     string `json:"status"`
	CartURL    string `json:"cart_url"`
	Added      int    `json:"added"`
	Unmatched  int    `json:"unmatched"`
	Code       string `json:"code,omitempty"`
	Message    string `json:"message"`
}

// TransferID is stable for a finalized list. IDs are scoped to the authenticated
// user and destination; possessing an ID never grants access.
func TransferID(listKey string) string {
	sum := sha256.Sum256([]byte(listKey))
	return hex.EncodeToString(sum[:])
}
