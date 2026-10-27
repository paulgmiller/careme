package kroger

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	locationtypes "careme/internal/locations/types"
	ordertypes "careme/internal/orders/types"
)

// orderRequest is the posted shopping list retained while connecting Kroger.
// UserID comes exclusively from authentication, never from the submitted form.
type orderRequest struct {
	UserID         string
	IdempotencyKey string
	StoreID        string
	Chain          string
	ReturnURL      string
	Items          []ai.ShoppingItem
}

type orderResult struct {
	Status string            `json:"status"`
	Added  int               `json:"added"`
	Gaps   []ai.ShoppingItem `json:"gaps,omitempty"`
}

func (a *OrderHandler) handleOrder(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	userID, ok := a.cartUser(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var input ordertypes.TransferRequest
	mediaType, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	switch mediaType {
	case "application/json":
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&input); err != nil {
			writeTransferError(w, http.StatusBadRequest, "invalid_request", "Invalid cart transfer.")
			return
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			writeTransferError(w, http.StatusBadRequest, "invalid_request", "Invalid cart transfer.")
			return
		}
	case "application/x-www-form-urlencoded":
		if err := r.ParseForm(); err != nil {
			writeTransferError(w, http.StatusBadRequest, "invalid_request", "Invalid cart transfer.")
			return
		}
		var location locationtypes.Location
		if json.Unmarshal([]byte(r.PostForm.Get("location")), &location) != nil || json.Unmarshal([]byte(r.PostForm.Get("items")), &input.Items) != nil {
			writeTransferError(w, http.StatusBadRequest, "invalid_request", "Invalid cart transfer.")
			return
		}
		input.Location, input.ReturnURL = location, r.PostForm.Get("return_url")
	default:
		writeTransferError(w, http.StatusUnsupportedMediaType, "unsupported_media_type", "Use JSON or a URL-encoded form.")
		return
	}
	request := orderRequest{UserID: userID, IdempotencyKey: r.Header.Get("Idempotency-Key"), StoreID: input.Location.ID, Chain: input.Location.Chain, ReturnURL: input.ReturnURL, Items: input.Items}
	if !localReturnURL(request.ReturnURL) || !validIdempotencyKey(request.IdempotencyKey) || !NewIdentityProvider().IsID(request.StoreID) || len(request.Items) == 0 {
		writeTransferError(w, http.StatusBadRequest, "invalid_request", "Invalid cart transfer.")
		return
	}
	a.createTransfer(w, r, request)
}

func validIdempotencyKey(key string) bool {
	return strings.TrimSpace(key) != "" && len(key) <= 256 && !strings.ContainsAny(key, "/\\") && key != "." && key != ".."
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

func orderFingerprint(request orderRequest) string {
	body, _ := json.Marshal(struct {
		StoreID string
		Items   []ai.ShoppingItem
	}{request.StoreID, request.Items})
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func (a *OrderHandler) order(ctx context.Context, request orderRequest, allowAuthorization bool) orderResult {
	key := submissionKey(request)
	var previous orderResult
	if err := a.loadJSON(ctx, key, &previous); err == nil {
		return previous
	} else if !errors.Is(err, cache.ErrNotFound) {
		return orderResult{Status: "start_failed"}
	}
	token, err := a.loadToken(ctx, request.UserID)
	if err != nil && !errors.Is(err, cache.ErrNotFound) {
		return orderResult{Status: "connect_failed"}
	}
	if err == nil && time.Now().After(token.ExpiresAt.Add(-time.Minute)) {
		token, err = a.client.Refresh(ctx, token.RefreshToken)
		if err == nil {
			if err = a.saveToken(ctx, request.UserID, token); err != nil {
				return orderResult{Status: "save_failed"}
			}
		}
	}
	if err != nil || token.AccessToken == "" {
		if allowAuthorization {
			return orderResult{Status: "pending_login"}
		} else {
			return orderResult{Status: "connect_failed"}
		}
	}
	items, gaps, err := a.resolveItems(ctx, request)
	if err != nil {
		return orderResult{Status: "lookup_failed"}
	}
	result := orderResult{Status: "pending", Gaps: gaps}
	if len(items) == 0 {
		result.Status = "no_matches"
		return result
	}
	if err := a.saveJSON(ctx, key, result, cache.IfNoneMatch()); err != nil {
		if errors.Is(err, cache.ErrAlreadyExists) {
			// Re-enter the read path; the winner owns the only cart write.
			return a.order(ctx, request, false)
		}
		result.Status = "start_failed"
		return result
	}
	if err := a.client.Add(ctx, token.AccessToken, items); err != nil {
		result.Status = "uncertain"
	} else {
		result.Status = "complete"
		result.Added = len(items)
	}
	if err := a.saveJSON(ctx, key, result, cache.Unconditional()); err != nil {
		result.Status = "confirmation_failed"
		return result
	}
	return result
}

func (a *OrderHandler) resolveItems(ctx context.Context, request orderRequest) ([]CartItem, []ai.ShoppingItem, error) {
	var items []CartItem
	var gaps []ai.ShoppingItem
	seen := make(map[string]bool)
	for _, item := range request.Items {
		if item.ProductID == "" {
			gaps = append(gaps, item)
			continue
		}
		upc, err := a.client.ProductUPC(ctx, item.ProductID, request.StoreID)
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
