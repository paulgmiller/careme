package main

import (
	"careme/internal/logsetup"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/clerk/clerk-sdk-go/v2"
	azureappinsights "github.com/microsoft/ApplicationInsights-Go/appinsights"
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
	//should we use auth client?
	user := ""
	if claims, ok := clerk.SessionClaimsFromContext(r.Context()); ok {
		user = claims.Subject
	}

	lrw := &loggingResponseWriter{w, http.StatusOK}
	l.Handler.ServeHTTP(lrw, r)
	if r.URL.Path == "/ready" {
		return
	}

	slog.Info("request", "method", r.Method, "url", r.URL.Path, "query", r.URL.Query(), "response", lrw.statusCode, "user", user, "form", r.Form, "duration", time.Since(start))
}

type requestTracker interface {
	TrackRequest(method, url string, duration time.Duration, responseCode string)
}

type appInsightsTracker struct {
	http.Handler
	tracker requestTracker
}

const appInsightsIngestionPath = "/v2/track"

func (a *appInsightsTracker) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	lrw := &loggingResponseWriter{w, http.StatusOK}
	a.Handler.ServeHTTP(lrw, r)

	if r.URL.Path == "/ready" {
		return
	}

	a.tracker.TrackRequest(r.Method, r.URL.String(), time.Since(start), strconv.Itoa(lrw.statusCode))
}

func newAppInsightsTracker(next http.Handler, connectionString string) (http.Handler, error) {
	client, err := newAppInsightsTelemetryClient(connectionString)
	if err != nil {
		return nil, err
	}
	return &appInsightsTracker{
		Handler: next,
		tracker: client,
	}, nil
}

func newAppInsightsTrackerFromEnv(next http.Handler) http.Handler {
	connectionString := os.Getenv(logsetup.AppInsightsConnectionStringEnv)
	if connectionString == "" {
		return next
	}

	handler, err := newAppInsightsTracker(next, connectionString)
	if err != nil {
		slog.Error("failed to configure app insights request tracking", "error", err)
		return next
	}

	return handler
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
	//app insights could also track this https://github.com/microsoft/ApplicationInsights-Go?tab=readme-ov-file#exceptions
	defer func() {
		if err := recover(); err != nil {
			slog.ErrorContext(req.Context(), "panic recovered", "error", err, "stack", debug.Stack())
			http.Error(w, "internal server error", http.StatusInternalServerError)
		}
	}()
	r.Handler.ServeHTTP(w, req)
}

func WithMiddleware(h http.Handler) http.Handler {
	h = &recoverer{h}
	h = newAppInsightsTrackerFromEnv(h)
	return &logger{h}
}
