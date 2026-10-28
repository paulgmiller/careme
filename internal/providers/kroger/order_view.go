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
	var transfer ordertypes.Transfer
	if err := json.NewDecoder(reader).Decode(&transfer); err != nil {
		return nil, fmt.Errorf("decode Kroger transfer: %w", err)
	}
	switch transfer.Status {
	case ordertypes.TransferFailed:
		return &ordertypes.OrderView{TransferFailed: true}, nil
	case ordertypes.TransferApplied, ordertypes.TransferPending:
		return &ordertypes.OrderView{ReviewCartURL: transfer.CartURL}, nil
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
		TransferRequestJSON: string(body),
	}

	return order, nil
}
