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
	root := transferKey(input.UserID, input.ListKey)
	for _, suffix := range []string{"/submission", "/state"} {
		reader, err := c.Get(ctx, root+suffix)
		if errors.Is(err, cache.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("read Kroger transfer%s: %w", suffix, err)
		}
		var result ordertypes.Transfer
		if suffix == "/submission" {
			err = json.NewDecoder(reader).Decode(&result)
		} else {
			var transfer cartTransfer
			err = json.NewDecoder(reader).Decode(&transfer)
			result = transfer.Result
		}
		_ = reader.Close()
		if err != nil {
			return nil, fmt.Errorf("decode Kroger transfer%s: %w", suffix, err)
		}
		switch result.Code {
		case "complete", "uncertain", "confirmation_failed":
			return &ordertypes.OrderView{ReviewCartURL: result.CartURL}, nil
		}
		break
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
		ActionLabel:         "Add to " + brand + " cart",
		Description:         "Adds one package of each matched product. Review amounts, store, and checkout in your cart.",
		TransferRequestJSON: string(body),
	}

	return order, nil
}

func krogerOrderNotice(code string) string {
	return map[string]string{
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
