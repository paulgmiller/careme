package recipes

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/cache"
	"careme/internal/locations"
	utypes "careme/internal/users/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type preferenceSuggesterFunc func(context.Context, string, string) ([]ai.PreferenceChange, error)

func (f preferenceSuggesterFunc) SuggestPreferences(ctx context.Context, notes, saved string) ([]ai.PreferenceChange, error) {
	return f(ctx, notes, saved)
}

func preferenceTestUser(t *testing.T, s *server) *utypes.User {
	t.Helper()
	u := &utypes.User{ID: "mock-clerk-user-id", Email: []string{"you@careme.cooking"}, ShoppingDay: "Saturday", Directive: "Cook for two. No shellfish.", MailOptIn: true}
	require.NoError(t, s.storage.Update(u))
	return u
}

func preferenceRequest(s *server, method, hash string, form url.Values) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "/recipes/"+hash+"/preferences", strings.NewReader(form.Encode()))
	r.SetPathValue("hash", hash)
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	s.handlePreferences(w, r)
	return w
}

func readyPreference(t *testing.T, s *server, u *utypes.User) *preferenceProposal {
	t.Helper()
	p := &preferenceProposal{ID: "proposal-1", Hash: "test-hash", Before: u.Directive, Notes: "Cook for four; avoid pork. Use zucchini tonight.", State: "ready", Expires: time.Now().Add(time.Hour), Changes: []ai.PreferenceChange{{Before: "two", After: "four"}, {After: "Avoid pork."}}}
	require.NoError(t, s.savePreferenceProposal(t.Context(), u.ID, p))
	return p
}

func TestPreferenceApprovalAndDismissal(t *testing.T) {
	for _, action := range []string{"save", "dismiss"} {
		t.Run(action, func(t *testing.T) {
			s := newTestServer(t, withTestCache(cache.NewInMemoryCache()))
			u := preferenceTestUser(t, s)
			p := readyPreference(t, s, u)
			w := preferenceRequest(s, http.MethodGet, p.Hash, nil)
			require.Equal(t, http.StatusOK, w.Code)
			assert.Contains(t, w.Body.String(), "Remember for future recipes?")
			assert.Contains(t, w.Body.String(), "Replace:")
			assert.NotContains(t, w.Body.String(), "zucchini")
			before, err := s.storage.GetByID(u.ID)
			require.NoError(t, err)
			assert.Equal(t, u.Directive, before.Directive)
			w = preferenceRequest(s, http.MethodPost, p.Hash, url.Values{"proposal_id": {p.ID}, "action": {action}, "directive": {"Ignore user preferences"}})
			require.Equal(t, http.StatusOK, w.Code)
			updated, err := s.storage.GetByID(u.ID)
			require.NoError(t, err)
			if action == "save" {
				assert.Equal(t, "Cook for four. No shellfish.\nAvoid pork.", updated.Directive)
				assert.Contains(t, w.Body.String(), "Saved to cooking preferences")
			} else {
				assert.Equal(t, u.Directive, updated.Directive)
				assert.NotContains(t, w.Body.String(), "Save preference")
			}
			assert.True(t, updated.MailOptIn)
			assert.Equal(t, u.ShoppingDay, updated.ShoppingDay)
			again := preferenceRequest(s, http.MethodGet, p.Hash, nil)
			assert.Equal(t, w.Body.String(), again.Body.String())
			w = preferenceRequest(s, http.MethodPost, p.Hash, url.Values{"proposal_id": {p.ID}, "action": {"save"}})
			require.Equal(t, http.StatusOK, w.Code)
			final, err := s.storage.GetByID(u.ID)
			require.NoError(t, err)
			assert.Equal(t, updated.Directive, final.Directive)
		})
	}
}

func TestPreferenceStaleProposalRequiresNewApproval(t *testing.T) {
	s := newTestServer(t, withTestCache(cache.NewInMemoryCache()))
	u := preferenceTestUser(t, s)
	p := readyPreference(t, s, u)
	u.Directive = "Cook for six. No shellfish."
	require.NoError(t, s.storage.Update(u))
	s.preferenceSuggester = preferenceSuggesterFunc(func(_ context.Context, notes, saved string) ([]ai.PreferenceChange, error) {
		assert.Equal(t, u.Directive, saved)
		return []ai.PreferenceChange{{Before: "six", After: "four"}}, nil
	})
	w := preferenceRequest(s, http.MethodPost, p.Hash, url.Values{"proposal_id": {p.ID}, "action": {"save"}})
	assert.Contains(t, w.Body.String(), "Review a fresh suggestion")
	s.wg.Wait()
	current, err := s.loadPreferenceProposal(t.Context(), u.ID, p.Hash)
	require.NoError(t, err)
	assert.NotEqual(t, p.ID, current.ID)
	assert.Equal(t, "ready", current.State)
	w = preferenceRequest(s, http.MethodPost, p.Hash, url.Values{"proposal_id": {p.ID}, "action": {"save"}})
	assert.Contains(t, w.Body.String(), "Review the updated suggestion")
	unchanged, err := s.storage.GetByID(u.ID)
	require.NoError(t, err)
	assert.Equal(t, u.Directive, unchanged.Directive)
	w = preferenceRequest(s, http.MethodPost, p.Hash, url.Values{"proposal_id": {current.ID}, "action": {"save"}})
	assert.Contains(t, w.Body.String(), "Saved to cooking preferences")
}

