package recipes

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
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"careme/internal/ai"
	"careme/internal/auth"
	"careme/internal/cache"
	"careme/internal/kroger"
)

const (
	krogerTokenPrefix    = "kroger_cart_tokens/"
	krogerTransferPrefix = "kroger_cart_transfers/"
	krogerCartURL        = "https://www.kroger.com/cart"
)

type krogerAuthState struct {
	Nonce    string    `json:"nonce"`
	Hash     string    `json:"hash"`
	UserID   string    `json:"user_id"`
	IssuedAt time.Time `json:"issued_at"`
}

type krogerTransfer struct {
	Status      string          `json:"status"`
	Added       int             `json:"added"`
	Gaps        []ai.Ingredient `json:"gaps,omitempty"`
	Fingerprint string          `json:"fingerprint,omitempty"`
}

func (s *server) cartTokenKey(userID string) string { return krogerTokenPrefix + userID }
func (s *server) cartTransferKey(userID, hash string) string {
	return krogerTransferPrefix + userID + "/" + hash
}

func (s *server) saveCartToken(ctx context.Context, userID string, token kroger.CartToken) error {
	block, err := aes.NewCipher(s.krogerCartKey)
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
	return s.Cache.Put(ctx, s.cartTokenKey(userID), base64.StdEncoding.EncodeToString(ciphertext), cache.Unconditional())
}

func (s *server) loadCartToken(ctx context.Context, userID string) (kroger.CartToken, error) {
	reader, err := s.Cache.Get(ctx, s.cartTokenKey(userID))
	if err != nil {
		return kroger.CartToken{}, err
	}
	defer func() { _ = reader.Close() }()
	encoded, err := io.ReadAll(io.LimitReader(reader, 8192))
	if err != nil {
		return kroger.CartToken{}, err
	}
	ciphertext, err := base64.StdEncoding.DecodeString(string(encoded))
	if err != nil {
		return kroger.CartToken{}, err
	}
	block, err := aes.NewCipher(s.krogerCartKey)
	if err != nil {
		return kroger.CartToken{}, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return kroger.CartToken{}, err
	}
	if len(ciphertext) < aead.NonceSize() {
		return kroger.CartToken{}, fmt.Errorf("short Kroger token record")
	}
	plaintext, err := aead.Open(nil, ciphertext[:aead.NonceSize()], ciphertext[aead.NonceSize():], []byte(userID))
	if err != nil {
		return kroger.CartToken{}, err
	}
	var token kroger.CartToken
	if err := json.Unmarshal(plaintext, &token); err != nil {
		return kroger.CartToken{}, err
	}
	return token, nil
}

func (s *server) loadCartTransfer(ctx context.Context, userID, hash string) (krogerTransfer, error) {
	reader, err := s.Cache.Get(ctx, s.cartTransferKey(userID, hash))
	if err != nil {
		return krogerTransfer{}, err
	}
	defer func() { _ = reader.Close() }()
	var result krogerTransfer
	if err := json.NewDecoder(reader).Decode(&result); err != nil {
		return krogerTransfer{}, err
	}
	return result, nil
}

func (s *server) saveCartTransfer(ctx context.Context, userID, hash string, result krogerTransfer, options cache.PutOptions) error {
	body, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return s.Cache.Put(ctx, s.cartTransferKey(userID, hash), string(body), options)
}

func (s *server) cartUser(w http.ResponseWriter, r *http.Request) (string, bool) {
	userID, err := s.clerk.GetUserIDFromRequest(r)
	if err == nil {
		return userID, true
	}
	if errors.Is(err, auth.ErrNoSession) {
		redirectToSignIn(w, r, http.StatusUnauthorized)
	} else {
		http.Error(w, "Unable to load account", http.StatusInternalServerError)
	}
	return "", false
}

