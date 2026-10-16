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
	"careme/internal/ordering"
)

const orderingTokenPrefix = "kroger_cart_tokens/"

type OrderingOptions struct {
	Cache    cache.Cache
	Client   *CartClient
	TokenKey []byte
	Origin   string
	Auth     auth.AuthClient
	Resume   func(http.ResponseWriter, *http.Request, string, string)
}

type OrderingAdapter struct {
	Client   *CartClient
	cache    cache.Cache
	tokenKey []byte
	origin   string
	auth     auth.AuthClient
	resume   func(http.ResponseWriter, *http.Request, string, string)
}

func NewOrderingAdapter(options OrderingOptions) (*OrderingAdapter, error) {
	if len(options.TokenKey) != 32 {
		return nil, fmt.Errorf("kroger token encryption key must contain 32 bytes")
	}
	if options.Cache == nil || options.Client == nil || options.Auth == nil || options.Resume == nil {
		return nil, fmt.Errorf("kroger ordering requires cache, client, authentication, and resume handler")
	}
	return &OrderingAdapter{Client: options.Client, cache: options.Cache, tokenKey: append([]byte(nil), options.TokenKey...), origin: options.Origin, auth: options.Auth, resume: options.Resume}, nil
}

type krogerAuthState struct {
	Nonce    string    `json:"nonce"`
	Hash     string    `json:"hash"`
	UserID   string    `json:"user_id"`
	IssuedAt time.Time `json:"issued_at"`
}

