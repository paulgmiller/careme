package kroger

import (
	"errors"
	"log/slog"
	"net/http"

	"careme/internal/cache"
	ordertypes "careme/internal/orders/types"
)

func transferKey(userID, id string) string {
	return "kroger_order_transfers/" + userID + "/" + id
}

func (a *OrderHandler) createTransfer(w http.ResponseWriter, r *http.Request, userID, idempotencyKey string, input ordertypes.TransferRequest) {
	ctx := r.Context()
	key := transferKey(userID, idempotencyKey)
	var previous ordertypes.Transfer
	if err := a.loadJSON(ctx, key, &previous); err == nil {
		// A finalized list key identifies the payload; always reuse the saved result.
		http.Redirect(w, r, input.ReturnURL, http.StatusSeeOther)
		return
	} else if !errors.Is(err, cache.ErrNotFound) {
		http.Error(w, "Unable to read cart transfer.", http.StatusInternalServerError)
		return
	}
	token, err := a.shopperToken(ctx, userID)
	if errors.Is(err, errConnectionRequired) {
		a.startAuthorization(w, r, userID, idempotencyKey, input)
		return
	}
	if err != nil {
		slog.ErrorContext(ctx, "Kroger authentication failed", "error", err)
		http.Error(w, "Unable to load Kroger connection.", http.StatusInternalServerError)
		return
	}
	// Authentication has succeeded. Only now create the transfer.
	transfer := ordertypes.Transfer{TransferID: idempotencyKey, Status: ordertypes.TransferPending, CartURL: cartURL(input.Location.Chain)}
	options := cache.IfNoneMatch()
	// TODO: Use ETags/IfMatch for updates; concurrent submissions may overwrite or submit twice.
	if err := a.saveJSON(r.Context(), key, transfer, options); err != nil {
		// how did one come in since oauth flow? just error rather than try and fix it.
		http.Error(w, "Unable to start cart transfer.", http.StatusConflict)
		return
	}
	transfer, err = a.order(ctx, idempotencyKey, input, token)
	if err != nil {
		slog.ErrorContext(ctx, "Kroger cart transfer failed", "error", err)
	}
	if err := a.saveJSON(ctx, transferKey(userID, idempotencyKey), transfer, cache.Unconditional()); err != nil {
		http.Error(w, "Unable to save your cart result. Review your Kroger cart.", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, input.ReturnURL, http.StatusSeeOther)
}