func (s *server) handleKrogerCart(w http.ResponseWriter, r *http.Request) {
	if s.krogerCart == nil {
		http.NotFound(w, r)
		return
	}
	userID, ok := s.cartUser(w, r)
	if !ok {
		return
	}
	hash := strings.TrimSpace(r.PathValue("hash"))
	ingredients, _, err := s.finalizedKrogerIngredients(r.Context(), hash)
	if err != nil {
		http.Error(w, "Shopping list unavailable", http.StatusBadRequest)
		return
	}
	if result, err := s.loadCartTransfer(r.Context(), userID, hash); err == nil {
		fingerprint, hashErr := shoppingQuantityFingerprint(ingredients)
		if hashErr != nil {
			http.Error(w, "Unable to check shopping list", http.StatusInternalServerError)
			return
		}
		if result.Status == "complete" && result.Fingerprint != fingerprint {
			result.Status = "changed"
		}
		s.showCartTransfer(w, result)
		return
	} else if !errors.Is(err, cache.ErrNotFound) {
		http.Error(w, "Unable to check Kroger transfer", http.StatusInternalServerError)
		return
	}
	token, err := s.loadCartToken(r.Context(), userID)
	if err != nil && !errors.Is(err, cache.ErrNotFound) {
		http.Error(w, "Unable to load Kroger connection", http.StatusInternalServerError)
		return
	}
	if err == nil && time.Now().After(token.ExpiresAt.Add(-time.Minute)) {
		token, err = s.krogerCart.Refresh(r.Context(), token.RefreshToken)
		if err == nil {
			err = s.saveCartToken(r.Context(), userID, token)
		}
	}
	if err == nil && token.AccessToken != "" {
		s.performCartTransfer(w, r, userID, hash, token)
		return
	}
	s.startKrogerAuthorization(w, r, userID, hash)
}

func (s *server) startKrogerAuthorization(w http.ResponseWriter, r *http.Request, userID, hash string) {
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		http.Error(w, "Unable to connect to Kroger", http.StatusInternalServerError)
		return
	}
	state := krogerAuthState{Nonce: base64.RawURLEncoding.EncodeToString(nonce), Hash: hash, UserID: userID, IssuedAt: time.Now()}
	body, _ := json.Marshal(state)
	http.SetCookie(w, &http.Cookie{Name: "careme_kroger_state", Value: base64.RawURLEncoding.EncodeToString(body), Path: "/kroger/callback", MaxAge: 600, HttpOnly: true, Secure: strings.HasPrefix(s.cfg.ResolvedPublicOrigin(), "https://"), SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, s.krogerCart.AuthorizationURL(state.Nonce), http.StatusSeeOther)
}

func (s *server) handleKrogerCallback(w http.ResponseWriter, r *http.Request) {
	if s.krogerCart == nil {
		http.NotFound(w, r)
		return
	}
	userID, ok := s.cartUser(w, r)
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
		http.Error(w, "Kroger connection was not approved", http.StatusBadRequest)
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "Kroger did not return a code", http.StatusBadRequest)
		return
	}
	token, err := s.krogerCart.Exchange(r.Context(), code)
	if err != nil {
		slog.ErrorContext(r.Context(), "Kroger token exchange failed", "error", err)
		http.Error(w, "Unable to connect to Kroger", http.StatusBadGateway)
		return
	}
	if err := s.saveCartToken(r.Context(), userID, token); err != nil {
		http.Error(w, "Unable to save Kroger connection", http.StatusInternalServerError)
		return
	}
	s.performCartTransfer(w, r, userID, state.Hash, token)
}

