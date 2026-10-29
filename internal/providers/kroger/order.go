package kroger

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"

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
		upc, err := resolved[item.ProductID].upc, resolved[item.ProductID].err
		if errors.Is(err, ErrProductUnavailable) {
			result.RejectedQuantity = quantity
			result.RejectionReason = "product_unavailable"
			transfer.Items = append(transfer.Items, result)
			continue
		}
		if err != nil {
			return transfer, fmt.Errorf("resolve Kroger order product: %w", err)
		}
		items = append(items, CartItem{UPC: upc, Quantity: quantity})
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

// productUPC caches the store-specific product mapping independently of inventory.
func (a *OrderHandler) productUPC(ctx context.Context, productID, locationID string) (string, error) {
	key := fmt.Sprintf("kroger_product_upcs/v1/%x", sha256.Sum256([]byte(locationID+"\x00"+productID)))
	var upc string
	if err := a.loadJSON(ctx, key, &upc); err == nil {
		return upc, nil
	} else if !errors.Is(err, cache.ErrNotFound) {
		return "", fmt.Errorf("read Kroger UPC cache: %w", err)
	}
	upc, err := a.client.ProductUPC(ctx, productID, locationID)
	if err != nil {
		return "", err
	}
	if err := a.saveJSON(ctx, key, upc, cache.Unconditional()); err != nil {
		return "", fmt.Errorf("save Kroger UPC cache: %w", err)
	}
	return upc, nil
}

type upcResult struct {
	upc string
	err error
}

func (a *OrderHandler) resolveUPCs(ctx context.Context, request ordertypes.TransferRequest) (map[string]*upcResult, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(map[string]*upcResult)
	var wg sync.WaitGroup
	var firstErr error
	var errOnce sync.Once
	for _, item := range request.Items {
		if item.ProductID == "" {
			continue
		}
		if _, exists := results[item.ProductID]; exists {
			continue
		}
		resolved := &upcResult{}
		results[item.ProductID] = resolved
		wg.Add(1)
		go func(productID string) {
			defer wg.Done()
			resolved.upc, resolved.err = a.productUPC(ctx, productID, request.Location.ID)
			if resolved.err != nil && !errors.Is(resolved.err, ErrProductUnavailable) {
				errOnce.Do(func() { firstErr = resolved.err; cancel() })
			}
		}(item.ProductID)
	}
	wg.Wait()
	if firstErr != nil {
		return nil, fmt.Errorf("resolve Kroger order product: %w", firstErr)
	}
	return results, nil
}
