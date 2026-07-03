package farmersmarket

import (
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
	"strings"
	"sync"
	"time"

	"careme/internal/ai"
	"careme/internal/auth"
	"careme/internal/cache"
	"careme/internal/locations/geo"
	locationtypes "careme/internal/locations/types"
	"careme/internal/routing"
	"careme/internal/seasons"
	"careme/internal/templates"

	"github.com/google/uuid"
	"github.com/samber/lo"
)

const (
	// TODO: Revisit upload memory before raising these caps. A max-size upload keeps
	// multipart data plus base64 data URLs in memory, and concurrent uploads can
	// pressure the 750Mi production pod limit.
	maxUploadBytes      = 90 << 20
	maxPhotoBytes       = 10 << 20
	maxPhotoCount       = 32
	storeDayStartHour   = 9
	farmersMarketAction = "/farmersmarket"
)

type IngredientExtractor interface {
	ExtractFarmersMarketIngredients(ctx context.Context, imageDataURL string) ([]ai.InputIngredient, error)
}

type authClient interface {
	GetUserIDFromRequest(r *http.Request) (string, error)
}

type LocationResolver interface {
	ZipFinder
	ZipCentroidByZIP(zip string) (locationtypes.ZipCentroid, bool)
}

type Handler struct {
	uploader    *uploader
	auth        authClient
	extractor   IngredientExtractor
	zipFinder   LocationResolver
	statusStore *analysisStatusStore
	// exposed for tests
	parsePhotos func(context.Context, *http.Request) ([]Photo, error)
	wg          sync.WaitGroup
}

type Photo struct {
	contentType string
	content     []byte
}

// who knew data: was  valid url just like http:? see comment in ai/farmersmarket.go
func (p Photo) dataURL() string {
	return "data:" + p.contentType + ";base64," + base64.StdEncoding.EncodeToString(p.content)
}

func NewHandler(uploader *uploader, statusCache cache.Cache, authClient authClient, extractor IngredientExtractor, zipFinder LocationResolver) *Handler {
	return &Handler{
		uploader:    uploader,
		auth:        authClient,
		extractor:   extractor,
		zipFinder:   zipFinder,
		statusStore: newAnalysisStatusStore(statusCache),
		parsePhotos: parseUploadedPhotos,
	}
}

func (h *Handler) Register(mux routing.Registrar) {
	mux.HandleFunc("GET /farmersmarket", h.handleGet)
	mux.HandleFunc("POST /farmersmarket", h.handlePost)
	mux.HandleFunc("GET /farmersmarket/status/{jobID}", h.handleStatus)
}

func (h *Handler) Wait() {
	h.wg.Wait()
}

