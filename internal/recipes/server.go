package recipes

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"careme/internal/ai"
	"careme/internal/auth"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/locations"
	"careme/internal/seasons"
	"careme/internal/templates"
	"careme/internal/users"
	utypes "careme/internal/users/types"

	"github.com/samber/lo"
)

func setTextContent(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
}

type locServer interface {
	GetLocationByID(ctx context.Context, locationID string) (*locations.Location, error)
}

type generator interface {
	GenerateRecipes(ctx context.Context, p *generatorParams) (*ai.ShoppingList, error)
	AskQuestion(ctx context.Context, question string, conversationID string) (string, error)
	PickAWine(ctx context.Context, conversationID string, location string, recipe ai.Recipe, date time.Time) (*ai.WineSelection, error)
	Ready(ctx context.Context) error
}

type server struct {
	recipeio
	cfg       *config.Config
	storage   *users.Storage
	cache     cache.Cache
	generator generator
	locServer locServer
	wg        sync.WaitGroup
	clerk     auth.AuthClient
}

// NewHandler returns an http.Handler serving the recipe endpoints under /recipes.
// cache must be connected to generator or this will not work. Should we enfroce that by getting cache from generator?
func NewHandler(cfg *config.Config, storage *users.Storage, generator generator, locServer locServer, c cache.Cache, clerkClient auth.AuthClient) *server {
	return &server{
		recipeio:  recipeio{Cache: c},
		cache:     c,
		cfg:       cfg,
		storage:   storage,
		generator: generator,
		locServer: locServer,
		clerk:     clerkClient,
	}
}

func (s *server) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /recipes", s.handleRecipes)
	mux.HandleFunc("POST /recipes/{hash}/regenerate", s.handleRegenerate)
	mux.HandleFunc("POST /recipes/{hash}/finalize", s.handleFinalize)
	mux.HandleFunc("GET /recipe/{hash}", s.handleSingle)
	mux.HandleFunc("POST /recipe/{hash}/question", s.handleQuestion)
	mux.HandleFunc("POST /recipe/{hash}/wine", s.handleWine)
	mux.HandleFunc("POST /recipe/{hash}/feedback", s.handleFeedback)
	mux.HandleFunc("POST /recipe/{hash}/save", s.handleSaveRecipe)
	mux.HandleFunc("POST /recipe/{hash}/dismiss", s.handleDismissRecipe)
}

