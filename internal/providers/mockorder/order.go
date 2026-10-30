// Package mockorder supplies local cart transfers for every location in mock mode.
package mockorder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"careme/internal/auth"
	"careme/internal/cache"
	ordertypes "careme/internal/orders/types"
	"careme/internal/routing"
)

const transferURL = "/mock/order-transfers"

type Handler struct {
	cache cache.Cache
	auth  auth.AuthClient
}

func NewHandler(c cache.Cache, authClient auth.AuthClient) *Handler {
	return &Handler{cache: c, auth: authClient}
}

func (h *Handler) Register(routes routing.Registrar) {
	routes.Handle("POST "+transferURL, http.NewCrossOriginProtection().Handler(http.HandlerFunc(h.transfer)))
	routes.HandleFunc("GET "+transferURL+"/{id}", h.cart)
}

func transferKey(userID, id string) string {
	return "mock_order_transfers/" + userID + "/" + id
}

func validKey(id string) bool {
	return strings.TrimSpace(id) != "" && len(id) <= 255 && !strings.ContainsAny(id, "/\\") && id != "." && id != ".."
}

func readTransfer(ctx context.Context, c cache.Cache, userID, id string) (ordertypes.Transfer, error) {
	reader, err := c.Get(ctx, transferKey(userID, id))
	if err != nil {
		return ordertypes.Transfer{}, err
	}
	defer func() { _ = reader.Close() }()
	var transfer ordertypes.Transfer
	err = json.NewDecoder(reader).Decode(&transfer)
	return transfer, err
}

// OrderView handles all locations, including IDs from providers without real ordering.
func OrderView(ctx context.Context, c cache.Cache, input ordertypes.OrderViewInput) (*ordertypes.OrderView, error) {
	if input.UserID == "" {
		return nil, fmt.Errorf("mock order view requires an authenticated user ID")
	}
	transfer, err := readTransfer(ctx, c, input.UserID, input.ListKey)
	if err == nil {
		switch transfer.Status {
		case ordertypes.TransferApplied, ordertypes.TransferPending:
			return &ordertypes.OrderView{ReviewCartURL: transfer.CartURL}, nil
		case ordertypes.TransferFailed:
			return &ordertypes.OrderView{TransferFailed: true}, nil
		default:
			return nil, fmt.Errorf("unknown mock transfer status %q", transfer.Status)
		}
	}
	if !errors.Is(err, cache.ErrNotFound) {
		return nil, fmt.Errorf("read mock transfer: %w", err)
	}
	body, err := json.Marshal(ordertypes.TransferRequest{Items: input.Items, Location: input.Location, ZipCode: input.ZipCode, ReturnURL: input.ReturnURL})
	if err != nil {
		return nil, fmt.Errorf("encode mock transfer: %w", err)
	}
	return &ordertypes.OrderView{TransferURL: transferURL, TransferLabel: "Add to mock cart", IdempotencyKey: input.ListKey, TransferRequestJSON: string(body)}, nil
}

func (h *Handler) transfer(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid mock transfer", http.StatusBadRequest)
		return
	}
	var input ordertypes.TransferRequest
	if err := json.Unmarshal([]byte(r.PostForm.Get("transfer_request")), &input); err != nil {
		http.Error(w, "Invalid mock transfer", http.StatusBadRequest)
		return
	}
	id := r.PostForm.Get("idempotency_key")
	returnURL, err := url.Parse(input.ReturnURL)
	if err != nil || returnURL.IsAbs() || returnURL.Host != "" || !strings.HasPrefix(input.ReturnURL, "/") || strings.HasPrefix(returnURL.Path, "//") || strings.ContainsAny(returnURL.Path, "\\\r\n") || !validKey(id) || len(input.Items) == 0 {
		http.Error(w, "Invalid mock transfer", http.StatusBadRequest)
		return
	}
	userID, err := h.auth.GetUserIDFromRequest(r)
	if err != nil {
		http.Error(w, "Sign in to transfer your cart", http.StatusUnauthorized)
		return
	}
	if _, err := readTransfer(r.Context(), h.cache, userID, id); errors.Is(err, cache.ErrNotFound) {
		// The mock accepts every shopping item without external product matching.
		transfer := ordertypes.Transfer{TransferID: id, Status: ordertypes.TransferApplied, CartURL: transferURL + "/" + url.PathEscape(id)}
		for _, item := range input.Items {
			quantity := item.PackageQuantity()
			transfer.Items = append(transfer.Items, ordertypes.TransferItem{ProductID: item.ProductID, Name: item.Name, AcceptedQuantity: quantity})
		}
		body, err := json.Marshal(transfer)
		if err != nil {
			http.Error(w, "Unable to encode mock transfer", http.StatusInternalServerError)
			return
		}
		if err := h.cache.Put(r.Context(), transferKey(userID, id), string(body), cache.IfNoneMatch()); err != nil {
			http.Error(w, "Unable to save mock transfer", http.StatusInternalServerError)
			return
		}
	} else if err != nil {
		http.Error(w, "Unable to read mock transfer", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, input.ReturnURL, http.StatusSeeOther)
}

// cart lets the local Review cart link display the mock's saved result.
func (h *Handler) cart(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if !validKey(r.PathValue("id")) {
		http.Error(w, "Invalid mock transfer ID", http.StatusBadRequest)
		return
	}
	userID, err := h.auth.GetUserIDFromRequest(r)
	if err != nil {
		http.Error(w, "Sign in to review your cart", http.StatusUnauthorized)
		return
	}
	transfer, err := readTransfer(r.Context(), h.cache, userID, r.PathValue("id"))
	if errors.Is(err, cache.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		http.Error(w, "Unable to read mock transfer", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(transfer)
}
