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
	"careme/internal/providers/kroger"
	"careme/internal/templates"
)

const shoppingQuantityCachePrefix = "shopping_quantities/v1/"

type ShoppingQuantityMerger interface {
	MergeShoppingQuantities(context.Context, []ai.ShoppingQuantityGroup) (map[string]string, error)
}

// SimpleShoppingQuantityMerger is used by the local mock server.
type SimpleShoppingQuantityMerger struct{}

func (SimpleShoppingQuantityMerger) MergeShoppingQuantities(_ context.Context, groups []ai.ShoppingQuantityGroup) (map[string]string, error) {
	result := make(map[string]string, len(groups))
	for _, group := range groups {
		quantity := ""
		for _, amount := range group.Quantities {
			quantity = mergeShoppingQuantities(quantity, amount)
		}
		result[group.Key] = quantity
	}
	return result, nil
}

func shoppingQuantityGroups(ingredients []ai.Ingredient) []ai.ShoppingQuantityGroup {
	groups := make([]ai.ShoppingQuantityGroup, 0)
	indexes := make(map[string]int)
	for _, item := range ingredients {
		key := shoppingListKey(item)
		if key == "name:" {
			continue
		}
		index, found := indexes[key]
		if !found {
			index = len(groups)
			indexes[key] = index
			groups = append(groups, ai.ShoppingQuantityGroup{Key: key, Name: item.Name})
		}
		groups[index].Quantities = append(groups[index].Quantities, strings.TrimSpace(item.Quantity))
	}
	return groups
}

func shoppingQuantityFingerprint(ingredients []ai.Ingredient) (string, error) {
	groupBytes, err := json.Marshal(shoppingQuantityGroups(ingredients))
	if err != nil {
		return "", fmt.Errorf("marshal shopping quantity groups: %w", err)
	}
	fingerprint := sha256.Sum256(groupBytes)
	return hex.EncodeToString(fingerprint[:]), nil
}

func (s *server) mergedShoppingList(ctx context.Context, hash string, ingredients []ai.Ingredient) ([]shoppingListGroup, error) {
	quantityGroups := shoppingQuantityGroups(ingredients)
	fingerprint, err := shoppingQuantityFingerprint(ingredients)
	if err != nil {
		return nil, err
	}
	key := shoppingQuantityCachePrefix + hash + "/" + fingerprint
	var amounts map[string]string
	reader, err := s.Cache.Get(ctx, key)
	switch {
	case err == nil:
		defer func() { _ = reader.Close() }()
		if err := json.NewDecoder(reader).Decode(&amounts); err != nil {
			return nil, fmt.Errorf("decode merged shopping quantities: %w", err)
		}
	case errors.Is(err, cache.ErrNotFound):
		if s.shoppingMerger == nil {
			return nil, fmt.Errorf("shopping quantity merger unavailable")
		}
		amounts, err = s.shoppingMerger.MergeShoppingQuantities(ctx, quantityGroups)
		if err != nil {
			return nil, err
		}
		body, err := json.Marshal(amounts)
		if err != nil {
			return nil, err
		}
		if err := s.Cache.Put(ctx, key, string(body), cache.IfNoneMatch()); err != nil {
			if !errors.Is(err, cache.ErrAlreadyExists) {
				return nil, fmt.Errorf("cache merged shopping quantities: %w", err)
			}
			winner, err := s.Cache.Get(ctx, key)
			if err != nil {
				return nil, fmt.Errorf("load concurrent shopping merge: %w", err)
			}
			defer func() { _ = winner.Close() }()
			if err := json.NewDecoder(winner).Decode(&amounts); err != nil {
				return nil, fmt.Errorf("decode concurrent shopping merge: %w", err)
			}
		}
	default:
		return nil, fmt.Errorf("load merged shopping quantities: %w", err)
	}
	list := shoppingListForDisplay(ingredients)
	for _, group := range list {
		for _, item := range group.Items {
			quantity, ok := amounts[shoppingListKey(*item)]
			if !ok {
				return nil, fmt.Errorf("missing merged shopping quantity for %q", item.Name)
			}
			item.Quantity = quantity
		}
	}
	return list, nil
}

