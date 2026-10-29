package kroger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"

	"careme/internal/cache"
	ordertypes "careme/internal/orders/types"
)

func (a *OrderHandler) handleOrder(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid cart transfer.", http.StatusBadRequest)
		return
	}
	var input ordertypes.TransferRequest
	if err := json.Unmarshal([]byte(r.PostForm.Get("transfer_request")), &input); err != nil {
		http.Error(w, "Invalid cart transfer.", http.StatusBadRequest)
		return
	}
	idempotencyKey := r.PostForm.Get("idempotency_key")
	if !localReturnURL(input.ReturnURL) || !validIdempotencyKey(idempotencyKey) || !NewIdentityProvider().IsID(input.Location.ID) || len(input.Items) == 0 {
		http.Error(w, "Invalid cart transfer.", http.StatusBadRequest)
		return
	}
	userID, ok := a.cartUser(w, r, input.ReturnURL)
	if !ok {
		return
	}
	a.createTransfer(w, r, userID, idempotencyKey, input)
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

func (a *OrderHandler) order(ctx context.Context, idempotencyKey string, input ordertypes.TransferRequest, token CartToken) (ordertypes.Transfer, error) {
	transfer := ordertypes.Transfer{TransferID: idempotencyKey, Status: ordertypes.TransferFailed, CartURL: cartURL(input.Location.Chain)}
	// Kroger adds whole packages for each unique UPC.
	var items []CartItem
	var accepted []ordertypes.TransferItem
	seen := make(map[string]bool)
	// TODO: Save UPCs with catalog ingredients or resolve them in parallel.
	for _, item := range input.Items {
		quantity := 1
		if item.Buy != nil {
			if math.IsNaN(*item.Buy) || *item.Buy <= 0 || *item.Buy > math.MaxInt32 {
				return transfer, fmt.Errorf("invalid Kroger package quantity for %q", item.Name)
			}
			quantity = int(math.Ceil(*item.Buy))
		}
		result := ordertypes.TransferItem{ProductID: item.ProductID, Name: item.Name, RejectedQuantity: quantity}
		if item.ProductID == "" {
			result.RejectionReason = "missing_product_id"
			transfer.Items = append(transfer.Items, result)
			continue
		}
		upc, err := a.client.ProductUPC(ctx, item.ProductID, input.Location.ID)
		if errors.Is(err, ErrProductUnavailable) {
			result.RejectionReason = "product_unavailable"
			transfer.Items = append(transfer.Items, result)
			continue
		}
		if err != nil {
			return transfer, fmt.Errorf("resolve Kroger order product: %w", err)
		}
		if seen[upc] {
			result.RejectionReason = "duplicate_upc"
			transfer.Items = append(transfer.Items, result)
			continue
		}
		seen[upc] = true
		items = append(items, CartItem{UPC: upc, Quantity: quantity})
		result.AcceptedQuantity, result.RejectedQuantity = quantity, 0
		accepted = append(accepted, result)
	}
	if len(items) == 0 {
		return transfer, nil
	}
	if err := a.client.Add(ctx, token.AccessToken, items); err != nil {
		return transfer, fmt.Errorf("add Kroger cart products: %w", err)
	}
	transfer.Status = ordertypes.TransferApplied
	transfer.Items = append(accepted, transfer.Items...)
	return transfer, nil
}
