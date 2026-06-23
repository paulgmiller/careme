package farmersmarket

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/auth"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/templates"

	utypes "careme/internal/users/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type staticZipFinder struct {
	zip string
	ok  bool
}

func (s staticZipFinder) NearestZIPToCoordinates(float64, float64) (string, bool) {
	return s.zip, s.ok
}

type fakeUserLookup struct {
	user *utypes.User
	err  error
}

func (f fakeUserLookup) FromRequest(context.Context, *http.Request, auth.AuthClient) (*utypes.User, error) {
	return f.user, f.err
}

type fakeExtractor struct {
	called bool
	mu     sync.Mutex
	calls  [][]ai.FarmersMarketPhoto
	fn     func(context.Context, []ai.FarmersMarketPhoto) ([]ai.InputIngredient, error)
}

func (f *fakeExtractor) ExtractFarmersMarketIngredients(ctx context.Context, photos []ai.FarmersMarketPhoto) ([]ai.InputIngredient, error) {
	f.mu.Lock()
	f.called = true
	f.calls = append(f.calls, photos)
	f.mu.Unlock()
	if f.fn != nil {
		return f.fn(ctx, photos)
	}
	return []ai.InputIngredient{{Brand: "Test Farm", Description: "apples"}}, nil
}

func TestSaveUploadCreatesAndMergesNearbyMarket(t *testing.T) {
	uploader := NewUploader(NewStore(cache.NewInMemoryCache()), staticZipFinder{zip: "98101", ok: true})
	date := time.Date(2026, 6, 5, 0, 0, 0, 0, time.UTC)

	first, ingredients, err := uploader.SaveUpload(t.Context(), "Saturday Market", 47.61, -122.33, 2, date, []ai.InputIngredient{
		{Brand: "River Farm", Description: "Strawberries", Size: "1 pint"},
	})
	require.NoError(t, err)
	require.Len(t, ingredients, 1)
	require.Equal(t, "98101", first.ZipCode)

	second, ingredients, err := uploader.SaveUpload(t.Context(), "River Stalls", 47.611, -122.331, 1, date, []ai.InputIngredient{
		{Brand: "River Farm", Description: "strawberries", Size: "1 pint"},
		{Brand: "Hill Farm", Description: "Fresh basil", Size: "1 bunch"},
	})
	require.NoError(t, err)

	require.Equal(t, first.ID, second.ID)
	require.ElementsMatch(t, []string{"Saturday Market", "River Stalls"}, second.Names)
	require.Equal(t, 3, second.PhotoCount)
	require.Len(t, ingredients, 2)
	assert.Equal(t, "River Farm", ingredients[0].Brand)
	assert.Equal(t, "Hill Farm", ingredients[1].Brand)
}

func TestFetchStaplesReturnsNewestFreshInventory(t *testing.T) {
	store := NewStore(cache.NewInMemoryCache())
	uploader := NewUploader(store, staticZipFinder{zip: "98101", ok: true})
	firstDate := time.Date(2026, 6, 5, 0, 0, 0, 0, time.UTC)
	secondDate := firstDate.AddDate(0, 0, 1)

	market, _, err := uploader.SaveUpload(t.Context(), "Daily Market", 47.61, -122.33, 1, firstDate, []ai.InputIngredient{
		{Brand: "Friday Farm", Description: "peas"},
	})
	require.NoError(t, err)
	_, _, err = uploader.SaveUpload(t.Context(), "Daily Market", 47.61, -122.33, 1, secondDate, []ai.InputIngredient{
		{Brand: "Saturday Farm", Description: "carrots"},
	})
	require.NoError(t, err)

	got, err := store.FetchStaples(t.Context(), market.ID)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "carrots", got[0].Description)
}

func TestFetchStaplesIgnoresInventoryOlderThan24Hours(t *testing.T) {
	cacheStore := cache.NewInMemoryCache()
	store := NewStore(cacheStore)
	locationID := LocationIDPrefix + "stale"
	raw, err := json.Marshal(inventoryRecord{
		CachedAt: time.Now().Add(-25 * time.Hour),
		Ingredients: []ai.InputIngredient{
			{Brand: "Old Farm", Description: "old lettuce"},
		},
	})
	require.NoError(t, err)
	require.NoError(t, cacheStore.Put(t.Context(), inventoryKey(locationID, time.Now()), string(raw), cache.Unconditional()))

	_, err = store.FetchStaples(t.Context(), locationID)
	require.ErrorIs(t, err, cache.ErrNotFound)
}

func TestGetLocationsByZipReturnsFarmersMarkets(t *testing.T) {
	store := NewStore(cache.NewInMemoryCache())
	uploader := NewUploader(store, staticZipFinder{zip: "98101", ok: true})
	_, _, err := uploader.SaveUpload(t.Context(), "Neighborhood Market", 47.61, -122.33, 1, time.Now(), []ai.InputIngredient{
		{Brand: "Farmers market", Description: "kale"},
	})
	require.NoError(t, err)

	got, err := store.GetLocationsByZip(t.Context(), "98101")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.True(t, store.HasInventory(got[0].ID))
	assert.Equal(t, "Neighborhood Market", got[0].Name)
	assert.Equal(t, ChainName, got[0].Chain)
}

