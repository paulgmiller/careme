package recipes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"careme/internal/ai"
	"careme/internal/auth"
	"careme/internal/cache"
	"careme/internal/locations"
	"careme/internal/ordering"
	"careme/internal/providers"
)

// ConnectionStarter is implemented only by adapters that connect shopper accounts.
type connectionStarter interface {
	StartAuthorization(http.ResponseWriter, *http.Request, ordering.Request)
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
	r.SetPathValue("provider", "kroger")
	s.handleOrdering(w, r)
}

func (s *server) handleOrdering(w http.ResponseWriter, r *http.Request) {
	providerID := r.PathValue("provider")
	if s.ordering == nil {
		http.NotFound(w, r)
		return
	}
	provider, ok := s.ordering.Provider(providerID)
	if !ok {
		http.NotFound(w, r)
		return
	}
	userID, ok := s.cartUser(w, r)
	if !ok {
		return
	}
	hash := strings.TrimSpace(r.PathValue("hash"))
	req, err := s.finalizedOrderingRequest(r.Context(), userID, hash)
	if err != nil || !provider.Supports(req) {
		http.Error(w, "Shopping list unavailable", http.StatusBadRequest)
		return
	}
	s.transferOrdering(w, r, providerID, req)
}

// ResumeOrdering resumes a verified provider connection using the current finalized snapshot.
func (s *server) ResumeOrdering(w http.ResponseWriter, r *http.Request, providerID, userID, hash string) {
	req, err := s.finalizedOrderingRequest(r.Context(), userID, hash)
	if err != nil {
		redirectOrderingShoppingList(w, r, providerID, hash, "list_unavailable")
		return
	}
	s.transferOrdering(w, r, providerID, req)
}

func (s *server) transferOrdering(w http.ResponseWriter, r *http.Request, providerID string, req ordering.Request) {
	result, err := s.ordering.Transfer(r.Context(), providerID, req)
	if err == nil {
		if result.Status == "complete" {
			http.Redirect(w, r, result.URL, http.StatusSeeOther)
			return
		}
		redirectOrderingShoppingList(w, r, providerID, req.Hash, "")
		return
	}
	var connection *ordering.ConnectionRequired
	if errors.As(err, &connection) {
		provider, _ := s.ordering.Provider(providerID)
		if starter, ok := provider.(connectionStarter); ok {
			starter.StartAuthorization(w, r, req)
			return
		}
	}
	slog.ErrorContext(r.Context(), "ordering handoff failed", "provider", providerID, "hash", req.Hash, "error", err)
	code := "prepare_failed"
	var step *ordering.StepError
	if errors.As(err, &step) {
		code = step.Code
	}
	redirectOrderingShoppingList(w, r, providerID, req.Hash, code)
}

func redirectOrderingShoppingList(w http.ResponseWriter, r *http.Request, providerID, hash, errorCode string) {
	query := url.Values{"h": {hash}}
	if errorCode != "" {
		if providerID == "kroger" {
			query.Set("kroger_error", errorCode)
		} else {
			query.Set("ordering_provider", providerID)
			query.Set("ordering_error", errorCode)
		}
	}
	http.Redirect(w, r, "/recipes?"+query.Encode()+"#shopping-list-section", http.StatusSeeOther)
}