func (s *server) handleSingle(w http.ResponseWriter, r *http.Request) {
	// This page has user-visible HTMX mutations (wine picks, feedback, Q&A).
	// If the browser restores it from history or an intermediary cache, the user can
	// see stale UI that no longer matches cache-backed state, so force a fresh GET.
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
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
	feedback := RecipeFeedback{}
	var thread []RecipeThreadEntry
	var wineRecommendation *ai.WineSelection
	var loadWG sync.WaitGroup
	loadWG.Add(3)
	go func() {
		defer loadWG.Done()
		existing, err := s.FeedbackFromCache(ctx, hash)
		if err != nil {
			if !errors.Is(err, cache.ErrNotFound) {
				slog.ErrorContext(ctx, "failed to load recipe feedback", "hash", hash, "error", err)
			}
			return
		}
		feedback = *existing
	}()
	go func() {
		defer loadWG.Done()
		existing, err := s.ThreadFromCache(ctx, hash)
		if err != nil {
			if !errors.Is(err, cache.ErrNotFound) {
				slog.ErrorContext(ctx, "failed to load recipe thread", "hash", hash, "error", err)
			}
			return
		}
		thread = existing
	}()
	go func() {
		defer loadWG.Done()
		selection, err := s.WineFromCache(ctx, hash)
		if err != nil {
			if !errors.Is(err, cache.ErrNotFound) {
				slog.ErrorContext(ctx, "failed to load cached wine recommendation", "hash", hash, "error", err)
			}
			return
		}
		wineRecommendation = selection
	}()
	loadWG.Wait()

	if recipe.OriginHash == "" {
		slog.WarnContext(ctx, "recipe missing origin hash Probably and old recipe", "hash", hash)
		p := DefaultParams(&locations.Location{
			ID:   "",
			Name: "Unknown Location",
		}, time.Now())
		FormatRecipeHTML(p, *recipe, signedIn, thread, feedback, wineRecommendation, w)
		return
	}
	// we didn't go back and update old recipes's  with new hash so have to handle that here. Could still backfill
	if normalizedHash, ok := legacyHashToCurrent(recipe.OriginHash, legacyRecipeHashSeed); ok {
		slog.InfoContext(ctx, "normalized legacy origin hash to current hash", "origin_hash", recipe.OriginHash, "hash", normalizedHash)
		recipe.OriginHash = normalizedHash
		// could resave to backfill but don't think we'll ever get them all without looping
	}
	p, err := s.ParamsFromCache(ctx, recipe.OriginHash)
	if err != nil {
		slog.ErrorContext(ctx, "failed to load params for hash", "hash", recipe.OriginHash, "error", err)
		// http.Error(w, "recipe not found or expired", http.StatusNotFound)
		// return
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

	slog.InfoContext(ctx, "serving shared recipe by hash", "hash", hash, "signedIn", signedIn)
	FormatRecipeHTML(p, *recipe, signedIn, thread, feedback, wineRecommendation, w)
}

func (s *server) handleQuestion(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !isHTMXRequest(r) {
		http.Error(w, "htmx request required", http.StatusBadRequest)
		return
	}
	hash := r.PathValue("hash")
	if hash == "" {
		http.Error(w, "missing recipe hash", http.StatusBadRequest)
		return
	}
	_, err := s.clerk.GetUserIDFromRequest(r)
	if errors.Is(err, auth.ErrNoSession) {
		w.Header().Set("HX-Redirect", "/sign-in")
		http.Error(w, "must be logged in to ask a question", http.StatusUnauthorized)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	question := strings.TrimSpace(r.FormValue("question"))
	if question == "" {
		http.Error(w, "missing question", http.StatusBadRequest)
		return
	}
	recipeTitle := strings.TrimSpace(r.FormValue("recipe_title"))
	questionForModel := question
	if recipeTitle != "" {
		questionForModel = fmt.Sprintf("Regarding %s: %s", recipeTitle, question)
	}

	// TODO: conversation id is user-provided form input.
	// Also still curious if we should fork conversation per recipe
	conversationID := strings.TrimSpace(r.FormValue("conversation_id"))
	if conversationID == "" {
		slog.ErrorContext(ctx, "failed to load conversation id", "hash", hash)
		http.Error(w, "conversation id not found", http.StatusInternalServerError)
		return
	}

	// this is going to take a while. Start a go routine? and spin?
	// can't use request context because it will be canceled when request finishes but we want to finish processing question and save it to cache.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), 45*time.Second)
	defer cancel()
	answer, err := s.generator.AskQuestion(ctx, questionForModel, conversationID)
	if err != nil {
		slog.ErrorContext(ctx, "failed to answer question", "hash", hash, "error", err)
		http.Error(w, "failed to answer question", http.StatusInternalServerError)
		return
	}

	thread, err := s.ThreadFromCache(ctx, hash)
	if err != nil && !errors.Is(err, cache.ErrNotFound) {
		slog.ErrorContext(ctx, "failed to load recipe thread", "hash", hash, "error", err)
		http.Error(w, "failed to load recipe thread", http.StatusInternalServerError)
		return
	}
	thread = append(thread, RecipeThreadEntry{
		Question:  question,
		Answer:    answer,
		CreatedAt: time.Now(),
	})
	if err := s.SaveThread(ctx, hash, thread); err != nil {
		http.Error(w, "failed to save question", http.StatusInternalServerError)
		return
	}

	FormatRecipeThreadHTML(thread, true, conversationID, w)
}

func (s *server) handleWine(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !isHTMXRequest(r) {
		http.Error(w, "htmx request required", http.StatusBadRequest)
		return
	}
	renderShoppingVariant := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("view")), "shopping")
	shoppingSlot := strings.TrimSpace(r.URL.Query().Get("slot"))
	hash := strings.TrimSpace(r.PathValue("hash"))
	if hash == "" {
		http.Error(w, "missing recipe hash", http.StatusBadRequest)
		return
	}
	if selection, err := s.WineFromCache(ctx, hash); err == nil {
		if selection == nil {
			http.Error(w, "failed to load wine recommendation", http.StatusInternalServerError)
			return
		}
		if renderShoppingVariant {
			FormatShoppingRecipeWineHTML(hash, shoppingSlot, selection, w)
		} else {
			FormatRecipeWineHTML(hash, selection, w)
		}
		return
	} else if !errors.Is(err, cache.ErrNotFound) {
		slog.ErrorContext(ctx, "failed to load cached wine recommendation", "hash", hash, "error", err)
	}

	recipe, err := s.SingleFromCache(ctx, hash)
	if err != nil {
		if errors.Is(err, cache.ErrNotFound) {
			http.Error(w, "recipe not found", http.StatusNotFound)
			return
		}
		slog.ErrorContext(ctx, "failed to load recipe for wine pick", "hash", hash, "error", err)
		http.Error(w, "failed to load recipe", http.StatusInternalServerError)
		return
	}

	p, err := s.ParamsFromCache(ctx, recipe.OriginHash)
	if err != nil {
		slog.ErrorContext(ctx, "failed to load params for wine pick", "hash", recipe.OriginHash, "error", err)
		http.Error(w, "failed to load recipe parameters", http.StatusInternalServerError)
		return
	}

	conversationID := strings.TrimSpace(loadConversationIDForRecipe(ctx, s.recipeio, recipe.OriginHash))
	if conversationID == "" {
		http.Error(w, "conversation id not found", http.StatusUnprocessableEntity)
		return
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 45*time.Second)
	defer cancel()
	selection, err := s.generator.PickAWine(ctx, conversationID, p.Location.ID, *recipe, p.Date)
	if err != nil {
		slog.ErrorContext(ctx, "failed to pick wine", "hash", hash, "conversation_id", conversationID, "error", err)
		http.Error(w, "failed to pick wine", http.StatusInternalServerError)
		return
	}
	if selection == nil {
		http.Error(w, "failed to pick wine", http.StatusInternalServerError)
		return
	}
	if err := s.SaveWine(ctx, hash, selection); err != nil {
		slog.ErrorContext(ctx, "failed to save wine recommendation", "hash", hash, "error", err)
	}

	if renderShoppingVariant {
		FormatShoppingRecipeWineHTML(hash, shoppingSlot, selection, w)
		return
	}
	FormatRecipeWineHTML(hash, selection, w)
}

