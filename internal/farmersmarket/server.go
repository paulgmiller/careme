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
	"careme/internal/locations/geo"
	"careme/internal/routing"
	"careme/internal/seasons"
	"careme/internal/templates"

	"github.com/samber/lo"
)

const (
	// TODO: Revisit upload memory before raising these caps. A max-size upload keeps
	// multipart data plus base64 data URLs in memory, and concurrent uploads can
	// pressure the 750Mi production pod limit.
	maxUploadBytes      = 90 << 20
	maxPhotoBytes       = 10 << 20
	maxPhotoCount       = 4
	photoAnalysisLimit  = 4
	storeDayStartHour   = 9
	farmersMarketAction = "/farmersmarket"
	analysisStaleAfter  = 5 * time.Minute
)

type IngredientExtractor interface {
	ExtractFarmersMarketIngredients(ctx context.Context, imageDataURL string) ([]ai.InputIngredient, error)
}

type authClient interface {
	GetUserIDFromRequest(r *http.Request) (string, error)
}

type Handler struct {
	uploader    *uploader
	auth        authClient
	extractor   IngredientExtractor
	zipFinder   ZipFinder
	statusStore *analysisStatusStore
	parsePhotos func(context.Context, *http.Request) ([]Photo, error)
	wg          sync.WaitGroup
}

type Photo struct {
	contentType string
	content     []byte
	coord       *Coordinate
}

// who knew data: was  valid url just like http:? see comment in ai/farmersmarket.go
func (p Photo) dataURL() string {
	return "data:" + p.contentType + ";base64," + base64.StdEncoding.EncodeToString(p.content)
}

func NewHandler(uploader *uploader, authClient authClient, extractor IngredientExtractor, zipFinder ZipFinder) *Handler {
	return &Handler{
		uploader:    uploader,
		auth:        authClient,
		extractor:   extractor,
		zipFinder:   zipFinder,
		statusStore: newAnalysisStatusStore(uploader.store.cache),
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
	}{
		ClarityScript:   templates.ClarityScript(r.Context()),
		GoogleTagScript: templates.GoogleTagScript(),
		Style:           seasons.GetCurrentStyle(),
	}
	if err := templates.FarmersMarket.Execute(w, data); err != nil {
		slog.ErrorContext(r.Context(), "farmers market template execute error", "error", err)
		http.Error(w, "template error", http.StatusInternalServerError)
	}
}

