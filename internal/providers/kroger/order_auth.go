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
	"careme/internal/httpx"
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

type orderContinuation struct {
	Request  orderRequest
	IssuedAt time.Time
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

func (a *OrderHandler) startAuthorization(w http.ResponseWriter, r *http.Request, request orderRequest) {
	banner := strings.ToLower(request.Chain)
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		http.Error(w, "Unable to connect to Kroger", http.StatusInternalServerError)
		return
	}
	state := base64.RawURLEncoding.EncodeToString(nonce)
	if err := a.saveJSON(r.Context(), "kroger_order_requests/"+state, orderContinuation{Request: request, IssuedAt: time.Now()}, cache.IfNoneMatch()); err != nil {
		http.Error(w, "Unable to save Kroger order", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "careme_kroger_state", Value: state, Path: "/kroger/callback", MaxAge: 600, HttpOnly: true, Secure: strings.HasPrefix(a.origin, "https://"), SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, a.client.AuthorizationURL(state, banner), http.StatusSeeOther)
}

func (a *OrderHandler) handleCallback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	userID, ok := a.cartUser(w, r)
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
		http.Error(w, "Invalid Kroger connection", http.StatusBadRequest)
		return
	}
	var continuation orderContinuation
	if err := a.loadJSON(r.Context(), "kroger_order_requests/"+state, &continuation); err != nil || continuation.Request.UserID != userID || continuation.IssuedAt.IsZero() || time.Since(continuation.IssuedAt) > 10*time.Minute || time.Until(continuation.IssuedAt) > time.Minute {
		http.Error(w, "Kroger connection expired or unavailable", http.StatusBadRequest)
		return
	}
	request := continuation.Request
	if r.URL.Query().Get("error") != "" {
		a.returnToList(w, r, request, orderResult{}, "denied")
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		a.returnToList(w, r, request, orderResult{}, "missing_code")
		return
	}
	token, err := a.client.Exchange(r.Context(), code)
	if err != nil {
		slog.ErrorContext(r.Context(), "Kroger token exchange failed", "error", err)
		a.returnToList(w, r, request, orderResult{}, "connect_failed")
		return
	}
	if err := a.saveToken(r.Context(), userID, token); err != nil {
		a.returnToList(w, r, request, orderResult{}, "save_failed")
		return
	}
	a.order(w, r, request, false)
}

func (a *OrderHandler) cartUser(w http.ResponseWriter, r *http.Request) (string, bool) {
	userID, err := a.auth.GetUserIDFromRequest(r)
	if err == nil {
		return userID, true
	}
	if errors.Is(err, auth.ErrNoSession) {
		redirectToOrderingSignIn(w, r)
	} else {
		http.Error(w, "Unable to load account", http.StatusInternalServerError)
	}
	return "", false
}

func redirectToOrderingSignIn(w http.ResponseWriter, r *http.Request) {
	if httpx.IsHTMX(r) {
		w.Header().Set("HX-Redirect", "/sign-in?return_to_b64="+url.QueryEscape(base64.RawURLEncoding.EncodeToString([]byte(httpx.RequestPath(r)))))
	}
	http.Error(w, "must be logged in", http.StatusUnauthorized)
}

func (a *OrderHandler) Register(routes routing.Registrar) {
	routes.Handle("POST /kroger/order", http.NewCrossOriginProtection().Handler(http.HandlerFunc(a.handleOrder)))
	routes.HandleFunc("GET /kroger/callback", a.handleCallback)
}