func (s *server) handleFeedback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !isHTMXRequest(r) {
		http.Error(w, "htmx request required", http.StatusBadRequest)
		return
	}
	hash := r.PathValue("hash")
	if hash == "" {
		http.Error(w, "missing recipe hash", http.StatusBadRequest)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	feedback := RecipeFeedback{}
	existing, err := s.FeedbackFromCache(ctx, hash)
	if err != nil {
		if !errors.Is(err, cache.ErrNotFound) {
			slog.ErrorContext(ctx, "failed to load existing feedback", "hash", hash, "error", err)
			http.Error(w, "failed to load existing feedback", http.StatusInternalServerError)
			return
		}
	} else {
		feedback = *existing
	}

	changed := false
	if values, ok := r.PostForm["cooked"]; ok && len(values) > 0 {
		cooked, err := parseFeedbackBool(values[len(values)-1])
		if err != nil {
			http.Error(w, "invalid cooked value", http.StatusBadRequest)
			return
		}
		feedback.Cooked = cooked
		changed = true
	}
	if values, ok := r.PostForm["stars"]; ok && len(values) > 0 {
		starValue := strings.TrimSpace(values[len(values)-1])
		if starValue == "" {
			feedback.Stars = 0
		} else {
			stars, err := strconv.Atoi(starValue)
			if err != nil || stars < 1 || stars > 5 {
				http.Error(w, "stars must be between 1 and 5", http.StatusBadRequest)
				return
			}
			feedback.Stars = stars
		}
		changed = true
	}
	if values, ok := r.PostForm["feedback"]; ok && len(values) > 0 {
		feedback.Comment = strings.TrimSpace(values[len(values)-1])
		changed = true
	}
	if !changed {
		http.Error(w, "no feedback provided", http.StatusBadRequest)
		return
	}

	feedback.UpdatedAt = time.Now()
	if err := s.SaveFeedback(ctx, hash, feedback); err != nil {
		http.Error(w, "failed to save feedback", http.StatusInternalServerError)
		return
	}

	setTextContent(w)
	_, err = fmt.Fprint(w, `<span class="inline-flex items-center gap-1 text-sm font-medium text-green-700"><span aria-hidden="true">✓</span>Saved</span>`)
	if err != nil {
		slog.ErrorContext(ctx, "failed to write feedback response", "hash", hash, "error", err)
		http.Error(w, "failed to write response", http.StatusInternalServerError)
	}
}