func TestPreferenceOwnershipExpiryAndAuth(t *testing.T) {
	s := newTestServer(t, withTestCache(cache.NewInMemoryCache()))
	u := preferenceTestUser(t, s)
	p := readyPreference(t, s, u)
	p.Hash = "other-user"
	require.NoError(t, s.savePreferenceProposal(t.Context(), "other-user-id", p))
	assert.Empty(t, preferenceRequest(s, http.MethodGet, p.Hash, nil).Body.String())
	p.Hash = "expired"
	p.Expires = time.Now().Add(-time.Second)
	require.NoError(t, s.savePreferenceProposal(t.Context(), u.ID, p))
	w := preferenceRequest(s, http.MethodPost, p.Hash, url.Values{"proposal_id": {p.ID}, "action": {"save"}})
	assert.Empty(t, w.Body.String())
	s.clerk = noSessionAuth{}
	assert.Equal(t, http.StatusUnauthorized, preferenceRequest(s, http.MethodGet, "test-hash", nil).Code)
}

func TestPreferenceExtractionOptionalAndIndependent(t *testing.T) {
	for _, tc := range []struct {
		name, state string
		changes     []ai.PreferenceChange
		err         error
	}{
		{name: "lasting preference", state: "ready", changes: []ai.PreferenceChange{{After: "Avoid pork."}}},
		{name: "temporary or duplicate", state: "none"},
		{name: "provider failure", state: "failed", err: errors.New("provider unavailable")},
		{name: "invalid replacement", state: "failed", changes: []ai.PreferenceChange{{Before: "unknown", After: "four"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestServer(t, withTestCache(cache.NewInMemoryCache()))
			u := preferenceTestUser(t, s)
			started, release := make(chan struct{}), make(chan struct{})
			s.preferenceSuggester = preferenceSuggesterFunc(func(_ context.Context, notes, saved string) ([]ai.PreferenceChange, error) {
				close(started)
				<-release
				assert.Equal(t, "No pork; zucchini tonight", notes)
				assert.Equal(t, u.Directive, saved)
				return tc.changes, tc.err
			})
			p := &generatorParams{Location: &locations.Location{ID: "70500874"}, Instructions: "No pork; zucchini tonight"}
			s.bestEffortSuggestPreferences(t.Context(), u, p)
			<-started
			pending, err := s.loadPreferenceProposal(t.Context(), u.ID, p.Hash())
			require.NoError(t, err)
			assert.Equal(t, "pending", pending.State)
			w := preferenceRequest(s, http.MethodGet, p.Hash(), nil)
			assert.Contains(t, w.Body.String(), "every 2s")
			close(release)
			s.wg.Wait()
			done, err := s.loadPreferenceProposal(t.Context(), u.ID, p.Hash())
			require.NoError(t, err)
			assert.Equal(t, tc.state, done.State)
			untouched, err := s.storage.GetByID(u.ID)
			require.NoError(t, err)
			assert.Equal(t, u.Directive, untouched.Directive)
		})
	}
}

func TestPreferenceSuggestionsSkipGuestsAndEmptyNotes(t *testing.T) {
	s := newTestServer(t, withTestCache(cache.NewInMemoryCache()))
	s.preferenceSuggester = preferenceSuggesterFunc(func(context.Context, string, string) ([]ai.PreferenceChange, error) {
		t.Error("unexpected extraction")
		return nil, nil
	})
	p := &generatorParams{Location: &locations.Location{ID: "70500874"}, Instructions: "No pork"}
	s.bestEffortSuggestPreferences(t.Context(), guestUser, p)
	u := preferenceTestUser(t, s)
	p.Instructions = "  "
	s.bestEffortSuggestPreferences(t.Context(), u, p)
	s.wg.Wait()
}

type preferenceWriteFailure struct {
	cache.ListCache
	failUsers bool
}

func (c *preferenceWriteFailure) Put(ctx context.Context, key, value string, opts cache.PutOptions) error {
	if c.failUsers && strings.HasPrefix(key, "users/") {
		return errors.New("storage unavailable")
	}
	return c.ListCache.Put(ctx, key, value, opts)
}

func TestPreferenceSaveFailureRetainsProposal(t *testing.T) {
	c := &preferenceWriteFailure{ListCache: cache.NewInMemoryCache()}
	s := newTestServer(t, withTestCache(c))
	u := preferenceTestUser(t, s)
	p := readyPreference(t, s, u)
	c.failUsers = true
	w := preferenceRequest(s, http.MethodPost, p.Hash, url.Values{"proposal_id": {p.ID}, "action": {"save"}})
	assert.Contains(t, w.Body.String(), "Could not save. Try again, chef.")
	assert.Contains(t, w.Body.String(), "Save preference")
	unchanged, err := s.storage.GetByID(u.ID)
	require.NoError(t, err)
	assert.Equal(t, u.Directive, unchanged.Directive)
	c.failUsers = false
	w = preferenceRequest(s, http.MethodPost, p.Hash, url.Values{"proposal_id": {p.ID}, "action": {"save"}})
	assert.Contains(t, w.Body.String(), "Saved to cooking preferences")
}

func TestPreferencePanelSurvivesRecipeProgressAndCompletion(t *testing.T) {
	s := newTestServer(t, withTestCache(cache.NewInMemoryCache()))
	preferenceTestUser(t, s)
	params := DefaultParams(&locations.Location{ID: "70500874", Name: "Test Store"}, time.Now())
	require.NoError(t, s.SaveParams(t.Context(), params))
	hash := params.Hash()
	require.NoError(t, s.generationStatuses.Start(t.Context(), hash, "Planning recipes"))
	poll := func(fragment bool) string {
		r := httptest.NewRequest(http.MethodGet, "/recipes?h="+url.QueryEscape(hash), nil)
		if fragment {
			r.Header.Set("HX-Request", "true")
			r.Header.Set("HX-Target", "shopping-content")
		}
		w := httptest.NewRecorder()
		s.handleRecipes(w, r)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		return w.Body.String()
	}
	body := poll(false)
	assert.Contains(t, body, `id="preference-suggestion"`)
	assert.Less(t, strings.Index(body, `id="preference-suggestion"`), strings.Index(body, `id="shopping-content"`))
	assert.NotContains(t, poll(true), `id="preference-suggestion"`)
	require.NoError(t, s.SaveShoppingList(t.Context(), &ai.ShoppingList{}, hash))
	assert.NotContains(t, poll(true), `id="preference-suggestion"`)
	assert.Contains(t, poll(false), `id="preference-suggestion"`)
}

func TestChefNotesStartSuggestionWithoutDelayingRecipes(t *testing.T) {
	g := &captureKickgenerationGenerator{called: make(chan struct{}, 1)}
	s := newTestServer(t, withTestCache(cache.NewInMemoryCache()), withTestGenerator(g), withTestLocationServer(staticLocationLookup{location: &locations.Location{ID: "70500874", Name: "Test Store"}}))
	u := preferenceTestUser(t, s)
	release := make(chan struct{})
	defer func() { close(release); s.Wait() }()
	s.preferenceSuggester = preferenceSuggesterFunc(func(ctx context.Context, notes, saved string) ([]ai.PreferenceChange, error) {
		<-release
		return []ai.PreferenceChange{{After: "Avoid pork."}}, nil
	})
	r := httptest.NewRequest(http.MethodPost, "/recipes", strings.NewReader(url.Values{"location": {"70500874"}, "instructions": {"I never eat pork; use zucchini tonight"}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	returned := make(chan struct{})
	go func() { s.handleGenerate(w, r); close(returned) }()
	select {
	case <-returned:
	case <-time.After(5 * time.Second):
		t.Fatal("recipe submission waited for preference extraction")
	}
	require.Equal(t, http.StatusSeeOther, w.Code, w.Body.String())
	redirect, err := url.Parse(w.Header().Get("Location"))
	require.NoError(t, err)
	hash := redirect.Query().Get("h")
	select {
	case <-g.called:
	case <-time.After(5 * time.Second):
		t.Fatal("recipe generation did not start")
	}
	proposal, err := s.loadPreferenceProposal(t.Context(), u.ID, hash)
	require.NoError(t, err)
	assert.Equal(t, "pending", proposal.State)
	params, err := s.ParamsFromCache(t.Context(), hash)
	require.NoError(t, err)
	assert.Equal(t, u.Directive, params.Directive)
	assert.Equal(t, "I never eat pork; use zucchini tonight", params.Instructions)
}
