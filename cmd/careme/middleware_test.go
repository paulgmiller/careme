package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"careme/internal/logsetup"
)

type trackedRequest struct {
	method       string
	url          string
	duration     time.Duration
	responseCode string
	operationID  string
	sessionID    string
}

type fakeRequestTracker struct {
	calls []trackedRequest
}

func (f *fakeRequestTracker) TrackRequest(ctx context.Context, method, url string, duration time.Duration, responseCode string) {
	operationID, _ := logsetup.OperationIDFromContext(ctx)
	sessionID, _ := logsetup.SessionIDFromContext(ctx)
	f.calls = append(f.calls, trackedRequest{
		method:       method,
		url:          url,
		duration:     duration,
		responseCode: responseCode,
		operationID:  operationID,
		sessionID:    sessionID,
	})
}

func TestAppInsightsTrackerTracksResponseCode(t *testing.T) {
	tracker := &fakeRequestTracker{}
	mw := &appInsightsTracker{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusCreated)
		}),
		tracker: tracker,
	}

	req := httptest.NewRequest(http.MethodPost, "https://careme.cooking/recipes?vegan=true", nil)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	if len(tracker.calls) != 1 {
		t.Fatalf("expected 1 tracked request, got %d", len(tracker.calls))
	}

	call := tracker.calls[0]
	if call.method != http.MethodPost {
		t.Fatalf("expected method %q, got %q", http.MethodPost, call.method)
	}
	if call.url != req.URL.String() {
		t.Fatalf("expected url %q, got %q", req.URL.String(), call.url)
	}
	if call.responseCode != "201" {
		t.Fatalf("expected response code 201, got %q", call.responseCode)
	}
	if call.duration <= 0 {
		t.Fatalf("expected positive duration, got %s", call.duration)
	}
}

func TestAppInsightsTrackerDefaultsStatusCodeTo200(t *testing.T) {
	tracker := &fakeRequestTracker{}
	mw := &appInsightsTracker{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("ok"))
		}),
		tracker: tracker,
	}

	req := httptest.NewRequest(http.MethodGet, "https://careme.cooking/about", nil)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	if len(tracker.calls) != 1 {
		t.Fatalf("expected 1 tracked request, got %d", len(tracker.calls))
	}
	if tracker.calls[0].responseCode != "200" {
		t.Fatalf("expected response code 200, got %q", tracker.calls[0].responseCode)
	}
}

func TestAppInsightsTrackerTracksRecoveredPanicAs500(t *testing.T) {
	tracker := &fakeRequestTracker{}
	mw := &appInsightsTracker{
		Handler: &recoverer{
			Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				panic("boom")
			}),
		},
		tracker: tracker,
	}

	req := httptest.NewRequest(http.MethodGet, "https://careme.cooking/panic", nil)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", rec.Code)
	}
	if len(tracker.calls) != 1 {
		t.Fatalf("expected 1 tracked request, got %d", len(tracker.calls))
	}
	if tracker.calls[0].responseCode != "500" {
		t.Fatalf("expected response code 500, got %q", tracker.calls[0].responseCode)
	}
}

func TestAppInsightsTrackerReusesOperationIDFromContext(t *testing.T) {
	tracker := &fakeRequestTracker{}
	mw := &appInsightsTracker{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusAccepted)
		}),
		tracker: tracker,
	}

	req := httptest.NewRequest(http.MethodGet, "https://careme.cooking/about", nil)
	req = req.WithContext(logsetup.WithOperationID(req.Context(), "op-555"))
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	if len(tracker.calls) != 1 {
		t.Fatalf("expected 1 tracked request, got %d", len(tracker.calls))
	}
	if tracker.calls[0].operationID != "op-555" {
		t.Fatalf("expected tracker to receive operation id op-555, got %q", tracker.calls[0].operationID)
	}
}

func TestAppInsightsTrackerIncludesSessionIDFromContext(t *testing.T) {
	tracker := &fakeRequestTracker{}
	mw := &appInsightsTracker{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusAccepted)
		}),
		tracker: tracker,
	}

	req := httptest.NewRequest(http.MethodGet, "https://careme.cooking/about", nil)
	req = req.WithContext(logsetup.WithSessionID(req.Context(), "sess-555"))
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	if len(tracker.calls) != 1 {
		t.Fatalf("expected 1 tracked request, got %d", len(tracker.calls))
	}
	if tracker.calls[0].sessionID != "sess-555" {
		t.Fatalf("expected tracker to receive session id sess-555, got %q", tracker.calls[0].sessionID)
	}
}

