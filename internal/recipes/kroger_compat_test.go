package recipes

// These helpers exercise legacy routes and persisted records through the new
// adapter while retaining the original regression scenarios.
import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/locations"
	"careme/internal/ordering"
	"careme/internal/providers"
	"careme/internal/providers/kroger"
	"github.com/stretchr/testify/require"
)

type krogerAuthState struct {
	Nonce, Hash, UserID string
	IssuedAt            time.Time
}

// Match the OAuth wire names when inspecting cookies.
func (s *krogerAuthState) UnmarshalJSON(body []byte) error {
	var wire struct {
		Nonce    string    `json:"nonce"`
		Hash     string    `json:"hash"`
		UserID   string    `json:"user_id"`
		IssuedAt time.Time `json:"issued_at"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return err
	}
	s.Nonce, s.Hash, s.UserID, s.IssuedAt = wire.Nonce, wire.Hash, wire.UserID, wire.IssuedAt
	return nil
}

func (s krogerAuthState) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Nonce    string    `json:"nonce"`
		Hash     string    `json:"hash"`
		UserID   string    `json:"user_id"`
		IssuedAt time.Time `json:"issued_at"`
	}{s.Nonce, s.Hash, s.UserID, s.IssuedAt})
}

type krogerTransfer struct {
	Status      string          `json:"status"`
	Added       int             `json:"added"`
	Sent        []ai.Ingredient `json:"sent,omitempty"`
	Gaps        []ai.Ingredient `json:"gaps,omitempty"`
	Fingerprint string          `json:"fingerprint,omitempty"`
}

func testKrogerAdapter(t testing.TB, s *server) *kroger.OrderingAdapter {
	t.Helper()
	if s.ordering != nil {
		if provider, ok := s.ordering.Provider("kroger"); ok {
			return provider.(*kroger.OrderingAdapter)
		}
	}
	return setTestKrogerKey(t, s, make([]byte, 32))
}

func setTestKrogerKey(t testing.TB, s *server, key []byte) *kroger.OrderingAdapter {
	t.Helper()
	client := &kroger.CartClient{}
	if s.ordering != nil {
		if provider, ok := s.ordering.Provider("kroger"); ok {
			client = provider.(*kroger.OrderingAdapter).Client
		}
	}
	adapter, err := kroger.NewOrderingAdapter(kroger.OrderingOptions{Cache: s.Cache, Client: client, TokenKey: key, Origin: s.cfg.ResolvedPublicOrigin(), Auth: s.clerk, Resume: func(w http.ResponseWriter, r *http.Request, userID, hash string) {
		s.ResumeOrdering(w, r, "kroger", userID, hash)
	}})
	require.NoError(t, err)
	s.ordering = ordering.New(s.Cache, []ordering.Provider{adapter})
	return adapter
}
func (s *server) cartTokenKey(userID string) string { return "kroger_cart_tokens/" + userID }
func (s *server) cartTransferKey(userID, hash string) string {
	return "kroger_cart_transfers/" + userID + "/" + hash
}

func (s *server) saveCartToken(ctx context.Context, userID string, token kroger.CartToken) error {
	provider, _ := s.ordering.Provider("kroger")
	return provider.(*kroger.OrderingAdapter).SaveToken(ctx, userID, token)
}

func (s *server) loadCartToken(ctx context.Context, userID string) (kroger.CartToken, error) {
	provider, _ := s.ordering.Provider("kroger")
	return provider.(*kroger.OrderingAdapter).LoadToken(ctx, userID)
}

func (s *server) loadCartTransfer(ctx context.Context, userID, hash string) (krogerTransfer, error) {
	reader, err := s.Cache.Get(ctx, s.cartTransferKey(userID, hash))
	if err != nil {
		return krogerTransfer{}, err
	}
	defer func() { _ = reader.Close() }()
	var result krogerTransfer
	err = json.NewDecoder(reader).Decode(&result)
	return result, err
}

func (s *server) saveCartTransfer(ctx context.Context, userID, hash string, result krogerTransfer, options cache.PutOptions) error {
	body, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return s.Cache.Put(ctx, s.cartTransferKey(userID, hash), string(body), options)
}

func (s *server) handleKrogerCallback(w http.ResponseWriter, r *http.Request) {
	provider, _ := s.ordering.Provider("kroger")
	provider.(*kroger.OrderingAdapter).Callback(w, r)
}

func (s *server) performCartTransfer(w http.ResponseWriter, r *http.Request, userID, hash string, token kroger.CartToken) {
	if token.ExpiresAt.IsZero() {
		token.ExpiresAt = time.Now().Add(time.Hour)
	}
	if err := s.saveCartToken(r.Context(), userID, token); err != nil {
		panic(err)
	}
	s.ResumeOrdering(w, r, "kroger", userID, hash)
}

func (s *server) finalizedKrogerPurchases(ctx context.Context, hash string) ([]shoppingPurchaseGroup, *locations.Location, error) {
	p, err := s.ParamsFromCache(ctx, hash)
	if err != nil {
		return nil, nil, err
	}
	if p.Location == nil || providers.CatalogProviderID(p.Location.ID) != "kroger" || len(p.Saved) == 0 {
		return nil, nil, fmt.Errorf("shopping list is not a finalized Kroger list")
	}
	purchases, location, err := s.finalizedOrderingPurchases(ctx, hash)
	if err != nil {
		return nil, nil, fmt.Errorf("load current Kroger location: %w", err)
	}
	return purchases, location, nil
}

func krogerCartLink(chain string) (string, string) {
	if strings.EqualFold(chain, "qfc") {
		return "https://www.qfc.com/cart", "QFC"
	}
	return "https://www.kroger.com/cart", "Kroger"
}