func (s *server) finalizedKrogerIngredients(ctx context.Context, hash string) ([]ai.Ingredient, *locations.Location, error) {
	p, err := s.ParamsFromCache(ctx, hash)
	if err != nil {
		return nil, nil, err
	}
	if p.Location == nil || !kroger.NewIdentityProvider().IsID(p.Location.ID) || len(p.Saved) == 0 {
		return nil, nil, fmt.Errorf("shopping list is not a finalized Kroger list")
	}
	ingredients := make([]ai.Ingredient, 0)
	for _, recipe := range p.Saved {
		var wine *ai.WineSelection
		wine, err = s.WineFromCache(ctx, recipe.ComputeHash())
		if err != nil && !errors.Is(err, cache.ErrNotFound) {
			return nil, nil, fmt.Errorf("load wine: %w", err)
		}
		ingredients = append(ingredients, ingredientsForDisplay(recipe.Ingredients, wine)...)
	}
	return ingredients, p.Location, nil
}

func (s *server) handleShoppingQuantities(w http.ResponseWriter, r *http.Request) {
	if s.krogerCart == nil {
		http.NotFound(w, r)
		return
	}
	userID, err := s.clerk.GetUserIDFromRequest(r)
	if err != nil {
		if errors.Is(err, auth.ErrNoSession) {
			http.Error(w, "Sign in to use the Kroger cart", http.StatusUnauthorized)
		} else {
			http.Error(w, "Unable to load account", http.StatusInternalServerError)
		}
		return
	}
	hash := strings.TrimSpace(r.PathValue("hash"))
	ingredients, location, err := s.finalizedKrogerIngredients(r.Context(), hash)
	if err != nil {
		http.Error(w, "Shopping list unavailable", http.StatusBadRequest)
		return
	}
	list, err := s.mergedShoppingList(r.Context(), hash, ingredients)
	if err != nil {
		slog.ErrorContext(r.Context(), "failed to merge shopping quantities", "hash", hash, "error", err)
		_, _ = w.Write([]byte(`<section id="shopping-list-section"><p>Could not combine your list. <button hx-get="/recipes/` + url.PathEscape(hash) + `/shopping-quantities" hx-target="#shopping-list-section" hx-swap="outerHTML">Try again, chef</button></p></section>`))
		return
	}
	cartURL, cartBrand := krogerCartLink(location.Chain)
	view := shoppingListPageView{Hash: hash, ShoppingList: list, KrogerCartNotice: krogerCartNotice(r.URL.Query().Get("kroger_error")), KrogerCartURL: cartURL, KrogerCartBrand: cartBrand}
	result, err := s.loadCartTransfer(r.Context(), userID, hash)
	if err == nil {
		fingerprint, hashErr := shoppingQuantityFingerprint(ingredients)
		if hashErr != nil {
			http.Error(w, "Unable to check shopping list", http.StatusInternalServerError)
			return
		}
		if result.Status == "complete" && result.Fingerprint != fingerprint {
			result.Status = "changed"
		}
		view.KrogerTransfer = &result
	} else if !errors.Is(err, cache.ErrNotFound) {
		http.Error(w, "Unable to load Kroger transfer", http.StatusInternalServerError)
		return
	}
	if view.KrogerTransfer == nil && r.URL.Query().Get("kroger_error") == "no_matches" {
		gaps := make([]ai.Ingredient, 0)
		for _, group := range list {
			for _, item := range group.Items {
				gaps = append(gaps, *item)
			}
		}
		view.KrogerTransfer = &krogerTransfer{Status: "no_matches", Gaps: gaps}
		view.KrogerCartNotice = ""
	}
	if err := templates.ShoppingList.ExecuteTemplate(w, "kroger_shopping_list_section", view); err != nil {
		slog.ErrorContext(r.Context(), "render shopping quantities", "error", err)
	}
}
