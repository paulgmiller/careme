package mnfoodclub

import (
	"context"
	"crypto/sha256"
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

const orderTransferURL = "/mnfoodclub/order-transfers"

type orderHandler struct {
	cache  cache.Cache
	auth   auth.AuthClient
	client *orderClient
}

func NewOrderHandler(c cache.Cache, authClient auth.AuthClient, client *orderClient) *orderHandler {
	return &orderHandler{cache: c, auth: authClient, client: client}
}

func (h *orderHandler) Register(routes routing.Registrar) {
	routes.Handle("POST "+orderTransferURL, http.NewCrossOriginProtection().Handler(http.HandlerFunc(h.handleOrder)))
}

func orderTransferKey(userID, key string) string {
	return "mnfoodclub_order_transfers/" + userID + "/" + key
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
		return fmt.Errorf("encode MN Food Club transfer: %w", err)
	}
	if err := c.Put(ctx, orderTransferKey(userID, key), string(body), options); err != nil {
		return fmt.Errorf("save MN Food Club transfer: %w", err)
	}
	return nil
}

// OrderViewForUser resolves a pending login on the server; the browser never polls.
func OrderViewForUser(ctx context.Context, c cache.Cache, client *orderClient, input ordertypes.OrderViewInput) (*ordertypes.OrderView, error) {
	if input.UserID == "" {
		return nil, fmt.Errorf("MN Food Club order view requires an authenticated user ID")
	}
	transfer, err := loadOrderTransfer(ctx, c, input.UserID, input.ListKey)
	if errors.Is(err, cache.ErrNotFound) {
		body, err := json.Marshal(ordertypes.TransferRequest{Items: input.Items, Location: input.Location, ZipCode: input.ZipCode, ReturnURL: input.ReturnURL})
		if err != nil {
			return nil, fmt.Errorf("encode MN Food Club transfer request: %w", err)
		}
		return &ordertypes.OrderView{TransferURL: orderTransferURL, TransferLabel: "Add to " + input.Location.Chain + " cart", IdempotencyKey: input.ListKey, TransferRequestJSON: string(body)}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read MN Food Club transfer: %w", err)
	}
	if transfer.Status == ordertypes.TransferPending {
		result, err := client.status(ctx, transfer.TransferID)
		if err != nil {
			return nil, err
		}
		transfer = result.Transfer
		// TODO: Save result.UserID as the Food Club customer mapping for this Careme user.
		// TODO: Use ETags/IfMatch; concurrent status reads currently overwrite on a race.
		if err := saveOrderTransfer(ctx, c, input.UserID, input.ListKey, transfer, cache.Unconditional()); err != nil {
			return nil, err
		}
	}
	switch transfer.Status {
	case ordertypes.TransferFailed:
		return &ordertypes.OrderView{TransferFailed: true}, nil
	case ordertypes.TransferApplied, ordertypes.TransferPending:
		return &ordertypes.OrderView{ReviewCartURL: transfer.CartURL}, nil
	default:
		return nil, fmt.Errorf("unknown MN Food Club saved transfer status %q", transfer.Status)
	}
}

func (h *orderHandler) handleOrder(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Invalid MN Food Club transfer.", http.StatusBadRequest)
		return
	}
	var input ordertypes.TransferRequest
	if err := json.Unmarshal([]byte(r.PostForm.Get("transfer_request")), &input); err != nil {
		http.Error(w, "Invalid MN Food Club transfer.", http.StatusBadRequest)
		return
	}
	key := r.PostForm.Get("idempotency_key")
	if !validOrderForm(key, input) {
		http.Error(w, "Invalid MN Food Club transfer.", http.StatusBadRequest)
		return
	}
	userID, err := h.auth.GetUserIDFromRequest(r)
	if err != nil {
		http.Redirect(w, r, "/sign-in?return_to_b64="+base64.RawURLEncoding.EncodeToString([]byte(input.ReturnURL)), http.StatusSeeOther)
		return
	}
	previous, err := loadOrderTransfer(r.Context(), h.cache, userID, key)
	if err == nil {
		redirectOrder(w, r, previous, input.ReturnURL)
		return
	}
	if !errors.Is(err, cache.ErrNotFound) {
		http.Error(w, "Unable to read MN Food Club transfer.", http.StatusInternalServerError)
		return
	}
	request := orderItems(input)
	email, err := h.auth.GetUserEmail(r.Context(), userID)
	if err != nil {
		http.Error(w, "Unable to load your email for MN Food Club.", http.StatusInternalServerError)
		return
	}
	if email != "" {
		request.IdentityHints.Emails = []string{email}
	}
	transfer := ordertypes.Transfer{Status: ordertypes.TransferPending}
	if err := saveOrderTransfer(r.Context(), h.cache, userID, key, transfer, cache.IfNoneMatch()); err != nil {
		http.Error(w, "Unable to start MN Food Club transfer.", http.StatusInternalServerError)
		return
	}
	// The partner scopes keys to the integration, so scope ours to the Careme user too.
	partnerKey := fmt.Sprintf("%x", sha256.Sum256([]byte(userID+"\x00"+key)))
	result, err := h.client.create(r.Context(), partnerKey, request)
	if err != nil {
		slog.ErrorContext(r.Context(), "MN Food Club transfer failed", "error", err)
		transfer.Status = ordertypes.TransferFailed
	} else {
		transfer = result.Transfer
		// TODO: Save result.UserID as the Food Club customer mapping for this Careme user.
	}
	// TODO: Use ETags/IfMatch; result writes currently overwrite on a race.
	if err := saveOrderTransfer(r.Context(), h.cache, userID, key, transfer, cache.Unconditional()); err != nil {
		http.Error(w, "Unable to save MN Food Club transfer.", http.StatusInternalServerError)
		return
	}
	redirectOrder(w, r, transfer, input.ReturnURL)
}

func validOrderForm(key string, input ordertypes.TransferRequest) bool {
	for _, item := range input.Items {
		if strings.TrimSpace(item.ProductID) == "" {
			return false
		}
	}
	u, err := url.Parse(input.ReturnURL)
	return err == nil && !u.IsAbs() && u.Host == "" && strings.HasPrefix(input.ReturnURL, "/") && !strings.HasPrefix(u.Path, "//") && !strings.ContainsAny(u.Path, "\\\r\n") &&
		strings.TrimSpace(key) != "" && len(key) <= 255 && !strings.ContainsAny(key, "/\\") && key != "." && key != ".." && len(input.Items) > 0 && NewIdentityProvider().IsID(input.Location.ID)
}

func orderItems(input ordertypes.TransferRequest) handoffRequest {
	request := handoffRequest{Items: make([]requestedItem, len(input.Items)), IdentityHints: identityHints{ZipCode: input.ZipCode}}
	for i, item := range input.Items {
		request.Items[i] = requestedItem{ProductID: item.ProductID, VariantID: item.VariantID, Quantity: item.PackageQuantity()}
	}
	return request
}

func redirectOrder(w http.ResponseWriter, r *http.Request, transfer ordertypes.Transfer, returnURL string) {
	if transfer.Status == ordertypes.TransferPending {
		if transfer.CartURL == "" {
			http.Error(w, "MN Food Club transfer has no saved handoff yet.", http.StatusInternalServerError)
			return
		}
		http.Redirect(w, r, transfer.CartURL, http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, returnURL, http.StatusSeeOther)
}