func (s *server) handleSaveRecipe(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !isHTMXRequest(r) {
		http.Error(w, "htmx request required", http.StatusBadRequest)
		return
	}
	recipeHash := strings.TrimSpace(r.PathValue("hash"))
	if recipeHash == "" {
		http.Error(w, "missing recipe hash", http.StatusBadRequest)
		return
	}

	currentUser, err := s.storage.FromRequest(ctx, r, s.clerk)
	if err != nil {
		if errors.Is(err, auth.ErrNoSession) {
			w.Header().Set("HX-Redirect", "/sign-in")
			http.Error(w, "must be logged in to save recipes", http.StatusUnauthorized)
			return
		}
		slog.ErrorContext(ctx, "failed to load user for recipe save", "error", err)
		http.Error(w, "unable to load account", http.StatusInternalServerError)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	selectionHash := strings.TrimSpace(r.FormValue(queryArgHash))
	if selectionHash == "" {
		http.Error(w, "recipe list hash not found", http.StatusBadRequest)
		return
	}
	selection, err := s.loadRecipeSelection(ctx, currentUser.ID, selectionHash)
	if err != nil {
		slog.ErrorContext(ctx, "failed to load recipe selection for save", "user_id", currentUser.ID, "selection_hash", selectionHash, "error", err)
		http.Error(w, "failed to save recipe", http.StatusInternalServerError)
		return
	}
	selection.markSaved(recipeHash)
	if err := s.saveRecipeSelection(ctx, currentUser.ID, selectionHash, selection); err != nil {
		slog.ErrorContext(ctx, "failed to save recipe selection", "user_id", currentUser.ID, "selection_hash", selectionHash, "error", err)
		http.Error(w, "failed to save recipe", http.StatusInternalServerError)
		return
	}

	// could pass this in with htmx instead of loading title
	recipe, err := s.SingleFromCache(ctx, recipeHash)
	if err != nil {
		if errors.Is(err, cache.ErrNotFound) {
			http.Error(w, "recipe not found", http.StatusNotFound)
			return
		}
		slog.ErrorContext(ctx, "failed to load recipe for profile save", "hash", recipeHash, "error", err)
		http.Error(w, "failed to load recipe", http.StatusInternalServerError)
		return
	}

	if err := s.saveRecipesToUserProfile(ctx, currentUser, *recipe); err != nil {
		slog.ErrorContext(ctx, "failed to save recipe to user profile", "user_id", currentUser.ID, "hash", recipeHash, "error", err)
		http.Error(w, "failed to save recipe", http.StatusInternalServerError)
		return
	}

	p, err := s.paramsForAction(ctx, selectionHash, currentUser.ID, "")
	if err != nil {
		slog.ErrorContext(ctx, "failed to load params for save response", "user_id", currentUser.ID, "selection_hash", selectionHash, "error", err)
		http.Error(w, "failed to save recipe", http.StatusInternalServerError)
		return
	}

	var response bytes.Buffer
	if _, err := fmt.Fprint(&response, `<span class="text-xs font-medium text-action-green-700">Saved to kitchen</span>`); err != nil {
		slog.ErrorContext(ctx, "failed to build save response", "hash", recipeHash, "error", err)
		http.Error(w, "failed to write response", http.StatusInternalServerError)
		return
	}
	if err := RenderShoppingFinalizeControlsHTML(selectionHash, len(p.Saved) > 0, &response); err != nil {
		slog.ErrorContext(ctx, "failed to render finalize controls after save", "selection_hash", selectionHash, "error", err)
		http.Error(w, "failed to write response", http.StatusInternalServerError)
		return
	}

	setTextContent(w)
	_, err = w.Write(response.Bytes())
	if err != nil {
		slog.ErrorContext(ctx, "failed to write save response", "hash", recipeHash, "error", err)
		http.Error(w, "failed to write response", http.StatusInternalServerError)
	}
}

func (s *server) handleDismissRecipe(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !isHTMXRequest(r) {
		http.Error(w, "htmx request required", http.StatusBadRequest)
		return
	}
	recipeHash := strings.TrimSpace(r.PathValue("hash"))
	if recipeHash == "" {
		http.Error(w, "missing recipe hash", http.StatusBadRequest)
		return
	}

	currentUser, err := s.storage.FromRequest(ctx, r, s.clerk)
	if err != nil {
		if errors.Is(err, auth.ErrNoSession) {
			w.Header().Set("HX-Redirect", "/sign-in")
			http.Error(w, "must be logged in to dismiss recipes", http.StatusUnauthorized)
			return
		}
		slog.ErrorContext(ctx, "failed to load user for recipe dismiss", "error", err)
		http.Error(w, "unable to load account", http.StatusInternalServerError)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	selectionHash := strings.TrimSpace(r.FormValue(queryArgHash))
	if selectionHash == "" {
		http.Error(w, "recipe list hash not found", http.StatusBadRequest)
		return
	}
	selection, err := s.loadRecipeSelection(ctx, currentUser.ID, selectionHash)
	if err != nil {
		slog.ErrorContext(ctx, "failed to load recipe selection for dismiss", "user_id", currentUser.ID, "selection_hash", selectionHash, "error", err)
		http.Error(w, "failed to dismiss recipe", http.StatusInternalServerError)
		return
	}
	selection.markDismissed(recipeHash)
	if err := s.saveRecipeSelection(ctx, currentUser.ID, selectionHash, selection); err != nil {
		slog.ErrorContext(ctx, "failed to save recipe selection for dismiss", "user_id", currentUser.ID, "selection_hash", selectionHash, "error", err)
		http.Error(w, "failed to dismiss recipe", http.StatusInternalServerError)
		return
	}

	if err := s.removeRecipeFromUserProfile(ctx, *currentUser, recipeHash); err != nil {
		slog.ErrorContext(ctx, "failed to remove recipe from user profile", "user_id", currentUser.ID, "hash", recipeHash, "error", err)
		http.Error(w, "failed to dismiss recipe", http.StatusInternalServerError)
		return
	}

	p, err := s.paramsForAction(ctx, selectionHash, currentUser.ID, "")
	if err != nil {
		slog.ErrorContext(ctx, "failed to load params for dismiss response", "user_id", currentUser.ID, "selection_hash", selectionHash, "error", err)
		http.Error(w, "failed to dismiss recipe", http.StatusInternalServerError)
		return
	}

	var response bytes.Buffer
	if _, err := fmt.Fprint(&response, `<span class="text-xs font-medium text-action-red-700">Removed from kitchen</span>`); err != nil {
		slog.ErrorContext(ctx, "failed to build dismiss response", "hash", recipeHash, "error", err)
		http.Error(w, "failed to write response", http.StatusInternalServerError)
		return
	}
	if err := RenderShoppingFinalizeControlsHTML(selectionHash, len(p.Saved) > 0, &response); err != nil {
		slog.ErrorContext(ctx, "failed to render finalize controls after dismiss", "selection_hash", selectionHash, "error", err)
		http.Error(w, "failed to write response", http.StatusInternalServerError)
		return
	}

	setTextContent(w)
	_, err = w.Write(response.Bytes())
	if err != nil {
		slog.ErrorContext(ctx, "failed to write dismiss response", "hash", recipeHash, "error", err)
		http.Error(w, "failed to write response", http.StatusInternalServerError)
	}
}

func (s *server) handleRegenerate(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	hash := strings.TrimSpace(r.PathValue("hash"))
	if hash == "" {
		http.Error(w, "missing recipe hash", http.StatusBadRequest)
		return
	}

	currentUser, err := s.storage.FromRequest(ctx, r, s.clerk)
	if err != nil {
		if errors.Is(err, auth.ErrNoSession) {
			if isHTMXRequest(r) {
				w.Header().Set("HX-Redirect", "/sign-in")
			}
			http.Error(w, "must be logged in to regenerate recipes", http.StatusUnauthorized)
			return
		}
		http.Error(w, "unable to load account", http.StatusInternalServerError)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	p, err := s.paramsForAction(ctx, hash, currentUser.ID, strings.TrimSpace(r.FormValue("instructions")))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	newHash := p.Hash()

	if err := s.SaveParams(ctx, p); err != nil && !errors.Is(err, ErrAlreadyExists) {
		slog.ErrorContext(ctx, "failed to save params for regenerate", "hash", newHash, "error", err)
		http.Error(w, "failed to prepare regeneration", http.StatusInternalServerError)
		return
	}
	// so we have a choice we could save slection here matching params
	// or backfill it on first load after regeneration Backfilling is a little more resilient
	// selection := recipeSelectionFromParams(p)
	// if err := s.saveRecipeSelection(ctx, currentUser.ID, newHash, selection);
	s.kickgeneration(ctx, p, currentUser)

	redirectToHash(w, r, newHash, true /*useStart*/)
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
			if isHTMXRequest(r) {
				w.Header().Set("HX-Redirect", "/sign-in")
			}
			http.Error(w, "must be logged in to finalize recipes", http.StatusUnauthorized)
			return
		}
		http.Error(w, "unable to load account", http.StatusInternalServerError)
		return
	}

	p, err := s.paramsForAction(ctx, hash, userid, "")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
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
		Recipes:        p.Saved,
		ConversationID: p.ConversationID,
	}
	if err := s.SaveShoppingList(ctx, shoppingList, newHash); err != nil {
		slog.ErrorContext(ctx, "failed to save finalized shopping list", "hash", newHash, "error", err)
		http.Error(w, "failed to finalize recipes", http.StatusInternalServerError)
		return
	}

	redirectToHash(w, r, newHash, false /*useStart*/)
}