func (s *server) performCartTransfer(w http.ResponseWriter, r *http.Request, userID, hash string, token kroger.CartToken) {
	ingredients, locationID, err := s.finalizedKrogerIngredients(r.Context(), hash)
	if err != nil {
		http.Error(w, "Shopping list unavailable", http.StatusBadRequest)
		return
	}
	list, err := s.mergedShoppingList(r.Context(), hash, ingredients)
	if err != nil {
		http.Error(w, "Unable to combine shopping quantities. Try again, chef.", http.StatusServiceUnavailable)
		return
	}
	var cartItems []kroger.CartItem
	var gaps []ai.Ingredient
	seenUPCs := make(map[string]bool)
	for _, group := range list {
		for _, item := range group.Items {
			if item.ProductID == "" {
				gaps = append(gaps, *item)
				continue
			}
			upc, err := s.krogerCart.ProductUPC(r.Context(), item.ProductID, locationID)
			if err != nil {
				if !errors.Is(err, kroger.ErrProductUnavailable) {
					slog.ErrorContext(r.Context(), "Kroger product lookup failed", "product_id", item.ProductID, "error", err)
					http.Error(w, "Unable to check Kroger products. Try again, chef.", http.StatusServiceUnavailable)
					return
				}
				gaps = append(gaps, *item)
				continue
			}
			if !seenUPCs[upc] {
				cartItems = append(cartItems, kroger.CartItem{UPC: upc, Quantity: 1})
				seenUPCs[upc] = true
			}
		}
	}
	if len(cartItems) == 0 {
		s.showCartTransfer(w, krogerTransfer{Status: "no_matches", Gaps: gaps})
		return
	}
	fingerprint, err := shoppingQuantityFingerprint(ingredients)
	if err != nil {
		http.Error(w, "Unable to prepare shopping list", http.StatusInternalServerError)
		return
	}
	if err := s.saveCartTransfer(r.Context(), userID, hash, krogerTransfer{Status: "pending", Gaps: gaps}, cache.IfNoneMatch()); err != nil {
		if errors.Is(err, cache.ErrAlreadyExists) {
			result, loadErr := s.loadCartTransfer(r.Context(), userID, hash)
			if loadErr == nil {
				s.showCartTransfer(w, result)
				return
			}
		}
		http.Error(w, "Unable to start Kroger transfer", http.StatusInternalServerError)
		return
	}
	if err := s.krogerCart.Add(r.Context(), token.AccessToken, cartItems); err != nil {
		slog.ErrorContext(r.Context(), "Kroger cart transfer uncertain", "hash", hash, "error", err)
		s.showCartTransfer(w, krogerTransfer{Status: "uncertain", Gaps: gaps})
		return
	}
	result := krogerTransfer{Status: "complete", Added: len(cartItems), Gaps: gaps, Fingerprint: fingerprint}
	if err := s.saveCartTransfer(r.Context(), userID, hash, result, cache.Unconditional()); err != nil {
		http.Error(w, "Items sent to Kroger, but confirmation could not be saved. Check your Kroger cart.", http.StatusInternalServerError)
		return
	}
	s.showCartTransfer(w, result)
}

func (s *server) showCartTransfer(w http.ResponseWriter, result krogerTransfer) {
	if result.Status == "complete" && len(result.Gaps) == 0 {
		w.Header().Set("Location", krogerCartURL)
		w.WriteHeader(http.StatusSeeOther)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	view := struct {
		Message string
		Gaps    []ai.Ingredient
		CartURL string
	}{Gaps: result.Gaps, CartURL: krogerCartURL}
	switch result.Status {
	case "complete":
		view.Message = fmt.Sprintf("Added %d products to your Kroger cart. Add or adjust these items in Kroger:", result.Added)
	case "no_matches":
		view.Message = "No products could be matched. Add these items in Kroger:"
	case "changed":
		view.Message = "Your shopping list changed after Careme added it to Kroger. Review the new items in Kroger; Careme will not add the earlier items twice."
	default:
		view.Message = "Careme could not confirm what Kroger added. Check your cart and add any missing items there."
	}
	_ = template.Must(template.New("result").Parse(`<!doctype html><html lang="en"><meta name="viewport" content="width=device-width, initial-scale=1"><title>Kroger cart</title><main style="max-width:42rem;margin:3rem auto;padding:1rem;font-family:sans-serif"><h1>Kroger cart</h1><p>{{.Message}}</p>{{if .Gaps}}<ul>{{range .Gaps}}<li>{{.Name}}{{if .Quantity}} — {{.Quantity}}{{end}}</li>{{end}}</ul>{{end}}<p><a href="{{.CartURL}}">Open Kroger cart</a></p></main></html>`)).Execute(w, view)
}
