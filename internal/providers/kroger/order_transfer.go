package kroger

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"careme/internal/cache"
	"careme/internal/httpx"
	locationtypes "careme/internal/locations/types"
	ordertypes "careme/internal/orders/types"
	"careme/internal/templates"
)

// The lifecycle record owns the request, OAuth state, and preparation result.
// Its submission subrecord reserves and records the only cart write.
type cartTransfer struct {
	Request       orderRequest           `json:"request"`
	Result        orderResult            `json:"result"`
	Authorization *transferAuthorization `json:"authorization,omitempty"`
}

type transferAuthorization struct {
	State    string    `json:"state"`
	IssuedAt time.Time `json:"issued_at"`
}

func submissionKey(request orderRequest) string {
	return transferKey(request.UserID, ordertypes.TransferID(request.IdempotencyKey)) + "/submission"
}

func transferKey(userID, id string) string {
	return "kroger_order_transfers/" + userID + "/" + id
}

func (a *OrderHandler) createTransfer(w http.ResponseWriter, r *http.Request, request orderRequest) {
	id := ordertypes.TransferID(request.IdempotencyKey)
	key := transferKey(request.UserID, id) + "/state"
	var previous cartTransfer
	if err := a.loadJSON(r.Context(), key, &previous); err == nil {
		if orderFingerprint(previous.Request) != orderFingerprint(request) || previous.Request.Chain != request.Chain || previous.Request.ReturnURL != request.ReturnURL {
			writeTransferError(w, http.StatusConflict, "idempotency_conflict", "This list already has a different cart transfer.")
			return
		}
		// Product lookup failed before any cart mutation. Retrying the same
		// transfer is safe; order still conditionally reserves the cart write.
		if previous.Result.Status == "lookup_failed" {
			previous.Result = a.order(r.Context(), previous.Request, true)
			if !a.saveTransfer(w, r, previous) {
				return
			}
		}
		a.writeCurrentTransfer(w, r, previous)
		return
	} else if !errors.Is(err, cache.ErrNotFound) {
		writeTransferError(w, http.StatusInternalServerError, "transfer_unavailable", "Unable to read cart transfer.")
		return
	}
	transfer := cartTransfer{Request: request, Result: orderResult{Status: "pending"}}
	if err := a.saveJSON(r.Context(), key, transfer, cache.IfNoneMatch()); err != nil {
		if errors.Is(err, cache.ErrAlreadyExists) {
			a.createTransfer(w, r, request)
			return
		}
		writeTransferError(w, http.StatusInternalServerError, "transfer_unavailable", "Unable to reserve cart transfer.")
		return
	}
	transfer.Result = a.order(r.Context(), request, true)
	if !a.saveTransfer(w, r, transfer) {
		return
	}
	a.writeCurrentTransfer(w, r, transfer)
}

func (a *OrderHandler) saveTransfer(w http.ResponseWriter, r *http.Request, transfer cartTransfer) bool {
	if err := a.saveJSON(r.Context(), transferKey(transfer.Request.UserID, ordertypes.TransferID(transfer.Request.IdempotencyKey))+"/state", transfer, cache.Unconditional()); err != nil {
		writeTransferError(w, http.StatusInternalServerError, "transfer_unavailable", "Unable to save cart transfer. Check its status before trying again.")
		return false
	}
	return true
}

func (a *OrderHandler) requestedTransfer(w http.ResponseWriter, r *http.Request) (cartTransfer, bool) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Vary", "HX-Request")
	userID, ok := a.cartUser(w, r)
	if !ok {
		return cartTransfer{}, false
	}
	id := r.PathValue("transfer_id")
	decoded, err := hex.DecodeString(id)
	if err != nil || len(decoded) != 32 || hex.EncodeToString(decoded) != id {
		writeTransferError(w, http.StatusNotFound, "transfer_not_found", "Transfer not found.")
		return cartTransfer{}, false
	}
	var transfer cartTransfer
	if err := a.loadJSON(r.Context(), transferKey(userID, id)+"/state", &transfer); err != nil {
		if errors.Is(err, cache.ErrNotFound) && httpx.IsHTMX(r) {
			// The initial status check leaves the existing form in place.
			w.WriteHeader(http.StatusNoContent)
		} else if errors.Is(err, cache.ErrNotFound) {
			writeTransferError(w, http.StatusNotFound, "transfer_not_found", "Transfer not found.")
		} else {
			writeTransferError(w, http.StatusInternalServerError, "transfer_unavailable", "Unable to read cart transfer.")
		}
		return cartTransfer{}, false
	}
	return transfer, true
}