func TestSessionIDHandlerIssuesCookieWhenMissing(t *testing.T) {
	var gotSessionID string
	handler := &sessionIDHandler{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var ok bool
			gotSessionID, ok = logsetup.SessionIDFromContext(r.Context())
			if !ok {
				t.Fatal("expected session id in context")
			}
			w.WriteHeader(http.StatusNoContent)
		}),
	}

	req := httptest.NewRequest(http.MethodGet, "http://careme.cooking/about", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	cookie := rec.Result().Cookies()
	if len(cookie) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookie))
	}
	if cookie[0].Name != sessionCookieName {
		t.Fatalf("expected cookie %q, got %q", sessionCookieName, cookie[0].Name)
	}
	if cookie[0].Value == "" {
		t.Fatal("expected non-empty session cookie value")
	}
	if gotSessionID != cookie[0].Value {
		t.Fatalf("expected context session id %q, got %q", cookie[0].Value, gotSessionID)
	}
	if cookie[0].MaxAge != sessionCookieMaxAge {
		t.Fatalf("expected MaxAge %d, got %d", sessionCookieMaxAge, cookie[0].MaxAge)
	}
	if !cookie[0].HttpOnly {
		t.Fatal("expected cookie to be HttpOnly")
	}
	if cookie[0].SameSite != http.SameSiteLaxMode {
		t.Fatalf("expected SameSite Lax, got %v", cookie[0].SameSite)
	}
	if cookie[0].Secure {
		t.Fatal("expected non-TLS request cookie to be insecure")
	}
}

func TestSessionIDHandlerReusesValidCookie(t *testing.T) {
	const sessionID = "8d31449a-1f55-4e8b-8812-9d9a3c0f45d7"
	var gotSessionID string
	handler := &sessionIDHandler{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotSessionID, _ = logsetup.SessionIDFromContext(r.Context())
			w.WriteHeader(http.StatusNoContent)
		}),
	}

	req := httptest.NewRequest(http.MethodGet, "https://careme.cooking/about", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: sessionID})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	cookie := findCookie(t, rec.Result().Cookies(), sessionCookieName)
	if cookie.Value != sessionID {
		t.Fatalf("expected session cookie %q, got %q", sessionID, cookie.Value)
	}
	if gotSessionID != sessionID {
		t.Fatalf("expected context session id %q, got %q", sessionID, gotSessionID)
	}
	if !cookie.Secure {
		t.Fatal("expected TLS request cookie to be secure")
	}
}

func TestSessionIDHandlerReplacesInvalidCookie(t *testing.T) {
	handler := &sessionIDHandler{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}),
	}

	req := httptest.NewRequest(http.MethodGet, "http://careme.cooking/about", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: "not-a-uuid"})
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	cookie := findCookie(t, rec.Result().Cookies(), sessionCookieName)
	if cookie.Value == "not-a-uuid" {
		t.Fatal("expected invalid session cookie to be replaced")
	}
}

func TestWithMiddlewareProvidesBothIDs(t *testing.T) {
	var operationID string
	var sessionID string
	handler := AppMiddleWare(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		operationID, _ = logsetup.OperationIDFromContext(r.Context())
		sessionID, _ = logsetup.SessionIDFromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}), &fakeRequestTracker{})

	req := httptest.NewRequest(http.MethodGet, "http://careme.cooking/about", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if operationID == "" {
		t.Fatal("expected operation id in context")
	}
	if sessionID == "" {
		t.Fatal("expected session id in context")
	}
	if rec.Header().Get("X-Operation-ID") != operationID {
		t.Fatalf("expected X-Operation-ID %q, got %q", operationID, rec.Header().Get("X-Operation-ID"))
	}
	cookie := findCookie(t, rec.Result().Cookies(), sessionCookieName)
	if cookie.Value != sessionID {
		t.Fatalf("expected session cookie %q, got %q", sessionID, cookie.Value)
	}
}