func TestAverageCoordinate(t *testing.T) {
	got, err := AverageCoordinate([]Coordinate{
		{Lat: 47.0, Lon: -122.0},
		{Lat: 49.0, Lon: -124.0},
	})
	require.NoError(t, err)
	assert.Equal(t, 48.0, got.Lat)
	assert.Equal(t, -123.0, got.Lon)
}

func TestParseUploadedPhotosRejectsImagesWithoutGPS(t *testing.T) {
	req := multipartRequest(t, "photos", "market.jpg", jpegBytes(t))
	require.NoError(t, req.ParseMultipartForm(maxUploadBytes))

	_, _, err := parseUploadedPhotos(t.Context(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "location saved")
}

func TestExtractFarmersMarketIngredientsAnalyzesEachPhoto(t *testing.T) {
	extractor := &fakeExtractor{
		fn: func(_ context.Context, photos []ai.FarmersMarketPhoto) ([]ai.InputIngredient, error) {
			if len(photos) != 1 {
				return nil, errors.New("expected one photo")
			}
			return []ai.InputIngredient{
				{Brand: "Farmers market", Description: photos[0].DataURL},
				{Brand: "Farmers market", Description: "shared basil"},
			}, nil
		},
	}

	got, err := extractFarmersMarketIngredients(t.Context(), extractor, []ai.FarmersMarketPhoto{
		{DataURL: "tomatoes"},
		{DataURL: "radishes"},
	})

	require.NoError(t, err)
	require.Len(t, extractor.calls, 2)
	assert.Len(t, extractor.calls[0], 1)
	assert.Len(t, extractor.calls[1], 1)
	assert.Len(t, got, 3)
	assert.Contains(t, []string{got[0].Description, got[1].Description, got[2].Description}, "tomatoes")
	assert.Contains(t, []string{got[0].Description, got[1].Description, got[2].Description}, "radishes")
	assert.Contains(t, []string{got[0].Description, got[1].Description, got[2].Description}, "shared basil")
}

func TestHandlePostDoesNotCallAIWhenPhotosHaveNoGPS(t *testing.T) {
	require.NoError(t, templates.Init(&config.Config{}, "dummy.css"))
	extractor := &fakeExtractor{}
	handler := NewHandler(
		NewUploader(NewStore(cache.NewInMemoryCache()), staticZipFinder{zip: "98101", ok: true}),
		fakeUserLookup{user: &utypes.User{ID: "user_123", Email: []string{"chef@example.com"}}},
		auth.DefaultMock(),
		extractor,
		staticZipFinder{zip: "98101", ok: true},
	)
	req := multipartRequest(t, "photos", "market.jpg", jpegBytes(t))
	rr := httptest.NewRecorder()

	handler.handlePost(rr, req)

	require.Equal(t, http.StatusBadRequest, rr.Code)
	assert.False(t, extractor.called)
	assert.Contains(t, rr.Body.String(), "location saved")
}

func TestHandleGetRendersClerkRefreshData(t *testing.T) {
	require.NoError(t, templates.Init(&config.Config{}, "dummy.css"))
	handler := NewHandler(
		NewUploader(NewStore(cache.NewInMemoryCache()), staticZipFinder{zip: "98101", ok: true}),
		fakeUserLookup{user: &utypes.User{ID: "user_123", Email: []string{"chef@example.com"}}},
		auth.DefaultMock(),
		&fakeExtractor{},
		staticZipFinder{zip: "98101", ok: true},
	)
	req := httptest.NewRequest(http.MethodGet, "/farmersmarket", nil)
	rr := httptest.NewRecorder()

	handler.handleGet(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "Farmers market finds")
}

func TestHandleGetRedirectsAnonymousUser(t *testing.T) {
	handler := NewHandler(
		NewUploader(NewStore(cache.NewInMemoryCache()), staticZipFinder{zip: "98101", ok: true}),
		fakeUserLookup{err: auth.ErrNoSession},
		auth.DefaultMock(),
		&fakeExtractor{},
		staticZipFinder{zip: "98101", ok: true},
	)
	req := httptest.NewRequest(http.MethodGet, "/farmersmarket", nil)
	rr := httptest.NewRecorder()

	handler.handleGet(rr, req)

	require.Equal(t, http.StatusSeeOther, rr.Code)
	assert.Contains(t, rr.Header().Get("Location"), "/sign-in")
}

func multipartRequest(t *testing.T, fieldName, fileName string, data []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile(fieldName, fileName)
	require.NoError(t, err)
	_, err = part.Write(data)
	require.NoError(t, err)
	require.NoError(t, writer.WriteField("name", "Test Market"))
	require.NoError(t, writer.Close())
	req := httptest.NewRequest(http.MethodPost, "/farmersmarket", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

func jpegBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.White)
	var b bytes.Buffer
	err := jpeg.Encode(&b, img, nil)
	require.NoError(t, err)
	return b.Bytes()
}
