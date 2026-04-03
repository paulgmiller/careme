package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"careme/internal/attribution"
	"careme/internal/logsetup"

	"github.com/clerk/clerk-sdk-go/v2"
	"github.com/google/uuid"
	azureappinsights "github.com/microsoft/ApplicationInsights-Go/appinsights"
	"github.com/microsoft/ApplicationInsights-Go/appinsights/contracts"
)

const (
	sessionCookieName   = "careme_session_id"
	sessionCookieMaxAge = 30 * 60
	attributionMaxAge   = 90 * 24 * 60 * 60
)

type logger struct {
	http.Handler
}

type loggingResponseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (lrw *loggingResponseWriter) WriteHeader(code int) {
	lrw.statusCode = code
	lrw.ResponseWriter.WriteHeader(code)
}

func (l *logger) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	// should we use auth client?
	user := ""
	if claims, ok := clerk.SessionClaimsFromContext(r.Context()); ok {
		user = claims.Subject
	}

	lrw := &loggingResponseWriter{w, http.StatusOK}
	l.Handler.ServeHTTP(lrw, r)

	slog.InfoContext(r.Context(), "request", "method", r.Method, "url", r.URL.Path, "query", r.URL.Query(), "response", lrw.statusCode, "user", user, "form", r.Form, "duration", time.Since(start))
}

type requestTracker interface {
	TrackRequest(ctx context.Context, method, url string, duration time.Duration, responseCode string)
}

type appInsightsTracker struct {
	http.Handler
	tracker requestTracker
}

const appInsightsIngestionPath = "/v2/track"

type appInsightsTelemetryTracker struct {
	client azureappinsights.TelemetryClient
}

func (t *appInsightsTelemetryTracker) TrackRequest(ctx context.Context, method, url string, duration time.Duration, responseCode string) {
	request := azureappinsights.NewRequestTelemetry(method, url, duration, responseCode)
	tags := contracts.ContextTags(request.ContextTags())
	if operationID, ok := logsetup.OperationIDFromContext(ctx); ok {
		tags.Operation().SetId(operationID)
	}
	if sessionID, ok := logsetup.SessionIDFromContext(ctx); ok {
		tags.Session().SetId(sessionID)
	}
	t.client.Track(request)
}

func (a *appInsightsTracker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	lrw := &loggingResponseWriter{w, http.StatusOK}
	a.Handler.ServeHTTP(lrw, r)

	a.tracker.TrackRequest(r.Context(), r.Method, r.URL.String(), time.Since(start), strconv.Itoa(lrw.statusCode))
}

func newAppInsightsTracker(next http.Handler, tracker requestTracker) http.Handler {
	if tracker == nil {
		return next
	}

	return &appInsightsTracker{
		Handler: next,
		tracker: tracker,
	}
}

func newRequestTracker(connectionString string) (requestTracker, error) {
	client, err := newAppInsightsTelemetryClient(connectionString)
	if err != nil {
		return nil, err
	}
	return &appInsightsTelemetryTracker{client: client}, nil
}

func newRequestTrackerFromEnv() requestTracker {
	connectionString := os.Getenv(logsetup.AppInsightsConnectionStringEnv)
	if connectionString == "" {
		return nil
	}

	tracker, err := newRequestTracker(connectionString)
	if err != nil {
		slog.Error("failed to configure app insights request tracking", "error", err)
		return nil
	}

	return tracker
}

func newAppInsightsTelemetryClient(connectionString string) (azureappinsights.TelemetryClient, error) {
	cfg, err := parseAppInsightsConnectionString(connectionString)
	if err != nil {
		return nil, err
	}
	return azureappinsights.NewTelemetryClientFromConfig(cfg), nil
}

