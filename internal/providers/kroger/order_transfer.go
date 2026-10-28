package kroger

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"careme/internal/cache"
	ordertypes "careme/internal/orders/types"
	"careme/internal/seasons"
	"careme/internal/templates"
)

// A transfer is created after authentication and owns the request and cart result.
type cartTransfer struct {
	UserID         string                     `json:"user_id"`
	IdempotencyKey string                     `json:"idempotency_key"`
	Request        ordertypes.TransferRequest `json:"request"`
	Result         ordertypes.Transfer        `json:"result"`
}

func transferKey(userID, id string) string {
	return "kroger_order_transfers/" + userID + "/" + id
}

func (a *OrderHandler) createTransfer(w http.ResponseWriter, r *http.Request, transfer cartTransfer) {
	key := transferKey(transfer.UserID, transfer.IdempotencyKey)
	var previous cartTransfer
	exists := false
	if err := a.loadJSON(r.Context(), key, &previous); err == nil {
		// A finalized list key identifies the payload; always reuse the saved request.
		if previous.Result.Status != "failed" {
			a.finishTransfer(w, r, previous)
			return
		}
		transfer = previous
		exists = true
	} else if !errors.Is(err, cache.ErrNotFound) {
		writeTransferError(w, http.StatusInternalServerError, "Unable to read cart transfer.")
		return
	}
	token, err := a.shopperToken(r.Context(), transfer.UserID)
	if errors.Is(err, errConnectionRequired) {
		a.startAuthorization(w, r, transfer)
		return
	}
	if err != nil {
		slog.ErrorContext(r.Context(), "Kroger authentication failed", "error", err)
		writeTransferError(w, http.StatusInternalServerError, "Unable to load Kroger connection.")
		return
	}
	// Authentication has succeeded. Only now create the transfer.
	transfer.Result = transfer.result("pending", 0, 0)
	options := cache.IfNoneMatch()
	if exists {
		options = cache.Unconditional()
	}
	// TODO: Use ETags/IfMatch for updates; concurrent retries may overwrite or submit twice.
	if err := a.saveJSON(r.Context(), key, transfer, options); err != nil {
		if errors.Is(err, cache.ErrAlreadyExists) {
			a.createTransfer(w, r, transfer)
			return
		}
		writeTransferError(w, http.StatusInternalServerError, "Unable to start cart transfer.")
		return
	}
	transfer.Result, err = a.order(r.Context(), transfer, token)
	if err != nil {
		slog.ErrorContext(r.Context(), "Kroger cart transfer failed", "error", err)
	}
	if !a.saveTransfer(r.Context(), w, transfer) {
		return
	}
	a.finishTransfer(w, r, transfer)
}

func (a *OrderHandler) saveTransfer(ctx context.Context, w http.ResponseWriter, transfer cartTransfer) bool {
	if err := a.saveJSON(ctx, transferKey(transfer.UserID, transfer.IdempotencyKey), transfer, cache.Unconditional()); err != nil {
		writeTransferError(w, http.StatusInternalServerError, "Unable to save your cart result. Review your cart before trying again.")
		return false
	}
	return true
}

// TODO: Use ETags/IfMatch for state transitions. Updates currently overwrite on
// races; concurrent retries or OAuth callbacks can duplicate cart writes or lose results.
func (a *OrderHandler) finishTransfer(w http.ResponseWriter, r *http.Request, transfer cartTransfer) {
	switch transfer.Result.Code {
	case "complete":
		http.Redirect(w, r, transfer.Request.ReturnURL, http.StatusSeeOther)
	default:
		a.writeTransferFailure(w, transfer)
	}
}

type transferFailureView struct {
	Page      templates.Page
	Message   string
	ReturnURL string
	CartURL   string
	Retry     *ordertypes.OrderView
}

func (a *OrderHandler) writeTransferFailure(w http.ResponseWriter, transfer cartTransfer) {
	view := transferFailureView{
		Page:      templates.Page{Title: "Cart transfer needs attention", Style: seasons.GetCurrentStyle()},
		Message:   transfer.Result.Message,
		ReturnURL: transfer.Request.ReturnURL,
	}
	if transfer.Result.Status == "failed" {
		retry, err := OrderView(ordertypes.OrderViewInput{Location: transfer.Request.Location, Items: transfer.Request.Items, ListKey: transfer.IdempotencyKey, ReturnURL: transfer.Request.ReturnURL})
		if err != nil {
			writeTransferError(w, http.StatusInternalServerError, "Unable to render cart transfer.")
			return
		}
		retry.TransferLabel = "Try again, chef"
		view.Retry = retry
	}
	if transfer.Result.Status != "failed" {
		view.CartURL = transfer.Result.CartURL
	}
	var body bytes.Buffer
	if err := templates.OrderTransfer.Execute(&body, view); err != nil {
		writeTransferError(w, http.StatusInternalServerError, "Unable to render cart transfer.")
		return
	}
	status := http.StatusInternalServerError
	if transfer.Result.Status != "failed" {
		status = http.StatusConflict
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
	case "complete":
		result.Status = "applied"
	case "uncertain":
		result.Status = "uncertain"
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
