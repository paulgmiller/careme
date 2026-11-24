package recipes

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"hash/fnv"
	"html/template"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"careme/internal/ai"
	"careme/internal/auth"
	"careme/internal/cache"
	"careme/internal/routing"
	"careme/internal/seasons"
	"careme/internal/templates"
	"careme/internal/users"
	utypes "careme/internal/users/types"
)

type leftoversAI interface {
	SuggestLeftoversIngredients(context.Context, ai.LeftoversInput) ([]string, error)
	GenerateLeftoversMeal(context.Context, ai.LeftoversInput) (*ai.LeftoversResult, error)
}

type leftoversHandler struct {
	ai     leftoversAI
	io     recipeio
	users  *users.Storage
	auth   auth.AuthClient
	locks  [32]sync.Mutex
	images *backgroundPostRecipeJobs
}

type leftoversSession struct {
	Owner       string
	Expires     time.Time
	Input       ai.LeftoversInput
	Questions   []string
	Recipe      *ai.Recipe
	Explanation string
	Round       int
}

type leftoversPage struct {
	Style           seasons.Style
	ClarityScript   template.HTML
	GoogleTagScript template.HTML
	ServerSignedIn  bool
	Leftovers       string
	Servings        int
	Meal            string
	Token           string
	Questions       []string
	Explanation     string
	Round           int
	Answers         map[int]string
	Error           string
}

func NewLeftoversHandler(client leftoversAI, c cache.Cache, storage *users.Storage, clerk auth.AuthClient, images ImageStore, imagegen ImageGen) *leftoversHandler {
	return &leftoversHandler{ai: client, io: IO(c), users: storage, auth: clerk, images: PostRecipeJobs{NewRecipeImageGenerator(images, imagegen)}.Background()}
}

func (h *leftoversHandler) Wait() { h.images.Wait() }

func (h *leftoversHandler) Register(mux routing.Registrar) {
	mux.HandleFunc("GET /leftovers", h.get)
	mux.HandleFunc("POST /leftovers", h.post)
	mux.HandleFunc("POST /leftovers/{token}", h.generate)
	mux.HandleFunc("GET /leftovers/{token}", h.sessionPage)
}

func (h *leftoversHandler) render(w http.ResponseWriter, r *http.Request, code int, page leftoversPage) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	page.Style = seasons.GetCurrentStyle()
	page.ClarityScript = templates.ClarityScript(r.Context())
	page.GoogleTagScript = templates.GoogleTagScript()
	_, page.ServerSignedIn = h.auth.GetUserIDFromRequest(r)
	w.WriteHeader(code)
	if err := templates.Leftovers.Execute(w, page); err != nil {
		slog.ErrorContext(r.Context(), "render leftovers", "error", err)
	}
}

func (h *leftoversHandler) get(w http.ResponseWriter, r *http.Request) {
	if h.currentUser(w, r) != nil {
		h.render(w, r, http.StatusOK, leftoversPage{Servings: 2})
	}
}

func (h *leftoversHandler) currentUser(w http.ResponseWriter, r *http.Request) *utypes.User {
	user, err := h.users.FromRequest(r.Context(), r, h.auth)
	if errors.Is(err, auth.ErrNoSession) || (err == nil && user == nil) {
		http.Redirect(w, r, signInPath("/leftovers"), http.StatusSeeOther)
		return nil
	}
	if err != nil {
		http.Error(w, "Unable to load your kitchen", http.StatusInternalServerError)
		return nil
	}
	return user
}

func parseLeftoversForm(w http.ResponseWriter, r *http.Request) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Please keep your leftovers description under 2,000 characters.", http.StatusBadRequest)
		return false
	}
	return true
}