// paramsForAction merges selction, old params, and selection(saved/dismissed) into a new params
func (s *server) paramsForAction(ctx context.Context, hash, userID, instructions string) (*generatorParams, error) {
	baseParams, err := s.ParamsFromCache(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("failed to load recipe parameters")
	}
	currentList, err := s.FromCache(ctx, hash)
	if err != nil {
		return nil, fmt.Errorf("failed to load recipe list")
	}

	selection, err := s.loadRecipeSelection(ctx, userID, hash)
	if err != nil {
		// should we just fall back to params? selection saving
		return nil, fmt.Errorf("failed to load recipe selection")
	}

	params := *baseParams
	params.Instructions = instructions
	s.mergeParamsWithSelection(ctx, &params, selection, currentList.Recipes)
	if params.ConversationID == "" {
		params.ConversationID = currentList.ConversationID
	}
	return &params, nil
}

const (
	queryArgHash  = "h"
	queryArgStart = "start"
)

func (s *server) notFound(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	startArg := r.URL.Query().Get(queryArgStart)
	hashParam := r.URL.Query().Get(queryArgHash)
	// okay give them a new start time.
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
	// The shopping list page is mutated in-place via HTMX (save/dismiss/wine picks).
	// We disable browser/intermediary caching so Back/Forward revalidation fetches the
	// latest server-rendered state instead of restoring a stale DOM snapshot.
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
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
		wineRecommendations := make(map[string]*ai.WineSelection, len(slist.Recipes))
		var wineWG sync.WaitGroup
		var wineMu sync.Mutex
		wineWG.Add(len(slist.Recipes))
		for _, recipe := range slist.Recipes {
			recipeHash := recipe.ComputeHash()
			go func(recipeHash string) {
				defer wineWG.Done()
				wineRecommendation, wineErr := s.WineFromCache(ctx, recipeHash)
				if wineErr != nil {
					if !errors.Is(wineErr, cache.ErrNotFound) {
						slog.ErrorContext(ctx, "failed to load cached wine recommendation for shopping list render", "recipe_hash", recipeHash, "error", wineErr)
					}
					return
				}
				wineMu.Lock()
				wineRecommendations[recipeHash] = wineRecommendation
				wineMu.Unlock()
			}(recipeHash)
		}
		wineWG.Wait()
		FormatShoppingListHTMLForHash(p, *slist, wineRecommendations, signedIn, hashParam, w)
		return
	}

	p, err := s.ParseQueryArgs(ctx, r)
	if err != nil {
		http.Error(w, fmt.Sprintf("invalid query parameters: %v", err), http.StatusBadRequest)
		return
	}
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

	p.Directive = currentUser.Directive
	p.UserID = currentUser.ID
	// if params are already saved redirect and assume someone kicks off genration

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

