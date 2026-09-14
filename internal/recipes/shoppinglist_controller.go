package recipes

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"careme/internal/seasons"

	"careme/internal/ai"
	"careme/internal/auth"
	"careme/internal/cache"
	"careme/internal/guest"
	"careme/internal/httpx"
	"careme/internal/locations"
	"careme/internal/parallelism"
	"careme/internal/recipes/status"
	"careme/internal/templates"
	utypes "careme/internal/users/types"

	"github.com/samber/lo"
)

func (s *server) handleRegenerate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	hash := strings.TrimSpace(r.PathValue("hash"))
	if hash == "" {
		http.Error(w, "missing recipe hash", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	instructions := strings.TrimSpace(r.FormValue(queryArgInstructions))

	currentUser, err := s.storage.FromRequest(ctx, r, s.clerk)
	if err != nil {
		if errors.Is(err, auth.ErrNoSession) {
			if !guest.UseShoppingList(w, r) {
				redirectToAccountRequired(
					w,
					r,
					auth.AccountRequiredGenerationLimit,
					shoppingListArgs(map[string]string{
						queryArgHash:         hash,
						queryArgInstructions: instructions,
					}),
				)
				return
			}
			currentUser = guestUser
		} else {
			http.Error(w, "unable to loadRecipeRegenerationJob account", http.StatusInternalServerError)
			return
		}
	}

	p, err := paramsForAction(ctx, hash, currentUser.ID, instructions, s.recipeio)
	if err != nil {
		slog.ErrorContext(ctx, "failed to start recipe regeneration", "hash", hash, "error", err)
		http.Error(w, "failed to prepare regeneration", http.StatusInternalServerError)
		return
	}
	if len(p.Dismissed) == 0 {
		currentList, err := s.FromCache(ctx, hash)
		if err != nil {
			slog.ErrorContext(ctx, "failed to load recipe list for regeneration", "hash", hash, "error", err)
			http.Error(w, "failed to prepare regeneration", http.StatusInternalServerError)
			return
		}
		p.Dismissed = recipesNotSaved(currentList.Recipes, p.Saved)
	}
	newHash := p.Hash()

	if err := s.SaveParams(ctx, p); err != nil && !errors.Is(err, ErrAlreadyExists) {
		slog.ErrorContext(ctx, "failed to save params for regeneration", "hash", newHash, "error", err)
		http.Error(w, "failed to prepare regeneration", http.StatusInternalServerError)
		return
	}
	p.LastRecipes = s.recentCookedTitles(ctx, currentUser.LastRecipes)
	if err := s.kickgeneration(ctx, p, currentUser.ID); err != nil {
		slog.ErrorContext(ctx, "failed to start recipe regeneration", "hash", newHash, "error", err)
		http.Error(w, "failed to start recipe regeneration", http.StatusInternalServerError)
		return
	}
	redirectToHashWithConversion(w, r, newHash, templates.RecipeGenerationConversion)
}

func shoppingListArgs(args map[string]string) string {
	values := url.Values{}
	for k, v := range args {
		if k != "" && v != "" {
			values.Set(k, v)
		}
	}
	return "/recipes?" + values.Encode()
}

func recipesNotSaved(recipes []ai.Recipe, saved []ai.Recipe) []ai.Recipe {
	savedByHash := make(map[string]struct{}, len(saved))
	for _, recipe := range saved {
		savedByHash[recipe.ComputeHash()] = struct{}{}
	}
	return lo.Filter(recipes, func(recipe ai.Recipe, _ int) bool {
		_, ok := savedByHash[recipe.ComputeHash()]
		return !ok
	})
}

func (s *server) handleFinalize(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	hash := strings.TrimSpace(r.PathValue("hash"))
	if hash == "" {
		http.Error(w, "missing recipe hash", http.StatusBadRequest)
		return
	}

	userid, err := s.clerk.GetUserIDFromRequest(r)
	if err != nil {
		if errors.Is(err, auth.ErrNoSession) {
			redirectToSignIn(w, r, http.StatusUnauthorized)
			return
		}
		http.Error(w, "unable to load account", http.StatusInternalServerError)
		return
	}

	p, err := paramsForAction(ctx, hash, userid, "", s.recipeio)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	currentList, err := s.FromCache(ctx, hash)
	if err != nil {
		slog.ErrorContext(ctx, "failed to load shopping list for finalize", "hash", hash, "error", err)
		http.Error(w, "failed to finalize recipes", http.StatusInternalServerError)
		return
	}
	if len(p.Saved) == 0 {
		// ui does not allow this
		slog.ErrorContext(ctx, "Got zero saved recipes finalize", "hash", hash)
		http.Error(w, "no recipes selected to save", http.StatusBadRequest)
		return
	}

	newHash := p.Hash()
	if err := s.SaveParams(ctx, p); err != nil && !errors.Is(err, ErrAlreadyExists) {
		slog.ErrorContext(ctx, "failed to save params for finalize", "hash", newHash, "error", err)
		http.Error(w, "failed to finalize recipes", http.StatusInternalServerError)
		return
	}

	shoppingList := &ai.ShoppingList{
		Recipes: p.Saved,
		Plan:    currentList.Plan,
	}
	if err := s.SaveShoppingList(ctx, shoppingList, newHash); err != nil {
		slog.ErrorContext(ctx, "failed to save finalized shopping list", "hash", newHash, "error", err)
		http.Error(w, "failed to finalize recipes", http.StatusInternalServerError)
		return
	}
	if err := s.recordShoppingListForUser(userid, newHash, p.Location); err != nil {
		slog.ErrorContext(ctx, "failed to remember finalized shopping list", "user_id", userid, "hash", newHash, "error", err)
		http.Error(w, "failed to finalize recipes", http.StatusInternalServerError)
		return
	}

	redirectToHash(w, r, newHash)
}

// paramsForAction merges old params saved recipes with current saved/dismissed selection into new params.
func paramsForAction(ctx context.Context, hash, userID, instructions string, io recipeio) (*generatorParams, error) {
	baseParams, err := io.ParamsFromCache(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("failed to load recipe parameters")
	}
	// good place to fetch meal plan? except we want to kill paramsForAction?
	currentList, err := io.FromCache(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("failed to load recipe list")
	}

	selection, err := io.loadRecipeSelection(ctx, userID, hash)
	if err != nil {
		// should we just fall back to params? selection saving
		return nil, fmt.Errorf("failed to load recipe selection")
	}

	params := *baseParams
	params.Instructions = instructions
	params.PriorSavedHashes = lo.Map(baseParams.Saved, func(r ai.Recipe, _ int) string { return r.ComputeHash() })
	if currentList.Plan != nil {
		params.PreviousMenuPlanResponseID = currentList.Plan.ResponseID
		params.PreviousMenuPlanPromptCacheKey = currentList.Plan.PromptCacheKey
	}
	originalSelection := selectionFromSaved(baseParams.Saved)
	selection = originalSelection.override(selection)
	all := append(params.Saved, params.Dismissed...)
	all = append(all, currentList.Recipes...)
	localRecipes := lo.SliceToMap(all,
		func(r ai.Recipe) (string, *ai.Recipe) {
			return r.ComputeHash(), &r
		})

	params.Saved = make([]ai.Recipe, 0, len(selection.SavedHashes))
	for _, hash := range selection.SavedHashes {
		r, found := localRecipes[hash]
		if !found {
			slog.ErrorContext(ctx, "missing hash while creating new params", "hash", hash)
			return nil, fmt.Errorf("missing hash while creating new params %s", hash)
		}
		params.Saved = append(params.Saved, *r)

	}
	params.Dismissed = make([]ai.Recipe, 0, len(selection.DismissedHashes))
	for _, hash := range selection.DismissedHashes {
		r, found := localRecipes[hash]
		if !found {
			slog.ErrorContext(ctx, "missing hash while creating new params", "hash", hash)
			return nil, fmt.Errorf("missing hash while creating new params %s", hash)
		}
		params.Dismissed = append(params.Dismissed, *r)
	}

	return &params, nil
}

const (
	queryArgHash         = "h"
	queryArgConversion   = "conversion"
	queryArgInstructions = "instructions"
	// QueryArgHelp carries campaign-specific shopping list help text through redirects.
	QueryArgHelp = "help"
)

// notFound renders the shopping list while recipes are still being generated.
func (s *server) notFound(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	hash := r.URL.Query().Get(queryArgHash)
	p, err := s.ParamsFromCache(ctx, hash)
	if err != nil {
		if errors.Is(err, cache.ErrNotFound) {
			// Random or expired hashes can reach this public endpoint without indicating an app bug.
			slog.InfoContext(ctx, "failed to load params for hash", "hash", hash, "error", err)
			http.Error(w, "shoppinglist not found", http.StatusNotFound)
			return
		}
		slog.ErrorContext(ctx, "failed to load params", "hash", hash, "error", err)
		http.Error(w, "failed to load recipe parameters", http.StatusInternalServerError)
		return
	}
	progress, err := s.generationStatuses.Load(ctx, hash)
	if err != nil {
		if errors.Is(err, cache.ErrNotFound) {
			progress.Failed = "Recipe generation did not start."
		} else {
			slog.ErrorContext(ctx, "failed to load generation progress", "hash", hash, "error", err)
			// A failed poll leaves existing cards intact; the next poll retries.
			if isShoppingPoll(r) {
				http.Error(w, "Unable to check progress. Please try again.", http.StatusServiceUnavailable)
				return
			}
		}
	}
	if progress.Failed != "" {
		if isShoppingPoll(r) {
			w.Header().Set("HX-Redirect", r.URL.RequestURI())
			w.WriteHeader(http.StatusOK)
			return
		}
		retryURL := url.URL{Path: "/recipes/" + url.PathEscape(hash) + "/retry"}
		renderGenerationRetry(ctx, w, r, retryURL.String(), progress.Failed)
		return
	}
	list := ai.ShoppingList{Recipes: p.Saved}
	finished := make(map[string]ai.Recipe, len(progress.Slots))
	// TODO parallize
	for _, slot := range progress.Slots {
		if slot.RecipeHash == "" {
			continue
		}
		recipe, err := s.SingleFromCache(ctx, slot.RecipeHash)
		if err != nil {
			http.Error(w, "failed to load ready recipe", http.StatusInternalServerError)
			return
		}
		finished[slot.RecipeHash] = *recipe
	}
	currentUser, err := s.storage.FromRequest(ctx, r, s.clerk)
	if err != nil && !errors.Is(err, auth.ErrNoSession) {
		http.Error(w, "unable to load account", http.StatusInternalServerError)
		return
	}

	s.renderShoppingList(w, r, p, &list, currentUser, shoppingProgress{
		StatusMessage: progress.Message,
		Slots:         progress.Slots,
		Finished:      finished,
		Generating:    true,
		Fragment:      isShoppingPoll(r),
	})
}

func isShoppingPoll(r *http.Request) bool {
	return httpx.IsHTMX(r) && r.Header.Get("HX-Target") == "shopping-content"
}

var guestUser = &utypes.User{ID: "00000000", Email: []string{"guest@careme.cooking"}}

func (s *server) handleRecipes(w http.ResponseWriter, r *http.Request) {
	// The shopping list page is mutated in-place via HTMX (save/dismiss/wine picks).
	// We disable browser/intermediary caching so Back/Forward revalidation fetches the
	// latest server-rendered state instead of restoring a stale DOM snapshot.
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	ctx := r.Context()

	currentUser, err := s.storage.FromRequest(ctx, r, s.clerk)
	if err != nil && !errors.Is(err, auth.ErrNoSession) {
		slog.ErrorContext(ctx, "failed to get user for recipe redirect", "error", err)
		http.Error(w, "unable to load account", http.StatusInternalServerError)
		return
	}

	hashParam := strings.TrimSpace(r.URL.Query().Get(queryArgHash))
	if hashParam == "" {
		// FormValue also reads URL query parameters, so links such as
		// /recipes?location=<id> can be redirected to their canonical hash URL.
		p, err := ParseGenerationForm(ctx, r, s.locServer)
		if err != nil {
			http.Error(w, fmt.Sprintf("invalid query parameters: %v", err), http.StatusBadRequest)
			return
		}

		if currentUser != nil {
			p.Directive = currentUser.Directive
		}
		redirectToHash(w, r, p.Hash(), QueryArgHelp)
		return
	}
	// TODO(pm): Revisit route shape for hash-based recipe lists. `h` is a derived key from
	// query params, so `/recipes?h=...` is defensible; decide later if we also want a
	// canonical path form like `/recipes/{h}` or just a redirect alias.
	if normalizedHash, ok := legacyHashToCurrent(hashParam, legacyRecipeHashSeed); ok {
		slog.InfoContext(ctx, "redirecting legacy hash to canonical hash", "legacy_hash", hashParam, "hash", normalizedHash)
		redirectToHash(w, r, normalizedHash, QueryArgHelp)
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

	p, err := s.ParamsFromCache(ctx, hashParam)
	if err != nil {
		slog.ErrorContext(ctx, "failed to load params for hash", "hash", hashParam, "error", err)
		http.Error(w, "failed to load recipe parameters", http.StatusInternalServerError)
		return
	}

	// The generation spinner polls this handler into #spin-page-work. Once the
	// recipes are ready, replace the spinner document with a real page load rather
	// than nesting the complete shopping-list document inside that element. The
	// latter leaves the spinner body's overflow-hidden class in place and prevents
	// the completed page from scrolling on mobile browsers.
	if httpx.IsHTMX(r) && strings.EqualFold(strings.TrimSpace(r.Header.Get("HX-Target")), "spin-page-work") {
		w.Header().Set("HX-Redirect", httpx.RequestPath(r))
		w.WriteHeader(http.StatusOK)
		return
	}

	s.renderShoppingList(w, r, p, slist, currentUser, shoppingProgress{Fragment: isShoppingPoll(r)})
}

func (s *server) renderShoppingList(w http.ResponseWriter, r *http.Request, p *generatorParams, slist *ai.ShoppingList, currentUser *utypes.User, progress shoppingProgress) {
	ctx := r.Context()
	hashParam := r.URL.Query().Get(queryArgHash)
	signedIn := currentUser != nil
	selection := selectionFromSaved(p.Saved)
	if signedIn {
		userSelection, err := s.loadRecipeSelection(ctx, currentUser.ID, hashParam)
		if err != nil {
			slog.ErrorContext(ctx, "failed to load recipe selection for render", "hash", hashParam, "error", err)
			http.Error(w, "failed to load recipe selection", http.StatusInternalServerError)
			return
		}
		selection = selection.override(userSelection)
	}
	if !signedIn && !progress.Generating {
		guest.EnsureShoppingListCount(w, r)
	}
	finishedRecipes := make(map[string]ai.Recipe, len(slist.Recipes)+len(progress.Finished))
	for hash, recipe := range progress.Finished {
		finishedRecipes[hash] = recipe
	}
	for _, recipe := range slist.Recipes {
		finishedRecipes[recipe.ComputeHash()] = recipe
	}
	wines := parallelism.NewSafeMap[string, *ai.WineSelection](len(slist.Recipes))
	images := parallelism.NewSafeMap[string, bool](len(slist.Recipes))
	var recipeWG sync.WaitGroup
	for recipeHash := range finishedRecipes {
		recipeWG.Go(func() {
			wineRecommendation, wineErr := s.WineFromCache(ctx, recipeHash)
			if wineErr != nil {
				if !errors.Is(wineErr, cache.ErrNotFound) {
					slog.ErrorContext(ctx, "failed to load cached wine recommendation for shopping list render", "recipe_hash", recipeHash, "error", wineErr)
				}
				return
			}
			wines.Set(recipeHash, wineRecommendation)
		})
		recipeWG.Go(func() {
			hasImage := s.recipeImageExistsForCard(ctx, recipeHash)
			images.Set(recipeHash, hasImage)
		})

	}
	recipeWG.Wait()

	help := r.URL.Query().Get(QueryArgHelp)
	instructions := strings.TrimSpace(r.URL.Query().Get(queryArgInstructions))
	writeShoppingListPage(ctx, w, shoppingListViewInput{
		params:              p,
		list:                *slist,
		wineRecommendations: wines.Clone(),
		recipeImages:        images.Clone(),
		currentUser:         currentUser,
		hash:                hashParam,
		selection:           selection,
		helpMessage:         help,
		pendingInstructions: instructions,
		progress:            progress,
	})
}

func (s *server) handleGenerate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	p, err := ParseGenerationForm(ctx, r, s.locServer)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid form parameters: %v", err), http.StatusBadRequest)
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
		if _, cacheErr := s.FromCache(ctx, p.Hash()); cacheErr == nil {
			redirectToHash(w, r, p.Hash(), QueryArgHelp)
			return
		}
		if !guest.UseShoppingList(w, r) {
			slog.InfoContext(ctx, "blocking guest recipe generation", "user_agent", r.UserAgent())
			redirectToAccountRequired(
				w,
				r,
				auth.AccountRequiredGenerationLimit,
				httpx.LocalReferrerPath(r),
			)
			return
		}
		// be careful. Formalize this more?
		currentUser = guestUser
	}

	s.setFavoriteStore(ctx, currentUser, p.Location)

	p.Directive = currentUser.Directive
	p.LastRecipes = s.recentCookedTitles(ctx, currentUser.LastRecipes)
	if err := s.SaveParams(ctx, p); err != nil {
		if errors.Is(err, ErrAlreadyExists) {
			// Another request with these content-addressed params owns the generation.
			// Redirecting lets this user poll for that shared result; only the owner
			// records it in their recent shopping lists when generation completes.
			slog.InfoContext(ctx, "params already existed redirecting", "hash", p.Hash())
			redirectToHash(w, r, p.Hash(), QueryArgHelp)
			return
		}
		slog.ErrorContext(ctx, "failed to save params", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	hash := p.Hash()

	if err := s.kickgeneration(ctx, p, currentUser.ID); err != nil {
		slog.ErrorContext(ctx, "failed to start recipe regeneration", "hash", hash, "error", err)
		http.Error(w, "failed to start recipe regeneration", http.StatusInternalServerError)
		return
	}
	redirectToHashWithConversion(w, r, hash, templates.RecipeGenerationConversion)
}

func (s *server) handleRetryGeneration(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	hash := strings.TrimSpace(r.PathValue("hash"))

	// Retry intentionally does not require a current session: it can only reuse
	// cached parameters from a generation request that already passed the
	// signed-in or guest-generation allowance check.
	if _, err := s.FromCache(ctx, hash); err == nil {
		redirectToHash(w, r, hash, QueryArgHelp)
		return
	} else if !errors.Is(err, cache.ErrNotFound) {
		slog.ErrorContext(ctx, "failed to check recipe list before retry", "hash", hash, "error", err)
		http.Error(w, "failed to retry recipe generation", http.StatusInternalServerError)
		return
	}

	userID, err := s.clerk.GetUserIDFromRequest(r)
	if err != nil {
		if !errors.Is(err, auth.ErrNoSession) {
			slog.ErrorContext(ctx, "failed to identify account for recipe generation retry", "error", err)
			http.Error(w, "unable to load account", http.StatusInternalServerError)
			return
		}
		userID = guestUser.ID
	}

	p, err := s.ParamsFromCache(ctx, hash)
	if err != nil {
		if errors.Is(err, cache.ErrNotFound) {
			http.Error(w, "shoppinglist not found or expired", http.StatusNotFound)
			return
		}
		slog.ErrorContext(ctx, "failed to load params for recipe retry", "hash", hash, "error", err)
		http.Error(w, "failed to retry recipe generation", http.StatusInternalServerError)
		return
	}

	if err := s.kickgeneration(ctx, p, userID); err != nil {
		slog.ErrorContext(ctx, "failed to start recipe regeneration", "hash", hash, "error", err)
		http.Error(w, "failed to start recipe regeneration", http.StatusInternalServerError)
		return
	}
	redirectToHashWithConversion(w, r, hash, templates.RecipeGenerationConversion)
}

// best effort attempt to set favorite store if non is thre
func (s *server) setFavoriteStore(ctx context.Context, currentUser *utypes.User, loc *locations.Location) {
	if strings.TrimSpace(currentUser.FavoriteStore) != "" {
		return
	}
	if currentUser.ID == guestUser.ID {
		return
	}

	currentUser.FavoriteStore = strings.TrimSpace(loc.ID)
	if err := s.storage.Update(currentUser); err != nil {
		slog.ErrorContext(ctx, "failed to set favorite store from generated recipes location", "location_id", currentUser.FavoriteStore, "error", err)
		return
	}
	slog.InfoContext(ctx, "set favorite store from recipe generation", "user_id", currentUser.ID, "location_id", currentUser.FavoriteStore)
}

func (s *server) recentCookedTitles(ctx context.Context, lastRecipes []utypes.Recipe) []string {
	recent := lo.Filter(lastRecipes, func(r utypes.Recipe, _ int) bool {
		// magic number of days. Also should we include non feedback ones in shorter window
		return r.CreatedAt.After(time.Now().AddDate(0, 0, -14))
	})
	hashes := make([]string, 0, len(recent))
	for _, recipe := range recent {
		hashes = append(hashes, recipe.Hash)
	}

	// just checking exist enough?
	cooked := s.FeedbackByHash(ctx, hashes)

	return lo.FilterMap(recent, func(r utypes.Recipe, _ int) (string, bool) {
		return r.Title, cooked[r.Hash].Cooked
	})
}

func (s *server) kickgeneration(ctx context.Context, p *generatorParams, userID string) error {
	hash := p.Hash()
	if err := s.generationStatuses.Start(ctx, hash, status.InitialMessage); err != nil {
		return fmt.Errorf("start generation status %w", err)
	}
	ctx = context.WithoutCancel(ctx)
	s.wg.Go(func() {
		slog.InfoContext(ctx, "generating cached recipes", "params", p.String(), "hash", hash)
		shoppingList, err := s.generator.GenerateRecipes(ctx, p)
		if err != nil {
			slog.ErrorContext(ctx, "generate error", "error", err)
			if statusErr := s.generationStatuses.Fail(ctx, hash, err); statusErr != nil {
				slog.ErrorContext(ctx, "failed to record recipe generation failure", "hash", hash, "error", statusErr)
			}
			return
		}

		if err := s.SaveShoppingList(ctx, shoppingList, hash); err != nil {
			slog.ErrorContext(ctx, "save error", "error", err)
			if statusErr := s.generationStatuses.Fail(ctx, hash, err); statusErr != nil {
				slog.ErrorContext(ctx, "failed to record shopping list save failure", "hash", hash, "error", statusErr)
			}
			return
		}
		if err := s.recordShoppingListForUser(userID, hash, p.Location); err != nil {
			slog.ErrorContext(ctx, "failed to remember generated shopping list", "user_id", userID, "hash", hash, "error", err)
			if statusErr := s.generationStatuses.Fail(ctx, hash, err); statusErr != nil {
				slog.ErrorContext(ctx, "failed to record shopping list history failure", "hash", hash, "error", statusErr)
			}
			return
		}
	})
	return nil
}

func (s *server) recordShoppingListForUser(userID, hash string, location *locations.Location) error {
	if userID == guestUser.ID {
		return nil
	}
	if strings.TrimSpace(userID) == "" {
		return fmt.Errorf("remember shopping list: user ID is required")
	}
	if location == nil {
		return fmt.Errorf("remember shopping list: location is required")
	}

	// TODO: Use storage ETags to compare-and-swap and retry this entire
	// read-modify-write operation so concurrent user updates are not lost.
	currentUser, err := s.storage.GetByID(userID)
	if err != nil {
		return fmt.Errorf("remember shopping list: %w", err)
	}
	currentUser.ShoppingLists = append(currentUser.ShoppingLists, utypes.ShoppingList{
		Hash:        hash,
		Name:        location.Name,
		CompletedAt: time.Now(),
	})
	if err := s.storage.Update(currentUser); err != nil {
		return fmt.Errorf("remember shopping list: %w", err)
	}
	return nil
}

func shoppingListIsOlderThanFreshIngredientsWindow(ctx context.Context, p *generatorParams) bool {
	today, err := locations.StoreToDate(ctx, nowFn(), p.Location)
	if err != nil {
		return false
	}
	return today.Sub(p.Date) > 24*time.Hour
}

func writeShoppingListPage(ctx context.Context, w http.ResponseWriter, input shoppingListViewInput) {
	input.clarityScript = templates.ClarityScript(ctx)
	input.googleTagScript = templates.GoogleTagScript()
	input.style = seasons.GetCurrentStyle()
	input.useTodaysIngredients = shoppingListIsOlderThanFreshIngredientsWindow(ctx, input.params)
	name := "shoppinglist.html"
	if input.progress.Fragment {
		name = "shopping_content"
	}
	writeHTMLResponse(w, func(writer io.Writer) error {
		view, err := newShoppingListPageView(input)
		if err != nil {
			return err
		}
		return renderShoppingListPage(writer, name, view)
	})
}
