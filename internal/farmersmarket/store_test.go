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
	"regexp"
	"sync"
	"testing"
	"time"

	"careme/internal/ai"
	"careme/internal/auth"
	"careme/internal/cache"
	"careme/internal/config"
	"careme/internal/locations/geo"
	locationtypes "careme/internal/locations/types"
	"careme/internal/templates"

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

type staticZipLookup map[string]geo.Coordinate

func (s staticZipLookup) ZipCentroidByZIP(zip string) (locationtypes.ZipCentroid, bool) {
	coord, ok := s[zip]
	return coord, ok
}

type fakeExtractor struct {
	called bool
	mu     sync.Mutex
	calls  []string
	fn     func(context.Context, string) ([]ai.InputIngredient, error)
}

type noSessionAuth struct{}

func (noSessionAuth) GetUserIDFromRequest(*http.Request) (string, error) {
	return "", auth.ErrNoSession
}

type fixedAuth struct {
	userID string
}

func (f fixedAuth) GetUserIDFromRequest(*http.Request) (string, error) {
	return f.userID, nil
}

func (f *fakeExtractor) ExtractFarmersMarketIngredients(ctx context.Context, imageDataURL string) ([]ai.InputIngredient, error) {
	f.mu.Lock()
	f.called = true
	f.calls = append(f.calls, imageDataURL)
	f.mu.Unlock()
	if f.fn != nil {
		return f.fn(ctx, imageDataURL)
	}
	return []ai.InputIngredient{{Brand: "Test Farm", Description: "apples"}}, nil
}

func TestSaveUploadCreatesAndMergesNearbyMarket(t *testing.T) {
	uploader := NewUploader(NewStore(cache.NewInMemoryCache()), staticZipFinder{zip: "98101", ok: true})
	date := time.Date(2026, 6, 5, 0, 0, 0, 0, time.UTC)

	first, ingredients, err := uploader.saveUpload(t.Context(), "Saturday Market", 47.61, -122.33, 2, date, []ai.InputIngredient{
		{ProductID: "A", Brand: "River Farm", Description: "Strawberries", Size: "1 pint"},
	})
	require.NoError(t, err)
	require.Len(t, ingredients, 1)
	require.Equal(t, "98101", first.ZipCode)

	second, ingredients, err := uploader.saveUpload(t.Context(), "River Stalls", 47.611, -122.331, 1, date, []ai.InputIngredient{
		{ProductID: "A", Brand: "River Farm", Description: "strawberries", Size: "1 pint"},
		{ProductID: "B", Brand: "Hill Farm", Description: "Fresh basil", Size: "1 bunch"},
	})
	require.NoError(t, err)

	require.Equal(t, first.ID, second.ID)
	require.ElementsMatch(t, []string{"Saturday Market", "River Stalls"}, second.Names)
	require.Equal(t, 3, second.PhotoCount)
	require.Len(t, ingredients, 2)
	assert.Equal(t, "River Farm", ingredients[0].Brand)
	assert.Equal(t, "Hill Farm", ingredients[1].Brand)
}

func TestFetchStaplesReturnsCurrentStoreDateInventory(t *testing.T) {
	store := NewStore(cache.NewInMemoryCache())
	provider := NewStaplesProviderFromStore(store)
	uploader := NewUploader(store, staticZipFinder{zip: "98101", ok: true})
	currentDate := farmersMarketDate(time.Now(), "98101")
	olderDate := currentDate.AddDate(0, 0, -1)

	market, _, err := uploader.saveUpload(t.Context(), "Daily Market", 47.61, -122.33, 1, olderDate, []ai.InputIngredient{
		{Brand: "Friday Farm", Description: "peas"},
	})
	require.NoError(t, err)
	_, _, err = uploader.saveUpload(t.Context(), "Daily Market", 47.61, -122.33, 1, currentDate, []ai.InputIngredient{
		{Brand: "Saturday Farm", Description: "carrots"},
	})
	require.NoError(t, err)

	got, err := provider.FetchStaples(t.Context(), market.ID)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "carrots", got[0].Description)
}

