package recipes

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"careme/internal/seasons"
	"careme/internal/templates"

	"careme/internal/ai"
	"careme/internal/auth"
	"careme/internal/cache"
	"careme/internal/httpx"
	"careme/internal/locations"
	"careme/internal/recipes/feedback"
	"careme/internal/recipes/status"
	utypes "careme/internal/users/types"
)

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
	currentUser, err := s.storage.FromRequest(ctx, r, s.clerk)
	if err != nil && !errors.Is(err, auth.ErrNoSession) {
		slog.ErrorContext(ctx, "failed to get user from request", "error", err)
		http.Error(w, "unable to load account", http.StatusInternalServerError)
		return
	}
	signedIn := currentUser != nil
	var recipeCritique *ai.RecipeCritique
	feedback := feedback.Feedback{}
	var thread []RecipeThreadEntry
	var wineRecommendation *ai.WineSelection
	var hasRecipeImage bool
	var loadWG sync.WaitGroup
	loadWG.Go(func() {
		existing, err := s.FeedbackFromCache(ctx, hash)
		if err != nil {
			if !errors.Is(err, cache.ErrNotFound) {
				slog.ErrorContext(ctx, "failed to load recipe feedback", "hash", hash, "error", err)
			}
			return
		}
		feedback = *existing
	})
	loadWG.Go(func() {
		existing, err := s.ThreadFromCache(ctx, hash)
		if err != nil {
			if !errors.Is(err, cache.ErrNotFound) {
				slog.ErrorContext(ctx, "failed to load recipe thread", "hash", hash, "error", err)
			}
			return
		}
		thread = existing
	})
	loadWG.Go(func() {
		selection, err := s.WineFromCache(ctx, hash)
		if err != nil {
			if !errors.Is(err, cache.ErrNotFound) {
				slog.ErrorContext(ctx, "failed to load cached wine recommendation", "hash", hash, "error", err)
			}
			return
		}
		wineRecommendation = selection
	})
	loadWG.Go(func() {
		exists, err := s.images.Exists(ctx, hash)
		if err != nil {
			slog.ErrorContext(ctx, "failed to check cached recipe image", "hash", hash, "error", err)
			return
		}
		hasRecipeImage = exists
	})
	loadWG.Go(func() {
		result, err := s.critiques.Load(ctx, hash)
		if err != nil {
			if !errors.Is(err, cache.ErrNotFound) {
				slog.ErrorContext(ctx, "failed to load recipe critique", "hash", hash, "error", err)
			}
			return
		}
		recipeCritique = result
	})
	loadWG.Wait()

	if recipe.OriginHash == "" {
		// Would like to make this an error however this in album is missing a origin hash and its too pretty to break
		// https://careme.cooking/recipe/mQs4oIYMJoqCmqDMXv74bA==
		if hash == "mQs4oIYMJoqCmqDMXv74bA==" {
			slog.InfoContext(ctx, "recipe missing origin hash Probably and old recipe", "hash", hash)
			p := DefaultParams(&locations.Location{
				ID:   "",
				Name: "Unknown Location",
			}, time.Now())
			writeRecipePage(ctx, w, recipeViewInput{
				params:             p,
				recipe:             *recipe,
				saved:              false,
				currentUser:        currentUser,
				recipeCritique:     recipeCritique,
				hasRecipeImage:     hasRecipeImage,
				thread:             thread,
				feedback:           feedback,
				wineRecommendation: wineRecommendation,
			})
			return
		}
		slog.ErrorContext(ctx, "No origin hash for recipe", "hash", hash, "error", err)
		http.Error(w, "no orginin hash", http.StatusInternalServerError)
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
		slog.ErrorContext(ctx, "failed to load params for hash", "origin hash", recipe.OriginHash, "hash", hash, "error", err)
		http.Error(w, "recipe's origin shpppinglist not found or expired", http.StatusInternalServerError)
		return
	}
	saved := false
	if signedIn {
		// this is going to be slow once we paginate recipes...
		saved = slices.ContainsFunc(currentUser.LastRecipes, func(r utypes.Recipe) bool {
			return r.Hash == hash
		})
	}

	slog.InfoContext(ctx, "serving recipe by hash", "hash", hash, "signedIn", signedIn)
	writeRecipePage(ctx, w, recipeViewInput{
		params:             p,
		recipe:             *recipe,
		saved:              saved,
		currentUser:        currentUser,
		recipeCritique:     recipeCritique,
		hasRecipeImage:     hasRecipeImage,
		thread:             thread,
		feedback:           feedback,
		wineRecommendation: wineRecommendation,
	})
}

