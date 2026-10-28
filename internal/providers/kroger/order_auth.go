package kroger

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"careme/internal/auth"
	"careme/internal/cache"
	ordertypes "careme/internal/orders/types"
	"careme/internal/routing"
)

const orderingTokenPrefix = "kroger_cart_tokens/"

type OrderOptions struct {
	Cache    cache.Cache
	Client   *CartClient
	TokenKey []byte
	Origin   string
	Auth     auth.AuthClient
}

type OrderHandler struct {
	client   *CartClient
	cache    cache.Cache
	tokenKey []byte
	origin   string
	auth     auth.AuthClient
}

func NewOrderHandler(options OrderOptions) (*OrderHandler, error) {
	if len(options.TokenKey) != 32 {
		return nil, fmt.Errorf("kroger token encryption key must contain 32 bytes")
	}
	if options.Cache == nil || options.Client == nil || options.Auth == nil {
		return nil, fmt.Errorf("kroger ordering requires cache, client, authentication")
	}
	return &OrderHandler{client: options.Client, cache: options.Cache, tokenKey: append([]byte(nil), options.TokenKey...), origin: options.Origin, auth: options.Auth}, nil
}

func (a *OrderHandler) saveToken(ctx context.Context, userID string, token CartToken) error {
	block, err := aes.NewCipher(a.tokenKey)
	if err != nil {
		return err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	plaintext, err := json.Marshal(token)
	if err != nil {
		return err
	}
	ciphertext := aead.Seal(nonce, nonce, plaintext, []byte(userID))
	return a.cache.Put(ctx, orderingTokenPrefix+userID, base64.StdEncoding.EncodeToString(ciphertext), cache.Unconditional())
}

func (a *OrderHandler) loadToken(ctx context.Context, userID string) (CartToken, error) {
	reader, err := a.cache.Get(ctx, orderingTokenPrefix+userID)
	if err != nil {
		return CartToken{}, err
	}
	defer func() { _ = reader.Close() }()
	encoded, err := io.ReadAll(io.LimitReader(reader, 8192))
	if err != nil {
		return CartToken{}, err
	}
	ciphertext, err := base64.StdEncoding.DecodeString(string(encoded))
	if err != nil {
		return CartToken{}, err
	}
	block, err := aes.NewCipher(a.tokenKey)
	if err != nil {
		return CartToken{}, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return CartToken{}, err
	}
	if len(ciphertext) < aead.NonceSize() {
		return CartToken{}, fmt.Errorf("short Kroger token record")
	}
	plaintext, err := aead.Open(nil, ciphertext[:aead.NonceSize()], ciphertext[aead.NonceSize():], []byte(userID))
	if err != nil {
		return CartToken{}, err
	}
	var token CartToken
	if err := json.Unmarshal(plaintext, &token); err != nil {
		return CartToken{}, err
	}
	return token, nil
}

type oauthContinuation struct {
	UserID         string
	IdempotencyKey string
	Request        ordertypes.TransferRequest
	IssuedAt       time.Time
}

func oauthContinuationKey(userID, state string) string {
	return "kroger_oauth_continuations/" + userID + "/" + state
}

var errConnectionRequired = errors.New("account connection required")

func (a *OrderHandler) shopperToken(ctx context.Context, userID string) (CartToken, error) {
	token, err := a.loadToken(ctx, userID)
	if errors.Is(err, cache.ErrNotFound) {
		return CartToken{}, errConnectionRequired
	}
	if err != nil {
		return CartToken{}, fmt.Errorf("load Kroger shopper token: %w", err)
	}
	if token.AccessToken == "" {
		return CartToken{}, errConnectionRequired
	}
	if time.Now().After(token.ExpiresAt.Add(-time.Minute)) {
		token, err = a.client.Refresh(ctx, token.RefreshToken)
		if err != nil {
			return CartToken{}, fmt.Errorf("refresh Kroger shopper token: %w", err)
		}
		if err = a.saveToken(ctx, userID, token); err != nil {
			return CartToken{}, fmt.Errorf("save Kroger shopper token: %w", err)
		}
	}
	return token, nil
}

func (a *OrderHandler) startAuthorization(w http.ResponseWriter, r *http.Request, transfer cartTransfer) {
	banner := authorizationBanner(transfer.Request.Location.Chain)
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		http.Error(w, "Unable to connect to Kroger", http.StatusInternalServerError)
		return
	}
	state := base64.RawURLEncoding.EncodeToString(nonce)
	continuation := oauthContinuation{UserID: transfer.UserID, IdempotencyKey: transfer.IdempotencyKey, Request: transfer.Request, IssuedAt: time.Now()}
	if err := a.saveJSON(r.Context(), oauthContinuationKey(transfer.UserID, state), continuation, cache.IfNoneMatch()); err != nil {
		writeTransferError(w, http.StatusInternalServerError, "Unable to start Kroger connection.")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "careme_kroger_state", Value: state, Path: "/kroger/callback", MaxAge: 600, HttpOnly: true, Secure: strings.HasPrefix(a.origin, "https://"), SameSite: http.SameSiteLaxMode})
	destination, err := a.client.AuthorizationURL(state, banner)
	if err != nil {
		http.Error(w, "Unable to start Kroger connection", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, destination, http.StatusSeeOther)
}

