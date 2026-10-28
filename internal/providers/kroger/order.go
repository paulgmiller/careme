package kroger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"careme/internal/ai"
	"careme/internal/cache"
	ordertypes "careme/internal/orders/types"
)

func (a *OrderHandler) handleOrder(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := r.ParseForm(); err != nil {
		writeTransferError(w, http.StatusBadRequest, "Invalid cart transfer.")
		return
	}
	var input ordertypes.TransferRequest
	if err := json.Unmarshal([]byte(r.PostForm.Get("transfer_request")), &input); err != nil {
		writeTransferError(w, http.StatusBadRequest, "Invalid cart transfer.")
		return
	}
	idempotencyKey := r.PostForm.Get("idempotency_key")
	if !localReturnURL(input.ReturnURL) || !validIdempotencyKey(idempotencyKey) || !NewIdentityProvider().IsID(input.Location.ID) || len(input.Items) == 0 {
		writeTransferError(w, http.StatusBadRequest, "Invalid cart transfer.")
		return
	}
	userID, ok := a.cartUser(w, r, input.ReturnURL)
	if !ok {
		return
	}
	a.createTransfer(w, r, cartTransfer{UserID: userID, IdempotencyKey: idempotencyKey, Request: input})
}

func validIdempotencyKey(key string) bool {
	return strings.TrimSpace(key) != "" && len(key) <= 255 && !strings.ContainsAny(key, "/\\") && key != "." && key != ".."
}

func localReturnURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && !u.IsAbs() && u.Host == "" && strings.HasPrefix(raw, "/") && !strings.HasPrefix(u.Path, "//") && !strings.ContainsAny(u.Path, "\\\r\n")
}

func (a *OrderHandler) loadJSON(ctx context.Context, key string, value any) error {
	reader, err := a.cache.Get(ctx, key)
	if err != nil {
		return err
	}
	defer func() { _ = reader.Close() }()
	return json.NewDecoder(reader).Decode(value)
}

func (a *OrderHandler) saveJSON(ctx context.Context, key string, value any, options cache.PutOptions) error {
	body, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return a.cache.Put(ctx, key, string(body), options)
}

func (a *OrderHandler) order(ctx context.Context, transfer cartTransfer, token CartToken) (ordertypes.Transfer, error) {
	items, gaps, err := a.resolveItems(ctx, transfer.Request)
	if err != nil {
		return transfer.result(ordertypes.TransferFailed, 0, 0), err
	}
	if len(items) == 0 {
		result := transfer.result(ordertypes.TransferFailed, 0, len(gaps))
		return result, nil
	}
	if err := a.client.Add(ctx, token.AccessToken, items); err != nil {
		return transfer.result(ordertypes.TransferFailed, 0, len(gaps)), fmt.Errorf("add Kroger cart products: %w", err)
	}
	return transfer.result(ordertypes.TransferApplied, len(items), len(gaps)), nil
}

func (a *OrderHandler) resolveItems(ctx context.Context, request ordertypes.TransferRequest) ([]CartItem, []ai.ShoppingItem, error) {
	var items []CartItem
	var gaps []ai.ShoppingItem
	seen := make(map[string]bool)
	for _, item := range request.Items {
		if item.ProductID == "" {
			gaps = append(gaps, item)
			continue
		}
		upc, err := a.client.ProductUPC(ctx, item.ProductID, request.Location.ID)
		if errors.Is(err, ErrProductUnavailable) {
			gaps = append(gaps, item)
			continue
		}
		if err != nil {
			return nil, nil, fmt.Errorf("resolve Kroger order product: %w", err)
		}
		if !seen[upc] {
			items = append(items, CartItem{UPC: upc, Quantity: 1})
			seen[upc] = true
		}
	}
	return items, gaps, nil
}