func (s *server) kickgeneration(ctx context.Context, p *generatorParams, currentUser *utypes.User) {
	for _, last := range currentUser.LastRecipes {
		if last.CreatedAt.Before(time.Now().AddDate(0, 0, -14)) {
			break
		}
		p.LastRecipes = append(p.LastRecipes, last.Title)
	}

	hash := p.Hash()

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		// copy over request id to new context? can't be same context because end of http request will cancel it.
		ctx := context.WithoutCancel(ctx)
		slog.InfoContext(ctx, "generating cached recipes", "params", p.String(), "hash", hash)
		shoppingList, err := s.generator.GenerateRecipes(ctx, p)
		if err != nil {
			slog.ErrorContext(ctx, "generate error", "error", err)
			return
		}

		// add saved recipes here rather than each

		if err := s.SaveShoppingList(ctx, shoppingList, hash); err != nil {
			slog.ErrorContext(ctx, "save error", "error", err)
			return
		}
	}()
}

func (s *server) Spin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	ctx := r.Context()
	spinnerData := struct {
		ClarityScript   template.HTML
		GoogleTagScript template.HTML
		Style           seasons.Style
		RefreshInterval string // seconds
	}{
		ClarityScript:   templates.ClarityScript(),
		GoogleTagScript: templates.GoogleTagScript(),
		Style:           seasons.GetCurrentStyle(),
		RefreshInterval: "10", // seconds
	}

	if err := templates.Spin.Execute(w, spinnerData); err != nil {
		slog.ErrorContext(ctx, "home template execute error", "error", err)
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

func redirectToHash(w http.ResponseWriter, r *http.Request, hash string, useStart bool) {
	u := url.URL{Path: "/recipes"}
	args := url.Values{} // intentioanlly clear other args
	args.Set(queryArgHash, hash)
	if useStart {
		args.Set(queryArgStart, time.Now().Format(time.RFC3339Nano))
	}
	u.RawQuery = args.Encode()
	if isHTMXRequest(r) {
		w.Header().Set("HX-Redirect", u.String())
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, u.String(), http.StatusSeeOther)
}

func isHTMXRequest(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("HX-Request"), "true")
}

