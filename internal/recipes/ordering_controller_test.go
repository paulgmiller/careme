package recipes

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/locations"
	"careme/internal/ordering"
	utypes "careme/internal/users/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeShoppingLinkProvider models a connection-free, name-based handoff. It never
// claims that its included items were matched to products or added to a cart.
type fakeShoppingLinkProvider struct {
	requests    []ordering.Request
	submissions int
}

func (*fakeShoppingLinkProvider) ID() string                         { return "shopping-link" }
func (*fakeShoppingLinkProvider) Workflow() ordering.Workflow        { return ordering.ShoppingListLink }
func (*fakeShoppingLinkProvider) Supports(req ordering.Request) bool { return len(req.Items) > 0 }
func (*fakeShoppingLinkProvider) Presentation(ordering.Request) ordering.Presentation {
	return ordering.Presentation{Brand: "Shopping link provider", ActionLabel: "Create shopping list"}
}

func (p *fakeShoppingLinkProvider) Prepare(_ context.Context, req ordering.Request) (ordering.Prepared, error) {
	p.requests = append(p.requests, req)
	return ordering.Prepared{Sent: req.Items}, nil
}

func (p *fakeShoppingLinkProvider) Submit(_ context.Context, _ ordering.Request, prepared ordering.Prepared) (ordering.Result, error) {
	p.submissions++
	return ordering.Result{Kind: ordering.ShoppingListCreated, URL: "https://shopping.example.test/lists/created", Sent: prepared.Sent}, nil
}

func orderingLinkFixture(t *testing.T, options ...testServerOption) (*server, *generatorParams, *fakeShoppingLinkProvider) {
	t.Helper()
	p := DefaultParams(&locations.Location{ID: "walmart_123", Name: "Source store", Chain: "Walmart"}, time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC))
	p.Saved = []ai.Recipe{{Title: "Dinner", Ingredients: []ai.Ingredient{{Name: "Garlic", Quantity: "2 cloves"}, {Name: "Parsley", Quantity: "1 bunch"}}}}
	options = append(options, withTestLocationServer(staticLocationLookup{location: p.Location}))
	s := newTestServer(t, options...)
	provider := &fakeShoppingLinkProvider{}
	s.ordering = ordering.New(s.Cache, []ordering.Provider{provider})
	require.NoError(t, s.SaveParams(t.Context(), p))
	return s, p, provider
}

func orderingLinkMux(s *server) *http.ServeMux {
	mux := http.NewServeMux()
	s.registerShoppingListRoutes(mux)
	return mux
}

func TestOrderingShoppingLinkRouteAndReload(t *testing.T) {
	s, p, provider := orderingLinkFixture(t)
	require.NoError(t, s.mergeAndCacheShoppingList(t.Context(), p.Hash(), p.Saved[0].Ingredients, nil))
	// Ordering must consume the finalized snapshot rather than regenerating it.
	s.shoppingMerger = shoppingMergerFunc(func(context.Context, []ai.Ingredient, []ai.InputIngredient) ([]ai.ShoppingItem, error) {
		t.Fatal("ordering must not rerun shopping quantity merging")
		return nil, nil
	})
	mux := orderingLinkMux(s)
	request := httptest.NewRequest(http.MethodPost, "/recipes/"+p.Hash()+"/ordering/"+provider.ID(), nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	require.Equal(t, http.StatusSeeOther, response.Code)
	redirect, err := url.Parse(response.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "/recipes", redirect.Path)
	assert.Equal(t, p.Hash(), redirect.Query().Get("h"))
	assert.Equal(t, "shopping-list-section", redirect.Fragment)
	assert.Empty(t, response.Result().Cookies(), "connection-free providers need no shopper OAuth cookie")
	require.Len(t, provider.requests, 1)
	req := provider.requests[0]
	assert.Equal(t, "mock-clerk-user-id", req.UserID)
	assert.Equal(t, p.Hash(), req.Hash)
	assert.Equal(t, ordering.Store{Provider: "walmart", ID: p.Location.ID, Chain: "Walmart"}, req.Source)
	assert.Equal(t, "Shopping list for Source store", req.Title)
	assert.Contains(t, req.LinkBackURL, "/recipes?h=")
	assert.NotEmpty(t, req.Fingerprint)
	require.Len(t, req.Items, 2)
	assert.Equal(t, "Garlic", req.Items[0].Name)
	assert.Equal(t, "2 cloves", req.Items[0].Needed)
	assert.Empty(t, req.Items[0].ProductID, "shopping links accept names without catalog identifiers")
	assert.Equal(t, "walmart", req.Items[0].Provider)
	assert.Equal(t, 1, provider.submissions)

	pageResponse := httptest.NewRecorder()
	pageRequest := httptest.NewRequest(http.MethodGet, redirect.RequestURI(), nil)
	user := &utypes.User{ID: "mock-clerk-user-id", Email: []string{"chef@example.test"}}
	s.renderShoppingList(pageResponse, pageRequest, p, &ai.ShoppingList{Recipes: p.Saved}, user, shoppingProgress{})
	require.Equal(t, http.StatusOK, pageResponse.Code)
	assert.Contains(t, pageResponse.Body.String(), "Your shopping list is ready.")
	assert.Contains(t, pageResponse.Body.String(), "Open shopping list")
	assert.Contains(t, pageResponse.Body.String(), `href="https://shopping.example.test/lists/created"`)
	assert.NotContains(t, pageResponse.Body.String(), "Products sent")
	assert.NotContains(t, pageResponse.Body.String(), "Create shopping list")

	// The shared POST route reuses the existing link without another submission.
	repeat := httptest.NewRecorder()
	mux.ServeHTTP(repeat, httptest.NewRequest(http.MethodPost, request.URL.String(), nil))
	assert.Equal(t, http.StatusSeeOther, repeat.Code)
	assert.Equal(t, 1, provider.submissions)
}

func TestOrderingShoppingLinkRequiresFinalizedSnapshot(t *testing.T) {
	s, p, provider := orderingLinkFixture(t)
	response := httptest.NewRecorder()
	orderingLinkMux(s).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/recipes/"+p.Hash()+"/ordering/"+provider.ID(), nil))
	assert.Equal(t, http.StatusBadRequest, response.Code)
	assert.Empty(t, provider.requests)
	assert.Zero(t, provider.submissions)
}

func TestOrderingRouteRejectsUnknownProvider(t *testing.T) {
	s, p, provider := orderingLinkFixture(t)
	response := httptest.NewRecorder()
	orderingLinkMux(s).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/recipes/"+p.Hash()+"/ordering/unknown", nil))
	assert.Equal(t, http.StatusNotFound, response.Code)
	assert.Empty(t, provider.requests)
	assert.Zero(t, provider.submissions)
}

func TestOrderingShoppingLinkRequiresSignedInUser(t *testing.T) {
	s, p, provider := orderingLinkFixture(t, withTestClerk(noSessionAuth{}))
	require.NoError(t, s.mergeAndCacheShoppingList(t.Context(), p.Hash(), p.Saved[0].Ingredients, nil))
	response := httptest.NewRecorder()
	orderingLinkMux(s).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/recipes/"+p.Hash()+"/ordering/"+provider.ID(), nil))
	assert.Equal(t, http.StatusUnauthorized, response.Code)
	assert.Contains(t, response.Body.String(), "must be logged in")
	assert.Empty(t, provider.requests)
	assert.Zero(t, provider.submissions)
}