// suprise there is not a parse function here. Chatgpt things github.com/Azure/go-autorest/autorest/azure.ParseConnectionString but codex coudln't find it
func parseAppInsightsConnectionString(connectionString string) (*azureappinsights.TelemetryConfiguration, error) {
	connectionString = strings.TrimSpace(connectionString)
	if connectionString == "" {
		return nil, errors.New("connection string is empty")
	}

	var instrumentationKey string
	var ingestionEndpoint string

	for _, value := range strings.Split(connectionString, ";") {
		pair := strings.SplitN(value, "=", 2)
		if len(pair) != 2 {
			continue
		}
		switch pair[0] {
		case "InstrumentationKey":
			instrumentationKey = pair[1]
		case "IngestionEndpoint":
			ingestionEndpoint = pair[1]
		}
	}

	if instrumentationKey == "" {
		return nil, errors.New("instrumentation key is missing")
	}
	if ingestionEndpoint == "" {
		return nil, errors.New("ingestion endpoint is missing")
	}

	ingestionURL, err := url.Parse(ingestionEndpoint)
	if err != nil {
		return nil, err
	}

	cfg := azureappinsights.NewTelemetryConfiguration(instrumentationKey)
	ingestionURL.Path = appInsightsIngestionPath
	cfg.EndpointUrl = ingestionURL.String()
	return cfg, nil
}

type recoverer struct {
	http.Handler
}

func (r *recoverer) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// app insights could also track this https://github.com/microsoft/ApplicationInsights-Go?tab=readme-ov-file#exceptions
	defer func() {
		if err := recover(); err != nil {
			slog.ErrorContext(req.Context(), "panic recovered", "error", err, "stack", debug.Stack())
			http.Error(w, "internal server error", http.StatusInternalServerError)
		}
	}()
	r.Handler.ServeHTTP(w, req)
}

type operationIDHandler struct {
	http.Handler
}

// extract or generate an operation ID for the request, add it to the context, and set it in the response header. The operation ID is used for correlating logs and telemetry.
func (h *operationIDHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	operationID := uuid.NewString()
	ctx := logsetup.WithOperationID(r.Context(), operationID)
	w.Header().Set("X-Operation-ID", operationID)
	h.Handler.ServeHTTP(w, r.WithContext(ctx))
}

type sessionIDHandler struct {
	http.Handler
}

type attributionHandler struct {
	http.Handler
}

func (h *sessionIDHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	sessionID := readOrCreateSessionID(r)
	ctx := logsetup.WithSessionID(r.Context(), sessionID)
	http.SetCookie(w, sessionCookie(r, sessionID))
	h.Handler.ServeHTTP(w, r.WithContext(ctx))
}

func (h *attributionHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if clickIDs, ok := attribution.CaptureFromRequest(r); ok {
		value, err := attribution.EncodeCookieValue(clickIDs)
		if err != nil {
			slog.ErrorContext(r.Context(), "failed to encode attribution cookie", "error", err)
		} else {
			http.SetCookie(w, attributionCookie(r, value))
		}
	}
	h.Handler.ServeHTTP(w, r)
}

func readOrCreateSessionID(r *http.Request) string {
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil || cookie.Value == "" {
		return uuid.NewString()
	}
	if _, err := uuid.Parse(cookie.Value); err != nil {
		return uuid.NewString()
	}
	return cookie.Value
}

func sessionCookie(r *http.Request, sessionID string) *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionID,
		Path:     "/",
		MaxAge:   sessionCookieMaxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
	}
}

func attributionCookie(r *http.Request, value string) *http.Cookie {
	return &http.Cookie{
		Name:     attribution.CookieName,
		Value:    value,
		Path:     "/",
		MaxAge:   attributionMaxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   r.TLS != nil,
	}
}

// just recover and log
func BaseMiddleware(h http.Handler) http.Handler {
	h = &recoverer{h}
	return &logger{h}
}

// instrument with app insights and log with operation and session ids.
func AppMiddleWare(h http.Handler, tracker requestTracker) http.Handler {
	h = BaseMiddleware(h)
	h = newAppInsightsTracker(h, tracker) // must be "inside" operatid and session handler.
	h = &operationIDHandler{h}
	h = &attributionHandler{h}
	return &sessionIDHandler{h}
}