func loadConversationIDForRecipe(ctx context.Context, rio recipeio, originHash string) string {
	originHash = strings.TrimSpace(originHash)
	if originHash == "" {
		return ""
	}
	if normalizedHash, ok := legacyHashToCurrent(originHash, legacyRecipeHashSeed); ok {
		originHash = normalizedHash
	}
	if p, err := rio.ParamsFromCache(ctx, originHash); err == nil {
		if conversationID := strings.TrimSpace(p.ConversationID); conversationID != "" {
			return conversationID
		}
	} else if !errors.Is(err, cache.ErrNotFound) {
		slog.ErrorContext(ctx, "failed to load recipe params for conversation", "hash", originHash, "error", err)
	}

	if slist, err := rio.FromCache(ctx, originHash); err == nil {
		return strings.TrimSpace(slist.ConversationID)
	} else if !errors.Is(err, cache.ErrNotFound) {
		slog.ErrorContext(ctx, "failed to load shopping list for conversation", "hash", originHash, "error", err)
	}
	return ""
}

func parseFeedbackBool(value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "on", "yes":
		return true, nil
	case "", "0", "false", "off", "no":
		return false, nil
	default:
		return false, fmt.Errorf("invalid boolean: %q", value)
	}
}

func (s *server) Wait() {
	s.wg.Wait()
}