func TestWithMiddlewareProvidesIDsWithoutTracker(t *testing.T) {
	var operationID string
	var sessionID string
	handler := AppMiddleWare(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		operationID, _ = logsetup.OperationIDFromContext(r.Context())
		sessionID, _ = logsetup.SessionIDFromContext(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}), nil)

	req := httptest.NewRequest(http.MethodGet, "http://careme.cooking/about", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected status 204, got %d", rec.Code)
	}
	if operationID == "" {
		t.Fatal("expected operation id in context")
	}
	if sessionID == "" {
		t.Fatal("expected session id in context")
	}
	if rec.Header().Get("X-Operation-ID") != operationID {
		t.Fatalf("expected X-Operation-ID %q, got %q", operationID, rec.Header().Get("X-Operation-ID"))
	}
	cookie := findCookie(t, rec.Result().Cookies(), sessionCookieName)
	if cookie.Value != sessionID {
		t.Fatalf("expected session cookie %q, got %q", sessionID, cookie.Value)
	}
}

func findCookie(t *testing.T, cookies []*http.Cookie, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	t.Fatalf("expected cookie %q", name)
	return nil
}

func TestRouteScopedMiddlewareSkipsSessionCookieForStaticRoutes(t *testing.T) {
	rootMux := http.NewServeMux()
	appMux := http.NewServeMux()
	infraMux := http.NewServeMux()
	infraMux.HandleFunc("/static/app.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.WriteHeader(http.StatusNoContent)
	})
	appMux.HandleFunc("/about", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	rootMux.Handle("/static/", BaseMiddleware(infraMux))
	rootMux.Handle("/", AppMiddleWare(appMux, &fakeRequestTracker{}))

	staticReq := httptest.NewRequest(http.MethodGet, "http://careme.cooking/static/app.js", nil)
	staticRec := httptest.NewRecorder()
	rootMux.ServeHTTP(staticRec, staticReq)

	if got := staticRec.Header().Values("Set-Cookie"); len(got) != 0 {
		t.Fatalf("expected no Set-Cookie on static route, got %v", got)
	}
	if staticRec.Header().Get("X-Operation-ID") != "" {
		t.Fatal("expected static route to NOT receive operation id from base middleware")
	}

	appReq := httptest.NewRequest(http.MethodGet, "http://careme.cooking/about", nil)
	appRec := httptest.NewRecorder()
	rootMux.ServeHTTP(appRec, appReq)

	if findCookie(t, appRec.Result().Cookies(), sessionCookieName).Value == "" {
		t.Fatal("expected session cookie on app route")
	}
}

func TestParseAppInsightsConnectionString(t *testing.T) {
	connectionString := "InstrumentationKey=test-key;IngestionEndpoint=https://westus3-1.in.applicationinsights.azure.com/;LiveEndpoint=https://westus3.livediagnostics.monitor.azure.com/;ApplicationId=app-id"
	cfg, err := parseAppInsightsConnectionString(connectionString)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.InstrumentationKey != "test-key" {
		t.Fatalf("expected instrumentation key test-key, got %q", cfg.InstrumentationKey)
	}
	if cfg.EndpointUrl != "https://westus3-1.in.applicationinsights.azure.com/v2/track" {
		t.Fatalf("unexpected ingestion endpoint: %q", cfg.EndpointUrl)
	}
}

func TestParseAppInsightsConnectionStringErrors(t *testing.T) {
	testCases := []struct {
		name        string
		value       string
		wantErrText string
	}{
		{
			name:        "empty",
			value:       "",
			wantErrText: "connection string is empty",
		},
		{
			name:        "missing instrumentation key",
			value:       "IngestionEndpoint=https://westus3-1.in.applicationinsights.azure.com/",
			wantErrText: "instrumentation key is missing",
		},
		{
			name:        "missing ingestion endpoint",
			value:       "InstrumentationKey=test-key",
			wantErrText: "ingestion endpoint is missing",
		},
		{
			name:        "bad ingestion endpoint",
			value:       "InstrumentationKey=test-key;IngestionEndpoint=:bad://",
			wantErrText: "missing protocol scheme",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseAppInsightsConnectionString(tc.value)
			if err == nil {
				t.Fatalf("expected error containing %q", tc.wantErrText)
			}
			if !strings.Contains(err.Error(), tc.wantErrText) {
				t.Fatalf("expected error containing %q, got %q", tc.wantErrText, err.Error())
			}
		})
	}
}
