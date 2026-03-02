package main

import (
	"errors"
	"log/slog"
	"net/http"
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
	connectionString := os.Getenv(appInsightsConnectionStringEnv)
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
	key, err := parseAppInsightsKey(connectionString)
	if err != nil {
		return nil, err
	}
	return azureappinsights.NewTelemetryClient(key), nil
	//if we want somethhing fancy we can do this.
	//return azureappinsights.NewTelemetryClientFromConfig(cfg), nil
}

// replace with github.com/Azure/go-autorest/autorest/azure.ParseConnectionString?
func parseAppInsightsKey(connectionString string) (string, error) {
	connectionString = strings.TrimSpace(connectionString)
	for _, value := range strings.Split(connectionString, ";") {
		pair := strings.SplitN(value, "=", 2)
		if len(pair) != 2 {
			continue
		}
		if pair[0] == "InstrumentationKey" {
			return pair[1], nil
		}
	}
	return "", errors.New("instrumentation key is missing")

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
