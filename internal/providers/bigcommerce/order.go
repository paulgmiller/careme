package bigcommerce

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"careme/internal/ai"
	"careme/internal/auth"
	"careme/internal/cache"
	locationtypes "careme/internal/locations/types"
	ordertypes "careme/internal/orders/types"
	"careme/internal/routing"
)

var transferPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

type OrderOptions struct {
	Cache          cache.Cache
	Client         *Client
	Auth           auth.AuthClient
	LocationPrefix string
}

type OrderHandler struct {
	cache          cache.Cache
	client         *Client
	auth           auth.AuthClient
	locationPrefix string
}

func NewOrderHandler(options OrderOptions) (*OrderHandler, error) {
	if options.Cache == nil || options.Client == nil || options.Auth == nil || strings.TrimSpace(options.LocationPrefix) == "" {
		return nil, fmt.Errorf("bigcommerce ordering requires cache, client, authentication, source location prefix")
	}
	return &OrderHandler{cache: options.Cache, client: options.Client, auth: options.Auth, locationPrefix: options.LocationPrefix}, nil
}

func (a *OrderHandler) Register(routes routing.Registrar) {
	routes.Handle("POST /bigcommerce/order", http.NewCrossOriginProtection().Handler(http.HandlerFunc(a.handleOrder)))
	routes.HandleFunc("GET /bigcommerce/cart", a.handleCart)
}

func (a *OrderHandler) user(w http.ResponseWriter, r *http.Request) (string, bool) {
	w.Header().Set("Cache-Control", "no-store")
	id, err := a.auth.GetUserIDFromRequest(r)
	if err == nil {
		return id, true
	}
	if errors.Is(err, auth.ErrNoSession) {
		http.Error(w, "must be logged in", http.StatusUnauthorized)
	} else {
		http.Error(w, "Unable to load account", http.StatusInternalServerError)
	}
	return "", false
}

type transfer struct {
	Status      string `json:"status"`
	Fingerprint string `json:"fingerprint"`
	CartID      string `json:"cart_id,omitempty"`
	Added       int    `json:"added"`
	Unmatched   int    `json:"unmatched"`
}

func (a *OrderHandler) key(userID, transferID string) string {
	return "bigcommerce_cart_transfers/" + userID + "/" + transferID
}

func (a *OrderHandler) load(ctx context.Context, key string) (transfer, error) {
	reader, err := a.cache.Get(ctx, key)
	if err != nil {
		return transfer{}, err
	}
	defer func() { _ = reader.Close() }()
	var record transfer
	if err := json.NewDecoder(reader).Decode(&record); err != nil {
		return transfer{}, fmt.Errorf("decode BigCommerce transfer: %w", err)
	}
	return record, nil
}

func (a *OrderHandler) save(ctx context.Context, key string, record transfer, options cache.PutOptions) error {
	body, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode BigCommerce transfer: %w", err)
	}
	return a.cache.Put(ctx, key, string(body), options)
}