func (s *server) handleRecipeImage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	hash := strings.TrimSpace(r.PathValue("hash"))
	if hash == "" {
		http.Error(w, "missing recipe hash", http.StatusBadRequest)
		return
	}

	imageBody, err := s.images.FromCache(ctx, hash)
	if err != nil {
		if errors.Is(err, cache.ErrNotFound) {
			http.Error(w, "recipe image not found", http.StatusNotFound)
			return
		}
		slog.ErrorContext(ctx, "failed to load cached recipe image", "hash", hash, "error", err)
		http.Error(w, "failed to load recipe image", http.StatusInternalServerError)
		return
	}
	defer func() {
		if err := imageBody.Close(); err != nil {
			slog.ErrorContext(ctx, "failed to close cached recipe image", "hash", hash, "error", err)
		}
	}()

	imageReader := bufio.NewReader(imageBody)
	header, err := imageReader.Peek(512)
	if err != nil && !errors.Is(err, bufio.ErrBufferFull) && !errors.Is(err, io.EOF) {
		slog.ErrorContext(ctx, "failed to sniff cached recipe image", "hash", hash, "error", err)
		http.Error(w, "failed to load recipe image", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", http.DetectContentType(header))
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	if _, err := io.Copy(w, imageReader); err != nil {
		if ctx.Err() != nil {
			slog.DebugContext(ctx, "image stream canceled", "hash", hash, "ctxErr", ctx.Err(), "error", err)
			return
		}
		slog.ErrorContext(ctx, "failed to stream cached recipe image", "hash", hash, "error", err)
	}
}

func (s *server) handleQuestion(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !httpx.IsHTMX(r) {
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
		redirectToSignIn(w, r, http.StatusUnauthorized)
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
		// we could drop this after first question
		questionForModel = fmt.Sprintf("Regarding %s: %s", recipeTitle, question)
	}

	responseID := strings.TrimSpace(r.FormValue("response_id"))
	promptCacheKey := strings.TrimSpace(r.FormValue("prompt_cache_key"))

	// this is going to take a while. Start a go routine? and spin?
	// can't use request context because it will be canceled when request finishes but we want to finish processing question and save it to cache.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 45*time.Second)
	defer cancel()
	previous := ai.ResponseRef{ID: responseID, PromptCacheKey: promptCacheKey}
	answer, err := s.generator.AskQuestion(ctx, questionForModel, previous)
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
		Question:   question,
		Answer:     answer.Answer,
		ResponseID: answer.ResponseID,
		CreatedAt:  time.Now(),
	})
	if err := s.SaveThread(ctx, hash, thread); err != nil {
		http.Error(w, "failed to save question", http.StatusInternalServerError)
		return
	}

	writeRecipeThread(w, newRecipeThreadView(thread, true, ai.ResponseRef{
		ID:             answer.ResponseID,
		PromptCacheKey: promptCacheKey,
	}, hash))
}

