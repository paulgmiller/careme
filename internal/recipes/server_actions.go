package recipes

import (
	"careme/internal/ai"
	"careme/internal/auth"
	"careme/internal/cache"
	utypes "careme/internal/users/types"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/samber/lo"
)

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

	//this is going to take a while. Start a go routine? and spin?
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
	selection.MarkSaved(recipeHash)
	if err := s.saveRecipeSelection(ctx, currentUser.ID, selectionHash, selection); err != nil {
		slog.ErrorContext(ctx, "failed to save recipe selection", "user_id", currentUser.ID, "selection_hash", selectionHash, "error", err)
		http.Error(w, "failed to save recipe", http.StatusInternalServerError)
		return
	}

	//could pass this in with htmx instead of loading title
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

	setTextContent(w)
	_, err = fmt.Fprint(w, `<span class="text-xs font-medium text-action-green-700">Saved to kitchen</span>`)
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
	selection.MarkDismissed(recipeHash)
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

	setTextContent(w)
	_, err = fmt.Fprint(w, `<span class="text-xs font-medium text-action-red-700">Removed from kitchen</span>`)
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
	//so we have a choice we could save slection here matching params
	// or backfill it on first load after regeneration Backfilling is a little more resilient
	//selection := recipeSelectionFromParams(p)
	//if err := s.saveRecipeSelection(ctx, currentUser.ID, newHash, selection);
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
		//ui should ideally not allow us to get here
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
		//should we just fall back to params? selection saving
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
