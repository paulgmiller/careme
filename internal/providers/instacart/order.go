package instacart

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"careme/internal/auth"
	"careme/internal/cache"
	ordertypes "careme/internal/orders/types"
	"careme/internal/routing"
)

const orderTransferURL = "/instacart/order-transfers"

type OrderHandler struct {
	cache  cache.Cache
	auth   auth.AuthClient
	client *OrderClient
}

func NewOrderHandler(c cache.Cache, authClient auth.AuthClient, client *OrderClient) *OrderHandler {
	return &OrderHandler{cache: c, auth: authClient, client: client}
}

func (h *OrderHandler) Register(routes routing.Registrar) {
	routes.Handle("POST "+orderTransferURL, http.NewCrossOriginProtection().Handler(http.HandlerFunc(h.handleOrder)))
}

func orderTransferKey(userID, key string) string {
	return "instacart_order_transfers/" + userID + "/" + key
}

func loadOrderTransfer(ctx context.Context, c cache.Cache, userID, key string) (ordertypes.Transfer, error) {
	reader, err := c.Get(ctx, orderTransferKey(userID, key))
	if err != nil {
		return ordertypes.Transfer{}, err
	}
	defer func() { _ = reader.Close() }()
	var transfer ordertypes.Transfer
	err = json.NewDecoder(reader).Decode(&transfer)
	return transfer, err
}

func saveOrderTransfer(ctx context.Context, c cache.Cache, userID, key string, transfer ordertypes.Transfer, options cache.PutOptions) error {
	body, err := json.Marshal(transfer)
	if err != nil {
		return fmt.Errorf("encode Instacart transfer: %w", err)
	}
	if err := c.Put(ctx, orderTransferKey(userID, key), string(body), options); err != nil {
		return fmt.Errorf("save Instacart transfer: %w", err)
	}
	return nil
}

// OrderViewForUser reads saved handoffs without contacting Instacart or creating a list.
func OrderViewForUser(ctx context.Context, c cache.Cache, input ordertypes.OrderViewInput) (*ordertypes.OrderView, error) {
	if input.UserID == "" {
		return nil, fmt.Errorf("instacart order view requires an authenticated user ID")
	}
	transfer, err := loadOrderTransfer(ctx, c, input.UserID, input.ListKey)
	if errors.Is(err, cache.ErrNotFound) {
		body, err := json.Marshal(ordertypes.TransferRequest{Items: input.Items, Location: input.Location, ReturnURL: input.ReturnURL})
		if err != nil {
			return nil, fmt.Errorf("encode Instacart transfer request: %w", err)
		}
		return &ordertypes.OrderView{TransferURL: orderTransferURL, TransferLabel: "Shop with Instacart", IdempotencyKey: input.ListKey, TransferRequestJSON: string(body)}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read Instacart transfer: %w", err)
	}
	switch transfer.Status {
	case ordertypes.TransferFailed:
		return &ordertypes.OrderView{TransferFailed: true}, nil
	case ordertypes.TransferApplied, ordertypes.TransferPending:
		return &ordertypes.OrderView{ReviewCartURL: transfer.CartURL}, nil
	default:
		return nil, fmt.Errorf("unknown Instacart saved transfer status %q", transfer.Status)
	}
}

func (h *OrderHandler) handleOrder(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid Instacart transfer.", http.StatusBadRequest)
		return
	}
	var input ordertypes.TransferRequest
	if err := json.Unmarshal([]byte(r.PostForm.Get("transfer_request")), &input); err != nil {
		http.Error(w, "Invalid Instacart transfer.", http.StatusBadRequest)
		return
	}
	key := r.PostForm.Get("idempotency_key")
	if !validOrderForm(key, input) {
		http.Error(w, "Invalid Instacart transfer.", http.StatusBadRequest)
		return
	}
	userID, err := h.auth.GetUserIDFromRequest(r)
	if errors.Is(err, auth.ErrNoSession) {
		http.Redirect(w, r, "/sign-in?return_to_b64="+base64.RawURLEncoding.EncodeToString([]byte(input.ReturnURL)), http.StatusSeeOther)
		return
	}
	if err != nil {
		http.Error(w, "Unable to load account.", http.StatusInternalServerError)
		return
	}
	_, err = loadOrderTransfer(r.Context(), h.cache, userID, key)
	if err == nil {
		http.Redirect(w, r, input.ReturnURL, http.StatusSeeOther)
		return
	}
	if !errors.Is(err, cache.ErrNotFound) {
		http.Error(w, "Unable to read Instacart transfer.", http.StatusInternalServerError)
		return
	}
	transfer := ordertypes.Transfer{TransferID: key, Status: ordertypes.TransferPending}
	if err := saveOrderTransfer(r.Context(), h.cache, userID, key, transfer, cache.IfNoneMatch()); err != nil {
		http.Error(w, "Unable to start Instacart transfer.", http.StatusConflict)
		return
	}
	destination, err := h.client.create(r.Context(), input)
	if err != nil {
		slog.ErrorContext(r.Context(), "Instacart transfer failed", "error", err)
		transfer.Status = ordertypes.TransferFailed
	} else {
		transfer.Status = ordertypes.TransferApplied
		transfer.CartURL = destination
	}
	if err := saveOrderTransfer(r.Context(), h.cache, userID, key, transfer, cache.Unconditional()); err != nil {
		http.Error(w, "Unable to save Instacart transfer.", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, input.ReturnURL, http.StatusSeeOther)
}

func validOrderForm(key string, input ordertypes.TransferRequest) bool {
	for _, item := range input.Items {
		// Instacart matches names, so pantry items and other stores' IDs are supported.
		if strings.TrimSpace(item.Name) == "" {
			return false
		}
	}
	u, err := url.Parse(input.ReturnURL)
	return err == nil && !u.IsAbs() && u.Host == "" && strings.HasPrefix(input.ReturnURL, "/") && !strings.HasPrefix(u.Path, "//") && !strings.ContainsAny(u.Path, "\\\r\n") &&
		strings.TrimSpace(key) != "" && len(key) <= 255 && !strings.ContainsAny(key, "/\\") && key != "." && key != ".." && len(input.Items) > 0 && strings.TrimSpace(input.Location.ID) != ""
}