func (s *server) handleRegenerateSingleRecipe(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	hash := strings.TrimSpace(r.PathValue("hash"))
	if hash == "" {
		http.Error(w, "missing recipe hash", http.StatusBadRequest)
		return
	}

	var (
		currentUser   *utypes.User
		recipe        *ai.Recipe
		thread        []RecipeThreadEntry
		userErr       error
		recipeErr     error
		threadErr     error
		critiqueFixes []string
		loadWG        sync.WaitGroup
	)
	loadWG.Go(func() {
		currentUser, userErr = s.storage.FromRequest(ctx, r, s.clerk)
	})
	loadWG.Go(func() {
		recipe, recipeErr = s.SingleFromCache(ctx, hash)
	})
	loadWG.Go(func() {
		thread, threadErr = s.ThreadFromCache(ctx, hash)
	})
	loadWG.Go(func() {
		c, err := s.critiques.Load(ctx, hash)
		if err != nil {
			if !errors.Is(err, cache.ErrNotFound) {
				slog.ErrorContext(ctx, "failed to load recipe critique", "hash", hash, "error", err)
			}
			return
		}
		critiqueFixes = c.SuggestedFixes
	})
	loadWG.Wait()

	if userErr != nil {
		if errors.Is(userErr, auth.ErrNoSession) {
			redirectToSignIn(w, r, http.StatusUnauthorized)
			return
		}
		slog.ErrorContext(ctx, "failed to load user for recipe regeneration", "hash", hash, "error", userErr)
		http.Error(w, "unable to load account", http.StatusInternalServerError)
		return
	}
	if recipeErr != nil {
		if errors.Is(recipeErr, cache.ErrNotFound) {
			http.Error(w, "recipe not found", http.StatusNotFound)
			return
		}
		slog.ErrorContext(ctx, "failed to load recipe for regeneration", "hash", hash, "error", recipeErr)
		http.Error(w, "failed to load recipe", http.StatusInternalServerError)
		return
	}
	if threadErr != nil {
		if errors.Is(threadErr, cache.ErrNotFound) {
			http.Error(w, "ask a question before refreshing this recipe", http.StatusBadRequest)
			return
		}
		slog.ErrorContext(ctx, "failed to load recipe thread for regeneration", "hash", hash, "error", threadErr)
		http.Error(w, "failed to load recipe questions", http.StatusInternalServerError)
		return
	}
	responseID := latestThreadResponseID(thread)
	if responseID == "" {
		// should never get here
		http.Error(w, "ask a question before refreshing this recipe", http.StatusBadRequest)
		return
	}

	instructions := singleRecipeRegenerationInstructions(critiqueFixes)
	previous := ai.ResponseRef{ID: responseID, PromptCacheKey: recipe.PromptCacheKey}
	id := status.ID(hash, responseID)

	status, err := s.generationStatuses.Load(ctx, id)
	if err == nil {
		// Running and completed jobs already have a polling URL. Failed jobs
		// fall through so Start resets their status for the explicit retry.
		if status.Failed == "" {
			redirectToRecipeRegeneration(w, r, hash, id)
			return
		}
	} else if !errors.Is(err, cache.ErrNotFound) {
		slog.ErrorContext(ctx, "failed to load recipe regeneration job", "hash", hash, "job_id", id, "error", err)
		http.Error(w, "failed to prepare recipe refresh", http.StatusInternalServerError)
		return
	}

	err = s.generationStatuses.Start(ctx, id, "") // put thread questin or critique here?
	if err != nil {
		if errors.Is(err, cache.ErrAlreadyExists) {
			redirectToRecipeRegeneration(w, r, hash, id)
			return
		}
		slog.ErrorContext(ctx, "failed to create recipe regeneration job", "hash", hash, "response_id", responseID, "error", err)
		http.Error(w, "failed to prepare recipe refresh", http.StatusInternalServerError)
		return
	}

	s.kickSingleRecipeRegeneration(ctx, id, currentUser, *recipe, instructions, previous)

	redirectToRecipeRegeneration(w, r, hash, id)
}

func singleRecipeRegenerationInstructions(critiqueFixes []string) []string {
	instructions := []string{"Rewrite the recipe to incorporate the user's question thread and your answers. Return a complete updated recipe."}
	if len(critiqueFixes) > 0 {
		instructions = append(instructions, "also incorporate these critique fixes")
		instructions = append(instructions, critiqueFixes...)
	}
	return instructions
}