func (a *OrderingAdapter) SaveToken(ctx context.Context, userID string, token CartToken) error {
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

func (a *OrderingAdapter) LoadToken(ctx context.Context, userID string) (CartToken, error) {
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

func (a *OrderingAdapter) StartAuthorization(w http.ResponseWriter, r *http.Request, request ordering.Request) {
	userID, hash, banner := request.UserID, request.Hash, strings.ToLower(request.Source.Chain)
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		http.Error(w, "Unable to connect to Kroger", http.StatusInternalServerError)
		return
	}
	state := krogerAuthState{Nonce: base64.RawURLEncoding.EncodeToString(nonce), Hash: hash, UserID: userID, IssuedAt: time.Now()}
	body, _ := json.Marshal(state)
	http.SetCookie(w, &http.Cookie{Name: "careme_kroger_state", Value: base64.RawURLEncoding.EncodeToString(body), Path: "/kroger/callback", MaxAge: 600, HttpOnly: true, Secure: strings.HasPrefix(a.origin, "https://"), SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, a.Client.AuthorizationURL(state.Nonce, banner), http.StatusSeeOther)
}

func (a *OrderingAdapter) Callback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if a.Client == nil {
		http.NotFound(w, r)
		return
	}
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
	body, err := base64.RawURLEncoding.DecodeString(cookie.Value)
	if err != nil {
		http.Error(w, "Invalid Kroger connection", http.StatusBadRequest)
		return
	}
	var state krogerAuthState
	if json.Unmarshal(body, &state) != nil || state.UserID != userID || subtle.ConstantTimeCompare([]byte(state.Nonce), []byte(r.URL.Query().Get("state"))) != 1 || state.Hash == "" || state.IssuedAt.IsZero() || time.Since(state.IssuedAt) > 10*time.Minute || time.Until(state.IssuedAt) > time.Minute {
		http.Error(w, "Invalid Kroger connection", http.StatusBadRequest)
		return
	}
	if r.URL.Query().Get("error") != "" {
		redirectKrogerShoppingList(w, r, state.Hash, "denied")
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		redirectKrogerShoppingList(w, r, state.Hash, "missing_code")
		return
	}
	token, err := a.Client.Exchange(r.Context(), code)
	if err != nil {
		slog.ErrorContext(r.Context(), "Kroger token exchange failed", "error", err)
		redirectKrogerShoppingList(w, r, state.Hash, "connect_failed")
		return
	}
	if err := a.SaveToken(r.Context(), userID, token); err != nil {
		redirectKrogerShoppingList(w, r, state.Hash, "save_failed")
		return
	}
	a.resume(w, r, userID, state.Hash)
}

func redirectKrogerShoppingList(w http.ResponseWriter, r *http.Request, hash, errorCode string) {
	query := url.Values{"h": {hash}}
	if errorCode != "" {
		query.Set("kroger_error", errorCode)
	}
	http.Redirect(w, r, "/recipes?"+query.Encode()+"#shopping-list-section", http.StatusSeeOther)
}

func (a *OrderingAdapter) cartUser(w http.ResponseWriter, r *http.Request) (string, bool) {
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

var _ ordering.Provider = (*OrderingAdapter)(nil)

func (*OrderingAdapter) ID() string                  { return "kroger" }
func (*OrderingAdapter) Workflow() ordering.Workflow { return ordering.CartTransfer }
func (*OrderingAdapter) Supports(request ordering.Request) bool {
	return request.Source.Provider == "kroger" && NewIdentityProvider().IsID(request.Source.ID)
}

func (*OrderingAdapter) Presentation(request ordering.Request) ordering.Presentation {
	brand, destination := "Kroger", "https://www.kroger.com/cart"
	if strings.EqualFold(request.Source.Chain, "qfc") {
		brand, destination = "QFC", "https://www.qfc.com/cart"
	}
	return ordering.Presentation{Brand: brand, URL: destination, ActionLabel: "Add to " + brand + " cart", Description: "Adds one package of each matched product. Review amounts, store, and checkout in " + brand + "."}
}

type preparedCart struct {
	token string
	items []CartItem
}

func (a *OrderingAdapter) Prepare(ctx context.Context, request ordering.Request) (ordering.Prepared, error) {
	token, err := a.LoadToken(ctx, request.UserID)
	if errors.Is(err, cache.ErrNotFound) {
		return ordering.Prepared{}, &ordering.ConnectionRequired{Provider: a.ID(), Err: err}
	}
	if err != nil {
		return ordering.Prepared{}, &ordering.StepError{Code: "connect_failed", Err: fmt.Errorf("load Kroger connection: %w", err)}
	}
	if time.Now().After(token.ExpiresAt.Add(-time.Minute)) {
		token, err = a.Client.Refresh(ctx, token.RefreshToken)
		if err != nil {
			return ordering.Prepared{}, &ordering.ConnectionRequired{Provider: a.ID(), Err: err}
		}
		if err := a.SaveToken(ctx, request.UserID, token); err != nil {
			return ordering.Prepared{}, &ordering.StepError{Code: "save_failed", Err: err}
		}
	}
	if token.AccessToken == "" {
		return ordering.Prepared{}, &ordering.ConnectionRequired{Provider: a.ID()}
	}
	prepared := ordering.Prepared{}
	data := preparedCart{token: token.AccessToken}
	seenUPCs := make(map[string]bool)
	for _, item := range request.Items {
		if item.ProductID == "" || item.Provider != a.ID() {
			prepared.Gaps = append(prepared.Gaps, item)
			continue
		}
		upc, err := a.Client.ProductUPC(ctx, item.ProductID, request.Source.ID)
		if err != nil {
			if !errors.Is(err, ErrProductUnavailable) {
				return ordering.Prepared{}, &ordering.StepError{Code: "lookup_failed", Err: err}
			}
			prepared.Gaps = append(prepared.Gaps, item)
			continue
		}
		if seenUPCs[upc] {
			continue
		}
		seenUPCs[upc] = true
		data.items = append(data.items, CartItem{UPC: upc, Quantity: 1})
		item.PackageQuantity = 1
		prepared.Sent = append(prepared.Sent, item)
	}
	prepared.Data = data
	return prepared, nil
}

func (a *OrderingAdapter) Submit(ctx context.Context, request ordering.Request, prepared ordering.Prepared) (ordering.Result, error) {
	data, ok := prepared.Data.(preparedCart)
	if !ok {
		return ordering.Result{}, fmt.Errorf("invalid Kroger prepared cart")
	}
	result := ordering.Result{Kind: ordering.CartUpdated, URL: a.Presentation(request).URL, Added: len(data.items), Sent: prepared.Sent, Gaps: prepared.Gaps}
	if err := a.Client.Add(ctx, data.token, data.items); err != nil {
		return result, err
	}
	result.Status = "complete"
	return result, nil
}
