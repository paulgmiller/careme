package recipes

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"careme/internal/ai"
	"careme/internal/auth"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/guest"
	"careme/internal/httpx"
	"careme/internal/locations"
	"careme/internal/parallelism"
	"careme/internal/recipes/critique"
	"careme/internal/recipes/feedback"
	recipestatus "careme/internal/recipes/status"
	"careme/internal/routing"
	"careme/internal/seasons"
	"careme/internal/templates"
	"careme/internal/users"

	utypes "careme/internal/users/types"

	"github.com/samber/lo"
)

func signInPath(returnTo string) string {
	returnTo = strings.TrimSpace(returnTo)
	if returnTo == "" {
		return "/sign-in"
	}
	// We base64-url encode the full relative target so nested query strings survive
	// Clerk's redirect_url handoff without splitting into separate top-level params.
	return "/sign-in?return_to_b64=" + url.QueryEscape(base64.RawURLEncoding.EncodeToString([]byte(returnTo)))
}

func redirectToSignIn(w http.ResponseWriter, r *http.Request, status int) {
	target := signInPath(httpx.RequestPath(r))
	if httpx.IsHTMX(r) {
		w.Header().Set("HX-Redirect", target)
	}
	http.Error(w, "must be logged in", status)
}

func redirectToAccountRequired(w http.ResponseWriter, r *http.Request, reason auth.AccountRequiredReason, returnTo string) {
	target := auth.AccountRequiredPath(reason, returnTo)
	if httpx.IsHTMX(r) {
		w.Header().Set("HX-Redirect", target)
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

type locServer interface {
	GetLocationByID(ctx context.Context, locationID string) (*locations.Location, error)
}

type generator interface {
	GenerateRecipes(ctx context.Context, p *generatorParams) (*ai.ShoppingList, error)
	RegenerateRecipe(ctx context.Context, instructions []string, previous ai.ResponseRef) (*ai.Recipe, error)
	AskQuestion(ctx context.Context, question string, previous ai.ResponseRef) (*ai.QuestionResponse, error)
	PickAWine(ctx context.Context, location string, recipe ai.Recipe, date time.Time) (*ai.WineSelection, error)
}

type ExtGenerator = generator

// should probably be in ai package?
type ImageGen interface {
	GenerateRecipeImage(ctx context.Context, recipe ai.Recipe) (*ai.GeneratedImage, error)
}

type server struct {
	recipeio
	imageio
	imagegen     ImageGen
	statusReader statusReader
	statusWriter statusWriter
	cfg          *config.Config
	storage      *users.Storage
	generator    generator
	locServer    locServer
	wg           sync.WaitGroup
	clerk        auth.AuthClient
	critiques    critiqueStore
}

type critiqueStore interface {
	Load(ctx context.Context, hash string) (*ai.RecipeCritique, error)
}

// NewHandler returns an http.Handler serving the recipe endpoints under /recipes.
// cache must be connected to generator or this will not work. Should we enfroce that by getting cache from generator?
func NewHandler(cfg *config.Config, storage *users.Storage, generator generator, locServer locServer, c cache.ListCache, imageCache cache.Cache, clerkClient auth.AuthClient, imagegen ImageGen) *server {
	statusStore := StatusStore(c)
	return &server{
		recipeio:     IO(c),
		imageio:      imageio{Cache: imageCache},
		imagegen:     imagegen,
		statusReader: statusStore,
		statusWriter: statusStore,
		cfg:          cfg,
		storage:      storage,
		generator:    generator,
		locServer:    locServer,
		clerk:        clerkClient,
		critiques:    critique.NewStore(c),
	}
}

func (s *server) Register(mux routing.Registrar) {
	mux.HandleFunc("GET /recipes", s.handleRecipes)
	mux.HandleFunc("POST /recipes", s.handleGenerate)
	mux.HandleFunc("POST /recipes/{hash}/retry", s.handleRetryGeneration)
	mux.HandleFunc("POST /recipes/{hash}/regenerate", s.handleRegenerate)
	mux.HandleFunc("POST /recipes/{hash}/finalize", s.handleFinalize)
	mux.HandleFunc("GET /recipe/{hash}", s.handleSingle)
	mux.HandleFunc("GET /recipe/{hash}/image", s.handleRecipeImage)
	mux.HandleFunc("POST /recipe/{hash}/question", s.handleQuestion)
	mux.HandleFunc("POST /recipe/{hash}/regenerate", s.handleRegenerateSingleRecipe)
	mux.HandleFunc("GET /recipe/{hash}/regen/{jobID}", s.handleSingleRecipeRegeneration)
	mux.HandleFunc("POST /recipe/{hash}/regen/{jobID}/retry", s.handleRetrySingleRecipeRegeneration)
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
	currentUser, err := s.storage.FromRequest(ctx, r, s.clerk)
	if err != nil && !errors.Is(err, auth.ErrNoSession) {
		slog.ErrorContext(ctx, "failed to get user from request", "error", err)
		http.Error(w, "unable to load account", http.StatusInternalServerError)
		return
	}
	signedIn := currentUser != nil
	var critiqueScore *int
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
		exists, err := s.RecipeImageExists(ctx, hash)
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
		score := result.OverallScore
		critiqueScore = &score
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
			FormatRecipeHTML(ctx, p, *recipe, false, currentUser, critiqueScore, hasRecipeImage, thread, feedback, wineRecommendation, w)
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
	FormatRecipeHTML(ctx, p, *recipe, saved, currentUser, critiqueScore, hasRecipeImage, thread, feedback, wineRecommendation, w)
}

func (s *server) handleRecipeImage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	hash := strings.TrimSpace(r.PathValue("hash"))
	if hash == "" {
		http.Error(w, "missing recipe hash", http.StatusBadRequest)
		return
	}

	imageBody, err := s.RecipeImageFromCache(ctx, hash)
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

	FormatRecipeThreadHTML(thread, true, ai.ResponseRef{
		ID:             answer.ResponseID,
		PromptCacheKey: promptCacheKey,
	}, hash, w)
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
	id := recipeRegenerationJobID(hash, responseID)
	err := s.startRecipeRegenerationJob(ctx, id, cache.IfNoneMatch())
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

	job, err := s.loadRecipeRegenerationJob(ctx, jobID)
	if err != nil {
		if errors.Is(err, cache.ErrNotFound) {
			http.Error(w, "recipe regeneration not found", http.StatusNotFound)
			return
		}
		slog.ErrorContext(ctx, "failed to load recipe regeneration job", "hash", hash, "job_id", jobID, "error", err)
		http.Error(w, "failed to load recipe refresh", http.StatusInternalServerError)
		return
	}

	switch job.State {
	case recipeRegenerationComplete:
		if strings.TrimSpace(job.NewHash) == "" {
			http.Error(w, "recipe regeneration has no result", http.StatusInternalServerError)
			return
		}
		redirectToRecipe(w, r, job.NewHash)
	case recipeRegenerationRunning:
		if generationWaitExpired(job.UpdatedAt) {
			s.renderRecipeRegenerationRetry(ctx, w, r, hash, jobID)
			return
		}
		s.spin(ctx, w, r, hash)
	default:
		http.Error(w, "invalid recipe regeneration state", http.StatusInternalServerError)
	}
}

func (s *server) handleRetrySingleRecipeRegeneration(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	hash := strings.TrimSpace(r.PathValue("hash"))
	jobID := strings.TrimSpace(r.PathValue("jobID"))
	job, err := s.loadRecipeRegenerationJob(ctx, jobID)
	if err != nil {
		if err != nil && !errors.Is(err, cache.ErrNotFound) {
			slog.ErrorContext(ctx, "failed to load recipe regeneration retry", "hash", hash, "job_id", jobID, "error", err)
		}
		http.Error(w, "recipe regeneration not found", http.StatusNotFound)
		return
	}
	if job.State == recipeRegenerationComplete && strings.TrimSpace(job.NewHash) != "" {
		redirectToRecipe(w, r, job.NewHash)
		return
	}
	if job.State == recipeRegenerationRunning && !generationWaitExpired(job.UpdatedAt) {
		redirectToRecipeRegeneration(w, r, hash, jobID)
		return
	}

	currentUser, err := s.storage.FromRequest(ctx, r, s.clerk)
	if err != nil {
		if errors.Is(err, auth.ErrNoSession) {
			redirectToSignIn(w, r, http.StatusUnauthorized)
			return
		}
		slog.ErrorContext(ctx, "failed to load user for recipe regeneration retry", "hash", hash, "job_id", jobID, "error", err)
		http.Error(w, "unable to load account", http.StatusInternalServerError)
		return
	}
	recipe, err := s.SingleFromCache(ctx, hash)
	if err != nil {
		if errors.Is(err, cache.ErrNotFound) {
			http.Error(w, "recipe not found", http.StatusNotFound)
			return
		}
		slog.ErrorContext(ctx, "failed to load recipe for regeneration retry", "hash", hash, "job_id", jobID, "error", err)
		http.Error(w, "failed to load recipe", http.StatusInternalServerError)
		return
	}
	thread, err := s.ThreadFromCache(ctx, hash)
	if err != nil {
		if errors.Is(err, cache.ErrNotFound) {
			http.Error(w, "ask a question before refreshing this recipe", http.StatusBadRequest)
			return
		}
		slog.ErrorContext(ctx, "failed to load recipe thread for regeneration retry", "hash", hash, "job_id", jobID, "error", err)
		http.Error(w, "failed to load recipe questions", http.StatusInternalServerError)
		return
	}
	responseID := latestThreadResponseID(thread)
	if responseID == "" {
		http.Error(w, "ask a question before refreshing this recipe", http.StatusBadRequest)
		return
	}

	var critiqueFixes []string
	if c, critiqueErr := s.critiques.Load(ctx, hash); critiqueErr == nil {
		critiqueFixes = c.SuggestedFixes
	} else if !errors.Is(critiqueErr, cache.ErrNotFound) {
		slog.ErrorContext(ctx, "failed to load recipe critique for retry", "hash", hash, "job_id", jobID, "error", critiqueErr)
	}
	instructions := singleRecipeRegenerationInstructions(critiqueFixes)
	previous := ai.ResponseRef{ID: responseID, PromptCacheKey: recipe.PromptCacheKey}
	err = s.startRecipeRegenerationJob(ctx, jobID, cache.Unconditional())
	if err != nil {
		slog.ErrorContext(ctx, "failed to create recipe regeneration retry", "hash", hash, "job_id", jobID, "error", err)
		http.Error(w, "failed to retry recipe refresh", http.StatusInternalServerError)
		return
	}
	s.kickSingleRecipeRegeneration(ctx, jobID, currentUser, *recipe, instructions, previous)
	redirectToRecipeRegeneration(w, r, hash, jobID)
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

func (s *server) handleSaveRecipe(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !httpx.IsHTMX(r) {
		http.Error(w, "htmx request required", http.StatusBadRequest)
		return
	}
	recipeHash := strings.TrimSpace(r.PathValue("hash"))
	if recipeHash == "" {
		http.Error(w, "missing recipe hash", http.StatusBadRequest)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}

	shoppingListHash := strings.TrimSpace(r.FormValue(queryArgHash))
	if shoppingListHash == "" {
		http.Error(w, "recipe list hash not found", http.StatusBadRequest)
		return
	}
	currentUser, err := s.storage.FromRequest(ctx, r, s.clerk)
	if err != nil {
		if errors.Is(err, auth.ErrNoSession) {
			returnTo := shoppingListArgs(map[string]string{
				queryArgHash: shoppingListHash,
			})
			redirectToAccountRequired(w, r, auth.AccountRequiredAddRecipe, returnTo)
			return
		}
		slog.ErrorContext(ctx, "failed to load user for recipe save", "error", err)
		http.Error(w, "unable to load account", http.StatusInternalServerError)
		return
	}
	recipe, err := s.saveRecipeForUser(ctx, currentUser, shoppingListHash, recipeHash)
	if err != nil {
		if errors.Is(err, cache.ErrNotFound) {
			http.Error(w, "recipe not found", http.StatusNotFound)
			return
		}
		slog.ErrorContext(ctx, "failed to save recipe", "shoppingListHash", shoppingListHash, "recipe_hash", recipeHash, "error", err)
		http.Error(w, "failed to save recipe", http.StatusInternalServerError)
		return
	}

	if err := s.writeRecipeSelectionResponse(ctx, w, r, recipeHash, *recipe, shoppingListHash, true); err != nil {
		slog.ErrorContext(ctx, "failed to render save response", "hash", recipeHash, "error", err)
		http.Error(w, "failed to write response", http.StatusInternalServerError)
	}
}

func (s *server) saveRecipeForUser(ctx context.Context, currentUser *utypes.User, shoppingListHash, recipeHash string) (*ai.Recipe, error) {
	selection, err := s.loadRecipeSelection(ctx, currentUser.ID, shoppingListHash)
	if err != nil {
		return nil, fmt.Errorf("load recipe selection: %w", err)
	}
	selection.markSaved(recipeHash)
	if err := s.saveRecipeSelection(ctx, currentUser.ID, shoppingListHash, selection); err != nil {
		return nil, fmt.Errorf("save recipe selection: %w", err)
	}

	recipe, err := s.SingleFromCache(ctx, recipeHash)
	if err != nil {
		return nil, fmt.Errorf("load recipe: %w", err)
	}
	if err := s.saveRecipesToUserProfile(ctx, currentUser, *recipe); err != nil {
		return nil, fmt.Errorf("save recipe to user profile: %w", err)
	}

	params, err := s.ParamsFromCache(ctx, shoppingListHash)
	if err != nil {
		return nil, fmt.Errorf("load recipe params: %w", err)
	}
	s.startSavedRecipeBackgroundGeneration(ctx, recipeHash, *recipe, params.Location.ID, params.Date)

	return recipe, nil
}

func (s *server) handleDismissRecipe(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if !httpx.IsHTMX(r) {
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
			redirectToSignIn(w, r, http.StatusUnauthorized)
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
		slog.ErrorContext(ctx, "failed to load recipe selection for dismiss", "selection_hash", selectionHash, "error", err)
		http.Error(w, "failed to dismiss recipe", http.StatusInternalServerError)
		return
	}
	selection.markDismissed(recipeHash)
	if err := s.saveRecipeSelection(ctx, currentUser.ID, selectionHash, selection); err != nil {
		slog.ErrorContext(ctx, "failed to save recipe selection for dismiss", "selection_hash", selectionHash, "error", err)
		http.Error(w, "failed to dismiss recipe", http.StatusInternalServerError)
		return
	}

	if _, err := s.storage.RemoveRecipe(currentUser, recipeHash); err != nil {
		slog.ErrorContext(ctx, "failed to remove recipe from storage", "hash", recipeHash, "error", err)
		http.Error(w, "failed to dismiss recipe", http.StatusInternalServerError)
		return
	}

	recipe, recipeErr := s.SingleFromCache(ctx, recipeHash)
	if recipeErr != nil {
		if errors.Is(recipeErr, cache.ErrNotFound) {
			http.Error(w, "recipe not found", http.StatusNotFound)
			return
		}
		slog.ErrorContext(ctx, "failed to load recipe for dismiss response", "hash", recipeHash, "error", recipeErr)
		http.Error(w, "failed to dismiss recipe", http.StatusInternalServerError)
		return
	}
	if err := s.writeRecipeSelectionResponse(ctx, w, r, recipeHash, *recipe, selectionHash, false); err != nil {
		slog.ErrorContext(ctx, "failed to render dismiss response", "hash", recipeHash, "error", err)
		http.Error(w, "failed to write response", http.StatusInternalServerError)
	}
}

func (s *server) writeRecipeSelectionResponse(ctx context.Context, w http.ResponseWriter, r *http.Request, recipeHash string, recipe ai.Recipe, shoppingListHash string, saved bool) error {
	var response bytes.Buffer
	if isSingleRecipeAction(r) {
		if err := RenderRecipeSaveActionHTML(recipe, shoppingListHash, saved, &response); err != nil {
			return fmt.Errorf("render recipe save action: %w", err)
		}
	} else {
		if err := RenderShoppingRecipeCardHTML(
			recipe,
			saved,
			shoppingListHash,
			s.wineRecommendationForCard(ctx, recipeHash),
			s.recipeImageExistsForCard(ctx, recipeHash),
			&response,
		); err != nil {
			return fmt.Errorf("render shopping recipe card: %w", err)
		}
		if err := RenderShoppingFinalizeControlsHTML(shoppingListHash, &response); err != nil {
			return fmt.Errorf("render shopping finalize controls: %w", err)
		}
	}

	httpx.SetHTMLContentType(w)
	if saved {
		w.Header().Set("HX-Trigger", `{"careme:saved-recipes-changed":{},"careme:recipe-saved":{}}`)
	}
	if _, err := w.Write(response.Bytes()); err != nil {
		return fmt.Errorf("write recipe selection response: %w", err)
	}
	return nil
}

func (s *server) wineRecommendationForCard(ctx context.Context, recipeHash string) *ai.WineSelection {
	wineRecommendation, err := s.WineFromCache(ctx, recipeHash)
	if err != nil {
		if !errors.Is(err, cache.ErrNotFound) {
			slog.ErrorContext(ctx, "failed to load cached wine recommendation for recipe card render", "recipe_hash", recipeHash, "error", err)
		}
		return nil
	}
	return wineRecommendation
}

func (s *server) recipeImageExistsForCard(ctx context.Context, recipeHash string) bool {
	exists, err := s.RecipeImageExists(ctx, recipeHash)
	if err != nil {
		slog.ErrorContext(ctx, "failed to check cached recipe image for recipe card render", "recipe_hash", recipeHash, "error", err)
		return false
	}
	return exists
}

func (s *server) startSavedRecipeBackgroundGeneration(ctx context.Context, recipeHash string, recipe ai.Recipe, locationID string, date time.Time) {
	s.wg.Go(func() {
		bgctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
		defer cancel()
		s.ensureSavedRecipeWine(bgctx, recipeHash, locationID, recipe, date)
	})
	s.wg.Go(func() {
		s.ensureRecipeImage(ctx, recipeHash, recipe)
	})
}

func (s *server) ensureSavedRecipeWine(ctx context.Context, recipeHash, locationID string, recipe ai.Recipe, date time.Time) {
	exists, err := s.WineExists(ctx, recipeHash)
	if err != nil {
		slog.ErrorContext(ctx, "failed to check cached wine selection after save", "hash", recipeHash, "error", err)
		return
	}
	if exists {
		return
	}
	slog.InfoContext(ctx, "generating wine picks on save", "hash", recipeHash)
	selection, err := s.generator.PickAWine(ctx, locationID, recipe, date)
	if err != nil {
		slog.ErrorContext(ctx, "failed to pick wine after save", "hash", recipeHash, "error", err)
		return
	}
	if err := s.SaveWine(ctx, recipeHash, selection); err != nil {
		slog.ErrorContext(ctx, "failed to save wine recommendation after save", "hash", recipeHash, "error", err)
	}
}

func (s *server) ensureRecipeImage(ctx context.Context, recipeHash string, recipe ai.Recipe) {
	// 4 minutes is a magical number here. neeed to look at data.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 4*time.Minute)
	defer cancel()

	exists, err := s.RecipeImageExists(ctx, recipeHash)
	if err != nil {
		slog.ErrorContext(ctx, "failed to check cached recipe image", "hash", recipeHash, "error", err)
		return
	}
	if exists {
		return
	}
	slog.InfoContext(ctx, "generating new recipe image", "hash", recipeHash)
	image, err := s.imagegen.GenerateRecipeImage(ctx, recipe)
	if err != nil {
		slog.ErrorContext(ctx, "failed to generate recipe image", "hash", recipeHash, "error", err)
		return
	}
	if err := s.SaveRecipeImage(ctx, recipeHash, image); err != nil {
		slog.ErrorContext(ctx, "failed to save recipe image", "hash", recipeHash, "error", err)
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
		if err := s.completeRecipeRegenerationJob(ctx, id, newHash); err != nil {
			slog.ErrorContext(ctx, "failed to complete recipe regeneration job", "job_id", id, "new_hash", newHash, "error", err)
		}
	})
}

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
	s.kickgeneration(ctx, p)
	redirectToHash(w, r, newHash, queryArgStart)
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
	generationWaitTimeout = 10 * time.Minute
	queryArgHash          = "h"
	queryArgStart         = "start"
	queryArgConversion    = "conversion"
	queryArgInstructions  = "instructions"
	// QueryArgHelp carries campaign-specific shopping list help text through redirects.
	QueryArgHelp = "help"
)