func TestFetchStaplesIgnoresPreviousMarketDateInventory(t *testing.T) {
	cacheStore := cache.NewInMemoryCache()
	store := NewStore(cacheStore)
	provider := NewStaplesProviderFromStore(store)
	locationID := LocationIDPrefix + "stale"
	currentDate := farmersMarketDate(time.Now(), "98101")
	olderDate := currentDate.AddDate(0, 0, -1)
	require.NoError(t, store.saveMarket(t.Context(), Market{
		ID:         locationID,
		Names:      []string{"Stale Market"},
		Coordinate: geo.Coordinate{Lat: 47.61, Lon: -122.33},
		ZipCode:    "98101",
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}))
	raw, err := json.Marshal(inventoryRecord{
		Ingredients: []ai.InputIngredient{
			{Brand: "Old Farm", Description: "old lettuce"},
		},
	})
	require.NoError(t, err)
	require.NoError(t, cacheStore.Put(t.Context(), inventoryKey(locationID, olderDate), string(raw), cache.Unconditional()))

	_, err = provider.FetchStaples(t.Context(), locationID)
	require.ErrorIs(t, err, cache.ErrNotFound)
	assert.False(t, NewLocationBackend(store, staticZipLookup{}).HasInventory(locationID))
}

func TestLocationBackendGetLocationsByZipReturnsNearbyFarmersMarkets(t *testing.T) {
	store := NewStore(cache.NewInMemoryCache())
	uploader := NewUploader(store, staticZipFinder{zip: "98199", ok: true})
	marketDate := farmersMarketDate(time.Now(), "98199")
	_, _, err := uploader.saveUpload(t.Context(), "Far Market", 48.2, -122.33, 1, marketDate, []ai.InputIngredient{
		{Brand: "Farmers market", Description: "turnips"},
	})
	require.NoError(t, err)
	_, _, err = uploader.saveUpload(t.Context(), "Near Market", 47.62, -122.33, 1, marketDate, []ai.InputIngredient{
		{Brand: "Farmers market", Description: "kale"},
	})
	require.NoError(t, err)
	_, _, err = uploader.saveUpload(t.Context(), "Closer Market", 47.611, -122.33, 1, marketDate, []ai.InputIngredient{
		{Brand: "Farmers market", Description: "chard"},
	})
	require.NoError(t, err)

	backend := NewLocationBackend(store, staticZipLookup{
		"98101": {Lat: 47.61, Lon: -122.33},
	})

	got, err := backend.GetLocationsByZip(t.Context(), "98101")
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.True(t, backend.HasInventory(got[0].ID))
	assert.Equal(t, "Closer Market", got[0].Name)
	assert.Equal(t, "Near Market", got[1].Name)
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

	_, err := parseUploadedPhotos(t.Context(), req)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "could not read location")
}

func TestParseUploadedPhotosRejectsTooManyPhotos(t *testing.T) {
	req := multipartRequest(t, "photos", "market.jpg", jpegBytes(t))
	require.NoError(t, req.ParseMultipartForm(maxUploadBytes))
	for len(req.MultipartForm.File["photos"]) < maxPhotoCount+1 {
		req.MultipartForm.File["photos"] = append(req.MultipartForm.File["photos"], req.MultipartForm.File["photos"][0])
	}

	_, err := parseUploadedPhotos(t.Context(), req)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "use 32 photos or fewer")
}

func TestExtractFarmersMarketIngredientsAnalyzesEachPhoto(t *testing.T) {
	extractor := &fakeExtractor{
		fn: func(_ context.Context, imageDataURL string) ([]ai.InputIngredient, error) {
			if imageDataURL == "" {
				return nil, errors.New("expected image data URL")
			}
			return []ai.InputIngredient{
				{ProductID: imageDataURL, Brand: "Farmers market", Description: imageDataURL},
				{ProductID: "B", Brand: "Farmers market", Description: "shared basil"},
			}, nil
		},
	}

	photos := []Photo{
		{contentType: "image/jpeg", content: []byte("tomatoes")},
		{contentType: "image/jpeg", content: []byte("radishes")},
	}

	got, err := extractFarmersMarketIngredients(t.Context(), extractor, photos)

	require.NoError(t, err)
	require.Len(t, extractor.calls, 2)
	assert.ElementsMatch(t, []string{photos[0].dataURL(), photos[1].dataURL()}, extractor.calls)
	require.Len(t, got, 3)
	assert.Contains(t, []string{got[0].Description, got[1].Description, got[2].Description}, photos[0].dataURL())
	assert.Contains(t, []string{got[0].Description, got[1].Description, got[2].Description}, photos[1].dataURL())
	assert.Contains(t, []string{got[0].Description, got[1].Description, got[2].Description}, "shared basil")
}

