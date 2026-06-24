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
	"strings"
	"time"

	"careme/internal/ai"
	"careme/internal/auth"
	"careme/internal/locations/geo"
	"careme/internal/parallelism"
	"careme/internal/routing"
	"careme/internal/seasons"
	"careme/internal/templates"

	utypes "careme/internal/users/types"

	"github.com/samber/lo"
)

const (
	// TODO: Revisit upload memory before raising these caps. A max-size upload keeps
	// multipart data plus base64 data URLs in memory, and concurrent uploads can
	// pressure the 750Mi production pod limit.
	maxUploadBytes      = 90 << 20
	maxPhotoBytes       = 10 << 20
	maxPhotoCount       = 32
	photoAnalysisLimit  = 4
	storeDayStartHour   = 9
	farmersMarketAction = "/farmersmarket"
)

type IngredientExtractor interface {
	ExtractFarmersMarketIngredients(ctx context.Context, imageDataURL string) ([]ai.InputIngredient, error)
}

type UserLookup interface {
	FromRequest(ctx context.Context, r *http.Request, authClient auth.AuthClient) (*utypes.User, error)
}

type Handler struct {
	uploader   *Uploader
	users      UserLookup
	authClient auth.AuthClient
	extractor  IngredientExtractor
	zipFinder  ZipFinder
}

type pageData struct {
	Error           string
	ClarityScript   template.HTML
	GoogleTagScript template.HTML
	Style           seasons.Style
	User            *utypes.User
	ServerSignedIn  bool
}

type Photo struct {
	contentType string
	content     []byte
	coord       *Coordinate
}

func NewHandler(uploader *Uploader, users UserLookup, authClient auth.AuthClient, extractor IngredientExtractor, zipFinder ZipFinder) *Handler {
	return &Handler{
		uploader:   uploader,
		users:      users,
		authClient: authClient,
		extractor:  extractor,
		zipFinder:  zipFinder,
	}
}

func (h *Handler) Register(mux routing.Registrar) {
	mux.HandleFunc("GET /farmersmarket", h.handleGet)
	mux.HandleFunc("POST /farmersmarket", h.handlePost)
}

func (h *Handler) handleGet(w http.ResponseWriter, r *http.Request) {
	currentUser, err := h.currentUser(r)
	if err != nil {
		if errors.Is(err, auth.ErrNoSession) {
			redirectToSignIn(w, r)
			return
		}
		slog.ErrorContext(r.Context(), "failed to load user for farmers market page", "error", err)
		http.Error(w, "unable to load account", http.StatusInternalServerError)
		return
	}
	h.render(w, r, currentUser, "")
}

func (h *Handler) handlePost(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	currentUser, err := h.currentUser(r)
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
		h.renderStatus(w, r, currentUser, "Could not read those photos. Try fewer or smaller images.", http.StatusBadRequest)
		return
	}

	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		h.renderStatus(w, r, currentUser, "Add a market name.", http.StatusBadRequest)
		return
	}
	photos, err := parseUploadedPhotos(ctx, r)
	if err != nil {
		h.renderStatus(w, r, currentUser, err.Error(), http.StatusBadRequest)
		return
	}
	coords := lo.Map(photos, func(photo Photo, _ int) Coordinate {
		return *photo.coord
	})
	avg, err := AverageCoordinate(coords)
	if err != nil {
		h.renderStatus(w, r, currentUser, "Add at least one photo with location saved.", http.StatusBadRequest)
		return
	}
	zip, ok := h.zipFinder.NearestZIPToCoordinates(avg.Lat, avg.Lon)
	if !ok {
		h.renderStatus(w, r, currentUser, "Could not match those photos to a ZIP code.", http.StatusBadRequest)
		return
	}

	ingredients, err := extractFarmersMarketIngredients(ctx, h.extractor, photos)
	if err != nil {
		slog.ErrorContext(ctx, "failed to extract farmers market ingredients", "error", err)
		h.renderStatus(w, r, currentUser, "Could not identify today's market finds. Try again, chef.", http.StatusBadGateway)
		return
	}
	if len(ingredients) == 0 {
		h.renderStatus(w, r, currentUser, "Could not spot recipe ingredients in those photos.", http.StatusBadRequest)
		return
	}

	date := farmersMarketDate(time.Now(), zip)
	market, _, err := h.uploader.SaveUpload(ctx, name, avg.Lat, avg.Lon, len(coords), date, ingredients)
	if err != nil {
		slog.ErrorContext(ctx, "failed to save farmers market upload", "error", err)
		h.renderStatus(w, r, currentUser, "Could not save this market. Try again, chef.", http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, "/recipes?location="+url.QueryEscape(market.ID)+"&date="+url.QueryEscape(date.Format("2006-01-02")), http.StatusSeeOther)
}

func extractFarmersMarketIngredients(ctx context.Context, extractor IngredientExtractor, photos []Photo) ([]ai.InputIngredient, error) {
	if len(photos) == 0 {
		return nil, fmt.Errorf("at least one photo is required")
	}

	slog.InfoContext(ctx, "starting farmers market photo analysis", "photo_count", len(photos))
	ingredients, err := parallelism.Flatten(photos, func(photo Photo) ([]ai.InputIngredient, error) {
		ingredients, err := extractor.ExtractFarmersMarketIngredients(ctx, photo.dataURL())
		slog.InfoContext(ctx, "finished farmers market photo analysis", "ingredient_count", len(ingredients))
		return ingredients, err
	})
	if err != nil {
		return nil, err
	}

	return lo.UniqBy(ingredients, func(i ai.InputIngredient) string {
		return i.ProductID
	}), nil
}

func (h *Handler) currentUser(r *http.Request) (*utypes.User, error) {
	return h.users.FromRequest(r.Context(), r, h.authClient)
}

func (h *Handler) renderStatus(w http.ResponseWriter, r *http.Request, user *utypes.User, message string, status int) {
	w.WriteHeader(status)
	h.render(w, r, user, message)
}

func (h *Handler) render(w http.ResponseWriter, r *http.Request, user *utypes.User, message string) {
	data := pageData{
		Error:           message,
		ClarityScript:   templates.ClarityScript(r.Context()),
		GoogleTagScript: templates.GoogleTagScript(),
		Style:           seasons.GetCurrentStyle(),
		User:            user,
		ServerSignedIn:  user != nil,
	}
	if err := templates.FarmersMarket.Execute(w, data); err != nil {
		slog.ErrorContext(r.Context(), "farmers market template execute error", "error", err)
		http.Error(w, "template error", http.StatusInternalServerError)
	}
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

// who knew data: was  valid url just like http:? see comment in ai/farmersmarket.go
func (p Photo) dataURL() string {
	return "data:" + p.contentType + ";base64," + base64.StdEncoding.EncodeToString(p.content)
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
	http.Redirect(w, r, target, http.StatusSeeOther)
}