func (h *Handler) handleGet(w http.ResponseWriter, r *http.Request) {
	_, err := h.auth.GetUserIDFromRequest(r)
	if err != nil {
		if errors.Is(err, auth.ErrNoSession) {
			redirectToSignIn(w, r)
			return
		}
		slog.ErrorContext(r.Context(), "failed to load user for farmers market page", "error", err)
		http.Error(w, "unable to load account", http.StatusInternalServerError)
		return
	}

	data := struct {
		ClarityScript   template.HTML
		GoogleTagScript template.HTML
		Style           seasons.Style
		ServerSignedIn  bool
	}{
		ClarityScript:   templates.ClarityScript(r.Context()),
		GoogleTagScript: templates.GoogleTagScript(),
		Style:           seasons.GetCurrentStyle(),
		ServerSignedIn:  true,
	}
	if err := templates.FarmersMarket.Execute(w, data); err != nil {
		slog.ErrorContext(r.Context(), "farmers market template execute error", "error", err)
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

func (h *Handler) handlePost(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if !isHTMXRequest(r) {
		http.Error(w, "htmx request required", http.StatusBadRequest)
		return
	}

	userID, err := h.auth.GetUserIDFromRequest(r)
	if err != nil {
		if errors.Is(err, auth.ErrNoSession) {
			redirectToSignIn(w, r)
			return
		}
		slog.ErrorContext(ctx, "failed to load user for farmers market upload", "error", err)
		http.Error(w, "unable to load account", http.StatusInternalServerError)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		renderError(ctx, w, "Could not read those photos. Try fewer or smaller images.")
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		renderError(ctx, w, "Add a market name.")
		return
	}
	photos, err := h.parsePhotos(ctx, r)
	if err != nil {
		renderError(ctx, w, err.Error())
		return
	}
	coord, zip, err := h.resolveMarketLocation(r)
	if err != nil {
		renderError(ctx, w, err.Error())
		return
	}

	jobID := uuid.NewString()
	status := analysisStatus{
		ID:         jobID,
		UserID:     userID,
		State:      analysisStateRunning,
		PhotoCount: len(photos),
		Message:    "Looking through your market photos.",
	}
	if err := h.statusStore.save(ctx, status); err != nil {
		slog.ErrorContext(ctx, "failed to save farmers market analysis status", "error", err)
		http.Error(w, "Could not start looking through those photos. Try again, chef.", http.StatusInternalServerError)
		return
	}

	h.wg.Go(func() {
		jobCtx := context.WithoutCancel(ctx)
		h.runAnalysisJob(jobCtx, status, name, photos, coord, zip)
	})

	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	if err := renderFarmersMarketProgress(w, status); err != nil {
		slog.ErrorContext(ctx, "failed to render farmers market analysis progress", "error", err)
	}
}

func (h *Handler) runAnalysisJob(ctx context.Context, status analysisStatus, name string, photos []Photo, coord geo.Coordinate, zip string) {
	update := func(next analysisStatus) {
		if err := h.statusStore.save(ctx, next); err != nil {
			slog.ErrorContext(ctx, "failed to save farmers market analysis status", "job_id", status.ID, "error", err)
		}
	}
	fail := func(message string, err error) {
		if err != nil {
			slog.ErrorContext(ctx, "farmers market analysis job failed", "job_id", status.ID, "error", err)
		}
		status.State = analysisStateFailed
		status.Message = message
		update(status)
	}

	ingredients, err := extractFarmersMarketIngredientsWithProgress(ctx, h.extractor, photos,
		func(photosAnalyzed int, ingredients []ai.InputIngredient) {
			status.PhotosAnalyzed = photosAnalyzed
			status.IngredientCount = len(ingredients)
			status.Message = fmt.Sprintf("Analyzed %d of %d market photos.", photosAnalyzed, len(photos))
			update(status)
		})
	if err != nil {
		fail("Could not identify today's market finds.", err)
		return
	}
	if len(ingredients) == 0 {
		fail("Could not spot recipe ingredients in those photos.", nil)
		return
	}

	status.PhotosAnalyzed = len(photos)
	status.IngredientCount = len(ingredients)
	status.Message = fmt.Sprintf("Found %d ingredients. Saving this market.", len(ingredients))
	update(status)

	date := farmersMarketDate(time.Now(), zip)
	market, _, err := h.uploader.saveUpload(ctx, name, coord.Lat, coord.Lon, len(photos), date, ingredients)
	if err != nil {
		fail("Could not save this market. Try again, chef.", err)
		return
	}

	status.State = analysisStateComplete
	status.RedirectURL = "/recipes?location=" + url.QueryEscape(market.ID) + "&date=" + url.QueryEscape(date.Format("2006-01-02"))
	status.Message = fmt.Sprintf("Found %d ingredients. Building dinner ideas.", len(ingredients))
	update(status)
}

func (h *Handler) handleStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	_, err := h.auth.GetUserIDFromRequest(r)
	if err != nil {
		if errors.Is(err, auth.ErrNoSession) {
			redirectToSignIn(w, r)
			return
		}
		slog.ErrorContext(ctx, "failed to load user for farmers market analysis status", "error", err)
		http.Error(w, "unable to load account", http.StatusInternalServerError)
		return
	}

	status, err := h.statusStore.load(ctx, r.PathValue("jobID"))
	if err != nil {
		http.Error(w, "analysis not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	if status.State == analysisStateComplete && status.RedirectURL != "" {
		w.Header().Set("HX-Redirect", status.RedirectURL)
		w.WriteHeader(http.StatusOK)
		return
	}
	if status.State == analysisStateFailed {
		renderError(ctx, w, status.Message)
		return
	}
	if err := renderFarmersMarketProgress(w, status); err != nil {
		slog.ErrorContext(ctx, "failed to render farmers market analysis progress", "error", err)
	}
}

func extractFarmersMarketIngredients(ctx context.Context, extractor IngredientExtractor, photos []Photo) ([]ai.InputIngredient, error) {
	return extractFarmersMarketIngredientsWithProgress(ctx, extractor, photos, nil)
}

func extractFarmersMarketIngredientsWithProgress(ctx context.Context, extractor IngredientExtractor, photos []Photo, progress func(int, []ai.InputIngredient)) ([]ai.InputIngredient, error) {
	slog.InfoContext(ctx, "starting farmers market photo analysis", "photo_count", len(photos))
	type result struct {
		ingredients []ai.InputIngredient
		err         error
	}
	results := make(chan result, len(photos))
	var wg sync.WaitGroup
	for _, photo := range photos {
		photo := photo
		wg.Go(func() {
			ingredients, err := extractor.ExtractFarmersMarketIngredients(ctx, photo.dataURL())
			slog.InfoContext(ctx, "finished farmers market photo analysis", "ingredient_count", len(ingredients))
			results <- result{ingredients: ingredients, err: err}
		})
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	ingredients := make([]ai.InputIngredient, 0)
	errs := make([]error, 0)
	photosAnalyzed := 0
	for r := range results {
		photosAnalyzed++
		if r.err != nil {
			errs = append(errs, r.err)
		} else {
			ingredients = append(ingredients, r.ingredients...)
		}
		ingredients = uniqueIngredients(ingredients)
		if progress != nil {
			progress(photosAnalyzed, slices.Clone(ingredients))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return uniqueIngredients(ingredients), nil
}

func uniqueIngredients(ingredients []ai.InputIngredient) []ai.InputIngredient {
	return lo.UniqBy(ingredients, func(i ai.InputIngredient) string {
		return i.ProductID
	})
}

func (h *Handler) resolveMarketLocation(r *http.Request) (geo.Coordinate, string, error) {
	latRaw := strings.TrimSpace(r.FormValue("lat"))
	lonRaw := strings.TrimSpace(r.FormValue("lon"))
	if latRaw != "" || lonRaw != "" {
		if latRaw == "" || lonRaw == "" {
			return geo.Coordinate{}, "", fmt.Errorf("use both latitude and longitude, or add a ZIP code")
		}
		coord, err := geo.FromString(latRaw, lonRaw)
		switch {
		case errors.Is(err, geo.ErrInvalidLatitude):
			return geo.Coordinate{}, "", fmt.Errorf("that latitude does not look right")
		case errors.Is(err, geo.ErrInvalidLongitude):
			return geo.Coordinate{}, "", fmt.Errorf("that longitude does not look right")
		case errors.Is(err, geo.ErrInvalidCoordinate):
			return geo.Coordinate{}, "", fmt.Errorf("that location does not look right")
		case err != nil:
			return geo.Coordinate{}, "", fmt.Errorf("that location does not look right")
		}
		zip, ok := h.zipFinder.NearestZIPToCoordinates(coord.Lat, coord.Lon)
		if !ok {
			return geo.Coordinate{}, "", fmt.Errorf("could not match that location to a ZIP code")
		}
		return coord, zip, nil
	}

	zip, ok := normalizeMarketZIP(r.FormValue("zip"))
	if !ok {
		return geo.Coordinate{}, "", fmt.Errorf("add a ZIP code or use your current location")
	}
	centroid, ok := h.zipFinder.ZipCentroidByZIP(zip)
	if !ok {
		return geo.Coordinate{}, "", fmt.Errorf("could not find that ZIP code")
	}
	coord := geo.Coordinate(centroid)
	if !coord.Valid() {
		return geo.Coordinate{}, "", fmt.Errorf("could not find that ZIP code")
	}
	return coord, zip, nil
}

func normalizeMarketZIP(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if len(raw) == 5 && isAllDigits(raw) {
		return raw, true
	}
	if len(raw) == 10 && raw[5] == '-' && isAllDigits(raw[:5]) && isAllDigits(raw[6:]) {
		return raw[:5], true
	}
	return "", false
}

func isAllDigits(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}

func parseUploadedPhotos(ctx context.Context, r *http.Request) ([]Photo, error) {
	if r.MultipartForm == nil || r.MultipartForm.File == nil {
		return nil, fmt.Errorf("add a few market photos")
	}
	files := r.MultipartForm.File["photos"]
	if len(files) == 0 {
		return nil, fmt.Errorf("add a few market photos")
	}
	if len(files) > maxPhotoCount {
		return nil, fmt.Errorf("use %d photos or fewer", maxPhotoCount)
	}

	photos := make([]Photo, 0, len(files))
	for i, header := range files {
		if header.Size > maxPhotoBytes {
			return nil, fmt.Errorf("keep each photo under 10 MB")
		}
		file, err := header.Open()
		if err != nil {
			return nil, fmt.Errorf("could not open one of those photos")
		}
		data, readErr := io.ReadAll(io.LimitReader(file, maxPhotoBytes+1))
		closeErr := file.Close()
		if readErr != nil {
			return nil, fmt.Errorf("could not read one of those photos")
		}
		if closeErr != nil {
			return nil, fmt.Errorf("could not read one of those photos")
		}
		if len(data) > maxPhotoBytes {
			return nil, fmt.Errorf("keep each photo under 10 MB")
		}
		contentType := http.DetectContentType(data)
		if !strings.HasPrefix(contentType, "image/") {
			return nil, fmt.Errorf("upload image files only")
		}

		photos = append(photos, Photo{contentType: contentType, content: data})
		slog.InfoContext(ctx, "received farmers market photo", "photo_number", i+1, "photo_count", len(files), "filename", header.Filename, "size_bytes", len(data), "content_type", contentType)
	}
	return photos, nil
}

func farmersMarketDate(now time.Time, zip string) time.Time {
	tzName, ok := geo.TimezoneNameForZip(zip)
	if !ok {
		tzName = "UTC"
	}
	storeLoc, err := time.LoadLocation(tzName)
	if err != nil {
		storeLoc = time.UTC
	}
	localNow := now.In(storeLoc)
	if localNow.Hour() < storeDayStartHour {
		localNow = localNow.AddDate(0, 0, -1)
	}
	return time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, storeLoc)
}

func redirectToSignIn(w http.ResponseWriter, r *http.Request) {
	target := "/sign-in?return_to_b64=" + url.QueryEscape(base64.RawURLEncoding.EncodeToString([]byte(r.URL.RequestURI())))
	if isHTMXRequest(r) {
		w.Header().Set("HX-Redirect", target)
		http.Error(w, "must be logged in", http.StatusUnauthorized)
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func isHTMXRequest(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("HX-Request"), "true")
}

func renderError(ctx context.Context, w http.ResponseWriter, message string) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("HX-Retarget", "#farmers-market-error")
	w.Header().Set("HX-Reswap", "outerHTML")
	if err := templates.FarmersMarket.ExecuteTemplate(w, "farmersmarket_error", message); err != nil {
		slog.ErrorContext(ctx, "failed to render farmers market upload error", "error", err)
	}
}

func renderFarmersMarketProgress(w http.ResponseWriter, status analysisStatus) error {
	return templates.FarmersMarket.ExecuteTemplate(w, "farmersmarket_progress", status)
}
