package users

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"careme/internal/cache"
	"careme/internal/templates"

	utypes "careme/internal/users/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStorageLinkPartnerCreatesReciprocalRelationship(t *testing.T) {
	t.Parallel()
	cacheStore := cache.NewFileCache(t.TempDir())
	storage := NewStorage(cacheStore)
	first := seedPartnerUser(t, storage, cacheStore, "user-1", "first@example.com")
	second := seedPartnerUser(t, storage, cacheStore, "user-2", "second@example.com")

	require.NoError(t, storage.LinkPartner(first.ID, " SECOND@EXAMPLE.COM "))

	storedFirst, err := storage.GetByID(first.ID)
	require.NoError(t, err)
	storedSecond, err := storage.GetByID(second.ID)
	require.NoError(t, err)
	assert.Equal(t, second.ID, storedFirst.PartnerID)
	assert.Equal(t, first.ID, storedSecond.PartnerID)
	resolved, err := storage.Partner(storedFirst)
	require.NoError(t, err)
	require.NotNil(t, resolved)
	assert.Equal(t, second.ID, resolved.ID)
}

func TestStorageLinkPartnerRejectsInvalidRelationships(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		prepare func(*testing.T, *Storage, *cache.FileCache, *utypes.User, *utypes.User)
		email   string
		wantErr error
	}{
		{
			name:    "unknown email",
			email:   "missing@example.com",
			wantErr: ErrPartnerNotFound,
		},
		{
			name:    "self",
			email:   "first@example.com",
			wantErr: ErrPartnerSelf,
		},
		{
			name: "current user already linked",
			prepare: func(t *testing.T, storage *Storage, cacheStore *cache.FileCache, first, _ *utypes.User) {
				third := seedPartnerUser(t, storage, cacheStore, "user-3", "third@example.com")
				require.NoError(t, storage.LinkPartner(first.ID, third.Email[0]))
			},
			email:   "second@example.com",
			wantErr: ErrUserAlreadyHasPartner,
		},
		{
			name: "recipient already linked",
			prepare: func(t *testing.T, storage *Storage, cacheStore *cache.FileCache, _, second *utypes.User) {
				third := seedPartnerUser(t, storage, cacheStore, "user-3", "third@example.com")
				require.NoError(t, storage.LinkPartner(second.ID, third.Email[0]))
			},
			email:   "second@example.com",
			wantErr: ErrPartnerUnavailable,
		},
		{
			name: "recipient opted out",
			prepare: func(t *testing.T, storage *Storage, _ *cache.FileCache, _, second *utypes.User) {
				require.NoError(t, storage.DisablePartnerSharing(second.ID))
			},
			email:   "second@example.com",
			wantErr: ErrPartnerUnavailable,
		},
		{
			name: "current user opted out",
			prepare: func(t *testing.T, storage *Storage, _ *cache.FileCache, first, _ *utypes.User) {
				require.NoError(t, storage.DisablePartnerSharing(first.ID))
			},
			email:   "second@example.com",
			wantErr: ErrUserAlreadyHasPartner,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cacheStore := cache.NewFileCache(t.TempDir())
			storage := NewStorage(cacheStore)
			first := seedPartnerUser(t, storage, cacheStore, "user-1", "first@example.com")
			second := seedPartnerUser(t, storage, cacheStore, "user-2", "second@example.com")
			if tt.prepare != nil {
				tt.prepare(t, storage, cacheStore, first, second)
			}

			err := storage.LinkPartner(first.ID, tt.email)

			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestStorageUnlinkPartnerFromEitherSide(t *testing.T) {
	t.Parallel()
	cacheStore := cache.NewFileCache(t.TempDir())
	storage := NewStorage(cacheStore)
	first := seedPartnerUser(t, storage, cacheStore, "user-1", "first@example.com")
	second := seedPartnerUser(t, storage, cacheStore, "user-2", "second@example.com")
	require.NoError(t, storage.LinkPartner(first.ID, second.Email[0]))

	require.NoError(t, storage.UnlinkPartner(second.ID))

	storedFirst, err := storage.GetByID(first.ID)
	require.NoError(t, err)
	storedSecond, err := storage.GetByID(second.ID)
	require.NoError(t, err)
	assert.Empty(t, storedFirst.PartnerID)
	assert.Empty(t, storedSecond.PartnerID)
	require.ErrorIs(t, storage.UnlinkPartner(first.ID), ErrNoPartner)
}

func TestStoragePartnerSharingOptOutCanBeReenabled(t *testing.T) {
	t.Parallel()
	cacheStore := cache.NewFileCache(t.TempDir())
	storage := NewStorage(cacheStore)
	user := seedPartnerUser(t, storage, cacheStore, "user-1", "first@example.com")

	require.NoError(t, storage.DisablePartnerSharing(user.ID))
	disabled, err := storage.GetByID(user.ID)
	require.NoError(t, err)
	assert.True(t, partnerSharingDisabled(disabled))
	partner, err := storage.Partner(disabled)
	require.NoError(t, err)
	assert.Nil(t, partner)

	require.NoError(t, storage.EnablePartnerSharing(user.ID))
	enabled, err := storage.GetByID(user.ID)
	require.NoError(t, err)
	assert.Empty(t, enabled.PartnerID)
}

func TestStorageUpdatePreservesManagedPartnerID(t *testing.T) {
	t.Parallel()
	cacheStore := cache.NewFileCache(t.TempDir())
	storage := NewStorage(cacheStore)
	first := seedPartnerUser(t, storage, cacheStore, "user-1", "first@example.com")
	second := seedPartnerUser(t, storage, cacheStore, "user-2", "second@example.com")
	staleFirst, err := storage.GetByID(first.ID)
	require.NoError(t, err)
	require.NoError(t, storage.LinkPartner(first.ID, second.Email[0]))

	staleFirst.Directive = "Use more vegetables."
	require.NoError(t, storage.Update(staleFirst))

	updated, err := storage.GetByID(first.ID)
	require.NoError(t, err)
	assert.Equal(t, second.ID, updated.PartnerID)
	assert.Equal(t, "Use more vegetables.", updated.Directive)
}

func TestStorageLinkPartnerRollsBackFirstWrite(t *testing.T) {
	t.Parallel()
	base := cache.NewFileCache(t.TempDir())
	storage := NewStorage(base)
	first := seedPartnerUser(t, storage, base, "user-1", "first@example.com")
	second := seedPartnerUser(t, storage, base, "user-2", "second@example.com")
	failingCache := &failOncePutCache{
		ListCache: base,
		key:       userPrefix + second.ID,
		err:       errors.New("forced partner write failure"),
	}
	storage = NewStorage(failingCache)

	err := storage.LinkPartner(first.ID, second.Email[0])

	require.ErrorContains(t, err, "forced partner write failure")
	storedFirst, getErr := storage.GetByID(first.ID)
	require.NoError(t, getErr)
	assert.Empty(t, storedFirst.PartnerID)
	storedSecond, getErr := storage.GetByID(second.ID)
	require.NoError(t, getErr)
	assert.Empty(t, storedSecond.PartnerID)
}

func TestHandlePartnerLinksAndRedirectsWithoutEmail(t *testing.T) {
	t.Parallel()
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	storage := NewStorage(cacheStore)
	first := seedPartnerUser(t, storage, cacheStore, "user-1", "user@example.com")
	second := seedPartnerUser(t, storage, cacheStore, "user-2", "partner@example.com")
	s := &server{storage: storage, clerk: testAuthClient{}}
	form := url.Values{"action": {"link"}, "email": {second.Email[0]}}
	req := httptest.NewRequest(http.MethodPost, "/user/partner", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	s.handlePartner(rr, req)

	require.Equal(t, http.StatusSeeOther, rr.Code)
	assert.Equal(t, "/user?tab=customize&partner_status=linked", rr.Header().Get("Location"))
	assert.NotContains(t, rr.Header().Get("Location"), second.Email[0])
	stored, err := storage.GetByID(first.ID)
	require.NoError(t, err)
	assert.Equal(t, second.ID, stored.PartnerID)
}

func TestHandlePartnerUnknownEmailReturnsFriendlyStatus(t *testing.T) {
	t.Parallel()
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	storage := NewStorage(cacheStore)
	seedPartnerUser(t, storage, cacheStore, "user-1", "user@example.com")
	s := &server{storage: storage, clerk: testAuthClient{}}
	form := url.Values{"action": {"link"}, "email": {"private@example.com"}}
	req := httptest.NewRequest(http.MethodPost, "/user/partner", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	s.handlePartner(rr, req)

	require.Equal(t, http.StatusSeeOther, rr.Code)
	assert.Equal(t, "/user?tab=customize&partner_status=not_found", rr.Header().Get("Location"))
	assert.NotContains(t, rr.Header().Get("Location"), "private@example.com")
}

func TestHandleUserShowsPartnerRecipesAfterOwnRecipesReadOnly(t *testing.T) {
	t.Parallel()
	cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
	storage := NewStorage(cacheStore)
	now := time.Now()
	first := seedPartnerUser(t, storage, cacheStore, "user-1", "user@example.com")
	second := seedPartnerUser(t, storage, cacheStore, "user-2", "partner@example.com")
	first.LastRecipes = []utypes.Recipe{{Title: "My Soup", Hash: "mine", CreatedAt: now}}
	second.LastRecipes = []utypes.Recipe{{Title: "Partner Pasta", Hash: "theirs", CreatedAt: now}}
	require.NoError(t, storage.Update(first))
	require.NoError(t, storage.Update(second))
	require.NoError(t, storage.LinkPartner(first.ID, second.Email[0]))
	s := &server{storage: storage, userTmpl: templates.User, clerk: testAuthClient{}}
	req := httptest.NewRequest(http.MethodGet, "/user?tab=past", nil)
	rr := httptest.NewRecorder()

	s.handleUser(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	body := rr.Body.String()
	assert.Contains(t, body, "Recipes from")
	assert.Contains(t, body, second.Email[0])
	assert.Less(t, strings.Index(body, "My Soup"), strings.Index(body, "Partner Pasta"))
	assert.Equal(t, 1, strings.Count(body, `hx-post="/user/recipes/remove"`))
}

func TestHandleUserShowsPartnerControlsForRelationshipState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		prepare   func(*testing.T, *Storage, *cache.FileCache)
		want      []string
		doNotWant []string
	}{
		{
			name:      "available",
			want:      []string{`id="partner_email"`, "Connect kitchens", "Keep my kitchen private"},
			doNotWant: []string{"Disconnect kitchens", "Allow partner sharing"},
		},
		{
			name: "linked",
			prepare: func(t *testing.T, storage *Storage, cacheStore *cache.FileCache) {
				partner := seedPartnerUser(t, storage, cacheStore, "user-2", "partner@example.com")
				require.NoError(t, storage.LinkPartner("user-1", partner.Email[0]))
			},
			want:      []string{"partner@example.com", "Disconnect kitchens"},
			doNotWant: []string{`id="partner_email"`, "Keep my kitchen private", "Allow partner sharing"},
		},
		{
			name: "disabled",
			prepare: func(t *testing.T, storage *Storage, _ *cache.FileCache) {
				require.NoError(t, storage.DisablePartnerSharing("user-1"))
			},
			want:      []string{"Your kitchen is private.", "Allow partner sharing"},
			doNotWant: []string{`id="partner_email"`, "Disconnect kitchens", "Keep my kitchen private"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cacheStore := cache.NewFileCache(filepath.Join(t.TempDir(), "cache"))
			storage := NewStorage(cacheStore)
			seedPartnerUser(t, storage, cacheStore, "user-1", "user@example.com")
			if tt.prepare != nil {
				tt.prepare(t, storage, cacheStore)
			}
			s := &server{storage: storage, userTmpl: templates.User, clerk: testAuthClient{}}
			req := httptest.NewRequest(http.MethodGet, "/user?tab=customize", nil)
			rr := httptest.NewRecorder()

			s.handleUser(rr, req)

			require.Equal(t, http.StatusOK, rr.Code)
			body := rr.Body.String()
			for _, want := range tt.want {
				assert.Contains(t, body, want)
			}
			for _, unwanted := range tt.doNotWant {
				assert.NotContains(t, body, unwanted)
			}
		})
	}
}

func seedPartnerUser(t *testing.T, storage *Storage, cacheStore cache.Cache, id, email string) *utypes.User {
	t.Helper()
	user := &utypes.User{
		ID:          id,
		Email:       []string{email},
		CreatedAt:   time.Now(),
		ShoppingDay: time.Saturday.String(),
	}
	require.NoError(t, storage.Update(user))
	require.NoError(t, cacheStore.Put(context.Background(), emailPrefix+normalizeEmail(email), id, cache.Unconditional()))
	return user
}

type failOncePutCache struct {
	cache.ListCache
	key string
	err error
}

func (c *failOncePutCache) Put(ctx context.Context, key, value string, opts cache.PutOptions) error {
	if key == c.key && c.err != nil {
		err := c.err
		c.err = nil
		return err
	}
	return c.ListCache.Put(ctx, key, value, opts)
}

func (c *failOncePutCache) PutReader(ctx context.Context, key string, reader io.Reader, opts cache.PutOptions) error {
	return c.ListCache.PutReader(ctx, key, reader, opts)
}
