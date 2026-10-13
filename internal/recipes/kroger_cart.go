package recipes

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"careme/internal/ai"
	"careme/internal/auth"
	"careme/internal/cache"
	"careme/internal/locations"
	"careme/internal/providers/kroger"
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
	Sent        []ai.Ingredient `json:"sent,omitempty"`
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
	_, location, err := s.finalizedKrogerPurchases(r.Context(), hash)
	if err != nil {
		http.Error(w, "Shopping list unavailable", http.StatusBadRequest)
		return
	}
	if _, err := s.loadCartTransfer(r.Context(), userID, hash); err == nil {
		redirectKrogerShoppingList(w, r, hash, "")
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
	s.startKrogerAuthorization(w, r, userID, hash, strings.ToLower(location.Chain))
}

func (s *server) startKrogerAuthorization(w http.ResponseWriter, r *http.Request, userID, hash, banner string) {
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		http.Error(w, "Unable to connect to Kroger", http.StatusInternalServerError)
		return
	}
	state := krogerAuthState{Nonce: base64.RawURLEncoding.EncodeToString(nonce), Hash: hash, UserID: userID, IssuedAt: time.Now()}
	body, _ := json.Marshal(state)
	http.SetCookie(w, &http.Cookie{Name: "careme_kroger_state", Value: base64.RawURLEncoding.EncodeToString(body), Path: "/kroger/callback", MaxAge: 600, HttpOnly: true, Secure: strings.HasPrefix(s.cfg.ResolvedPublicOrigin(), "https://"), SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, s.krogerCart.AuthorizationURL(state.Nonce, banner), http.StatusSeeOther)
}

func (s *server) handleKrogerCallback(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
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
		redirectKrogerShoppingList(w, r, state.Hash, "denied")
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		redirectKrogerShoppingList(w, r, state.Hash, "missing_code")
		return
	}
	token, err := s.krogerCart.Exchange(r.Context(), code)
	if err != nil {
		slog.ErrorContext(r.Context(), "Kroger token exchange failed", "error", err)
		redirectKrogerShoppingList(w, r, state.Hash, "connect_failed")
		return
	}
	if err := s.saveCartToken(r.Context(), userID, token); err != nil {
		redirectKrogerShoppingList(w, r, state.Hash, "save_failed")
		return
	}
	s.performCartTransfer(w, r, userID, state.Hash, token)
}

func (s *server) performCartTransfer(w http.ResponseWriter, r *http.Request, userID, hash string, token kroger.CartToken) {
	list, location, err := s.finalizedKrogerPurchases(r.Context(), hash)
	if err != nil {
		redirectKrogerShoppingList(w, r, hash, "list_unavailable")
		return
	}
	var cartItems []kroger.CartItem
	var sent []ai.Ingredient
	var gaps []ai.Ingredient
	seenUPCs := make(map[string]bool)
	for _, group := range list {
		for _, item := range group.Items {
			if item.ProductID == "" {
				gaps = append(gaps, krogerPurchaseIngredient(item))
				continue
			}
			upc, err := s.krogerCart.ProductUPC(r.Context(), item.ProductID, location.ID)
			if err != nil {
				if !errors.Is(err, kroger.ErrProductUnavailable) {
					slog.ErrorContext(r.Context(), "Kroger product lookup failed", "product_id", item.ProductID, "error", err)
					redirectKrogerShoppingList(w, r, hash, "lookup_failed")
					return
				}
				gaps = append(gaps, krogerPurchaseIngredient(item))
				continue
			}
			if !seenUPCs[upc] {
				cartItems = append(cartItems, kroger.CartItem{UPC: upc, Quantity: 1})
				sent = append(sent, krogerPurchaseIngredient(item))
				seenUPCs[upc] = true
			}
		}
	}
	if len(cartItems) == 0 {
		redirectKrogerShoppingList(w, r, hash, "no_matches")
		return
	}
	fingerprint, err := krogerPurchaseFingerprint(list)
	if err != nil {
		redirectKrogerShoppingList(w, r, hash, "prepare_failed")
		return
	}
	if err := s.saveCartTransfer(r.Context(), userID, hash, krogerTransfer{Status: "pending", Sent: sent, Gaps: gaps}, cache.IfNoneMatch()); err != nil {
		if errors.Is(err, cache.ErrAlreadyExists) {
			_, loadErr := s.loadCartTransfer(r.Context(), userID, hash)
			if loadErr == nil {
				redirectKrogerShoppingList(w, r, hash, "")
				return
			}
		}
		redirectKrogerShoppingList(w, r, hash, "start_failed")
		return
	}
	if err := s.krogerCart.Add(r.Context(), token.AccessToken, cartItems); err != nil {
		slog.ErrorContext(r.Context(), "Kroger cart transfer uncertain", "hash", hash, "error", err)
		if err := s.saveCartTransfer(r.Context(), userID, hash, krogerTransfer{Status: "uncertain", Sent: sent, Gaps: gaps}, cache.Unconditional()); err != nil {
			redirectKrogerShoppingList(w, r, hash, "confirmation_failed")
			return
		}
		redirectKrogerShoppingList(w, r, hash, "")
		return
	}
	result := krogerTransfer{Status: "complete", Added: len(cartItems), Sent: sent, Gaps: gaps, Fingerprint: fingerprint}
	if err := s.saveCartTransfer(r.Context(), userID, hash, result, cache.Unconditional()); err != nil {
		redirectKrogerShoppingList(w, r, hash, "confirmation_failed")
		return
	}
	redirectKrogerShoppingList(w, r, hash, "")
}