func (a *OrderHandler) handleOrder(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.user(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid BigCommerce order", http.StatusBadRequest)
		return
	}
	back := r.PostForm.Get("return_url")
	listKey := r.PostForm.Get("list_key")
	var location locationtypes.Location
	var items []ai.ShoppingItem
	if !localReturnURL(back) || strings.TrimSpace(listKey) == "" || len(listKey) > 256 || json.Unmarshal([]byte(r.PostForm.Get("location")), &location) != nil || !strings.HasPrefix(location.ID, a.locationPrefix) || json.Unmarshal([]byte(r.PostForm.Get("items")), &items) != nil || len(items) == 0 {
		http.Error(w, "Invalid BigCommerce order", http.StatusBadRequest)
		return
	}
	for _, item := range items {
		if strings.TrimSpace(item.Name) == "" || (item.Buy != nil && (*item.Buy <= 0 || math.IsNaN(*item.Buy) || math.IsInf(*item.Buy, 0) || *item.Buy > 2147483647)) {
			http.Error(w, "Invalid BigCommerce item", http.StatusBadRequest)
			return
		}
	}
	sum := sha256.Sum256([]byte(a.client.apiPath + "\n" + location.ID + "\n" + listKey))
	transferID := hex.EncodeToString(sum[:])
	body, _ := json.Marshal(items)
	fingerprint := fmt.Sprintf("%x", sha256.Sum256(body))
	record, err := a.load(r.Context(), a.key(userID, transferID))
	if err == nil {
		if record.Fingerprint != fingerprint {
			record.Status = "changed"
		}
		a.returnToList(w, r, back, transferID, record)
		return
	}
	if !errors.Is(err, cache.ErrNotFound) {
		a.fail(w, r, back, "start_failed", err)
		return
	}
	lines, unmatched, err := a.resolve(r.Context(), items)
	if err != nil {
		a.fail(w, r, back, "lookup_failed", err)
		return
	}
	record = transfer{Status: "pending", Fingerprint: fingerprint, Unmatched: unmatched}
	if len(lines) == 0 {
		record.Status = "no_matches"
		a.returnToList(w, r, back, transferID, record)
		return
	}
	key := a.key(userID, transferID)
	if err := a.save(r.Context(), key, record, cache.IfNoneMatch()); err != nil {
		if errors.Is(err, cache.ErrAlreadyExists) {
			previous, err := a.load(r.Context(), key)
			if err != nil {
				a.fail(w, r, back, "start_failed", err)
				return
			}
			if previous.Fingerprint != fingerprint {
				previous.Status = "changed"
			}
			a.returnToList(w, r, back, transferID, previous)
			return
		}
		a.fail(w, r, back, "start_failed", err)
		return
	}
	cartID, err := a.client.CreateCart(r.Context(), lines)
	if err != nil {
		slog.ErrorContext(r.Context(), "Create BigCommerce cart", "error", err)
		record.Status = "uncertain"
	} else {
		record.Status = "complete"
		record.CartID = cartID
		record.Added = len(lines)
	}
	if err := a.save(r.Context(), key, record, cache.Unconditional()); err != nil {
		a.fail(w, r, back, "confirmation_failed", err)
		return
	}
	a.returnToList(w, r, back, transferID, record)
}

func (a *OrderHandler) resolve(ctx context.Context, items []ai.ShoppingItem) ([]LineItem, int, error) {
	var lines []LineItem
	unmatched := 0
	seen := map[int]bool{}
	for _, item := range items {
		id, err := strconv.Atoi(item.ProductID)
		// Pantry and static produce-share entries do not identify purchasable products.
		if err != nil || id <= 0 {
			unmatched++
			continue
		}
		if _, ok := seen[id]; ok {
			return nil, 0, fmt.Errorf("duplicate BigCommerce product %d in merged list", id)
		}
		seen[id] = true
		line, err := a.client.Resolve(ctx, id)
		if errors.Is(err, ErrUnavailable) {
			unmatched++
			continue
		}
		if err != nil {
			return nil, 0, err
		}
		if item.Buy != nil {
			line.Quantity = int(math.Ceil(*item.Buy))
		}
		lines = append(lines, line)
	}
	return lines, unmatched, nil
}

func (a *OrderHandler) handleCart(w http.ResponseWriter, r *http.Request) {
	userID, ok := a.user(w, r)
	if !ok {
		return
	}
	transferID := r.URL.Query().Get("transfer")
	if !transferPattern.MatchString(transferID) {
		http.Error(w, "Invalid cart reference", http.StatusBadRequest)
		return
	}
	record, err := a.load(r.Context(), a.key(userID, transferID))
	if errors.Is(err, cache.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "Could not load your cart. Try again, chef.", http.StatusInternalServerError)
		return
	}
	if record.Status != "complete" {
		http.Error(w, "Your cart is not ready for review.", http.StatusConflict)
		return
	}
	// BigCommerce redirect tokens may be visited only once. Never cache these URLs.
	destination, err := a.client.CartRedirect(r.Context(), record.CartID)
	if err != nil {
		slog.ErrorContext(r.Context(), "Review BigCommerce cart", "error", err)
		http.Error(w, "Could not open your cart. It may have expired or already been checked out. Try again, chef.", http.StatusBadGateway)
		return
	}
	http.Redirect(w, r, destination, http.StatusSeeOther)
}