// saveRecipesToUserProfile adds saved recipes to the user's profile
func (s *server) saveRecipesToUserProfile(ctx context.Context, currentUser *utypes.User, recipe ai.Recipe) error {
	if currentUser == nil {
		return fmt.Errorf("invalid user")
	}

	// Check if reciProfilepe already exists in user's last recipes
	hash := recipe.ComputeHash()

	_, exists := lo.Find(currentUser.LastRecipes, func(r utypes.Recipe) bool {
		return r.Hash == hash
	})
	if exists {
		return nil
	}
	newRecipe := utypes.Recipe{
		Title:     recipe.Title,
		Hash:      hash,
		CreatedAt: time.Now(),
	}
	currentUser.LastRecipes = append(currentUser.LastRecipes, newRecipe)

	// etag mismatch fun!
	if err := s.storage.Update(currentUser); err != nil {
		return fmt.Errorf("failed to update user with saved recipes: %w", err)
	}
	slog.InfoContext(ctx, "added saved recipe to user profile", "user_id", currentUser.ID, "title", recipe.Title)

	return nil
}

func (s *server) removeRecipeFromUserProfile(ctx context.Context, currentUser utypes.User, recipeHash string) error {
	recipeHash = strings.TrimSpace(recipeHash)
	if recipeHash == "" {
		return fmt.Errorf("invalid recipe hash")
	}

	before := len(currentUser.LastRecipes)
	currentUser.LastRecipes = lo.Filter(currentUser.LastRecipes, func(r utypes.Recipe, _ int) bool {
		return r.Hash != recipeHash
	})

	if len(currentUser.LastRecipes) == before {
		return nil
	}

	if err := s.storage.Update(&currentUser); err != nil {
		return fmt.Errorf("failed to update user when dismissing recipe: %w", err)
	}
	slog.InfoContext(ctx, "removed recipe from user profile", "user_id", currentUser.ID, "hash", recipeHash)
	return nil
}
