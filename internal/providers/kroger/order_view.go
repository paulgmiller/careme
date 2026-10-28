package kroger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"careme/internal/cache"

	ordertypes "careme/internal/orders/types"
)

// OrderViewForUser reads transfer history without contacting Kroger or changing the cart.
func OrderViewForUser(ctx context.Context, c cache.Cache, input ordertypes.OrderViewInput) (*ordertypes.OrderView, error) {
	if input.UserID == "" {
		return nil, fmt.Errorf("order view requires an authenticated Kroger user ID")
	}
	reader, err := c.Get(ctx, transferKey(input.UserID, input.ListKey))
	if errors.Is(err, cache.ErrNotFound) {
		return OrderView(input)
	}
	if err != nil {
		return nil, fmt.Errorf("read Kroger transfer: %w", err)
	}
	defer func() { _ = reader.Close() }()
	var transfer cartTransfer
	if err := json.NewDecoder(reader).Decode(&transfer); err != nil {
		return nil, fmt.Errorf("decode Kroger transfer: %w", err)
	}
	switch transfer.Result.Code {
	case "complete", "pending", "uncertain", "confirmation_failed":
		return &ordertypes.OrderView{ReviewCartURL: transfer.Result.CartURL}, nil
	}

	return OrderView(input)
}

// OrderView supplies the generic cart-transfer UI with Kroger endpoints.
func OrderView(input ordertypes.OrderViewInput) (*ordertypes.OrderView, error) {
	body, err := json.Marshal(ordertypes.TransferRequest{Items: input.Items, Location: input.Location, ReturnURL: input.ReturnURL})
	if err != nil {
		return nil, fmt.Errorf("encode Kroger transfer request: %w", err)
	}
	brand := input.Location.Chain
	if brand == "" {
		brand = "Kroger"
	}
	order := &ordertypes.OrderView{
		IdempotencyKey:      input.ListKey,
		TransferURL:         "/kroger/order-transfers",
		TransferLabel:       "Add to " + brand + " cart",
		Description:         "Adds one package of each matched product. Review amounts, store, and checkout in your cart.",
		TransferRequestJSON: string(body),
	}

	return order, nil
}

func krogerOrderNotice(code string) string {
	return map[string]string{
		"pending":             "Your transfer is still running or could not be confirmed. Review your cart before trying again.",
		"complete":            "Your products were sent to your cart. Review amounts before checkout.",
		"no_matches":          "No products could be matched. Nothing was sent to Kroger.",
		"uncertain":           "Careme could not confirm what Kroger added. Review your cart before adding missing items.",
		"denied":              "Kroger connection was not approved. Try again, chef.",
		"missing_code":        "Kroger did not finish connecting your account. Try again, chef.",
		"connect_failed":      "Unable to connect to Kroger. Try again, chef.",
		"save_failed":         "Unable to save your Kroger connection. Try again, chef.",
		"lookup_failed":       "Could not check Kroger products. Nothing was sent. Try again, chef.",
		"start_failed":        "Could not start your Kroger transfer. Check your cart before trying again.",
		"confirmation_failed": "Could not save the transfer result. Items may have been sent. Review your Kroger cart.",
	}[code]
}
