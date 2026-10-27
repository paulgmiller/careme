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
	ordertypes "careme/internal/orders/types"
	"careme/internal/templates"
)

// The lifecycle record owns the request, OAuth state, and preparation result.
// Its submission subrecord reserves and records the only cart write.
type cartTransfer struct {
	UserID         string                     `json:"user_id"`
	IdempotencyKey string                     `json:"idempotency_key"`
	Request        ordertypes.TransferRequest `json:"request"`
	Result         ordertypes.Transfer        `json:"result"`
	Authorization  *transferAuthorization     `json:"authorization,omitempty"`
}

type transferAuthorization struct {
	State    string    `json:"state"`
	IssuedAt time.Time `json:"issued_at"`
}

func submissionKey(transfer cartTransfer) string {
	return transferKey(transfer.UserID, ordertypes.TransferID(transfer.IdempotencyKey)) + "/submission"
}

func transferKey(userID, id string) string {
	return "kroger_order_transfers/" + userID + "/" + id
}

func (a *OrderHandler) createTransfer(w http.ResponseWriter, r *http.Request, transfer cartTransfer) {
	request := transfer.Request
	id := ordertypes.TransferID(transfer.IdempotencyKey)
	key := transferKey(transfer.UserID, id) + "/state"
	var previous cartTransfer
	if err := a.loadJSON(r.Context(), key, &previous); err == nil {
		if orderFingerprint(previous.Request) != orderFingerprint(request) || previous.Request.Location.Chain != request.Location.Chain || previous.Request.ReturnURL != request.ReturnURL {
			writeTransferError(w, http.StatusConflict, "idempotency_conflict", "This list already has a different cart transfer.")
			return
		}
		// Product lookup failed before any cart mutation. Retrying the same
		// transfer is safe; order still conditionally reserves the cart write.
		if previous.Result.Code == "lookup_failed" {
			previous.Result = a.order(r.Context(), previous, true)
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
	transfer.Result = transfer.result("pending", 0, 0)
	if err := a.saveJSON(r.Context(), key, transfer, cache.IfNoneMatch()); err != nil {
		if errors.Is(err, cache.ErrAlreadyExists) {
			a.createTransfer(w, r, transfer)
			return
		}
		writeTransferError(w, http.StatusInternalServerError, "transfer_unavailable", "Unable to reserve cart transfer.")
		return
	}
	transfer.Result = a.order(r.Context(), transfer, true)
	if !a.saveTransfer(w, r, transfer) {
		return
	}
	a.writeCurrentTransfer(w, r, transfer)
}

func (a *OrderHandler) saveTransfer(w http.ResponseWriter, r *http.Request, transfer cartTransfer) bool {
	if err := a.saveJSON(r.Context(), transferKey(transfer.UserID, ordertypes.TransferID(transfer.IdempotencyKey))+"/state", transfer, cache.Unconditional()); err != nil {
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
	var result ordertypes.Transfer
	err := a.loadJSON(r.Context(), submissionKey(transfer), &result)
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
	writeTransferJSON(w, http.StatusOK, transfer.Result)
}

// The API still returns JSON to ordinary clients. HTMX requests use the same
// saved result but receive the shared shopping-list fragment instead.
func (a *OrderHandler) writeTransferView(w http.ResponseWriter, r *http.Request, transfer cartTransfer) {
	result := transfer.Result
	w.Header().Set("Vary", "HX-Request")
	if r.Method == http.MethodPost && result.Status == "pending_login" {
		w.Header().Set("HX-Redirect", result.CartURL)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	request := transfer.Request
	view, err := OrderView(ordertypes.OrderViewInput{
		Location: request.Location,
		Items:    request.Items, ListKey: transfer.IdempotencyKey, ReturnURL: request.ReturnURL,
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
	if transfer.Result.Status != "pending_login" {
		http.Redirect(w, r, transfer.Request.ReturnURL, http.StatusSeeOther)
		return
	}
	a.startAuthorization(w, r, transfer)
}

func (t cartTransfer) result(code string, added, unmatched int) ordertypes.Transfer {
	result := ordertypes.Transfer{
		TransferID: ordertypes.TransferID(t.IdempotencyKey),
		Status:     "failed", CartURL: cartURL(t.Request.Location.Chain),
		Added: added, Unmatched: unmatched,
		Code: code, Message: krogerOrderNotice(code),
	}
	switch code {
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
	if code == "pending_login" {
		result.Message = "Connect your Kroger account to send this shopping list."
	}
	if code == "complete" {
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
