package kroger

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
)

// orderRequest is the posted shopping list retained while connecting Kroger.
// UserID comes exclusively from authentication, never from the submitted form.
type orderRequest struct {
	UserID    string
	ListKey   string
	StoreID   string
	Chain     string
	ReturnURL string
	Items     []ai.ShoppingItem
}

// Keep the original transfer record fields readable across the HTTP handoff change.
type orderResult struct {
	Status      string            `json:"status"`
	Added       int               `json:"added"`
	Gaps        []ai.ShoppingItem `json:"gaps,omitempty"`
	Fingerprint string            `json:"fingerprint,omitempty"`
}

func (a *OrderHandler) handleOrder(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	userID, ok := a.cartUser(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid Kroger order", http.StatusBadRequest)
		return
	}
	request := orderRequest{UserID: userID, ListKey: r.PostForm.Get("list_key"), StoreID: r.PostForm.Get("store_id"), Chain: r.PostForm.Get("chain"), ReturnURL: r.PostForm.Get("return_url")}
	if !localReturnURL(request.ReturnURL) || !validListKey(request.ListKey) || !NewIdentityProvider().IsID(request.StoreID) || json.Unmarshal([]byte(r.PostForm.Get("items")), &request.Items) != nil || len(request.Items) == 0 {
		http.Error(w, "Invalid Kroger order", http.StatusBadRequest)
		return
	}
	a.order(w, r, request, true)
}

func validListKey(key string) bool {
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

func cartURL(chain string) string {
	if strings.EqualFold(chain, "qfc") {
		return "https://www.qfc.com/cart"
	}
	return "https://www.kroger.com/cart"
}

func (a *OrderHandler) returnToList(w http.ResponseWriter, r *http.Request, request orderRequest, result orderResult, code string) {
	destination, _ := url.Parse(request.ReturnURL)
	query := destination.Query()
	if code == "" {
		code = result.Status
	}
	query.Set("kroger_status", code)
	query.Set("kroger_added", strconv.Itoa(result.Added))
	query.Set("kroger_unmatched", strconv.Itoa(len(result.Gaps)))
	query.Set("kroger_cart", cartURL(request.Chain))
	destination.RawQuery = query.Encode()
	http.Redirect(w, r, destination.String(), http.StatusSeeOther)
}

func (a *OrderHandler) order(w http.ResponseWriter, r *http.Request, request orderRequest, allowAuthorization bool) {
	ctx := r.Context()
	key := "kroger_cart_transfers/" + request.UserID + "/" + request.ListKey
	fingerprint := orderFingerprint(request)
	var previous orderResult
	if err := a.loadJSON(ctx, key, &previous); err == nil {
		if previous.Status == "complete" && previous.Fingerprint != fingerprint {
			previous.Status = "changed"
		}
		a.returnToList(w, r, request, previous, "")
		return
	} else if !errors.Is(err, cache.ErrNotFound) {
		a.returnToList(w, r, request, orderResult{}, "start_failed")
		return
	}
	token, err := a.loadToken(ctx, request.UserID)
	if err != nil && !errors.Is(err, cache.ErrNotFound) {
		a.returnToList(w, r, request, orderResult{}, "connect_failed")
		return
	}
	if err == nil && time.Now().After(token.ExpiresAt.Add(-time.Minute)) {
		token, err = a.client.Refresh(ctx, token.RefreshToken)
		if err == nil {
			if err = a.saveToken(ctx, request.UserID, token); err != nil {
				a.returnToList(w, r, request, orderResult{}, "save_failed")
				return
			}
		}
	}
	if err != nil || token.AccessToken == "" {
		if allowAuthorization {
			a.startAuthorization(w, r, request)
		} else {
			a.returnToList(w, r, request, orderResult{}, "connect_failed")
		}
		return
	}
	items, gaps, err := a.resolveItems(ctx, request)
	if err != nil {
		a.returnToList(w, r, request, orderResult{}, "lookup_failed")
		return
	}
	result := orderResult{Status: "pending", Gaps: gaps, Fingerprint: fingerprint}
	if len(items) == 0 {
		a.returnToList(w, r, request, result, "no_matches")
		return
	}
	if err := a.saveJSON(ctx, key, result, cache.IfNoneMatch()); err != nil {
		if errors.Is(err, cache.ErrAlreadyExists) {
			// Re-enter the read path; the winner owns the only cart write.
			a.order(w, r, request, false)
			return
		}
		a.returnToList(w, r, request, result, "start_failed")
		return
	}
	if err := a.client.Add(ctx, token.AccessToken, items); err != nil {
		result.Status = "uncertain"
	} else {
		result.Status = "complete"
		result.Added = len(items)
	}
	if err := a.saveJSON(ctx, key, result, cache.Unconditional()); err != nil {
		a.returnToList(w, r, request, result, "confirmation_failed")
		return
	}
	a.returnToList(w, r, request, result, "")
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
