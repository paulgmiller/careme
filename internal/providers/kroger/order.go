package kroger

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"careme/internal/cache"
	ordertypes "careme/internal/orders/types"
	"careme/internal/parallelism"
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
	for _, item := range input.Items {
		if strings.TrimSpace(item.ProductID) == "" {
			slog.ErrorContext(r.Context(), "Kroger shopping item has no product ID", "item_name", item.Name, "transfer_id", idempotencyKey)
			http.Error(w, "Cart transfer contains an item without a product ID.", http.StatusBadRequest)
			return
		}
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
	// Kroger adds the requested whole-package quantity for each product.
	var items []CartItem
	var accepted []ordertypes.TransferItem
	resolved, err := a.resolveUPCs(ctx, input)
	if err != nil {
		return transfer, err
	}
	for _, item := range input.Items {
		quantity := item.PackageQuantity()
		result := ordertypes.TransferItem{ProductID: item.ProductID, Name: item.Name}
		lookup := resolved[item.ProductID]
		if lookup.unavailable {
			result.RejectedQuantity = quantity
			result.RejectionReason = "product_unavailable"
			transfer.Items = append(transfer.Items, result)
			continue
		}
		items = append(items, CartItem{UPC: lookup.upc, Quantity: quantity})
		result.AcceptedQuantity = quantity
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

type upcResult struct {
	upc         string
	unavailable bool
}

func (a *OrderHandler) resolveUPCs(ctx context.Context, request ordertypes.TransferRequest) (map[string]*upcResult, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(map[string]*upcResult)
	var productIDs []string
	for _, item := range request.Items {
		if strings.TrimSpace(item.ProductID) == "" {
			return nil, fmt.Errorf("resolve Kroger order product %q: missing product ID", item.Name)
		}
		if _, exists := results[item.ProductID]; exists {
			continue
		}
		results[item.ProductID] = nil
		productIDs = append(productIDs, item.ProductID)
	}
	resolved, err := parallelism.MapWithErrors(productIDs, func(productID string) (*upcResult, error) {
		upc, err := a.client.ProductUPC(ctx, productID, request.Location.ID)
		if errors.Is(err, ErrProductUnavailable) {
			return &upcResult{unavailable: true}, nil
		}
		if err != nil {
			cancel()
			return nil, fmt.Errorf("resolve Kroger order product %q: %w", productID, err)
		}
		return &upcResult{upc: upc}, nil
	})
	if err != nil {
		return nil, err
	}
	for i, productID := range productIDs {
		results[productID] = resolved[i]
	}
	return results, nil
}