func (a *OrderHandler) handleCallback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	userID, ok := a.cartUser(w, r, r.URL.RequestURI())
	if !ok {
		return
	}
	cookie, err := r.Cookie("careme_kroger_state")
	if err != nil {
		http.Error(w, "Kroger connection expired", http.StatusBadRequest)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "careme_kroger_state", Path: "/kroger/callback", MaxAge: -1, HttpOnly: true})
	state := cookie.Value
	if len(state) != 43 || strings.ContainsAny(state, "/\\") || subtle.ConstantTimeCompare([]byte(state), []byte(r.URL.Query().Get("state"))) != 1 {
		writeTransferError(w, http.StatusBadRequest, "Invalid Kroger connection.")
		return
	}
	var continuation oauthContinuation
	if err := a.loadJSON(r.Context(), oauthContinuationKey(userID, state), &continuation); err != nil || continuation.UserID != userID || continuation.IssuedAt.IsZero() || time.Since(continuation.IssuedAt) > 10*time.Minute || time.Until(continuation.IssuedAt) > time.Minute {
		writeTransferError(w, http.StatusBadRequest, "Kroger connection expired or unavailable.")
		return
	}
	var existing cartTransfer
	if err := a.loadJSON(r.Context(), transferKey(userID, continuation.IdempotencyKey), &existing); err == nil && existing.Result.Status != "failed" {
		a.finishTransfer(w, r, existing)
		return
	} else if err != nil && !errors.Is(err, cache.ErrNotFound) {
		writeTransferError(w, http.StatusInternalServerError, "Unable to read cart transfer.")
		return
	}
	if r.URL.Query().Get("error") != "" {
		writeTransferError(w, http.StatusBadRequest, "Kroger connection was not approved.")
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		writeTransferError(w, http.StatusBadRequest, "Kroger did not finish connecting your account.")
		return
	}
	token, err := a.client.Exchange(r.Context(), code)
	if err != nil {
		slog.ErrorContext(r.Context(), "Kroger token exchange failed", "error", err)
		writeTransferError(w, http.StatusInternalServerError, "Unable to connect to Kroger.")
		return
	}
	if err := a.saveToken(r.Context(), userID, token); err != nil {
		writeTransferError(w, http.StatusInternalServerError, "Unable to save Kroger connection.")
		return
	}
	a.createTransfer(w, r, cartTransfer{UserID: userID, IdempotencyKey: continuation.IdempotencyKey, Request: continuation.Request})
}

func (a *OrderHandler) cartUser(w http.ResponseWriter, r *http.Request, returnURL string) (string, bool) {
	userID, err := a.auth.GetUserIDFromRequest(r)
	if err == nil {
		return userID, true
	}
	// will this ever actually happen?
	if errors.Is(err, auth.ErrNoSession) {
		http.Redirect(w, r, "/sign-in?return_to_b64="+url.QueryEscape(base64.RawURLEncoding.EncodeToString([]byte(returnURL))), http.StatusSeeOther)
	} else {
		http.Error(w, "Unable to load account", http.StatusInternalServerError)
	}
	return "", false
}

func (a *OrderHandler) Register(routes routing.Registrar) {
	routes.Handle("POST /kroger/order-transfers", http.NewCrossOriginProtection().Handler(http.HandlerFunc(a.handleOrder)))
	routes.HandleFunc("GET /kroger/callback", a.handleCallback)
}