func (h *Handler) handlePost(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
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
		h.uploadError(w, r, "Could not read those photos. Try fewer or smaller images.", http.StatusBadRequest)
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		h.uploadError(w, r, "Add a market name.", http.StatusBadRequest)
		return
	}
	photos, err := h.parsePhotos(ctx, r)
	if err != nil {
		h.uploadError(w, r, err.Error(), http.StatusBadRequest)
		return
	}
	coords := lo.Map(photos, func(photo Photo, _ int) Coordinate {
		return *photo.coord
	})
	avg, err := AverageCoordinate(coords)
	if err != nil {
		h.uploadError(w, r, "Add at least one photo with location saved.", http.StatusBadRequest)
		return
	}
	zip, ok := h.zipFinder.NearestZIPToCoordinates(avg.Lat, avg.Lon)
	if !ok {
		h.uploadError(w, r, "Could not match those photos to a ZIP code.", http.StatusBadRequest)
		return
	}

	if isHTMXRequest(r) {
		h.startAnalysisJob(w, r, userID, name, photos, coords, avg, zip)
		return
	}

	ingredients, err := extractFarmersMarketIngredients(ctx, h.extractor, photos)
	if err != nil {
		slog.ErrorContext(ctx, "failed to extract farmers market ingredients", "error", err)
		http.Error(w, "Could not identify today's market finds. Try again, chef.", http.StatusBadGateway)
		return
	}
	if len(ingredients) == 0 {
		http.Error(w, "Could not spot recipe ingredients in those photos.", http.StatusBadRequest)
		return
	}

	date := farmersMarketDate(time.Now(), zip)
	market, _, err := h.uploader.saveUpload(ctx, name, avg.Lat, avg.Lon, len(coords), date, ingredients)
	if err != nil {
		slog.ErrorContext(ctx, "failed to save farmers market upload", "error", err)
		http.Error(w, "Could not save this market. Try again, chef.", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/recipes?location="+url.QueryEscape(market.ID)+"&date="+url.QueryEscape(date.Format("2006-01-02")), http.StatusSeeOther)
}

func (h *Handler) startAnalysisJob(w http.ResponseWriter, r *http.Request, userID, name string, photos []Photo, coords []Coordinate, avg Coordinate, zip string) {
	ctx := r.Context()
	jobID, err := newAnalysisJobID()
	if err != nil {
		slog.ErrorContext(ctx, "failed to create farmers market analysis job id", "error", err)
		h.uploadError(w, r, "Could not start looking through those photos. Try again, chef.", http.StatusInternalServerError)
		return
	}
	status := analysisStatus{
		ID:         jobID,
		UserID:     userID,
		State:      analysisStateRunning,
		PhotoCount: len(photos),
		Message:    "Looking through your market photos.",
	}
	if err := h.statusStore.save(ctx, status); err != nil {
		slog.ErrorContext(ctx, "failed to save farmers market analysis status", "error", err)
		h.uploadError(w, r, "Could not start looking through those photos. Try again, chef.", http.StatusInternalServerError)
		return
	}

	h.wg.Go(func() {
		jobCtx := context.WithoutCancel(ctx)
		h.runAnalysisJob(jobCtx, status, name, photos, coords, avg, zip)
	})

	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	if err := renderFarmersMarketProgress(w, status); err != nil {
		slog.ErrorContext(ctx, "failed to render farmers market analysis progress", "error", err)
	}
}

func (h *Handler) runAnalysisJob(ctx context.Context, status analysisStatus, name string, photos []Photo, coords []Coordinate, avg Coordinate, zip string) {
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
		status.Error = message
		update(status)
	}

	ingredients, err := extractFarmersMarketIngredientsWithProgress(ctx, h.extractor, photos, func(photosAnalyzed int, ingredients []ai.InputIngredient) {
		status.PhotosAnalyzed = photosAnalyzed
		status.IngredientCount = len(ingredients)
		status.Message = fmt.Sprintf("Analyzed %d of %d market photos.", photosAnalyzed, len(photos))
		update(status)
	})
	if err != nil {
		fail("Could not identify today's market finds. Try again, chef.", err)
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
	market, _, err := h.uploader.saveUpload(ctx, name, avg.Lat, avg.Lon, len(coords), date, ingredients)
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
	userID, err := h.auth.GetUserIDFromRequest(r)
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
	if status.UserID != userID {
		http.Error(w, "analysis not found", http.StatusNotFound)
		return
	}

	if status.State == analysisStateRunning && time.Since(status.UpdatedAt) > analysisStaleAfter {
		status.State = analysisStateFailed
		status.Message = "That took too long. Try again with a few fewer photos."
		status.Error = status.Message
		if err := h.statusStore.save(ctx, status); err != nil {
			slog.ErrorContext(ctx, "failed to save stale farmers market analysis status", "job_id", status.ID, "error", err)
		}
	}

	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	if status.State == analysisStateComplete && status.RedirectURL != "" {
		w.Header().Set("HX-Redirect", status.RedirectURL)
		w.WriteHeader(http.StatusOK)
		return
	}
	if status.State == analysisStateFailed {
		if err := renderFarmersMarketForm(w, status.Error); err != nil {
			slog.ErrorContext(ctx, "failed to render farmers market retry form", "error", err)
		}
		return
	}
	if err := renderFarmersMarketProgress(w, status); err != nil {
		slog.ErrorContext(ctx, "failed to render farmers market analysis progress", "error", err)
	}
}

func (h *Handler) uploadError(w http.ResponseWriter, r *http.Request, message string, code int) {
	if !isHTMXRequest(r) {
		http.Error(w, message, code)
		return
	}
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	if err := renderFarmersMarketForm(w, message); err != nil {
		slog.ErrorContext(r.Context(), "failed to render farmers market upload error", "error", err)
	}
}

func extractFarmersMarketIngredients(ctx context.Context, extractor IngredientExtractor, photos []Photo) ([]ai.InputIngredient, error) {
	return extractFarmersMarketIngredientsWithProgress(ctx, extractor, photos, nil)
}

func extractFarmersMarketIngredientsWithProgress(ctx context.Context, extractor IngredientExtractor, photos []Photo, progress func(int, []ai.InputIngredient)) ([]ai.InputIngredient, error) {
	if len(photos) == 0 {
		return nil, fmt.Errorf("at least one photo is required")
	}

	slog.InfoContext(ctx, "starting farmers market photo analysis", "photo_count", len(photos))
	type result struct {
		ingredients []ai.InputIngredient
		err         error
	}
	results := make(chan result, len(photos))
	sem := make(chan struct{}, photoAnalysisLimit)
	var wg sync.WaitGroup
	for _, photo := range photos {
		photo := photo
		wg.Go(func() {
			sem <- struct{}{}
			defer func() {
				<-sem
			}()
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

		photo := Photo{contentType: contentType, content: data}
		coord, err := GPSFromImage(data)
		if err != nil {
			return nil, fmt.Errorf("could not read location from one of those photos %w", err)
		}
		photo.coord = &coord

		photos = append(photos, photo)
		slog.InfoContext(ctx, "received farmers market photo", "photo_number", i+1, "photo_count", len(files), "filename", header.Filename, "size_bytes", len(data), "content_type", contentType, "has_location", photo.coord != nil)
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

func renderFarmersMarketForm(w http.ResponseWriter, errorMessage string) error {
	return templates.FarmersMarket.ExecuteTemplate(w, "farmersmarket_form", errorMessage)
}

func renderFarmersMarketProgress(w http.ResponseWriter, status analysisStatus) error {
	return templates.FarmersMarket.ExecuteTemplate(w, "farmersmarket_progress", status)
}