func (s *server) finalizedOrderingPurchases(ctx context.Context, hash string) ([]shoppingPurchaseGroup, *locations.Location, error) {
	p, err := s.ParamsFromCache(ctx, hash)
	if err != nil {
		return nil, nil, err
	}
	if p.Location == nil || len(p.Saved) == 0 {
		return nil, nil, fmt.Errorf("shopping list is not finalized")
	}
	location, err := s.locServer.GetLocationByID(ctx, p.Location.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("load current shopping location: %w", err)
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

func (s *server) finalizedOrderingRequest(ctx context.Context, userID, hash string) (ordering.Request, error) {
	purchases, location, err := s.finalizedOrderingPurchases(ctx, hash)
	if err != nil {
		return ordering.Request{}, err
	}
	return s.orderingRequest(userID, hash, purchases, location)
}

func (s *server) orderingRequest(userID, hash string, purchases []shoppingPurchaseGroup, location *locations.Location) (ordering.Request, error) {
	fingerprint, err := krogerPurchaseFingerprint(purchases)
	if err != nil {
		return ordering.Request{}, err
	}
	source := ordering.Store{Provider: providers.CatalogProviderID(location.ID), ID: location.ID, Chain: location.Chain}
	req := ordering.Request{UserID: userID, Hash: hash, Fingerprint: fingerprint, Title: "Shopping list for " + location.Name, LinkBackURL: s.cfg.ResolvedPublicOrigin() + "/recipes?h=" + url.QueryEscape(hash), Source: source}
	for _, group := range purchases {
		for _, item := range group.Items {
			req.Items = append(req.Items, ordering.Item{Provider: source.Provider, ProductID: item.ProductID, Name: item.Name, Needed: item.Needed, Size: item.Size, Aisle: item.Aisle, Buy: item.Buy})
		}
	}
	return req, nil
}

func (s *server) populateOrderingView(ctx context.Context, view *shoppingListPageView, userID string, r *http.Request) error {
	if s.ordering == nil || len(s.ordering.Providers()) == 0 {
		return nil
	}
	location, err := s.locServer.GetLocationByID(ctx, view.Location.ID)
	if err != nil {
		return fmt.Errorf("load ordering location: %w", err)
	}
	req, err := s.orderingRequest(userID, view.Hash, view.Purchases, location)
	if err != nil {
		return err
	}
	for _, provider := range s.ordering.Providers() {
		if !provider.Supports(req) {
			continue
		}
		meta := provider.Presentation(req)
		code := ""
		if provider.ID() == "kroger" {
			code = r.URL.Query().Get("kroger_error")
		} else if r.URL.Query().Get("ordering_provider") == provider.ID() {
			code = r.URL.Query().Get("ordering_error")
		}
		action := orderingActionView{Provider: provider.ID(), Brand: meta.Brand, URL: meta.URL, ActionLabel: meta.ActionLabel, Description: meta.Description, ActionURL: "/recipes/" + url.PathEscape(view.Hash) + "/ordering/" + url.PathEscape(provider.ID()), CanSubmit: true, Notice: orderingNotice(provider.ID(), meta.Brand, code)}
		result, err := s.ordering.Load(ctx, provider.ID(), req)
		if err == nil {
			action.Result = &result
			action.CanSubmit = false
			if result.URL != "" {
				action.URL = result.URL
			}
			action.Message = orderingMessage(result, provider.ID(), meta.Brand)
		} else if !errors.Is(err, cache.ErrNotFound) {
			return fmt.Errorf("load ordering result: %w", err)
		}
		if action.Result == nil && code == "no_matches" {
			result = ordering.Result{Kind: ordering.CartUpdated, Status: "no_matches", Gaps: req.Items}
			action.Result = &result
			action.Message = orderingMessage(result, provider.ID(), meta.Brand)
			action.Notice = ""
		}
		view.OrderingActions = append(view.OrderingActions, action)
	}
	return nil
}

func orderingMessage(result ordering.Result, providerID, brand string) string {
	if providerID == "kroger" {
		brand = "Kroger"
	}
	switch result.Status {
	case "complete":
		if result.Added == 1 {
			return "Sent 1 product to your cart."
		}
		return fmt.Sprintf("Sent %d products to your cart.", result.Added)
	case "no_matches":
		return "No products could be matched. Nothing was sent to " + brand + "."
	case "changed":
		return "Your shopping list changed after it was sent to " + brand + ". Review your cart; the earlier items will not be sent twice."
	default:
		return "Careme could not confirm what " + brand + " added. Review your cart before adding any missing items."
	}
}

func orderingNotice(providerID, brand, code string) string {
	if providerID == "kroger" {
		return krogerCartNotice(code)
	}
	return map[string]string{
		"no_matches":          "No products could be matched. Nothing was sent to " + brand + ".",
		"prepare_failed":      "Could not prepare your shopping list. Try again, chef.",
		"lookup_failed":       "Could not check products. Nothing was sent. Try again, chef.",
		"start_failed":        "Could not start your transfer. Review your cart before trying again.",
		"submit_failed":       "Could not create your shopping list. Try again, chef.",
		"confirmation_failed": "Could not save the result. Review " + brand + " before trying again.",
		"list_unavailable":    "Your shopping list could not be loaded. Try again, chef.",
	}[code]
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

func krogerPurchaseFingerprint(purchases []shoppingPurchaseGroup) (string, error) {
	body, err := json.Marshal(purchases)
	if err != nil {
		return "", fmt.Errorf("marshal Kroger purchases: %w", err)
	}
	fingerprint := sha256.Sum256(body)
	return hex.EncodeToString(fingerprint[:]), nil
}
