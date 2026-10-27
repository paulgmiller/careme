package kroger

import (
	"encoding/json"
	"fmt"
	"net/url"

	ordertypes "careme/internal/orders/types"
)

// OrderView supplies the generic cart-transfer UI with Kroger endpoints.
func OrderView(input ordertypes.OrderViewInput) (*ordertypes.OrderView, error) {
	body, err := json.Marshal(input.Items)
	if err != nil {
		return nil, fmt.Errorf("encode shopping items for Kroger: %w", err)
	}
	location, err := json.Marshal(input.Location)
	if err != nil {
		return nil, fmt.Errorf("encode ordering location: %w", err)
	}
	headers, err := json.Marshal(map[string]string{"Idempotency-Key": input.ListKey})
	if err != nil {
		return nil, fmt.Errorf("encode cart transfer headers: %w", err)
	}
	brand := input.Location.Chain
	if brand == "" {
		brand = "Kroger"
	}
	order := &ordertypes.OrderView{
		TransferHeadersJSON: string(headers),
		TransferURL:         "/kroger/cart-transfers",
		TransferStatusURL:   "/kroger/cart-transfers/" + url.PathEscape(input.ListKey),
		ActionLabel:         "Add to " + brand + " cart",
		Description:         "Adds one package of each matched product. Review amounts, store, and checkout in your cart.",
		ReviewLabel:         "Review cart (opens a new tab)",
		Fields: ordertypes.OrderFields{
			Items: string(body), Location: string(location),
			ReturnURL: input.ReturnURL,
		},
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
