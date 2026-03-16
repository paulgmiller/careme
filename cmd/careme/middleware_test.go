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
}

type fakeRequestTracker struct {
	calls []trackedRequest
}

func (f *fakeRequestTracker) TrackRequest(ctx context.Context, method, url string, duration time.Duration, responseCode string) {
	operationID, _ := logsetup.OperationIDFromContext(ctx)
	f.calls = append(f.calls, trackedRequest{
		method:       method,
		url:          url,
		duration:     duration,
		responseCode: responseCode,
		operationID:  operationID,
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

func TestAppInsightsTrackerSkipsReady(t *testing.T) {
	tracker := &fakeRequestTracker{}
	mw := &appInsightsTracker{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
		tracker: tracker,
	}

	req := httptest.NewRequest(http.MethodGet, "https://careme.cooking/ready", nil)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	if len(tracker.calls) != 0 {
		t.Fatalf("expected 0 tracked requests for /ready, got %d", len(tracker.calls))
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