func (a *OrderHandler) fail(w http.ResponseWriter, r *http.Request, back, code string, err error) {
	slog.ErrorContext(r.Context(), "Prepare BigCommerce order", "error", err)
	a.returnToList(w, r, back, "", transfer{Status: code})
}

func (a *OrderHandler) returnToList(w http.ResponseWriter, r *http.Request, back, transferID string, record transfer) {
	u, _ := url.Parse(back)
	q := u.Query()
	q.Set("bigcommerce_status", record.Status)
	q.Set("bigcommerce_added", strconv.Itoa(record.Added))
	q.Set("bigcommerce_unmatched", strconv.Itoa(record.Unmatched))
	q.Del("bigcommerce_transfer")
	if record.CartID != "" {
		q.Set("bigcommerce_transfer", transferID)
	}
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusSeeOther)
}

func localReturnURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && !u.IsAbs() && u.Host == "" && strings.HasPrefix(raw, "/") && !strings.HasPrefix(u.Path, "//") && !strings.ContainsAny(u.Path, "\\\r\n")
}

func OrderView(input ordertypes.OrderViewInput, storeName string) (*ordertypes.OrderView, error) {
	items, err := json.Marshal(input.Items)
	if err != nil {
		return nil, fmt.Errorf("encode BigCommerce items: %w", err)
	}
	location, err := json.Marshal(input.Location)
	if err != nil {
		return nil, fmt.Errorf("encode BigCommerce location: %w", err)
	}
	view := &ordertypes.OrderView{ActionURL: "/bigcommerce/order", ActionLabel: "Add to " + storeName + " cart", Description: "Adds matched catalog products to a new cart. Package counts are rounded up; unknown amounts use one package. Pantry, produce-share contents, and unavailable products are not sent. Review products, options, and amounts before checkout.", ReviewLabel: "Review cart (opens a new tab)", Fields: ordertypes.OrderFields{Items: string(items), Location: string(location), ListKey: input.ListKey, ReturnURL: input.ReturnURL}}
	status := input.Query.Get("bigcommerce_status")
	view.Notice = map[string]string{
		"complete":            "Your matched products were added to a cart. Review products and amounts before checkout.",
		"changed":             "Your list changed after its cart was created. Review the earlier cart; products will not be sent twice.",
		"pending":             "Your cart creation is in progress or could not be confirmed. It will not be submitted again.",
		"uncertain":           "Careme could not confirm whether the cart was created. Check your store account before creating another list.",
		"no_matches":          "No purchasable catalog products could be matched. Nothing was sent.",
		"lookup_failed":       "Could not check store products. Nothing was sent. Try again, chef.",
		"start_failed":        "Could not start your cart transfer. Try again, chef.",
		"confirmation_failed": "Could not save the cart result. A cart may have been created; the list will not be submitted again.",
	}[status]
	if status == "complete" {
		added, addedErr := strconv.Atoi(input.Query.Get("bigcommerce_added"))
		unmatched, unmatchedErr := strconv.Atoi(input.Query.Get("bigcommerce_unmatched"))
		if addedErr == nil && unmatchedErr == nil && added >= 0 && unmatched >= 0 {
			products := "products"
			if added == 1 {
				products = "product"
			}
			notSent := "items were"
			if unmatched == 1 {
				notSent = "item was"
			}
			view.Notice = fmt.Sprintf("Sent %d matched %s to a cart. %d %s not sent. Review products and amounts before checkout.", added, products, unmatched, notSent)
		}
	}
	id := input.Query.Get("bigcommerce_transfer")
	if (status == "complete" || status == "changed") && transferPattern.MatchString(id) {
		view.ReviewURL = "/bigcommerce/cart?transfer=" + id
	}
	return view, nil
}