func redirectKrogerShoppingList(w http.ResponseWriter, r *http.Request, hash, errorCode string) {
	query := url.Values{"h": {hash}}
	if errorCode != "" {
		query.Set("kroger_error", errorCode)
	}
	http.Redirect(w, r, "/recipes?"+query.Encode()+"#shopping-list-section", http.StatusSeeOther)
}

func krogerCartNotice(code string) string {
	return map[string]string{
		"no_matches":          "No products could be matched. Nothing was sent to Kroger.",
		"denied":              "Kroger connection was not approved. Try again, chef.",
		"missing_code":        "Kroger did not finish connecting your account. Try again, chef.",
		"connect_failed":      "Unable to connect to Kroger. Try again, chef.",
		"save_failed":         "Unable to save your Kroger connection. Try again, chef.",
		"list_unavailable":    "Your shopping list could not be loaded. Try again, chef.",
		"lookup_failed":       "Could not check Kroger products. Nothing was sent. Try again, chef.",
		"prepare_failed":      "Could not prepare your shopping list. Nothing was sent. Try again, chef.",
		"start_failed":        "Could not start your Kroger transfer. Check your cart before trying again.",
		"confirmation_failed": "Could not save the transfer result. Items may have been sent. Review your Kroger cart.",
	}[code]
}

func (result krogerTransfer) Message() string {
	switch result.Status {
	case "complete":
		if result.Added == 1 {
			return "Sent 1 product to your cart."
		}
		return fmt.Sprintf("Sent %d products to your cart.", result.Added)
	case "no_matches":
		return "No products could be matched. Nothing was sent to Kroger."
	case "changed":
		return "Your shopping list changed after it was sent to Kroger. Review your cart; the earlier items will not be sent twice."
	default:
		return "Careme could not confirm what Kroger added. Review your cart before adding any missing items."
	}
}

func krogerCartLink(chain string) (string, string) {
	if strings.EqualFold(chain, "qfc") {
		return "https://www.qfc.com/cart", "QFC"
	}
	return krogerCartURL, "Kroger"
}

func (s *server) finalizedKrogerPurchases(ctx context.Context, hash string) ([]shoppingPurchaseGroup, *locations.Location, error) {
	p, err := s.ParamsFromCache(ctx, hash)
	if err != nil {
		return nil, nil, err
	}
	if p.Location == nil || !kroger.NewIdentityProvider().IsID(p.Location.ID) || len(p.Saved) == 0 {
		return nil, nil, fmt.Errorf("shopping list is not a finalized Kroger list")
	}
	location, err := s.locServer.GetLocationByID(ctx, p.Location.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("load current Kroger location: %w", err)
	}
	purchases, err := s.cachedShoppingPurchases(ctx, hash)
	if err != nil {
		return nil, nil, fmt.Errorf("load finalized shopping purchases: %w", err)
	}
	wines := make(map[string]*ai.WineSelection)
	for _, recipe := range p.Saved {
		wine, err := s.WineFromCache(ctx, recipe.ComputeHash())
		if err != nil && !errors.Is(err, cache.ErrNotFound) {
			return nil, nil, fmt.Errorf("load wine: %w", err)
		}
		wines[recipe.ComputeHash()] = wine
	}
	return shoppingPurchasesWithWine(purchases, p.Saved, wines), location, nil
}

func krogerPurchaseFingerprint(purchases []shoppingPurchaseGroup) (string, error) {
	body, err := json.Marshal(purchases)
	if err != nil {
		return "", fmt.Errorf("marshal Kroger purchases: %w", err)
	}
	fingerprint := sha256.Sum256(body)
	return hex.EncodeToString(fingerprint[:]), nil
}

func (s *server) populateKrogerCartView(ctx context.Context, view *shoppingListPageView, userID, errorCode string) error {
	location, err := s.locServer.GetLocationByID(ctx, view.Location.ID)
	if err != nil {
		return err
	}
	view.KrogerCartURL, view.KrogerCartBrand = krogerCartLink(location.Chain)
	view.KrogerCartNotice = krogerCartNotice(errorCode)
	result, err := s.loadCartTransfer(ctx, userID, view.Hash)
	if err == nil {
		fingerprint, err := krogerPurchaseFingerprint(view.Purchases)
		if err != nil {
			return err
		}
		if result.Status == "complete" && result.Fingerprint != fingerprint {
			result.Status = "changed"
		}
		view.KrogerTransfer = &result
	} else if !errors.Is(err, cache.ErrNotFound) {
		return fmt.Errorf("load Kroger transfer: %w", err)
	}
	if view.KrogerTransfer == nil && errorCode == "no_matches" {
		gaps := make([]ai.Ingredient, 0)
		for _, group := range view.Purchases {
			for _, item := range group.Items {
				gaps = append(gaps, krogerPurchaseIngredient(item))
			}
		}
		view.KrogerTransfer = &krogerTransfer{Status: "no_matches", Gaps: gaps}
		view.KrogerCartNotice = ""
	}
	return nil
}

func krogerPurchaseIngredient(item shoppingPurchaseItem) ai.Ingredient {
	return ai.Ingredient{ProductID: item.ProductID, Name: item.Name, Quantity: item.Needed, AisleNumber: item.Aisle}
}
