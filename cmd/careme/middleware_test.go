package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"careme/internal/logsetup"

	"github.com/clerk/clerk-sdk-go/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	oteltrace "go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

func TestTelemetryHandlerRecordsResponseCode(t *testing.T) {
	recorder := installTestTracerProvider(t)

	handler := newTelemetryHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))

	req := httptest.NewRequest(http.MethodPost, "https://careme.cooking/recipes?vegan=true", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	span := spans[0]
	if span.Name() != "/recipes" {
		t.Fatalf("expected span name %q, got %q", "/recipes", span.Name())
	}
	attrs := spanAttributes(span)
	if got := attrs["http.method"].AsString(); got != http.MethodPost {
		t.Fatalf("expected http.method %q, got %q", http.MethodPost, got)
	}
	if got := int(attrs["http.status_code"].AsInt64()); got != http.StatusCreated {
		t.Fatalf("expected http.status_code %d, got %d", http.StatusCreated, got)
	}
}

func TestTelemetryHandlerRecordsRecoveredPanicAs500(t *testing.T) {
	recorder := installTestTracerProvider(t)

	handler := newTelemetryHandler(&recoverer{
		Handler: http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			panic("boom")
		}),
	})

	req := httptest.NewRequest(http.MethodGet, "https://careme.cooking/panic", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected status 500, got %d", rec.Code)
	}

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].Status().Code != codes.Error {
		t.Fatalf("expected span status %v, got %v", codes.Error, spans[0].Status().Code)
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

func TestWithMiddlewareProvidesTraceAndSessionContext(t *testing.T) {
	recorder := installTestTracerProvider(t)

	var sessionID string
	var traceID string
	handler := appMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sessionID, _ = logsetup.SessionIDFromContext(r.Context())
		traceID = oteltrace.SpanContextFromContext(r.Context()).TraceID().String()
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "http://careme.cooking/about", nil)
	req = req.WithContext(clerk.ContextWithSessionClaims(req.Context(), &clerk.SessionClaims{
		RegisteredClaims: clerk.RegisteredClaims{
			Subject: "user-123",
		},
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if traceID == "" {
		t.Fatal("expected trace id in context")
	}
	if sessionID == "" {
		t.Fatal("expected session id in context")
	}
	cookie := findCookie(t, rec.Result().Cookies(), sessionCookieName)
	if cookie.Value != sessionID {
		t.Fatalf("expected session cookie %q, got %q", sessionID, cookie.Value)
	}
	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	attrs := spanAttributes(spans[0])
	if got := attrs["session.id"].AsString(); got != sessionID {
		t.Fatalf("expected session.id %q, got %q", sessionID, got)
	}
	if got := attrs["enduser.id"].AsString(); got != "user-123" {
		t.Fatalf("expected enduser.id %q, got %q", "user-123", got)
	}
}

func TestWithMiddlewarePreservesIncomingTraceContext(t *testing.T) {
	installTestTracerProvider(t)

	var traceID string
	handler := appMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceID = oteltrace.SpanContextFromContext(r.Context()).TraceID().String()
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodGet, "http://careme.cooking/about", nil)
	req = req.WithContext(oteltrace.ContextWithSpanContext(req.Context(), oteltrace.NewSpanContext(oteltrace.SpanContextConfig{
		TraceID:    oteltrace.TraceID{1, 2, 3},
		SpanID:     oteltrace.SpanID{4, 5, 6},
		TraceFlags: oteltrace.FlagsSampled,
		Remote:     true,
	})))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if traceID != "01020300000000000000000000000000" {
		t.Fatalf("expected preserved trace id, got %q", traceID)
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
	rootMux.Handle("/static/", baseMiddleware(infraMux))
	rootMux.Handle("/", appMiddleware(appMux))

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

func installTestTracerProvider(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()

	recorder := tracetest.NewSpanRecorder()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		_ = provider.Shutdown(context.Background())
		otel.SetTracerProvider(noop.NewTracerProvider())
	})

	return recorder
}

func spanAttributes(span sdktrace.ReadOnlySpan) map[string]attribute.Value {
	attrs := make(map[string]attribute.Value, len(span.Attributes()))
	for _, attr := range span.Attributes() {
		attrs[string(attr.Key)] = attr.Value
	}
	return attrs
}