func (s *server) handleSingleRecipeRegeneration(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	hash := strings.TrimSpace(r.PathValue("hash"))
	jobID := strings.TrimSpace(r.PathValue("jobID"))
	if hash == "" || jobID == "" {
		http.Error(w, "missing recipe regeneration", http.StatusBadRequest)
		return
	}
	if !status.IsValidID(jobID) {
		http.Error(w, "recipe regeneration not found", http.StatusNotFound)
		return
	}

	payload, err := s.generationStatuses.Load(ctx, jobID)
	if err != nil {
		if errors.Is(err, cache.ErrNotFound) {
			http.Error(w, "recipe regeneration not found", http.StatusNotFound)
			return
		}
		slog.ErrorContext(ctx, "failed to load recipe regeneration job", "hash", hash, "job_id", jobID, "error", err)
		http.Error(w, "failed to load recipe refresh", http.StatusInternalServerError)
		return
	}

	if payload.Redirect != "" {
		redirectToRecipe(w, r, payload.Redirect)
		return
	}
	if payload.Failed != "" {
		s.renderRecipeRegenerationRetry(ctx, w, r, hash)
		return
	}

	spin(ctx, w, r, payload.Message)
}

func (s *server) handleFeedback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !httpx.IsHTMX(r) {
		http.Error(w, "htmx request required", http.StatusBadRequest)
		return
	}
	hash := r.PathValue("hash")
	if hash == "" {
		http.Error(w, "missing recipe hash", http.StatusBadRequest)
		return
	}
	if _, err := s.clerk.GetUserIDFromRequest(r); errors.Is(err, auth.ErrNoSession) {
		redirectToSignIn(w, r, http.StatusUnauthorized)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	feedback := feedback.Feedback{}
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

	httpx.SetHTMLContentType(w)
	_, err = fmt.Fprint(w, `<span class="inline-flex items-center gap-1 text-sm font-medium text-green-700"><span aria-hidden="true">✓</span>Saved</span>`)
	if err != nil {
		slog.ErrorContext(ctx, "failed to write feedback response", "hash", hash, "error", err)
		http.Error(w, "failed to write response", http.StatusInternalServerError)
	}
}

func (s *server) kickSingleRecipeRegeneration(ctx context.Context, id string, currentUser *utypes.User, recipe ai.Recipe, instructions []string, previous ai.ResponseRef) {
	s.wg.Go(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 90*time.Second)
		defer cancel()

		replacement, err := s.generator.RegenerateRecipe(ctx, instructions, previous)
		if err != nil {
			slog.ErrorContext(ctx, "failed generation", "job", id, "error", err)
			return
		}
		// TODO generate a new shoppinglist? only if ingredients changed or user asked?
		replacement.OriginHash = recipe.OriginHash
		oldHash := recipe.ComputeHash()
		replacement.ParentHash = oldHash
		newHash := replacement.ComputeHash()
		if err := s.SaveRecipe(ctx, *replacement); err != nil {
			slog.ErrorContext(ctx, "failed to save", "job", id, "error", err)
			return
		}
		replaced, err := s.storage.ReplaceRecipe(currentUser, oldHash, utypes.Recipe{
			Title:     replacement.Title,
			Hash:      newHash,
			CreatedAt: time.Now(),
		})
		if err != nil {
			slog.ErrorContext(ctx, "failed to replace", "job", id, "error", err)
			return
		}
		if replaced {
			if params, err := s.ParamsFromCache(ctx, recipe.OriginHash); err != nil {
				slog.ErrorContext(ctx, "couldn't look up params", "hash", newHash, "origin", recipe.OriginHash)
			} else {
				s.startSavedRecipeBackgroundGeneration(ctx, newHash, *replacement, params.Location.ID, params.Date)
			}
		}
		if err := s.generationStatuses.Complete(ctx, id, newHash); err != nil {
			slog.ErrorContext(ctx, "failed to complete recipe regeneration job", "job_id", id, "new_hash", newHash, "error", err)
		}
	})
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

func writeRecipePage(ctx context.Context, w http.ResponseWriter, input recipeViewInput) {
	input.clarityScript = templates.ClarityScript(ctx)
	input.googleTagScript = templates.GoogleTagScript()
	input.style = seasons.GetCurrentStyle()
	writeHTMLResponse(w, func(writer io.Writer) error {
		view, err := newRecipePageView(input)
		if err != nil {
			return err
		}
		return renderRecipePage(writer, view)
	})
}

func writeRecipeThread(w http.ResponseWriter, view recipeThreadView) {
	writeHTMLResponse(w, func(writer io.Writer) error { return renderRecipeThread(writer, view) })
}
