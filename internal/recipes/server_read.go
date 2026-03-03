package recipes

import (
	"careme/internal/ai"
	"careme/internal/auth"
	"careme/internal/cache"
	"careme/internal/kroger"
	"careme/internal/locations"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/samber/lo"
)

func (s *server) handleSingle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	hash := r.PathValue("hash")
	if hash == "" {
		http.Error(w, "missing recipe hash", http.StatusBadRequest)
		return
	}

	recipe, err := s.SingleFromCache(ctx, hash)
	if err != nil {
		http.Error(w, "recipe not found", http.StatusNotFound)
		return
	}
	_, err = s.clerk.GetUserIDFromRequest(r)
	signedIn := !errors.Is(err, auth.ErrNoSession)
	feedback, err := s.FeedbackFromCache(ctx, hash)
	if err != nil {
		if !errors.Is(err, cache.ErrNotFound) {
			slog.ErrorContext(ctx, "failed to load recipe feedback", "hash", hash, "error", err)
		}
		feedback = &RecipeFeedback{}
	}

	if recipe.OriginHash == "" {
		slog.WarnContext(ctx, "recipe missing origin hash Probably and old recipe", "hash", hash)
		p := DefaultParams(&locations.Location{
			ID:   "",
			Name: "Unknown Location",
		}, time.Now())
		thread, err := s.ThreadFromCache(ctx, hash)
		if err != nil && !errors.Is(err, cache.ErrNotFound) {
			slog.ErrorContext(ctx, "failed to load recipe thread", "hash", hash, "error", err)
		}
		FormatRecipeHTML(p, *recipe, signedIn, thread, *feedback, w)
		return
	}
	//we didn't go back and update old recipes's  with new hash so have to handle that here. Could still backfill
	if normalizedHash, ok := legacyHashToCurrent(recipe.OriginHash, legacyRecipeHashSeed); ok {
		slog.InfoContext(ctx, "normalized legacy origin hash to current hash", "origin_hash", recipe.OriginHash, "hash", normalizedHash)
		recipe.OriginHash = normalizedHash
		//could resave to backfill but don't think we'll ever get them all without looping
	}
	p, err := s.ParamsFromCache(ctx, recipe.OriginHash)
	if err != nil {
		slog.ErrorContext(ctx, "failed to load params for hash", "hash", recipe.OriginHash, "error", err)
		//http.Error(w, "recipe not found or expired", http.StatusNotFound)
		//return
		p = DefaultParams(&locations.Location{
			ID:   "",
			Name: "Unknown Location",
		}, time.Now())
	}

	if p.ConversationID == "" {
		if slist, err := s.FromCache(ctx, recipe.OriginHash); err == nil {
			p.ConversationID = slist.ConversationID
		} else if !errors.Is(err, cache.ErrNotFound) {
			slog.ErrorContext(ctx, "failed to load conversation id", "hash", recipe.OriginHash, "error", err)
		}
	}

	thread, err := s.ThreadFromCache(ctx, hash)
	if err != nil && !errors.Is(err, cache.ErrNotFound) {
		slog.ErrorContext(ctx, "failed to load recipe thread", "hash", hash, "error", err)
	}

	slog.InfoContext(ctx, "serving shared recipe by hash", "hash", hash, "signedIn", signedIn)
	FormatRecipeHTML(p, *recipe, signedIn, thread, *feedback, w)
}

func (s *server) notFound(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	startArg := r.URL.Query().Get(queryArgStart)
	hashParam := r.URL.Query().Get(queryArgHash)
	//okay give them a new start time.
	if startArg == "" {
		redirectToHash(w, r, hashParam, true /*useStart*/)
		return
	}

	if startTime, err := time.Parse(time.RFC3339Nano, startArg); err == nil {
		if time.Since(startTime) > time.Minute*10 {
			p, err := s.ParamsFromCache(ctx, hashParam)
			if err != nil {
				slog.ErrorContext(ctx, "failed to load params for hash", "hash", hashParam, "error", err)
				http.Error(w, "recipe not found or expired", http.StatusNotFound)
				return
			}

			currentUser, err := s.storage.FromRequest(ctx, r, s.clerk) // just for logging purposes in kickgeneration. We could do this in the generateion function instead to avoid the extra call on every not found.
			if err != nil {
				if !errors.Is(err, auth.ErrNoSession) {
					slog.ErrorContext(ctx, "failed to get clerk user ID", "error", err)
					http.Error(w, "unable to load account", http.StatusInternalServerError)
					return
				}
				http.Redirect(w, r, "/", http.StatusSeeOther)
				return
			}
			s.kickgeneration(ctx, p, currentUser)
			redirectToHash(w, r, p.Hash(), true /*useStart*/)
			return
		}
	}
	s.Spin(w, r)
}