func (h *leftoversHandler) post(w http.ResponseWriter, r *http.Request) {
	user := h.currentUser(w, r)
	if user == nil || !parseLeftoversForm(w, r) {
		return
	}
	leftovers, meal := strings.TrimSpace(r.PostForm.Get("leftovers")), r.PostForm.Get("meal")
	servings, servingsErr := strconv.Atoi(r.PostForm.Get("servings"))
	page := leftoversPage{Leftovers: leftovers, Meal: meal, Servings: servings}
	if leftovers == "" || len(leftovers) > 2000 {
		page.Error = "Tell us what leftovers you have first (up to 2,000 characters)."
		h.render(w, r, http.StatusBadRequest, page)
		return
	}
	if meal != "breakfast" && meal != "lunch" && meal != "dinner" {
		page.Error = "Choose breakfast, lunch or dinner."
		h.render(w, r, http.StatusBadRequest, page)
		return
	}
	if servingsErr != nil || servings < 1 || servings > 12 {
		page.Error = "Choose between 1 and 12 servings, chef."
		h.render(w, r, http.StatusBadRequest, page)
		return
	}
	input := ai.LeftoversInput{Leftovers: leftovers, Meal: meal, Servings: servings, Directive: user.Directive}
	h.recentMeals(user, &input)
	questions, err := h.ai.SuggestLeftoversIngredients(r.Context(), input)
	if err != nil {
		h.fail(w, r, page, err)
		return
	}
	token := rand.Text()
	input.Asked = append([]string{}, questions...)
	session := leftoversSession{Owner: user.ID, Expires: time.Now().Add(time.Hour), Input: input, Questions: questions}
	if err := h.saveSession(r.Context(), token, session); err != nil {
		h.fail(w, r, page, err)
		return
	}
	http.Redirect(w, r, "/leftovers/"+token, http.StatusSeeOther)
}

func (h *leftoversHandler) saveSession(ctx context.Context, token string, session leftoversSession) error {
	data, err := json.Marshal(session)
	if err != nil {
		return err
	}
	return h.io.Cache.Put(ctx, "leftovers/"+token, string(data), cache.Unconditional())
}

func (h *leftoversHandler) loadSession(w http.ResponseWriter, r *http.Request, user *utypes.User) (*leftoversSession, bool) {
	token := r.PathValue("token")
	if token == "" || len(token) > 64 || strings.ContainsAny(token, ". /\\") {
		http.NotFound(w, r)
		return nil, false
	}
	reader, err := h.io.Cache.Get(r.Context(), "leftovers/"+token)
	if err != nil {
		if errors.Is(err, cache.ErrNotFound) {
			http.NotFound(w, r)
		} else {
			h.fail(w, r, leftoversPage{}, err)
		}
		return nil, false
	}
	defer func() {
		if err := reader.Close(); err != nil {
			slog.ErrorContext(r.Context(), "close leftovers session", "error", err)
		}
	}()
	var session leftoversSession
	if err := json.NewDecoder(reader).Decode(&session); err != nil {
		h.fail(w, r, leftoversPage{}, err)
		return nil, false
	}
	if session.Owner != user.ID {
		http.NotFound(w, r)
		return nil, false
	}
	if time.Now().After(session.Expires) {
		http.Error(w, "These questions expired. Start again at /leftovers, chef.", http.StatusGone)
		return nil, false
	}
	return &session, true
}

func sessionView(token string, session *leftoversSession) leftoversPage {
	return leftoversPage{Leftovers: session.Input.Leftovers, Meal: session.Input.Meal, Servings: session.Input.Servings, Token: token, Questions: session.Questions, Round: session.Round, Explanation: session.Explanation}
}

func (h *leftoversHandler) sessionPage(w http.ResponseWriter, r *http.Request) {
	user := h.currentUser(w, r)
	if user == nil {
		return
	}
	session, ok := h.loadSession(w, r, user)
	if !ok {
		return
	}
	if session.Recipe != nil {
		http.Redirect(w, r, "/recipe/"+session.Recipe.ComputeHash(), http.StatusSeeOther)
		return
	}
	h.render(w, r, http.StatusOK, sessionView(r.PathValue("token"), session))
}