// notFound handles a missing generated shopping list by showing the generation
// spinner while work is in progress and the retry page after generation times out.
func (s *server) notFound(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	startArg := r.URL.Query().Get(queryArgStart)
	hashParam := r.URL.Query().Get(queryArgHash)
	// okay give them a new start time.
	if startArg == "" {
		// don't restart clock if we don't have the params. How did we even get here though.
		_, err := s.ParamsFromCache(ctx, hashParam)
		if err != nil {
			// not erroring because any rando on internet can send us things AND we seem to be missing
			// at least http://careme.cooking/recipes?h=3i3rbrZv0mk seems permabroke but very old
			// but a high level of these could signal a bug.
			slog.InfoContext(ctx, "failed to load params for hash", "hash", hashParam, "error", err)
			http.Error(w, "shoppinglist not found or expired", http.StatusNotFound)
			return
		}
		redirectToHash(w, r, hashParam, queryArgStart, QueryArgHelp)
		return
	}

	startTime, err := time.Parse(time.RFC3339Nano, startArg)
	if err != nil {
		slog.ErrorContext(ctx, "failed to parse start time", "time", startArg, "error", err)
		redirectToHash(w, r, hashParam, queryArgStart, QueryArgHelp)
		return
	}

	if !generationWaitExpired(startTime) {
		s.spin(ctx, w, r, hashParam)
		return
	}
	slog.WarnContext(ctx, "recipe generation timed out", "time", startArg, "hash", hashParam)
	generationTimedOut(ctx, w, r, hashParam)
}