func TestHandlePostDoesNotCallAIWhenPhotosHaveNoGPS(t *testing.T) {
	require.NoError(t, templates.Init(&config.Config{}, "dummy.css"))
	extractor := &fakeExtractor{}
	cacheStore := cache.NewInMemoryCache()
	handler := NewHandler(
		NewUploader(NewStore(cacheStore), staticZipFinder{zip: "98101", ok: true}),
		cacheStore,
		auth.DefaultMock(),
		extractor,
		staticZipFinder{zip: "98101", ok: true},
	)
	req := multipartRequest(t, "photos", "market.jpg", jpegBytes(t))
	req.Header.Set("HX-Request", "true")
	rr := httptest.NewRecorder()

	handler.handlePost(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	assert.False(t, extractor.called)
	assert.Contains(t, rr.Body.String(), "could not read location")
	assert.Equal(t, "#farmers-market-error", rr.Header().Get("HX-Retarget"))
	assert.Equal(t, "outerHTML", rr.Header().Get("HX-Reswap"))
	assert.Contains(t, rr.Body.String(), `id="farmers-market-error"`)
}

func TestHandlePostRejectsNonHTMXBeforeParsingUpload(t *testing.T) {
	require.NoError(t, templates.Init(&config.Config{}, "dummy.css"))
	handler := newTestHandler(t, fixedAuth{userID: "user-1"}, &fakeExtractor{})
	handler.parsePhotos = func(context.Context, *http.Request) ([]Photo, error) {
		t.Fatal("parsePhotos should not be called for non-HTMX posts")
		return nil, nil
	}
	req := multipartRequest(t, "photos", "market.jpg", jpegBytes(t))
	rr := httptest.NewRecorder()

	handler.handlePost(rr, req)

	require.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "htmx request required")
}