func (a *OrderHandler) handleTransfer(w http.ResponseWriter, r *http.Request) {
	transfer, ok := a.requestedTransfer(w, r)
	if ok {
		a.writeCurrentTransfer(w, r, transfer)
	}
}

func (a *OrderHandler) writeCurrentTransfer(w http.ResponseWriter, r *http.Request, transfer cartTransfer) {
	w.Header().Set("Vary", "HX-Request")
	// The write reservation remains authoritative if the lifecycle save fails
	// or an OAuth callback arrives after the cart was already submitted.
	var result orderResult
	err := a.loadJSON(r.Context(), submissionKey(transfer.Request), &result)
	if err == nil {
		transfer.Result = result
	} else if !errors.Is(err, cache.ErrNotFound) {
		writeTransferError(w, http.StatusInternalServerError, "transfer_unavailable", "Unable to read cart result.")
		return
	}
	if httpx.IsHTMX(r) {
		a.writeTransferView(w, r, transfer)
		return
	}
	writeTransferJSON(w, http.StatusOK, transfer.response())
}

// The API still returns JSON to ordinary clients. HTMX requests use the same
// saved result but receive the shared shopping-list fragment instead.
func (a *OrderHandler) writeTransferView(w http.ResponseWriter, r *http.Request, transfer cartTransfer) {
	result := transfer.response()
	w.Header().Set("Vary", "HX-Request")
	if r.Method == http.MethodPost && result.Status == "pending_login" {
		w.Header().Set("HX-Redirect", result.CartURL)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	request := transfer.Request
	view, err := OrderView(ordertypes.OrderViewInput{
		Location: locationtypes.Location{ID: request.StoreID, Chain: request.Chain},
		Items:    request.Items, ListKey: request.IdempotencyKey, ReturnURL: request.ReturnURL,
	})
	if err != nil {
		http.Error(w, "Unable to render cart transfer", http.StatusInternalServerError)
		return
	}
	view.Transfer = &result
	var body bytes.Buffer
	if err := templates.ShoppingList.ExecuteTemplate(&body, "shopping_order_form", view); err != nil {
		http.Error(w, "Unable to render cart transfer", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(body.Bytes())
}

func (a *OrderHandler) handleConnect(w http.ResponseWriter, r *http.Request) {
	transfer, ok := a.requestedTransfer(w, r)
	if !ok {
		return
	}
	if transfer.response().Status != "pending_login" {
		http.Redirect(w, r, transfer.Request.ReturnURL, http.StatusSeeOther)
		return
	}
	a.startAuthorization(w, r, transfer)
}

func (t cartTransfer) response() ordertypes.Transfer {
	result := ordertypes.Transfer{
		TransferID: ordertypes.TransferID(t.Request.IdempotencyKey),
		Status:     "failed", CartURL: cartURL(t.Request.Chain),
		Added: t.Result.Added, Unmatched: len(t.Result.Gaps),
		Code: t.Result.Status, Message: krogerOrderNotice(t.Result.Status),
	}
	switch t.Result.Status {
	case "complete", "no_matches":
		result.Status = "applied"
	case "pending":
		result.Status = "pending"
	case "uncertain", "confirmation_failed", "start_failed":
		result.Status = "uncertain"
	case "pending_login", "denied", "missing_code", "connect_failed", "save_failed":
		result.Status = "pending_login"
		result.CartURL = "/kroger/cart-transfers/" + result.TransferID + "/connect"
	}
	if t.Result.Status == "pending_login" {
		result.Message = "Connect your Kroger account to send this shopping list."
	}
	if t.Result.Status == "complete" {
		result.Message = fmt.Sprintf("Sent %d products to your cart. Review amounts before checkout.", result.Added)
		if result.Unmatched == 1 {
			result.Message += " 1 item could not be matched and was not sent."
		} else if result.Unmatched > 1 {
			result.Message += fmt.Sprintf(" %d items could not be matched and were not sent.", result.Unmatched)
		}
	}
	return result
}

func writeTransferJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Vary", "HX-Request")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeTransferError(w http.ResponseWriter, status int, code, message string) {
	writeTransferJSON(w, status, struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}{code, message})
}