func generationWaitExpired(updatedAt time.Time) bool {
	return time.Since(updatedAt) >= generationWaitTimeout
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
	if r.URL.Query().Has(queryArgStart) {
		redirectToHashWithConversion(w, r, hashParam, templates.RecipeGenerationConversion)
		return
	}

	p, err := s.ParamsFromCache(ctx, hashParam)
	if err != nil {
		slog.ErrorContext(ctx, "failed to load params for hash", "hash", hashParam, "error", err)
		http.Error(w, "failed to load recipe parameters", http.StatusInternalServerError)
		return
	}

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
	if r.URL.Query().Get("mail") == "true" {
		tf := users.NewUnsubscribeTokenFactory(*s.cfg)
		var unsubscribeURL string
		if signedIn {
			unsubscribeURL = s.cfg.ResolvedPublicOrigin() + "/user/unsubscribe?" + url.Values{
				"user":  []string{currentUser.ID},
				"token": []string{tf.UnsubscribeToken(currentUser.ID)},
			}.Encode()
		}
		if err := FormatMail(p, *slist, s.cfg.ResolvedPublicOrigin(), unsubscribeURL, w); err != nil {
			slog.ErrorContext(ctx, "failed to render mail template", "error", err)
			http.Error(w, "failed to render mail template", http.StatusInternalServerError)
		}
		return
	}
	if !signedIn {
		guest.EnsureShoppingListCount(w, r)
	}
	wines := parallelism.NewSafeMap[string, *ai.WineSelection](len(slist.Recipes))
	images := parallelism.NewSafeMap[string, bool](len(slist.Recipes))
	var recipeWG sync.WaitGroup
	for _, recipe := range slist.Recipes {
		recipeHash := recipe.ComputeHash()
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
	FormatShoppingListHTMLForHashWithHelp(ctx, p, *slist, wines.Clone(), images.Clone(), currentUser,
		hashParam, selection, help, instructions, w)
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
	// if params are already saved redirect and assume someone kicks off genration

	if err := s.SaveParams(ctx, p); err != nil {
		if errors.Is(err, ErrAlreadyExists) {
			slog.InfoContext(ctx, "params already existed redirecting", "hash", p.Hash())
			redirectToHash(w, r, p.Hash(), QueryArgHelp)
			return
		}
		slog.ErrorContext(ctx, "failed to save params", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	hash := p.Hash()

	s.kickgeneration(ctx, p)

	redirectToHash(w, r, hash, queryArgStart, QueryArgHelp)
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

	s.writeGenerationStatus(ctx, hash, "Trying again, chef.")
	s.kickgeneration(ctx, p)
	redirectToHash(w, r, hash, queryArgStart, QueryArgHelp)
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

func (s *server) kickgeneration(ctx context.Context, p *generatorParams) {
	hash := p.Hash()
	ctx = context.WithoutCancel(ctx)
	s.wg.Go(func() {
		slog.InfoContext(ctx, "generating cached recipes", "params", p.String(), "hash", hash)
		shoppingList, err := s.generator.GenerateRecipes(ctx, p)
		if err != nil {
			slog.ErrorContext(ctx, "generate error", "error", err)
			s.writeGenerationStatus(ctx, hash, recipestatus.Error(err))
			return
		}

		if err := s.SaveShoppingList(ctx, shoppingList, hash); err != nil {
			slog.ErrorContext(ctx, "save error", "error", err)
			return
		}
	})
}

// Almost same as kick generation except
// 1 doesn't bother to write status.
// 2 saves params and skips if already there
// 3 generate images.
// Could try and consolidate and
func (s *server) KickGenerationIfNotPresent(ctx context.Context, p *GeneratorParams) {
	s.wg.Go(func() {
		// 5 minutes is magic what should it be?
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Minute)
		defer cancel()
		if err := s.SaveParams(ctx, p); err != nil {
			if errors.Is(err, ErrAlreadyExists) {
				slog.ErrorContext(ctx, "save params for campaigns already exists")
				return
			}
			slog.ErrorContext(ctx, "save params for campaigns", "error", err)
			return
		}
		hash := p.Hash()

		slog.InfoContext(ctx, "generating campaign recipes", "params", p.String(), "hash", hash)
		shoppingList, err := s.generator.GenerateRecipes(ctx, p)
		if err != nil {
			slog.ErrorContext(ctx, "generate error", "error", err)
			return
		}

		if err := s.SaveShoppingList(ctx, shoppingList, hash); err != nil {
			slog.ErrorContext(ctx, "save error", "error", err)
			return
		}

		// don't really need to wait on full shopping list but generator doesn't have a channel
		for _, recipe := range shoppingList.Recipes {
			s.wg.Go(func() {
				s.ensureRecipeImage(ctx, recipe.ComputeHash(), recipe)
			})
		}
	})
}

func (s *server) writeGenerationStatus(ctx context.Context, hash, status string) {
	if s.statusWriter == nil || strings.TrimSpace(hash) == "" {
		return
	}
	if err := s.statusWriter.SaveGenerationStatus(ctx, hash, status); err != nil {
		slog.ErrorContext(ctx, "failed to save generation status", "hash", hash, "status", status, "error", err)
	}
}

type spinnerData struct {
	ClarityScript   template.HTML
	GoogleTagScript template.HTML
	Style           seasons.Style
	RefreshInterval string // seconds
	StatusMessage   string
	ServerSignedIn  bool
	CurrentPath     string
	RetryPath       string
}

func newSpinnerData(ctx context.Context) spinnerData {
	return spinnerData{
		ClarityScript:   templates.ClarityScript(ctx),
		GoogleTagScript: templates.GoogleTagScript(),
		Style:           seasons.GetCurrentStyle(),
		ServerSignedIn:  true, // clerk refresh doesn't need to reload because spin will just do it anyways
	}
}

func (s *server) spin(ctx context.Context, w http.ResponseWriter, r *http.Request, hash string) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")

	status, err := s.statusReader.GenerationStatusFromCache(ctx, hash)
	if err != nil && !errors.Is(err, cache.ErrNotFound) {
		slog.ErrorContext(ctx, "failed to load generation status", "hash", hash, "error", err)
	}

	data := newSpinnerData(ctx)
	data.RefreshInterval = "10" // seconds
	data.StatusMessage = status
	data.CurrentPath = r.URL.RequestURI()

	if httpx.IsHTMX(r) {
		if err := templates.Spin.ExecuteTemplate(w, "spin_progress", data); err != nil {
			slog.ErrorContext(ctx, "spin progress template execute error", "error", err)
			http.Error(w, "template error", http.StatusInternalServerError)
		}
		return
	}

	if err := templates.Spin.Execute(w, data); err != nil {
		slog.ErrorContext(ctx, "home template execute error", "error", err)
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

func (s *server) renderRecipeRegenerationRetry(ctx context.Context, w http.ResponseWriter, r *http.Request, hash, id string) {
	retryURL := url.URL{Path: "/recipe/" + url.PathEscape(hash) + "/regen/" + url.PathEscape(id) + "/retry"}
	renderGenerationRetry(ctx, w, r, retryURL.String())
}

func generationTimedOut(ctx context.Context, w http.ResponseWriter, r *http.Request, hash string) {
	retryURL := url.URL{Path: "/recipes/" + hash + "/retry"}
	retryQuery := url.Values{}
	retryQuery.Set(QueryArgHelp, r.URL.Query().Get(QueryArgHelp))
	retryURL.RawQuery = retryQuery.Encode()

	renderGenerationRetry(ctx, w, r, retryURL.String())
}

func renderGenerationRetry(ctx context.Context, w http.ResponseWriter, r *http.Request, retryPath string) {
	data := newSpinnerData(ctx)
	data.RetryPath = retryPath

	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	if httpx.IsHTMX(r) {
		if err := templates.Spin.ExecuteTemplate(w, "generation_retry", data); err != nil {
			slog.ErrorContext(ctx, "generation retry template execute error", "error", err)
			http.Error(w, "template error", http.StatusInternalServerError)
		}
		return
	}
	if err := templates.Spin.Execute(w, data); err != nil {
		slog.ErrorContext(ctx, "generation retry page execute error", "error", err)
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

// redirectToHash keeps only query arguments explicitly named by the caller.
func redirectToHash(w http.ResponseWriter, r *http.Request, hash string, argsToKeep ...string) {
	args := url.Values{} // intentionally clear other args
	if slices.Contains(argsToKeep, queryArgStart) {
		args.Set(queryArgStart, time.Now().Format(time.RFC3339Nano))
	}
	if slices.Contains(argsToKeep, QueryArgHelp) {
		args.Set(QueryArgHelp, r.URL.Query().Get(QueryArgHelp))
	}
	redirectToHashWithArgs(w, r, hash, args)
}

func redirectToRecipe(w http.ResponseWriter, r *http.Request, hash string) {
	u := url.URL{Path: "/recipe/" + url.PathEscape(hash)}
	if httpx.IsHTMX(r) {
		w.Header().Set("HX-Redirect", u.String())
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, u.String(), http.StatusSeeOther)
}

func redirectToRecipeRegeneration(w http.ResponseWriter, r *http.Request, hash, jobID string) {
	u := url.URL{Path: "/recipe/" + url.PathEscape(hash) + "/regen/" + url.PathEscape(jobID)}
	if httpx.IsHTMX(r) {
		w.Header().Set("HX-Redirect", u.String())
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, u.String(), http.StatusSeeOther)
}

func redirectToHashWithConversion(w http.ResponseWriter, r *http.Request, hash string, event templates.ConversionEvent) {
	args := url.Values{}
	args.Add(queryArgConversion, string(event))
	if help := r.URL.Query().Get(QueryArgHelp); help != "" {
		args.Set(QueryArgHelp, help)
	}
	redirectToHashWithArgs(w, r, hash, args)
}

func redirectToHashWithArgs(w http.ResponseWriter, r *http.Request, hash string, args url.Values) {
	u := url.URL{Path: "/recipes"}
	args.Set(queryArgHash, hash)

	u.RawQuery = args.Encode()
	if httpx.IsHTMX(r) {
		w.Header().Set("HX-Redirect", u.String())
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, u.String(), http.StatusSeeOther)
}

func isSingleRecipeAction(r *http.Request) bool {
	return strings.EqualFold(strings.TrimSpace(r.FormValue("source")), "recipe")
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
	slog.InfoContext(ctx, "added saved recipe to user profile", "title", recipe.Title)

	return nil
}