func (h *leftoversHandler) generate(w http.ResponseWriter, r *http.Request) {
	user := h.currentUser(w, r)
	if user == nil || !parseLeftoversForm(w, r) {
		return
	}
	token := r.PathValue("token")
	// Serialize duplicate submissions for a session without retaining unbounded locks.
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(token))
	lock := &h.locks[hasher.Sum32()%uint32(len(h.locks))]
	lock.Lock()
	defer lock.Unlock()
	session, ok := h.loadSession(w, r, user)
	if !ok {
		return
	}
	if session.Recipe != nil {
		http.Redirect(w, r, "/recipe/"+session.Recipe.ComputeHash(), http.StatusSeeOther)
		return
	}
	page := sessionView(token, session)
	round, err := strconv.Atoi(r.PostForm.Get("round"))
	if err != nil || round != session.Round || session.Explanation != "" {
		http.Redirect(w, r, "/leftovers/"+token, http.StatusSeeOther)
		return
	}
	input := session.Input
	input.Available = append([]string{}, input.Available...)
	input.Unavailable = append([]string{}, input.Unavailable...)
	input.MakeNow = r.PostForm.Get("action") == "now"
	page.Answers = map[int]string{}
	for i, ingredient := range session.Questions {
		answers := r.PostForm["ingredient-"+strconv.Itoa(i)]
		if len(answers) == 0 && input.MakeNow {
			input.Unavailable = append(input.Unavailable, ingredient)
			continue
		}
		if len(answers) != 1 || (answers[0] != "yes" && answers[0] != "no") {
			page.Error = "Answer yes or no for each ingredient, or choose Just make something."
			h.render(w, r, http.StatusBadRequest, page)
			return
		}
		page.Answers[i] = answers[0]
		if answers[0] == "yes" {
			input.Available = append(input.Available, ingredient)
		} else {
			input.Unavailable = append(input.Unavailable, ingredient)
		}
	}
	// Refresh preferences and saved history before the direct recipe request.
	input.Directive = user.Directive
	h.recentMeals(user, &input)
	result, err := h.ai.GenerateLeftoversMeal(r.Context(), input)
	if err != nil {
		h.fail(w, r, page, err)
		return
	}
	session.Input = input
	session.Round++
	session.Questions = result.Questions
	session.Explanation = result.Explanation
	if len(result.Questions) > 0 {
		session.Input.Asked = append(session.Input.Asked, result.Questions...)
	}
	if len(result.Recipes) == 1 {
		recipe := result.Recipes[0]
		recipe.Leftovers = true
		if err := h.io.SaveRecipe(r.Context(), recipe); err != nil {
			h.fail(w, r, page, err)
			return
		}
		session.Recipe = &recipe
	}
	if err := h.saveSession(r.Context(), token, *session); err != nil {
		h.fail(w, r, page, err)
		return
	}
	if session.Recipe != nil {
		// Optional illustration work survives request cancellation and never delays the recipe.
		if err := h.images.RecipeReady(r.Context(), *session.Recipe, "", time.Now()); err != nil {
			slog.ErrorContext(r.Context(), "start leftovers image", "error", err)
		}
	}
	http.Redirect(w, r, "/leftovers/"+token, http.StatusSeeOther)
}

// Use the existing saved history until the separate pastdecisions branch lands.
func (h *leftoversHandler) recentMeals(user *utypes.User, input *ai.LeftoversInput) {
	input.RecentMeals = nil
	since := time.Now().AddDate(0, 0, -14)
	for _, recipe := range user.LastRecipes {
		if recipe.CreatedAt.After(since) {
			input.RecentMeals = append(input.RecentMeals, recipe.Title)
		}
	}
}

func (h *leftoversHandler) fail(w http.ResponseWriter, r *http.Request, page leftoversPage, err error) {
	slog.ErrorContext(r.Context(), "leftovers failed", "error", err)
	page.Error = "Try again, chef. We couldn't finish your leftovers meal."
	h.render(w, r, http.StatusInternalServerError, page)
}

// MockLeftoversAI keeps the local flow usable without provider credentials.
type MockLeftoversAI struct{}

func (MockLeftoversAI) SuggestLeftoversIngredients(context.Context, ai.LeftoversInput) ([]string, error) {
	return []string{"eggs", "bread", "spinach", "olive oil"}, nil
}

func (MockLeftoversAI) GenerateLeftoversMeal(_ context.Context, input ai.LeftoversInput) (*ai.LeftoversResult, error) {
	ingredients := []ai.Ingredient{{Name: input.Leftovers, Quantity: strconv.Itoa(input.Servings) + " portions"}}
	for _, name := range input.Available {
		ingredients = append(ingredients, ai.Ingredient{Name: name, Quantity: "to taste"})
	}
	return &ai.LeftoversResult{Recipes: []ai.Recipe{{Title: "Leftovers for " + input.Meal, Description: "A fresh meal from what you have on hand.", Properties: ai.RecipeProperties{Servings: input.Servings}, Ingredients: ingredients, Instructions: []string{"Prepare " + strconv.Itoa(input.Servings) + " portions of your leftovers and the confirmed ingredients.", "Reheat cooked leftovers thoroughly to 165°F, then plate and serve."}}}}, nil
}
