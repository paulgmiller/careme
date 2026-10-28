package kroger

import (
	"errors"
	"log/slog"
	"net/http"

	"careme/internal/cache"
	ordertypes "careme/internal/orders/types"
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
	ctx := r.Context()
	key := transferKey(transfer.UserID, transfer.IdempotencyKey)
	var previous cartTransfer
	if err := a.loadJSON(ctx, key, &previous); err == nil {
		// A finalized list key identifies the payload; always reuse the saved result.
		a.finishTransfer(w, r, previous)
		return
	} else if !errors.Is(err, cache.ErrNotFound) {
		writeTransferError(w, http.StatusInternalServerError, "Unable to read cart transfer.")
		return
	}
	token, err := a.shopperToken(ctx, transfer.UserID)
	if errors.Is(err, errConnectionRequired) {
		a.startAuthorization(w, r, transfer)
		return
	}
	if err != nil {
		slog.ErrorContext(ctx, "Kroger authentication failed", "error", err)
		writeTransferError(w, http.StatusInternalServerError, "Unable to load Kroger connection.")
		return
	}
	// Authentication has succeeded. Only now create the transfer.
	transfer.Result = transfer.result(ordertypes.TransferPending, 0, 0)
	options := cache.IfNoneMatch()
	// TODO: Use ETags/IfMatch for updates; concurrent submissions may overwrite or submit twice.
	if err := a.saveJSON(r.Context(), key, transfer, options); err != nil {
		// how did one come in since oauth flow? just error rather than try and fix it.
		writeTransferError(w, http.StatusConflict, "Unable to start cart transfer.")
		return
	}
	transfer.Result, err = a.order(ctx, transfer, token)
	if err != nil {
		slog.ErrorContext(ctx, "Kroger cart transfer failed", "error", err)
	}
	if err := a.saveJSON(ctx, transferKey(transfer.UserID, transfer.IdempotencyKey), transfer, cache.Unconditional()); err != nil {
		writeTransferError(w, http.StatusInternalServerError, "Unable to save your cart result. Review your Kroger cart.")
		return
	}
	a.finishTransfer(w, r, transfer)
}

func (a *OrderHandler) finishTransfer(w http.ResponseWriter, r *http.Request, transfer cartTransfer) {
	http.Redirect(w, r, transfer.Request.ReturnURL, http.StatusSeeOther)
}

func (t cartTransfer) result(status ordertypes.TransferStatus, added, unmatched int) ordertypes.Transfer {
	return ordertypes.Transfer{
		TransferID: t.IdempotencyKey,
		Status:     status, CartURL: cartURL(t.Request.Location.Chain),
		Added: added, Unmatched: unmatched,
	}
}

func writeTransferError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Cache-Control", "no-store")
	http.Error(w, message, status)
}
