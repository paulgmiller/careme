package kroger

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"time"

	"careme/internal/cache"
	ordertypes "careme/internal/orders/types"
	"careme/internal/seasons"
	"careme/internal/templates"
)

// A single record owns the request, OAuth state, and cart-write result.
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

func transferKey(userID, id string) string {
	return "kroger_order_transfers/" + userID + "/" + id
}

func (a *OrderHandler) createTransfer(w http.ResponseWriter, r *http.Request, transfer cartTransfer) {
	request := transfer.Request
	id := transfer.IdempotencyKey
	key := transferKey(transfer.UserID, id)
	var previous cartTransfer
	if err := a.loadJSON(r.Context(), key, &previous); err == nil {
		if orderFingerprint(previous.Request) != orderFingerprint(request) || previous.Request.Location.Chain != request.Location.Chain || previous.Request.ReturnURL != request.ReturnURL {
			writeTransferError(w, http.StatusConflict, "This list already has a different cart transfer.")
			return
		}
		// Preparation or account connection failed before a cart mutation.
		// Updates can overwrite concurrent attempts until IfMatch is supported.
		if retryableTransfer(previous.Result.Code) {
			previous.Result = a.order(r.Context(), previous, true)
			if !a.saveTransfer(w, r, previous) {
				return
			}
		}
		a.finishTransfer(w, r, previous)
		return
	} else if !errors.Is(err, cache.ErrNotFound) {
		writeTransferError(w, http.StatusInternalServerError, "Unable to read cart transfer.")
		return
	}
	transfer.Result = transfer.result("pending", 0, 0)
	if err := a.saveJSON(r.Context(), key, transfer, cache.IfNoneMatch()); err != nil {
		if errors.Is(err, cache.ErrAlreadyExists) {
			a.createTransfer(w, r, transfer)
			return
		}
		writeTransferError(w, http.StatusInternalServerError, "Unable to reserve cart transfer.")
		return
	}
	transfer.Result = a.order(r.Context(), transfer, true)
	if !a.saveTransfer(w, r, transfer) {
		return
	}
	a.finishTransfer(w, r, transfer)
}

func (a *OrderHandler) saveTransfer(w http.ResponseWriter, r *http.Request, transfer cartTransfer) bool {
	if err := a.saveJSON(r.Context(), transferKey(transfer.UserID, transfer.IdempotencyKey), transfer, cache.Unconditional()); err != nil {
		writeTransferError(w, http.StatusInternalServerError, "Unable to save your cart result. Review your cart before trying again.")
		return false
	}
	return true
}

// TODO: Use ETags/IfMatch for state transitions. Updates currently overwrite on
// races; concurrent retries or OAuth callbacks can duplicate cart writes or lose results.
func (a *OrderHandler) finishTransfer(w http.ResponseWriter, r *http.Request, transfer cartTransfer) {
	switch transfer.Result.Code {
	case "pending_login":
		a.startAuthorization(w, r, transfer)
	case "complete":
		http.Redirect(w, r, transfer.Request.ReturnURL, http.StatusSeeOther)
	default:
		a.writeTransferFailure(w, r, transfer)
	}
}

func retryableTransfer(code string) bool {
	switch code {
	case "start_failed", "lookup_failed", "denied", "missing_code", "connect_failed", "save_failed":
		return true
	}
	return false
}

type transferFailureView struct {
	Page      templates.Page
	Message   string
	ReturnURL string
	CartURL   string
	Retry     *ordertypes.OrderView
}

func (a *OrderHandler) writeTransferFailure(w http.ResponseWriter, r *http.Request, transfer cartTransfer) {
	view := transferFailureView{
		Page:      templates.Page{Title: "Cart transfer needs attention", Style: seasons.GetCurrentStyle()},
		Message:   transfer.Result.Message,
		ReturnURL: transfer.Request.ReturnURL, CartURL: cartURL(transfer.Request.Location.Chain),
	}
	if retryableTransfer(transfer.Result.Code) {
		retry, err := OrderView(ordertypes.OrderViewInput{Location: transfer.Request.Location, Items: transfer.Request.Items, ListKey: transfer.IdempotencyKey, ReturnURL: transfer.Request.ReturnURL})
		if err != nil {
			writeTransferError(w, http.StatusInternalServerError, "Unable to render cart transfer.")
			return
		}
		retry.TransferLabel = "Try again, chef"
		view.Retry = retry
	}
	var body bytes.Buffer
	if err := templates.OrderTransfer.Execute(&body, view); err != nil {
		writeTransferError(w, http.StatusInternalServerError, "Unable to render cart transfer.")
		return
	}
	status := http.StatusBadGateway
	switch transfer.Result.Code {
	case "pending", "uncertain", "start_failed", "confirmation_failed":
		status = http.StatusConflict
	case "denied", "missing_code":
		status = http.StatusBadRequest
	case "save_failed":
		status = http.StatusInternalServerError
	case "no_matches":
		status = http.StatusUnprocessableEntity
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body.Bytes())
}

func (t cartTransfer) result(code string, added, unmatched int) ordertypes.Transfer {
	result := ordertypes.Transfer{
		TransferID: t.IdempotencyKey,
		Status:     "failed", CartURL: cartURL(t.Request.Location.Chain),
		Added: added, Unmatched: unmatched,
		Code: code, Message: krogerOrderNotice(code),
	}
	switch code {
	case "pending":
		result.Status = "pending"
	case "complete", "no_matches":
		result.Status = "applied"
	case "uncertain", "confirmation_failed", "start_failed":
		result.Status = "uncertain"
	case "pending_login", "denied", "missing_code", "connect_failed", "save_failed":
		result.Status = "pending_login"
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

func writeTransferError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Cache-Control", "no-store")
	http.Error(w, message, status)
}