func TestHandlePostHTMXStartsAnalysisAndReturnsProgress(t *testing.T) {
	require.NoError(t, templates.Init(&config.Config{}, "dummy.css"))
	release := make(chan struct{})
	var releaseOnce sync.Once
	extractor := &fakeExtractor{
		fn: func(ctx context.Context, _ string) ([]ai.InputIngredient, error) {
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return []ai.InputIngredient{{ProductID: "A", Brand: "Test Farm", Description: "apples"}}, nil
		},
	}
	t.Cleanup(func() {
		releaseOnce.Do(func() {
			close(release)
		})
	})
	handler := newTestHandler(t, fixedAuth{userID: "user-1"}, extractor)
	handler.parsePhotos = func(context.Context, *http.Request) ([]Photo, error) {
		return []Photo{{contentType: "image/jpeg", content: []byte("apples"), coord: &Coordinate{Lat: 47.61, Lon: -122.33}}}, nil
	}
	req := multipartRequest(t, "photos", "market.jpg", jpegBytes(t))
	req.Header.Set("HX-Request", "true")
	rr := httptest.NewRecorder()

	handler.handlePost(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	body := rr.Body.String()
	assert.Contains(t, body, `id="farmers-market-work"`)
	assert.Contains(t, body, `hx-get="/farmersmarket/status/`)
	assert.Contains(t, body, "0 of 1")

	matches := regexp.MustCompile(`/farmersmarket/status/([^"]+)`).FindStringSubmatch(body)
	require.Len(t, matches, 2)
	status, err := handler.statusStore.load(t.Context(), matches[1])
	require.NoError(t, err)
	assert.Equal(t, analysisStateRunning, status.State)
	assert.Equal(t, "user-1", status.UserID)
	releaseOnce.Do(func() {
		close(release)
	})
	handler.Wait()
}

func TestHandleStatusRendersPhotoAndIngredientProgress(t *testing.T) {
	require.NoError(t, templates.Init(&config.Config{}, "dummy.css"))
	handler := newTestHandler(t, fixedAuth{userID: "user-1"}, &fakeExtractor{})
	status := analysisStatus{
		ID:              "job-running",
		UserID:          "user-1",
		State:           analysisStateRunning,
		PhotoCount:      5,
		PhotosAnalyzed:  2,
		IngredientCount: 11,
		Message:         "Analyzed 2 of 5 market photos.",
	}
	require.NoError(t, handler.statusStore.save(t.Context(), status))
	req := httptest.NewRequest(http.MethodGet, "/farmersmarket/status/job-running", nil)
	req.SetPathValue("jobID", "job-running")
	rr := httptest.NewRecorder()

	handler.handleStatus(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	body := rr.Body.String()
	assert.Contains(t, body, "Analyzed 2 of 5 market photos.")
	assert.Contains(t, body, "2 of 5")
	assert.Contains(t, body, ">11<")
}

func TestHandleStatusRedirectsCompletedJob(t *testing.T) {
	handler := newTestHandler(t, fixedAuth{userID: "user-1"}, &fakeExtractor{})
	require.NoError(t, handler.statusStore.save(t.Context(), analysisStatus{
		ID:          "job-complete",
		UserID:      "user-1",
		State:       analysisStateComplete,
		RedirectURL: "/recipes?location=farmersmarket_abc&date=2026-06-24",
	}))
	req := httptest.NewRequest(http.MethodGet, "/farmersmarket/status/job-complete", nil)
	req.SetPathValue("jobID", "job-complete")
	rr := httptest.NewRecorder()

	handler.handleStatus(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "/recipes?location=farmersmarket_abc&date=2026-06-24", rr.Header().Get("HX-Redirect"))
}

func TestHandleStatusReturnsFailedJobAsErrorFragment(t *testing.T) {
	require.NoError(t, templates.Init(&config.Config{}, "dummy.css"))
	handler := newTestHandler(t, fixedAuth{userID: "user-1"}, &fakeExtractor{})
	require.NoError(t, handler.statusStore.save(t.Context(), analysisStatus{
		ID:      "job-failed",
		UserID:  "user-1",
		State:   analysisStateFailed,
		Message: "Could not spot recipe ingredients in those photos.",
	}))
	req := httptest.NewRequest(http.MethodGet, "/farmersmarket/status/job-failed", nil)
	req.Header.Set("HX-Request", "true")
	req.SetPathValue("jobID", "job-failed")
	rr := httptest.NewRecorder()

	handler.handleStatus(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "Could not spot recipe ingredients in those photos.")
	assert.NotContains(t, rr.Body.String(), `hx-post="/farmersmarket"`)
	assert.Equal(t, "#farmers-market-error", rr.Header().Get("HX-Retarget"))
	assert.Equal(t, "outerHTML", rr.Header().Get("HX-Reswap"))
	assert.Contains(t, rr.Body.String(), `id="farmers-market-error"`)
}

func TestHandleStatusAllowsAnotherUserJob(t *testing.T) {
	handler := newTestHandler(t, fixedAuth{userID: "user-2"}, &fakeExtractor{})
	require.NoError(t, handler.statusStore.save(t.Context(), analysisStatus{
		ID:         "job-owned",
		UserID:     "user-1",
		State:      analysisStateRunning,
		PhotoCount: 3,
		Message:    "Looking through your market photos.",
	}))
	req := httptest.NewRequest(http.MethodGet, "/farmersmarket/status/job-owned", nil)
	req.SetPathValue("jobID", "job-owned")
	rr := httptest.NewRecorder()

	handler.handleStatus(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "Looking through your market photos.")
}

func TestHandleStatusRejectsAnonymousUser(t *testing.T) {
	handler := newTestHandler(t, noSessionAuth{}, &fakeExtractor{})
	req := httptest.NewRequest(http.MethodGet, "/farmersmarket/status/job-owned", nil)
	req.Header.Set("HX-Request", "true")
	req.SetPathValue("jobID", "job-owned")
	rr := httptest.NewRecorder()

	handler.handleStatus(rr, req)

	require.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.Contains(t, rr.Header().Get("HX-Redirect"), "/sign-in")
}

func TestHandleGetRendersClerkRefreshData(t *testing.T) {
	require.NoError(t, templates.Init(&config.Config{}, "dummy.css"))
	cacheStore := cache.NewInMemoryCache()
	handler := NewHandler(
		NewUploader(NewStore(cacheStore), staticZipFinder{zip: "98101", ok: true}),
		cacheStore,
		auth.DefaultMock(),
		&fakeExtractor{},
		staticZipFinder{zip: "98101", ok: true},
	)
	req := httptest.NewRequest(http.MethodGet, "/farmersmarket", nil)
	rr := httptest.NewRecorder()

	handler.handleGet(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Body.String(), "Farmers market finds")
	assert.NotContains(t, rr.Body.String(), "template error")
}

func TestHandleGetRedirectsAnonymousUser(t *testing.T) {
	cacheStore := cache.NewInMemoryCache()
	handler := NewHandler(
		NewUploader(NewStore(cacheStore), staticZipFinder{zip: "98101", ok: true}),
		cacheStore,
		noSessionAuth{},
		&fakeExtractor{},
		staticZipFinder{zip: "98101", ok: true},
	)
	req := httptest.NewRequest(http.MethodGet, "/farmersmarket", nil)
	rr := httptest.NewRecorder()

	handler.handleGet(rr, req)

	require.Equal(t, http.StatusSeeOther, rr.Code)
	assert.Contains(t, rr.Header().Get("Location"), "/sign-in")
}

func newTestHandler(t *testing.T, authClient authClient, extractor IngredientExtractor) *Handler {
	t.Helper()
	cacheStore := cache.NewInMemoryCache()
	return NewHandler(
		NewUploader(NewStore(cacheStore), staticZipFinder{zip: "98101", ok: true}),
		cacheStore,
		authClient,
		extractor,
		staticZipFinder{zip: "98101", ok: true},
	)
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