func (s *server) handleRecipes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// TODO(pm): Revisit route shape for hash-based recipe lists. `h` is a derived key from
	// query params, so `/recipes?h=...` is defensible; decide later if we also want a
	// canonical path form like `/recipes/{h}` or just a redirect alias.
	if hashParam := r.URL.Query().Get(queryArgHash); hashParam != "" {
		if normalizedHash, ok := legacyHashToCurrent(hashParam, legacyRecipeHashSeed); ok {
			slog.InfoContext(ctx, "redirecting legacy hash to canonical hash", "legacy_hash", hashParam, "hash", normalizedHash)
			redirectToHash(w, r, normalizedHash, false /*useStart*/)
			return
		}
		slist, err := s.FromCache(ctx, hashParam) // ideally should memory cache this so lots of reloads don't constantly go out to azure
		if err != nil {
			if errors.Is(err, cache.ErrNotFound) {
				s.notFound(ctx, w, r)
				return
			}
			slog.ErrorContext(ctx, "failed to load recipe list for hash", "hash", hashParam, "error", err)
			http.Error(w, "invalid recipe", http.StatusInternalServerError)
			return
		}
		if r.URL.Query().Has(queryArgStart) {
			redirectToHash(w, r, hashParam, false /*useStart*/)
			return
		}

		p, err := s.ParamsFromCache(ctx, hashParam)
		if err != nil {
			slog.ErrorContext(ctx, "failed to load params for hash", "hash", hashParam, "error", err)
			http.Error(w, "failed to load recipe parameters", http.StatusInternalServerError)
			return
		}
		if r.URL.Query().Get("mail") == "true" {
			if err := FormatMail(p, *slist, w); err != nil {
				slog.ErrorContext(ctx, "failed to render mail template", "error", err)
				http.Error(w, "failed to render mail template", http.StatusInternalServerError)
			}
			return
		}
		styles := wineStyles(slist.Recipes)
		slog.InfoContext(ctx, "wines!", "hash", hashParam, "wine_styles", styles)
		userID, err := s.clerk.GetUserIDFromRequest(r)
		signedIn := !errors.Is(err, auth.ErrNoSession)
		if signedIn {
			fromStore, selErr := s.loadRecipeSelection(ctx, userID, hashParam)
			if selErr != nil {
				slog.ErrorContext(ctx, "failed to load recipe selection for render", "user_id", userID, "hash", hashParam, "error", selErr)
				http.Error(w, "failed to load recipe selection", http.StatusInternalServerError)
				return
			}
			s.mergeParamsWithSelection(ctx, p, fromStore, slist.Recipes)
		}
		applySavedToRecipes(slist.Recipes, p)
		FormatShoppingListHTMLForHash(p, *slist, signedIn, hashParam, w)
		return
	}

	p, err := s.ParseQueryArgs(ctx, r)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid query parameters: %v", err), http.StatusBadRequest)
		return
	}
	// what do we do with this?
	// p.UserID = currentUser.ID

	currentUser, err := s.storage.FromRequest(ctx, r, s.clerk) // just for logging purposes in kickgeneration. We could do this in the generateion function instead to avoid the extra call on every not found.
	if err != nil {
		if !errors.Is(err, auth.ErrNoSession) {
			slog.ErrorContext(ctx, "failed to get clerk user ID", "error", err)
			http.Error(w, "unable to load account", http.StatusInternalServerError)
			return
		}
		slog.InfoContext(ctx, "failed got no sesion from request", "error", err, "url", r.URL.String())
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	//if params are already saved redirect and assume someone kicks off genration

	if err := s.SaveParams(ctx, p); err != nil {
		if errors.Is(err, ErrAlreadyExists) {
			slog.InfoContext(ctx, "params already existed redirecting", "hash", p.Hash())
			redirectToHash(w, r, p.Hash(), false /*useStart*/)
			return
		}
		slog.ErrorContext(ctx, "failed to save params", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	hash := p.Hash()

	s.kickgeneration(ctx, p, currentUser)

	redirectToHash(w, r, hash, true /*useStart*/)
}

func wineStyles(recipes []ai.Recipe) []string {
	styles := lo.Flatten(lo.Map(recipes, func(r ai.Recipe, _ int) []string {
		return r.WineStyles
	}))
	return lo.Uniq(styles)
}

// move to admin? Nah let the people see
func (s *server) ingredients(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	loc := r.PathValue("location")
	l, err := s.locServer.GetLocationByID(ctx, loc)
	if err != nil {
		http.Error(w, "invalid location id", http.StatusBadRequest)
		return
	}
	// later use saved items
	p := DefaultParams(l, time.Now())

	lochash := p.LocationHash()
	ingredientblob, err := s.cache.Get(ctx, lochash)
	if err != nil {
		http.Error(w, "ingredients not found in cache", http.StatusNotFound)
		return
	}
	slog.Info("serving cached ingredients", "location", p.String(), "hash", lochash)
	defer func() {
		if err := ingredientblob.Close(); err != nil {
			slog.ErrorContext(ctx, "failed to close cached ingredients", "location", p.String(), "error", err)
		}
	}()
	dec := json.NewDecoder(ingredientblob)
	var ingredients []kroger.Ingredient
	err = dec.Decode(&ingredients)
	if err != nil {
		http.Error(w, "failed to decode ingredients", http.StatusInternalServerError)
		return
	}
	// make this a html thats readable.
	w.Header().Add("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(ingredients); err != nil {
		http.Error(w, "failed to encode ingredients", http.StatusInternalServerError)
		return
	}
}
