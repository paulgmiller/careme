package kroger

import (
	"encoding/json"
	"fmt"
	"strconv"

	ordertypes "careme/internal/orders/types"
)

// OrderView builds Kroger's form and interprets its informational return fields.
func OrderView(input ordertypes.OrderViewInput) (*ordertypes.OrderView, error) {
	body, err := json.Marshal(input.Items)
	if err != nil {
		return nil, fmt.Errorf("encode shopping items for Kroger: %w", err)
	}
	location, err := json.Marshal(input.Location)
	if err != nil {
		return nil, fmt.Errorf("encode ordering location: %w", err)
	}
	brand := input.Location.Chain
	if brand == "" {
		brand = "Kroger"
	}
	order := &ordertypes.OrderView{
		ActionURL:   "/kroger/order",
		ActionLabel: "Add to " + brand + " cart",
		Description: "Adds one package of each matched product. Review amounts, store, and checkout in your cart.",
		ReviewLabel: "Review cart (opens a new tab)",
		Fields: ordertypes.OrderFields{
			Items: string(body), Location: string(location),
			ListKey: input.ListKey, ReturnURL: input.ReturnURL,
		},
	}
	status := input.Query.Get("kroger_status")
	order.Notice = krogerOrderNotice(status)
	if status == "complete" {
		order.ActionURL = ""
		order.ActionLabel = ""
		order.Description = ""
		added, addedErr := strconv.Atoi(input.Query.Get("kroger_added"))
		unmatched, unmatchedErr := strconv.Atoi(input.Query.Get("kroger_unmatched"))
		if addedErr == nil && unmatchedErr == nil && added >= 0 && unmatched >= 0 {
			order.Notice = fmt.Sprintf("Sent %d products to your cart. Review amounts before checkout.", added)
			if unmatched > 0 {
				if unmatched == 1 {
					order.Notice += " 1 item could not be matched and was not sent."
				} else {
					order.Notice += fmt.Sprintf(" %d items could not be matched and were not sent.", unmatched)
				}
			}
		}
	}
	if order.Notice != "" {
		if destination := input.Query.Get("kroger_cart"); validCartURL(destination) {
			order.ReviewURL = destination
		}
	}
	return order, nil
}

func krogerOrderNotice(code string) string {
	return map[string]string{
		"complete":            "Your products were sent to your cart. Review amounts before checkout.",
		"no_matches":          "No products could be matched. Nothing was sent to Kroger.",
		"changed":             "Your list changed after it was sent. Review your cart; earlier items will not be sent twice.",
		"pending":             "Your cart update is in progress or could not be confirmed. Review your cart before adding missing items.",
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

// orderViewWithTransfer formats transfer state already loaded by the ordering provider.
func orderViewWithTransfer(input ordertypes.OrderViewInput, latest orderResult, attempt int) (*ordertypes.OrderView, error) {
	view, err := OrderView(input)
	if err != nil {
		return nil, err
	}
	if attempt == 0 {
		return view, nil
	}
	view.ActionURL = ""
	view.ActionLabel = ""
	view.Description = ""
	if view.Notice == "" || input.Query.Get("kroger_status") == "complete" {
		view.Notice = krogerOrderNotice(latest.Status)
		if latest.Status == "complete" {
			view.Notice = fmt.Sprintf("Sent %d products to your cart. Review amounts before checkout.", latest.Added)
			if len(latest.Gaps) == 1 {
				view.Notice += " 1 item could not be matched and was not sent."
			} else if len(latest.Gaps) > 1 {
				view.Notice += fmt.Sprintf(" %d items could not be matched and were not sent.", len(latest.Gaps))
			}
		}
	}
	view.ReviewURL = cartURL(input.Location.Chain)
	if latest.Status == "complete" || latest.Status == "uncertain" {
		view.ActionURL = "/kroger/order"
		view.ActionLabel = "Add again"
		view.Secondary = true
		view.Fields.Attempt = strconv.Itoa(attempt)
		view.Description = "Adds this list again, including products already in your cart."
	}
	return view, nil
}
